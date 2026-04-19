package main

import (
	"os/exec"
	"testing"
)

func TestApplyPIDToCPUSetCanSwitchBetweenDisjointMasks(t *testing.T) {
	current, err := getAffinity(0, 1023)
	if err != nil {
		t.Fatal(err)
	}
	cpus := current.CPUs()
	if len(cpus) < 2 {
		t.Skip("need at least two available CPUs")
	}

	maskA := NewCPUSet()
	maskA.Add(cpus[0])
	maskB := NewCPUSet()
	maskB.Add(cpus[len(cpus)-1])
	if maskA.Equal(maskB) {
		t.Skip("need two distinct CPU masks")
	}

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	pid := cmd.Process.Pid
	if _, err := applyPIDToCPUSet(pid, current.MaxCPU(), maskA); err != nil {
		t.Fatal(err)
	}
	gotA, err := getAffinity(pid, current.MaxCPU())
	if err != nil {
		t.Fatal(err)
	}
	if !gotA.Equal(maskA) {
		t.Fatalf("unexpected first affinity: got %s want %s", gotA.String(), maskA.String())
	}

	if _, err := applyPIDToCPUSet(pid, current.MaxCPU(), maskB); err != nil {
		t.Fatal(err)
	}
	gotB, err := getAffinity(pid, current.MaxCPU())
	if err != nil {
		t.Fatal(err)
	}
	if !gotB.Equal(maskB) {
		t.Fatalf("unexpected second affinity: got %s want %s", gotB.String(), maskB.String())
	}
}
