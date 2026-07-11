package main

import (
	"errors"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
)

func TestApplyPIDToCPUSetCanSwitchBetweenDisjointMasks(t *testing.T) {
	current, err := getAffinity(0)
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
	if _, err := applyPIDToCPUSet(pid, maskA); err != nil {
		t.Fatal(err)
	}
	gotA, err := getAffinity(pid)
	if err != nil {
		t.Fatal(err)
	}
	if !gotA.Equal(maskA) {
		t.Fatalf("unexpected first affinity: got %s want %s", gotA.String(), maskA.String())
	}

	if _, err := applyPIDToCPUSet(pid, maskB); err != nil {
		t.Fatal(err)
	}
	gotB, err := getAffinity(pid)
	if err != nil {
		t.Fatal(err)
	}
	if !gotB.Equal(maskB) {
		t.Fatalf("unexpected second affinity: got %s want %s", gotB.String(), maskB.String())
	}
}

func TestGetAffinityGrowsMaskUntilKernelAcceptsIt(t *testing.T) {
	var sizes []int
	reader := func(tid int, buf []byte) (int, error) {
		sizes = append(sizes, len(buf))
		if len(buf) < 128 {
			return 0, syscall.EINVAL
		}
		buf[8] = 1
		return len(buf), nil
	}

	mask, err := getAffinityWithReader(123, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sizes, []int{8, 16, 32, 64, 128}) {
		t.Fatalf("unexpected attempted sizes: %v", sizes)
	}
	if !mask.Has(64) {
		t.Fatalf("expected CPU 64 in mask, got %s", mask.String())
	}
}

func TestGetAffinityStopsOnNonSizeError(t *testing.T) {
	want := syscall.ESRCH
	_, err := getAffinityWithReader(123, func(int, []byte) (int, error) {
		return 0, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetAffinityRejectsInvalidReturnedSize(t *testing.T) {
	_, err := getAffinityWithReader(123, func(_ int, buf []byte) (int, error) {
		return len(buf) + 1, nil
	})
	if err == nil {
		t.Fatal("expected invalid returned size error")
	}
}
