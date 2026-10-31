//go:build samples

package bios

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The emitted hardware must parse as a module before any page runs it
func TestHardwareSyntax(t *testing.T) {
	var buf bytes.Buffer
	if err := (&Page{}).WriteHardware(&buf); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hardware.mjs")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, "--check", path).CombinedOutput()
	if err != nil {
		t.Fatalf("node --check: %v\n%s", err, out)
	}
}
