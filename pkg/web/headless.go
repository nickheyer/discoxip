package web

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Blocks the headless run executes before it stops as still running, and the runtime the browser exposes to the
// page that the headless run provides from the file system and its own memory instead
const headlessBlocks = 400000000

// The bridge a headless run hands the page, scripts run in the one context, files read from the web directory,
// no key or gamepad held, the audio counted by frame and peak and nothing kept across runs
const headlessBridge = `
import { readFile } from "node:fs/promises";
import vm from "node:vm";
const dir = %q;
const kept = new Map();
let audioFrames = 0, audioPeak = 0;
const finish = (text) => { console.log(text + ", " + audioFrames + " audio frames with peak " + audioPeak); process.exit(0); };
const load = async (name) => { vm.runInThisContext(await readFile(dir + "/" + name, "utf8"), { filename: name }); };
await load(%q);
await start({
  now: () => performance.now(),
  clock: () => Date.now(),
  later: (ms, fn) => setTimeout(fn, ms),
  load,
  screen: () => null,
  pixels: () => ({ width: 0, height: 0 }),
  audio: (rate) => ({ rate, push: (samples) => { audioFrames += samples.length / 2; for (let i = 0; i < samples.length; i++) audioPeak = Math.max(audioPeak, Math.abs(samples[i])); } }),
  keys: () => new Set(),
  gamepads: () => [],
  shown: () => {},
  read: async (name) => new Uint8Array(await readFile(dir + "/" + %q + "/" + name)),
  store: (key, bytes) => kept.set(key, bytes),
  loaded: (key) => kept.get(key) ?? null,
  loadedAll: (prefix) => Array.from(kept.entries()).filter(([k]) => k.startsWith(prefix)),
  status: (text) => { console.log("status " + text); if (text.includes(" blocks") && Number(text.split(" ")[1]) > %d) finish("still running after " + text); },
  stopped: (text) => finish("stopped " + text),
});
`

// RunHeadless boots the page under node for at most a time, reporting how the run ended
func RunHeadless(webDir string, timeout time.Duration) (string, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("web: the headless run needs node: %w", err)
	}
	abs, err := filepath.Abs(webDir)
	if err != nil {
		return "", err
	}
	driver := filepath.Join(abs, "headless.mjs")
	data := fmt.Sprintf(headlessBridge, abs, RuntimeFile, DataDir, headlessBlocks)
	if err := os.WriteFile(driver, []byte(data), 0o644); err != nil {
		return "", err
	}
	defer os.Remove(driver)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--stack-size=4096", driver)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	text := strings.TrimSpace(out.String())
	if ctx.Err() != nil {
		return text, fmt.Errorf("web: the headless run exceeded %s", timeout)
	}
	if runErr != nil {
		return text, fmt.Errorf("web: node: %w", runErr)
	}
	return text, nil
}
