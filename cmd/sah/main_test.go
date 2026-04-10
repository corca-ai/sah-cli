package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferredLaunchdExecutableUsesStableHomebrewSymlink(t *testing.T) {
	dir := t.TempDir()
	cellarDir := filepath.Join(dir, "Cellar", "sah-cli", "0.2.3", "bin")
	if err := os.MkdirAll(cellarDir, 0o755); err != nil {
		t.Fatalf("mkdir cellar dir: %v", err)
	}

	cellarBinary := filepath.Join(cellarDir, "sah")
	if err := os.WriteFile(cellarBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write cellar binary: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}

	stableBinary := filepath.Join(binDir, "sah")
	if err := os.Symlink(cellarBinary, stableBinary); err != nil {
		t.Fatalf("symlink stable binary: %v", err)
	}

	selected, err := selectLaunchdExecutable(cellarBinary, []string{stableBinary})
	if err != nil {
		t.Fatalf("selectLaunchdExecutable returned error: %v", err)
	}
	if selected != stableBinary {
		t.Fatalf("expected stable symlink %q, got %q", stableBinary, selected)
	}
}

func TestPreferredLaunchdExecutableFallsBackToResolvedBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "sah")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	selected, err := selectLaunchdExecutable(binary, nil)
	if err != nil {
		t.Fatalf("selectLaunchdExecutable returned error: %v", err)
	}
	expected, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatalf("resolve binary symlink: %v", err)
	}
	if selected != expected {
		t.Fatalf("expected resolved binary %q, got %q", expected, selected)
	}
}
