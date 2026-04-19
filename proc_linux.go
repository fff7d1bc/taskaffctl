package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type Task struct {
	PID int
	TID int
}

func listPIDs(procRoot string) ([]int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err == nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids, nil
}

func listTasks(procRoot string, pid int) ([]Task, error) {
	taskDir := filepath.Join(procRoot, strconv.Itoa(pid), "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		tid, err := strconv.Atoi(entry.Name())
		if err == nil {
			tasks = append(tasks, Task{PID: pid, TID: tid})
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TID < tasks[j].TID })
	return tasks, nil
}

func isProcRace(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func readProcessComm(procRoot string, pid int) string {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readProcessExeBase(procRoot string, pid int) string {
	path, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "exe"))
	if err != nil {
		return ""
	}
	return filepath.Base(path)
}

func processMatchesName(procRoot string, pid int, name string) bool {
	if name == "" {
		return false
	}
	if readProcessComm(procRoot, pid) == name {
		return true
	}
	return readProcessExeBase(procRoot, pid) == name
}

func readProcessPPid(procRoot string, pid int) (int, error) {
	file, err := os.Open(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("malformed PPid line for pid %d", pid)
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, err
		}
		return ppid, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("PPid not found for pid %d", pid)
}

func expandPIDTree(procRoot string, roots []int) ([]int, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	pids, err := listPIDs(procRoot)
	if err != nil {
		return nil, err
	}
	children := map[int][]int{}
	for _, pid := range pids {
		ppid, err := readProcessPPid(procRoot, pid)
		if err != nil {
			if isProcRace(err) {
				continue
			}
			return nil, err
		}
		children[ppid] = append(children[ppid], pid)
	}

	seen := map[int]struct{}{}
	queue := append([]int(nil), roots...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		queue = append(queue, children[pid]...)
	}

	out := make([]int, 0, len(seen))
	for pid := range seen {
		out = append(out, pid)
	}
	sort.Ints(out)
	return out, nil
}
