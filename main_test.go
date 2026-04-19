package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePIDList(t *testing.T) {
	got, err := parsePIDList("123,456 789\n456")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{123, 456, 789}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected pid list: got %v want %v", got, want)
	}
}

func TestParsePIDListRejectsInvalidPID(t *testing.T) {
	if _, err := parsePIDList("123,abc"); err == nil {
		t.Fatal("expected invalid pid error")
	}
}

func TestRunRejectsTopologyConflicts(t *testing.T) {
	err := run([]string{"--topology", "--tag", "all-cores"})
	if err == nil || !strings.Contains(err.Error(), "--topology cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}

	err = run([]string{"--topology", "extra"})
	if err == nil || !strings.Contains(err.Error(), "--topology cannot be combined with a command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsDescendantsWithoutSelector(t *testing.T) {
	err := run([]string{"--descendants"})
	if err == nil || !strings.Contains(err.Error(), "--descendants requires --pid or --comm") {
		t.Fatalf("unexpected error: %v", err)
	}
}
