package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Topology struct {
	CPUModel string
	Online   CPUSet
	Clusters []Cluster
}

type Cluster struct {
	Key               string
	CPUs              CPUSet
	DeclaredCPUs      CPUSet
	L3SizeBytes       int64
	AMDPstateMaxFreqs map[int]int64
	HighestPerf       map[int]int
	PhysicalCoreKeys  map[string]struct{}
	PhysicalCoreCount int
}

type onlineCPU struct {
	ID  int
	Dir string
}

type ClusterSelection struct {
	Cluster  Cluster
	Reliable bool
	Reason   string
}

type ClusterTags struct {
	ByCPUSet     map[string][]string
	Unassigned   []string
	AllCanonical []string
	Special      []string
}

func ReadTopology(sysfsRoot string) (*Topology, error) {
	cpus, online, err := readOnlineCPUInventory(sysfsRoot)
	if err != nil {
		return nil, err
	}
	if online.IsEmpty() {
		return nil, errors.New("no online CPUs found")
	}

	topo := &Topology{Online: online}
	clusterMap := map[string]*Cluster{}

	for _, cpuInfo := range cpus {
		cpu := cpuInfo.ID
		cpuDir := cpuInfo.Dir

		cacheDir, err := findL3CacheDir(cpuDir)
		if err != nil {
			return nil, fmt.Errorf("cpu %d: find L3 cache: %w", cpu, err)
		}
		// Ryzen firmware can report a single cluster_id even when the useful
		// scheduling boundary is really the set of CPUs sharing one L3 cache.
		shared, err := readSharedCPUSet(cacheDir)
		if err != nil {
			return nil, fmt.Errorf("cpu %d: %w", cpu, err)
		}
		if !shared.Has(cpu) {
			return nil, fmt.Errorf("cpu %d: L3 shared CPU set %q does not contain the CPU", cpu, shared.String())
		}
		key := shared.String()
		size, err := parseCacheSize(readTrimmed(filepath.Join(cacheDir, "size")))
		if err != nil {
			return nil, fmt.Errorf("cpu %d: read L3 cache size: %w", cpu, err)
		}
		coreKey, err := readCoreKey(cpuDir, cpu)
		if err != nil {
			return nil, err
		}

		cluster := clusterMap[key]
		if cluster == nil {
			cluster = &Cluster{
				Key:               key,
				DeclaredCPUs:      shared,
				L3SizeBytes:       size,
				AMDPstateMaxFreqs: map[int]int64{},
				HighestPerf:       map[int]int{},
				PhysicalCoreKeys:  map[string]struct{}{},
			}
			clusterMap[key] = cluster
		} else if cluster.L3SizeBytes != size {
			return nil, fmt.Errorf("cpu %d: inconsistent L3 cache size for shared CPU set %q", cpu, key)
		}
		cluster.CPUs.Add(cpu)
		if freq, ok := readOptionalInt64(filepath.Join(sysfsRoot, "cpufreq", fmt.Sprintf("policy%d", cpu), "amd_pstate_max_freq")); ok {
			cluster.AMDPstateMaxFreqs[cpu] = freq
		}
		if perf, ok := readOptionalInt(filepath.Join(cpuDir, "acpi_cppc", "highest_perf")); ok {
			cluster.HighestPerf[cpu] = perf
		}
		cluster.PhysicalCoreKeys[coreKey] = struct{}{}
	}

	for _, cluster := range clusterMap {
		expected := cluster.DeclaredCPUs.Intersect(topo.Online)
		if !cluster.CPUs.Equal(expected) {
			return nil, fmt.Errorf("incomplete L3 cluster %q: observed %q, expected online CPUs %q", cluster.Key, cluster.CPUs.String(), expected.String())
		}
		cluster.PhysicalCoreCount = len(cluster.PhysicalCoreKeys)
		topo.Clusters = append(topo.Clusters, *cluster)
	}
	sort.Slice(topo.Clusters, func(i, j int) bool {
		return topo.Clusters[i].CPUs.CPUs()[0] < topo.Clusters[j].CPUs.CPUs()[0]
	})
	return topo, nil
}

func ReadOnlineCPUSet(sysfsRoot string) (CPUSet, error) {
	_, online, err := readOnlineCPUInventory(sysfsRoot)
	if err != nil {
		return CPUSet{}, err
	}
	if online.IsEmpty() {
		return CPUSet{}, errors.New("no online CPUs found")
	}
	return online, nil
}

func readOnlineCPUInventory(sysfsRoot string) ([]onlineCPU, CPUSet, error) {
	cpuDirs, err := filepath.Glob(filepath.Join(sysfsRoot, "cpu[0-9]*"))
	if err != nil {
		return nil, CPUSet{}, fmt.Errorf("scan CPUs: %w", err)
	}
	sort.Slice(cpuDirs, func(i, j int) bool {
		return cpuDirID(cpuDirs[i]) < cpuDirID(cpuDirs[j])
	})

	var cpus []onlineCPU
	var onlineSet CPUSet
	for _, cpuDir := range cpuDirs {
		cpu := cpuDirID(cpuDir)
		if cpu < 0 {
			continue
		}
		online, err := isCPUOnline(cpuDir, cpu)
		if err != nil {
			return nil, CPUSet{}, err
		}
		if !online {
			continue
		}
		onlineSet.Add(cpu)
		cpus = append(cpus, onlineCPU{ID: cpu, Dir: cpuDir})
	}
	return cpus, onlineSet, nil
}

func (c Cluster) AvgHighestPerf() float64 {
	if len(c.HighestPerf) == 0 {
		return 0
	}
	total := 0
	for _, value := range c.HighestPerf {
		total += value
	}
	return float64(total) / float64(len(c.HighestPerf))
}

func (c Cluster) MinHighestPerf() (int, bool) {
	first := true
	min := 0
	for _, value := range c.HighestPerf {
		if first || value < min {
			min = value
			first = false
		}
	}
	return min, !first
}

func (c Cluster) MaxHighestPerf() (int, bool) {
	first := true
	max := 0
	for _, value := range c.HighestPerf {
		if first || value > max {
			max = value
			first = false
		}
	}
	return max, !first
}

func (c Cluster) L3PerCore() float64 {
	if c.PhysicalCoreCount <= 0 {
		return 0
	}
	return float64(c.L3SizeBytes) / float64(c.PhysicalCoreCount)
}

func (c Cluster) AMDPstateMaxFreqMHzList() []int64 {
	if len(c.AMDPstateMaxFreqs) == 0 {
		return nil
	}
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(c.AMDPstateMaxFreqs))
	for _, value := range c.AMDPstateMaxFreqs {
		mhz := value / 1000
		if mhz <= 0 {
			continue
		}
		if _, ok := seen[mhz]; ok {
			continue
		}
		seen[mhz] = struct{}{}
		out = append(out, mhz)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func SelectClusterByTag(topo *Topology, tag string) (*ClusterSelection, error) {
	if topo == nil {
		return nil, errors.New("nil topology")
	}
	canonical, err := ParseClusterTag(tag)
	if err != nil {
		return nil, err
	}
	if canonical == "all-cores" {
		if topo.Online.IsEmpty() {
			return nil, errors.New("no online CPUs found")
		}
		// all-cores is intentionally outside normal cluster tagging and just means
		// "restore the full online mask".
		return &ClusterSelection{
			Cluster:  Cluster{CPUs: topo.Online},
			Reliable: true,
			Reason:   "selected special tag \"all-cores\"",
		}, nil
	}
	if len(topo.Clusters) == 0 {
		return nil, errors.New("no L3 clusters found")
	}
	tags := BuildClusterTags(topo)
	if !tags.HasAssignedClusterTags() {
		return nil, errors.New("no unique cluster tags are available on this topology")
	}
	var matched *Cluster
	for _, cluster := range topo.Clusters {
		for _, clusterTag := range tags.ByCPUSet[cluster.CPUs.String()] {
			if clusterTag == canonical {
				clusterCopy := cluster
				matched = &clusterCopy
				break
			}
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("cluster tag %q is unavailable or not unique on this topology", canonical)
	}
	return &ClusterSelection{
		Cluster:  *matched,
		Reliable: true,
		Reason:   fmt.Sprintf("selected by unique cluster tag %q", canonical),
	}, nil
}

func ResolveClusterSelection(topo *Topology, tag string) (*ClusterSelection, error) {
	return SelectClusterByTag(topo, tag)
}

func BuildClusterTags(topo *Topology) ClusterTags {
	tags := ClusterTags{
		ByCPUSet:     map[string][]string{},
		AllCanonical: allClusterTags(),
		Special:      specialClusterTags(),
	}
	if topo == nil || len(topo.Clusters) == 0 {
		tags.Unassigned = append([]string(nil), tags.AllCanonical...)
		return tags
	}

	// Performance tags only make sense when every online CPU in every detected
	// cluster exposed CPPC highest_perf.
	if clustersHaveCompleteCPPC(topo.Clusters) {
		addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "highest-perf-cores", func(c Cluster) float64 {
			return c.AvgHighestPerf()
		}, true)
		addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "lowest-perf-cores", func(c Cluster) float64 {
			return c.AvgHighestPerf()
		}, false)
	}
	addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "most-cache", func(c Cluster) float64 {
		return float64(c.L3SizeBytes)
	}, true)
	addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "least-cache", func(c Cluster) float64 {
		return float64(c.L3SizeBytes)
	}, false)
	addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "most-cache-per-core", func(c Cluster) float64 {
		return c.L3PerCore()
	}, true)
	addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "least-cache-per-core", func(c Cluster) float64 {
		return c.L3PerCore()
	}, false)
	addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "most-cores", func(c Cluster) float64 {
		return float64(c.PhysicalCoreCount)
	}, true)
	addUniqueExtremaTag(topo.Clusters, tags.ByCPUSet, "least-cores", func(c Cluster) float64 {
		return float64(c.PhysicalCoreCount)
	}, false)

	for cpus, list := range tags.ByCPUSet {
		sort.Strings(list)
		tags.ByCPUSet[cpus] = dedupeStrings(list)
	}
	assigned := map[string]struct{}{}
	for _, list := range tags.ByCPUSet {
		for _, tag := range list {
			assigned[tag] = struct{}{}
		}
	}
	for _, tag := range tags.AllCanonical {
		if _, ok := assigned[tag]; !ok {
			tags.Unassigned = append(tags.Unassigned, tag)
		}
	}
	return tags
}

func (t ClusterTags) HasAssignedClusterTags() bool {
	for _, list := range t.ByCPUSet {
		if len(list) != 0 {
			return true
		}
	}
	return false
}

func clustersHaveCompleteCPPC(clusters []Cluster) bool {
	if len(clusters) == 0 {
		return false
	}
	for _, cluster := range clusters {
		if cluster.CPUs.Count() == 0 {
			return false
		}
		if len(cluster.HighestPerf) != cluster.CPUs.Count() {
			return false
		}
	}
	return true
}

func cpuDirID(path string) int {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "cpu") {
		return -1
	}
	id, err := strconv.Atoi(base[3:])
	if err != nil {
		return -1
	}
	return id
}

func isCPUOnline(cpuDir string, cpu int) (bool, error) {
	onlinePath := filepath.Join(cpuDir, "online")
	data, err := os.ReadFile(onlinePath)
	if err == nil {
		return strings.TrimSpace(string(data)) == "1", nil
	}
	if cpu == 0 && errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, fmt.Errorf("read %s: %w", onlinePath, err)
}

func findL3CacheDir(cpuDir string) (string, error) {
	indexes, err := filepath.Glob(filepath.Join(cpuDir, "cache", "index*"))
	if err != nil {
		return "", err
	}
	for _, idx := range indexes {
		if readTrimmed(filepath.Join(idx, "level")) == "3" {
			return idx, nil
		}
	}
	return "", errors.New("no L3 cache index")
}

func readSharedCPUSet(cacheDir string) (CPUSet, error) {
	list := readTrimmed(filepath.Join(cacheDir, "shared_cpu_list"))
	if list != "" {
		set, err := ParseCPUSet(list)
		if err != nil {
			return CPUSet{}, fmt.Errorf("parse L3 shared_cpu_list: %w", err)
		}
		if set.IsEmpty() {
			return CPUSet{}, errors.New("empty L3 shared_cpu_list")
		}
		return set, nil
	}

	cpuMap := readTrimmed(filepath.Join(cacheDir, "shared_cpu_map"))
	if cpuMap == "" {
		return CPUSet{}, errors.New("missing L3 shared_cpu_list and shared_cpu_map")
	}
	set, err := parseCPUMap(cpuMap)
	if err != nil {
		return CPUSet{}, fmt.Errorf("parse L3 shared_cpu_map: %w", err)
	}
	return set, nil
}

func parseCPUMap(s string) (CPUSet, error) {
	hexDigits := strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if hexDigits == "" {
		return CPUSet{}, errors.New("empty CPU map")
	}
	var set CPUSet
	for nibbleIndex, i := 0, len(hexDigits)-1; i >= 0; nibbleIndex, i = nibbleIndex+1, i-1 {
		value, err := strconv.ParseUint(hexDigits[i:i+1], 16, 4)
		if err != nil {
			return CPUSet{}, fmt.Errorf("invalid CPU map %q", s)
		}
		for bit := 0; bit < 4; bit++ {
			if value&(1<<bit) != 0 {
				set.Add(nibbleIndex*4 + bit)
			}
		}
	}
	if set.IsEmpty() {
		return CPUSet{}, errors.New("empty CPU map")
	}
	return set, nil
}

func parseCacheSize(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty cache size")
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "M")
	}
	value, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("cache size must be positive, got %d", value)
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if value > maxInt64/multiplier {
		return 0, fmt.Errorf("cache size %q overflows bytes", s)
	}
	return value * multiplier, nil
}

func readCoreKey(cpuDir string, cpu int) (string, error) {
	coreID := readTrimmed(filepath.Join(cpuDir, "topology", "core_id"))
	if coreID == "" {
		return "", fmt.Errorf("cpu %d: missing topology/core_id", cpu)
	}
	if _, err := strconv.Atoi(coreID); err != nil {
		return "", fmt.Errorf("cpu %d: invalid topology/core_id %q", cpu, coreID)
	}
	packageID := readTrimmed(filepath.Join(cpuDir, "topology", "physical_package_id"))
	if packageID == "" {
		return "", fmt.Errorf("cpu %d: missing topology/physical_package_id", cpu)
	}
	if _, err := strconv.Atoi(packageID); err != nil {
		return "", fmt.Errorf("cpu %d: invalid topology/physical_package_id %q", cpu, packageID)
	}
	return packageID + ":" + coreID, nil
}

func readOptionalInt(path string) (int, bool) {
	data := readTrimmed(path)
	if data == "" {
		return 0, false
	}
	value, err := strconv.Atoi(data)
	if err != nil {
		return 0, false
	}
	return value, true
}

func readOptionalInt64(path string) (int64, bool) {
	data := readTrimmed(path)
	if data == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(data, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func ReadCPUModel(procCPUInfoPath string) string {
	data, err := os.ReadFile(procCPUInfoPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name\t:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "model name\t:"))
		}
		if strings.HasPrefix(line, "Hardware\t:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Hardware\t:"))
		}
	}
	return ""
}

func addUniqueExtremaTag(clusters []Cluster, out map[string][]string, tag string, metric func(Cluster) float64, wantMax bool) {
	if len(clusters) == 0 {
		return
	}
	bestIndex := 0
	best := metric(clusters[0])
	tied := false
	for idx, cluster := range clusters[1:] {
		value := metric(cluster)
		if wantMax {
			if value > best {
				best = value
				bestIndex = idx + 1
				tied = false
			} else if value == best {
				tied = true
			}
		} else if value < best {
			best = value
			bestIndex = idx + 1
			tied = false
		} else if value == best {
			tied = true
		}
	}
	if tied {
		// Tags are unique by design. If multiple clusters tie for a tag, none of
		// them get it and the tag moves to unassigned_tags.
		return
	}
	out[clusters[bestIndex].CPUs.String()] = append(out[clusters[bestIndex].CPUs.String()], tag)
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	last := ""
	for _, item := range in {
		if len(out) == 0 || item != last {
			out = append(out, item)
			last = item
		}
	}
	return out
}

func ParseClusterTag(s string) (string, error) {
	for _, tag := range allSelectableTags() {
		if s == tag {
			return s, nil
		}
	}
	return "", fmt.Errorf("unknown cluster tag %q", s)
}

func allSelectableTags() []string {
	return append(append([]string{}, allClusterTags()...), specialClusterTags()...)
}

func allClusterTags() []string {
	return []string{
		"lowest-perf-cores",
		"highest-perf-cores",
		"most-cache",
		"least-cache",
		"most-cache-per-core",
		"least-cache-per-core",
		"most-cores",
		"least-cores",
	}
}

func specialClusterTags() []string {
	return []string{"all-cores"}
}
