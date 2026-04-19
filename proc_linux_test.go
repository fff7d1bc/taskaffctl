package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessMatchesNameUsesExeBase(t *testing.T) {
	root := t.TempDir()
	pidDir := filepath.Join(root, "123")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte("verylongprocess\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/usr/bin/verylongprocessname", filepath.Join(pidDir, "exe")); err != nil {
		t.Fatal(err)
	}

	if !processMatchesName(root, 123, "verylongprocessname") {
		t.Fatal("expected full exe basename to match")
	}
	if !processMatchesName(root, 123, "verylongprocess") {
		t.Fatal("expected comm name to still match")
	}
	if processMatchesName(root, 123, "other") {
		t.Fatal("did not expect unrelated name to match")
	}
}
