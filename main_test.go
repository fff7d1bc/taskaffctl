package main

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

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

func TestWritePIDReportsYAMLish(t *testing.T) {
	reports := []pidAffinityReport{{
		PID:    123,
		Comm:   "worker",
		Status: "updated",
		From:   "0-1",
		To:     "2-3",
	}}
	var out bytes.Buffer
	if err := writePIDReports(&out, reports, false); err != nil {
		t.Fatal(err)
	}
	want := "reports:\n  - pid: 123\n    comm: worker\n    status: updated\n    from: 0-1\n    to: 2-3\n"
	if out.String() != want {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestWritePIDReportsJSON(t *testing.T) {
	reports := []pidAffinityReport{{PID: 123, Status: "failed", Err: io.EOF}}
	var out bytes.Buffer
	if err := writePIDReports(&out, reports, true); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"reports\": [\n    {\n      \"pid\": 123,\n      \"comm\": \"?\",\n      \"status\": \"failed\",\n      \"error\": \"EOF\"\n    }\n  ]\n}\n"
	if out.String() != want {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestWritePIDReportsPropagatesWriterError(t *testing.T) {
	want := errors.New("write failed")
	for _, jsonOutput := range []bool{false, true} {
		err := writePIDReports(errorWriter{err: want}, []pidAffinityReport{{PID: 123}}, jsonOutput)
		if !errors.Is(err, want) {
			t.Fatalf("json=%t: unexpected error: %v", jsonOutput, err)
		}
	}
}
