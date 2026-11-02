package web

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nickheyer/discoxip/pkg/bios"
	"github.com/nickheyer/discoxip/pkg/xbe"
)

// RuntimeFile is the name of the runtime the page loads, DataDir where its data files lie and BlocksDir where the
// scripts holding the translated blocks lie
const (
	RuntimeFile = "runtime.js"
	DataDir     = "data"
	BlocksDir   = "blocks"
)

// Blocks each script of the translation holds, blocks the run loop executes between looks at the clock, the
// milliseconds of each frame the run loop takes before yielding to the browser and how often the page reports
// what it is doing
const (
	blocksPerFile    = 8192
	sliceBlocks      = 1 << 16
	frameBudgetMs    = 12
	statusIntervalMs = 5000
)

// RuntimeInput is what the runtime is written from, the executable, the flash, the ROM and the console directory
// the launch partition carries
type RuntimeInput struct {
	Dash       *xbe.Disassembly
	Bank       *bios.Bank
	MCPX       *bios.MCPX
	Console    bios.Console
	ConsoleDir string
}

// WriteRuntime derives the console the page boots and writes its runtime and data files into the output directory
func WriteRuntime(outputDir string, in RuntimeInput) error {
	page, err := bios.DerivePage(in.Bank, in.MCPX, in.ConsoleDir, in.Console)
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	xbe.Stage("console derived")
	return writePage(outputDir, page, in.Dash)
}

// Writes the runtime of a derived console, the lifted blocks of the loader, the kernel and the executable in
// scripts beside it that the runtime loads in order
func writePage(outputDir string, page *bios.Page, d *xbe.Disassembly) error {
	dash, err := xbe.LiftExecutable(d, page.Lifted.KernelDis, page.Lifted.Exports)
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	if err := page.DeriveExecutable(d, dash); err != nil {
		return fmt.Errorf("web: %w", err)
	}
	xbe.Stage("executable polls answered and lines connected")
	for _, mm := range []*xbe.MachineModel{page.Lifted.Model, dash} {
		if err := mm.CheckOperations(); err != nil {
			return fmt.Errorf("web: %w", err)
		}
	}
	dataDir := filepath.Join(outputDir, DataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, bios.DiskRunsDir), 0o755); err != nil {
		return err
	}
	files, err := page.Files(func(name string, data []byte) error {
		return os.WriteFile(filepath.Join(dataDir, filepath.FromSlash(name)), data, 0o644)
	})
	if err != nil {
		return err
	}
	blocksDir := filepath.Join(outputDir, BlocksDir)
	if err := os.RemoveAll(blocksDir); err != nil {
		return err
	}
	if err := os.MkdirAll(blocksDir, 0o755); err != nil {
		return err
	}
	var blockFiles []string
	open := func(stem string) func(int) (io.WriteCloser, error) {
		return func(index int) (io.WriteCloser, error) {
			name := fmt.Sprintf("%s/%s-%d.js", BlocksDir, stem, index)
			blockFiles = append(blockFiles, name)
			return os.Create(filepath.Join(outputDir, filepath.FromSlash(name)))
		}
	}
	boot, err := page.Lifted.Model.WriteBlocks(open("boot"), blocksPerFile, "OOT", "PLAIN_BOOT", nil)
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	table, err := dash.WriteBlocks(open("exe"), blocksPerFile, "", "PLAIN", nil)
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	xbe.Stage("blocks written")
	f, err := os.Create(filepath.Join(outputDir, RuntimeFile))
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	if err := page.WriteTables(w, files, blockFiles); err != nil {
		return err
	}
	if err := xbe.WriteCore(w); err != nil {
		return err
	}
	if err := page.WriteHardware(w); err != nil {
		return err
	}
	writeStart(w)
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %d blocks of the loader and the kernel, %d of the executable, %d blocks the lifts could not read, in %d scripts\n", boot.Blocks, table.Blocks, len(boot.Errors)+len(table.Errors), boot.Files+table.Files)
	return nil
}

// The page's entry, loading the block scripts and the data files through the bridge, booting the console and
// running it for part of each frame between the browser's frames, the bridge told of faults and progress
func writeStart(w *bufio.Writer) {
	js := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	js("// ---- the page's entry ----")
	js("let scheduled = false, lastStatus = 0;")
	js("function schedule(ms) { if (scheduled) return; scheduled = true; bridge.later(ms, run); }")
	js("function status() { const now = bridge.now(); if (now - lastStatus < %d) return; lastStatus = now; bridge.status(Math.round(hwElapsedSeconds()) + \"s \" + stepsRun + \" blocks\" + (phaseBoot ? \", booting\" : inExecutable ? \", running the executable\" : \", running the kernel\") + (next === null ? (idle ? \", idle at \" + hex(haltedAt) : \", halted\") : \" at \" + next.name) + hwStatus()); }", statusIntervalMs)
	js("function run() {")
	js("scheduled = false;")
	js("if (fatal !== null) return;")
	js("status();")
	js("const until = bridge.now() + %d;", frameBudgetMs)
	js("for (;;) {")
	js("hwAdvance();")
	js("const outcome = runSlice(%d);", sliceBlocks)
	js("if (outcome === \"fatal\") { bridge.stopped(describeFault(fatal)); return; }")
	js("if (outcome === \"halted\") { schedule(Math.min(hwDeadline(), 1000)); return; }")
	js("if (bridge.now() >= until) break;")
	js("}")
	js("schedule(0);")
	js("}")
	js("async function start(b) {")
	js("bridge = b;")
	js("bridge.wake = () => schedule(0);")
	js("for (const name of TABLES.block_files) { bridge.status(\"loading \" + name); await bridge.load(name); }")
	js("const files = {};")
	js("for (const [key, name] of Object.entries(TABLES.files)) if (!key.startsWith(\"run\")) files[name] = await bridge.read(name);")
	js("try { hwBoot(files); } catch (e) { fatal = e; bridge.stopped(describeFault(e)); return; }")
	js("schedule(0);")
	js("}")
}
