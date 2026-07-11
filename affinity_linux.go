package main

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

type affinityReader func(tid int, buf []byte) (int, error)

func getAffinity(tid int) (CPUSet, error) {
	return getAffinityWithReader(tid, rawGetAffinity)
}

func getAffinityWithReader(tid int, read affinityReader) (CPUSet, error) {
	for size := 8; ; size *= 2 {
		buf := make([]byte, size)
		n, err := read(tid, buf)
		if err == nil {
			if n <= 0 || n > len(buf) {
				return CPUSet{}, fmt.Errorf("sched_getaffinity(%d) returned invalid mask size %d", tid, n)
			}
			return cpuSetFromBytes(buf[:n]), nil
		}
		if !errors.Is(err, syscall.EINVAL) {
			return CPUSet{}, fmt.Errorf("sched_getaffinity(%d): %w", tid, err)
		}
		const maxInt = int(^uint(0) >> 1)
		if size > maxInt/2 {
			return CPUSet{}, fmt.Errorf("sched_getaffinity(%d): affinity mask size overflow", tid)
		}
	}
}

func rawGetAffinity(tid int, buf []byte) (int, error) {
	n, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_GETAFFINITY, uintptr(tid), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

func setAffinity(tid int, cpus CPUSet) error {
	// Go's syscall package does not expose sched_setaffinity directly, so call
	// the raw syscall with the packed cpu mask bytes.
	buf := cpus.toBytes()
	_, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY, uintptr(tid), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return fmt.Errorf("sched_setaffinity(%d): %w", tid, errno)
	}
	return nil
}
