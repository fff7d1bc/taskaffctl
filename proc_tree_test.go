package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestExpandPIDTree(t *testing.T) {
	root := t.TempDir()
	writeProcStatus(t, root, 100, 1)
	writeProcStatus(t, root, 101, 100)
	writeProcStatus(t, root, 102, 100)
	writeProcStatus(t, root, 103, 101)
	writeProcStatus(t, root, 200, 1)

	got, err := expandPIDTree(root, []int{100})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{100, 101, 102, 103}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected pid tree: got %v want %v", got, want)
	}
}

func writeProcStatus(t *testing.T, procRoot string, pid int, ppid int) {
	t.Helper()
	pidDir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "Name:\ttest\nState:\tS (sleeping)\nTgid:\t" + strconv.Itoa(pid) + "\nPid:\t" + strconv.Itoa(pid) + "\nPPid:\t" + strconv.Itoa(ppid) + "\n"
	if err := os.WriteFile(filepath.Join(pidDir, "status"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
