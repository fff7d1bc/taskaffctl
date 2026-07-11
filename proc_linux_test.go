package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidatePIDsExistAcceptsCompleteSet(t *testing.T) {
	root := t.TempDir()
	for _, pid := range []string{"123", "456"} {
		if err := os.Mkdir(filepath.Join(root, pid), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := validatePIDsExist(root, []int{123, 456}); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePIDsExistReportsEveryInvalidRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "456"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := validatePIDsExist(root, []int{123, 456, 789})
	if err == nil {
		t.Fatal("expected invalid PID roots error")
	}
	if strings.Contains(err.Error(), "pid 123") {
		t.Fatalf("valid PID was reported: %v", err)
	}
	for _, want := range []string{"pid 456", "pid 789"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}
