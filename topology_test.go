package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestResolveClusterSelectionLowestPerf(t *testing.T) {
	root := t.TempDir()
	writeCPUFixture(t, root, 0, "0-1", "16384K", "200")
	writeCPUFixture(t, root, 1, "0-1", "16384K", "198")
	writeCPUFixture(t, root, 2, "2-3", "8192K", "125")
	writeCPUFixture(t, root, 3, "2-3", "8192K", "125")

	topo, err := ReadTopology(root)
	if err != nil {
		t.Fatal(err)
	}

	selection, err := ResolveClusterSelection(topo, "lowest-perf-cores")
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Reliable {
		t.Fatalf("expected reliable selection, got %q", selection.Reason)
	}
	if got := selection.Cluster.CPUs.String(); got != "2-3" {
		t.Fatalf("unexpected selected cluster: %s", got)
	}
}

func TestResolveClusterSelectionRejectsUnavailableTag(t *testing.T) {
	root := t.TempDir()
	writeCPUFixture(t, root, 0, "0-1", "16384K", "200")
	writeCPUFixture(t, root, 1, "0-1", "16384K", "200")
	writeCPUFixture(t, root, 2, "2-3", "16384K", "150")
	writeCPUFixture(t, root, 3, "2-3", "16384K", "150")

	topo, err := ReadTopology(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveClusterSelection(topo, "most-cache"); err == nil {
		t.Fatal("expected tied most-cache tag to be unavailable")
	}
}

func TestResolveClusterSelectionRejectsTopologyWithoutUniqueTags(t *testing.T) {
	topo := &Topology{
		Clusters: []Cluster{
			{
				CPUs:              mustParseCPUSet(t, "0-1"),
				HighestPerf:       map[int]int{0: 150, 1: 150},
				L3SizeBytes:       16 * 1024 * 1024,
				PhysicalCoreCount: 2,
			},
			{
				CPUs:              mustParseCPUSet(t, "2-3"),
				HighestPerf:       map[int]int{2: 150, 3: 150},
				L3SizeBytes:       16 * 1024 * 1024,
				PhysicalCoreCount: 2,
			},
		},
	}

	_, err := ResolveClusterSelection(topo, "lowest-perf-cores")
	if err == nil || !strings.Contains(err.Error(), "no unique cluster tags are available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildClusterTagsIncludesUnassigned(t *testing.T) {
	root := t.TempDir()
	writeCPUFixture(t, root, 0, "0-1", "16384K", "200")
	writeCPUFixture(t, root, 1, "0-1", "16384K", "200")
	writeCPUFixture(t, root, 2, "2-3", "16384K", "150")
	writeCPUFixture(t, root, 3, "2-3", "16384K", "150")

	topo, err := ReadTopology(root)
	if err != nil {
		t.Fatal(err)
	}

	tags := BuildClusterTags(topo)
	if !strings.Contains(strings.Join(tags.ByCPUSet["0-1"], ","), "highest-perf-cores") {
		t.Fatalf("expected highest-perf-cores tag on 0-1, got %v", tags.ByCPUSet["0-1"])
	}
	if strings.Contains(strings.Join(tags.ByCPUSet["0-1"], ","), "most-cache") {
		t.Fatalf("expected tied cache tags to be omitted, got %v", tags.ByCPUSet["0-1"])
	}
	if !strings.Contains(strings.Join(tags.Unassigned, ","), "most-cache") {
		t.Fatalf("expected most-cache in unassigned tags, got %v", tags.Unassigned)
	}
	if !strings.Contains(strings.Join(tags.Unassigned, ","), "least-cores") {
		t.Fatalf("expected least-cores in unassigned tags, got %v", tags.Unassigned)
	}
	if !strings.Contains(strings.Join(tags.Special, ","), "all-cores") {
		t.Fatalf("expected all-cores in special tags, got %v", tags.Special)
	}
}

func TestBuildClusterTagsSkipsPerfTagsWhenCPPCIsIncomplete(t *testing.T) {
	topo := &Topology{
		Clusters: []Cluster{
			{
				CPUs:              mustParseCPUSet(t, "0-1"),
				HighestPerf:       map[int]int{0: 200, 1: 200},
				PhysicalCoreCount: 2,
			},
			{
				CPUs:              mustParseCPUSet(t, "2-3"),
				HighestPerf:       map[int]int{},
				PhysicalCoreCount: 2,
			},
		},
	}

	tags := BuildClusterTags(topo)
	for _, list := range tags.ByCPUSet {
		joined := strings.Join(list, ",")
		if strings.Contains(joined, "highest-perf-cores") || strings.Contains(joined, "lowest-perf-cores") {
			t.Fatalf("expected performance tags to be omitted when CPPC is incomplete, got %v", tags.ByCPUSet)
		}
	}
	if !strings.Contains(strings.Join(tags.Unassigned, ","), "highest-perf-cores") {
		t.Fatalf("expected highest-perf-cores to be unassigned, got %v", tags.Unassigned)
	}
	if !strings.Contains(strings.Join(tags.Unassigned, ","), "lowest-perf-cores") {
		t.Fatalf("expected lowest-perf-cores to be unassigned, got %v", tags.Unassigned)
	}
}

func TestBuildClusterTagsReportsNoAssignedClusterTags(t *testing.T) {
	topo := &Topology{
		Clusters: []Cluster{
			{
				CPUs:              mustParseCPUSet(t, "0-1"),
				HighestPerf:       map[int]int{0: 150, 1: 150},
				L3SizeBytes:       16 * 1024 * 1024,
				PhysicalCoreCount: 2,
			},
			{
				CPUs:              mustParseCPUSet(t, "2-3"),
				HighestPerf:       map[int]int{2: 150, 3: 150},
				L3SizeBytes:       16 * 1024 * 1024,
				PhysicalCoreCount: 2,
			},
		},
	}

	tags := BuildClusterTags(topo)
	if tags.HasAssignedClusterTags() {
		t.Fatalf("expected no assigned cluster tags, got %v", tags.ByCPUSet)
	}
}

func TestReadCPUModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpuinfo")
	mustWriteFile(t, path, "processor\t: 0\nmodel name\t: AMD Ryzen AI 9 HX 370 w/ Radeon 890M\n")
	if got := ReadCPUModel(path); got != "AMD Ryzen AI 9 HX 370 w/ Radeon 890M" {
		t.Fatalf("unexpected cpu model: %q", got)
	}
}

func TestClusterAMDPstateMaxFreqMHzListDedupesAndSorts(t *testing.T) {
	root := t.TempDir()
	writeCPUFixture(t, root, 0, "0-1", "16384K", "200")
	writeCPUFixture(t, root, 1, "0-1", "16384K", "198")
	mustMkdirAll(t, filepath.Join(root, "cpufreq", "policy0"))
	mustMkdirAll(t, filepath.Join(root, "cpufreq", "policy1"))
	mustWriteFile(t, filepath.Join(root, "cpufreq", "policy0", "amd_pstate_max_freq"), "5157895\n")
	mustWriteFile(t, filepath.Join(root, "cpufreq", "policy1", "amd_pstate_max_freq"), "5157895\n")

	topo, err := ReadTopology(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Clusters) != 1 {
		t.Fatalf("unexpected cluster count: %d", len(topo.Clusters))
	}
	got := topo.Clusters[0].AMDPstateMaxFreqMHzList()
	if len(got) != 1 || got[0] != 5157 {
		t.Fatalf("unexpected amd_pstate_max_freq list: %v", got)
	}
}

func TestParseClusterTagAcceptsAllCores(t *testing.T) {
	got, err := ParseClusterTag("all-cores")
	if err != nil {
		t.Fatal(err)
	}
	if got != "all-cores" {
		t.Fatalf("unexpected parsed tag: %s", got)
	}
}

func TestResolveClusterSelectionAllCoresWithoutClusters(t *testing.T) {
	topo := &Topology{Online: mustParseCPUSet(t, "0-3")}
	selection, err := ResolveClusterSelection(topo, "all-cores")
	if err != nil {
		t.Fatal(err)
	}
	if got := selection.Cluster.CPUs.String(); got != "0-3" {
		t.Fatalf("unexpected all-cores selection: %s", got)
	}
}

func writeCPUFixture(t *testing.T, root string, cpu int, shared string, l3Size string, highestPerf string) {
	t.Helper()
	base := filepath.Join(root, "cpu"+strconv.Itoa(cpu))
	mustMkdirAll(t, filepath.Join(base, "cache", "index3"))
	mustMkdirAll(t, filepath.Join(base, "acpi_cppc"))
	mustMkdirAll(t, filepath.Join(base, "topology"))
	mustWriteFile(t, filepath.Join(base, "online"), "1\n")
	mustWriteFile(t, filepath.Join(base, "cache", "index3", "level"), "3\n")
	mustWriteFile(t, filepath.Join(base, "cache", "index3", "shared_cpu_list"), shared+"\n")
	mustWriteFile(t, filepath.Join(base, "cache", "index3", "size"), l3Size+"\n")
	mustWriteFile(t, filepath.Join(base, "acpi_cppc", "highest_perf"), highestPerf+"\n")
	mustWriteFile(t, filepath.Join(base, "topology", "core_id"), strconv.Itoa(cpu)+"\n")
	mustWriteFile(t, filepath.Join(base, "topology", "physical_package_id"), "0\n")
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustParseCPUSet(t *testing.T, s string) CPUSet {
	t.Helper()
	set, err := ParseCPUSet(s)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
