package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

func getAffinity(tid int, maxCPU int) (CPUSet, error) {
	size := 8
	if maxCPU >= 0 {
		size = ((maxCPU / 64) + 1) * 8
	}
	buf := make([]byte, size)
	_, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_GETAFFINITY, uintptr(tid), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return CPUSet{}, fmt.Errorf("sched_getaffinity(%d): %w", tid, errno)
	}
	return cpuSetFromBytes(buf), nil
}

func setAffinity(tid int, cpus CPUSet) error {
	buf := cpus.toBytes()
	_, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY, uintptr(tid), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return fmt.Errorf("sched_setaffinity(%d): %w", tid, errno)
	}
	return nil
}
