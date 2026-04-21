package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

var errHelpRequested = errors.New("help requested")

func main() {
	log.SetFlags(log.LstdFlags)
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errHelpRequested) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Println(usageText())
		return errHelpRequested
	}
	if err := enforceDoubleDashLongFlags(args); err != nil {
		return err
	}

	fs := newFlagSet("taskaffctl", usageText())
	tagLong := fs.String("tag", "", "")
	tagShort := fs.String("t", "", "")
	topologyLong := fs.Bool("topology", false, "")
	topologyShort := fs.Bool("T", false, "")
	jsonLong := fs.Bool("json", false, "")
	jsonShort := fs.Bool("j", false, "")
	pidLong := fs.String("pid", "", "")
	pidShort := fs.String("p", "", "")
	descendantsLong := fs.Bool("descendants", false, "")
	descendantsShort := fs.Bool("d", false, "")
	commLong := fs.String("comm", "", "")
	commShort := fs.String("c", "", "")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelpRequested
		}
		return err
	}

	clusterTag, err := resolveStringFlagAlias("tag", *tagLong, *tagShort)
	if err != nil {
		return err
	}
	topologyMode, err := resolveBoolFlagAlias("topology", *topologyLong, *topologyShort)
	if err != nil {
		return err
	}
	jsonOutput, err := resolveBoolFlagAlias("json", *jsonLong, *jsonShort)
	if err != nil {
		return err
	}
	descendants, err := resolveBoolFlagAlias("descendants", *descendantsLong, *descendantsShort)
	if err != nil {
		return err
	}
	pidValue, err := resolveStringFlagAlias("pid", *pidLong, *pidShort)
	if err != nil {
		return err
	}
	comm, err := resolveStringFlagAlias("comm", *commLong, *commShort)
	if err != nil {
		return err
	}

	pids, err := parsePIDList(pidValue)
	if err != nil {
		return err
	}
	if topologyMode {
		if len(fs.Args()) != 0 {
			return errors.New("--topology cannot be combined with a command")
		}
		if len(pids) != 0 || comm != "" || clusterTag != "" || descendants {
			return errors.New("--topology cannot be combined with --tag, --pid, --descendants, or --comm")
		}
		return runTopology(jsonOutput)
	}
	if descendants && len(pids) == 0 && comm == "" {
		return errors.New("--descendants requires --pid or --comm")
	}
	if len(pids) != 0 && comm != "" {
		return errors.New("--pid and --comm cannot be combined")
	}
	if clusterTag != "" {
		if _, err := ParseClusterTag(clusterTag); err != nil {
			return err
		}
	}

	if len(pids) != 0 {
		if len(fs.Args()) != 0 {
			return errors.New("--pid cannot be combined with a command")
		}
		if clusterTag == "" {
			return errors.New("--tag is required with --pid")
		}
		if descendants {
			pids, err = expandPIDTree("/proc", pids)
			if err != nil {
				return err
			}
		}
		return applyExistingPIDs(clusterTag, pids, jsonOutput)
	}
	if comm != "" {
		if len(fs.Args()) != 0 {
			return errors.New("--comm cannot be combined with a command")
		}
		if clusterTag == "" {
			return errors.New("--tag is required with --comm")
		}
		return applyExistingComm(clusterTag, comm, jsonOutput, descendants)
	}

	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("a command is required unless --topology, --pid, or --comm is used")
	}
	if jsonOutput {
		return errors.New("--json is only valid with --topology, --pid, or --comm")
	}
	if clusterTag == "" {
		return errors.New("--tag is required unless --topology is used")
	}
	return launchWithCluster(clusterTag, cmdArgs)
}

func runTopology(jsonOutput bool) error {
	topo, err := ReadTopology("/sys/devices/system/cpu")
	if err != nil {
		return err
	}
	topo.CPUModel = ReadCPUModel("/proc/cpuinfo")
	clusterTags := BuildClusterTags(topo)
	if jsonOutput {
		return writeTopologyJSON(topo, clusterTags)
	}
	return writeTopologyYAMLish(topo, clusterTags)
}

func launchWithCluster(clusterTag string, cmdArgs []string) error {
	topo, err := ReadTopology("/sys/devices/system/cpu")
	if err != nil {
		return err
	}
	selection, err := ResolveClusterSelection(topo, clusterTag)
	if err != nil {
		return err
	}
	if !selection.Reliable {
		return fmt.Errorf("cluster selection uncertain: %s", selection.Reason)
	}
	if err := setAffinity(0, selection.Cluster.CPUs); err != nil {
		return err
	}
	path, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, cmdArgs, os.Environ())
}

func applyExistingPIDs(clusterTag string, pids []int, jsonOutput bool) error {
	topo, err := ReadTopology("/sys/devices/system/cpu")
	if err != nil {
		return err
	}
	selection, err := ResolveClusterSelection(topo, clusterTag)
	if err != nil {
		return err
	}
	if !selection.Reliable {
		return fmt.Errorf("cluster selection uncertain: %s", selection.Reason)
	}
	var reports []pidAffinityReport
	var failures []string
	for _, pid := range pids {
		report, err := applyPIDToCPUSet(pid, topo.Online.MaxCPU(), selection.Cluster.CPUs)
		reports = append(reports, report)
		if err != nil {
			if isProcRace(err) {
				continue
			}
			failures = append(failures, fmt.Sprintf("pid %d: %v", pid, err))
		}
	}
	printPIDReports(reports, jsonOutput)
	if len(failures) != 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

type pidAffinityReport struct {
	PID    int
	Comm   string
	Status string
	From   string
	To     string
	Err    error
}

func applyPIDToCPUSet(pid int, maxCPU int, target CPUSet) (pidAffinityReport, error) {
	report := pidAffinityReport{PID: pid, Comm: readProcessComm("/proc", pid)}
	before, err := summarizePIDAffinity("/proc", pid, maxCPU)
	if err != nil {
		report.Status = "failed"
		report.Err = err
		return report, err
	}
	report.From = before
	tasks, err := listTasks("/proc", pid)
	if err != nil {
		report.Status = "failed"
		report.Err = err
		return report, err
	}
	for _, task := range tasks {
		if err := setAffinity(task.TID, target); err != nil {
			if isProcRace(err) {
				continue
			}
			report.Status = "failed"
			report.Err = err
			return report, err
		}
	}
	after, err := summarizePIDAffinity("/proc", pid, maxCPU)
	if err != nil {
		report.Status = "failed"
		report.Err = err
		return report, err
	}
	report.To = after
	if report.From == report.To {
		report.Status = "unchanged"
	} else {
		report.Status = "updated"
	}
	return report, nil
}

func applyExistingComm(clusterTag string, comm string, jsonOutput bool, pidTree bool) error {
	topo, err := ReadTopology("/sys/devices/system/cpu")
	if err != nil {
		return err
	}
	selection, err := ResolveClusterSelection(topo, clusterTag)
	if err != nil {
		return err
	}
	if !selection.Reliable {
		return fmt.Errorf("cluster selection uncertain: %s", selection.Reason)
	}
	pids, err := listPIDs("/proc")
	if err != nil {
		return err
	}
	matches := 0
	maxCPU := topo.Online.MaxCPU()
	target := selection.Cluster.CPUs
	var failures []string
	var reports []pidAffinityReport
	selected := []int{}
	for _, pid := range pids {
		if !processMatchesName("/proc", pid, comm) {
			continue
		}
		selected = append(selected, pid)
	}
	if len(selected) == 0 {
		return fmt.Errorf("no running processes matched --comm %q", comm)
	}
	if pidTree {
		selected, err = expandPIDTree("/proc", selected)
		if err != nil {
			return err
		}
	}
	for _, pid := range selected {
		report, err := applyPIDToCPUSet(pid, maxCPU, target)
		reports = append(reports, report)
		if err != nil {
			if isProcRace(err) {
				continue
			}
			failures = append(failures, fmt.Sprintf("pid %d: %v", pid, err))
		}
		matches++
	}
	printPIDReports(reports, jsonOutput)
	if len(failures) != 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

type pidAffinityOutput struct {
	Reports []pidAffinityOutputItem `json:"reports"`
}

type pidAffinityOutputItem struct {
	PID    int    `json:"pid"`
	Comm   string `json:"comm"`
	Status string `json:"status"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Error  string `json:"error,omitempty"`
}

func printPIDReports(reports []pidAffinityReport, jsonOutput bool) {
	out := pidAffinityOutput{Reports: make([]pidAffinityOutputItem, 0, len(reports))}
	for _, report := range reports {
		item := pidAffinityOutputItem{
			PID:    report.PID,
			Comm:   report.Comm,
			Status: report.Status,
			From:   report.From,
			To:     report.To,
		}
		if item.Comm == "" {
			item.Comm = "?"
		}
		if report.Err != nil {
			item.Error = report.Err.Error()
		}
		out.Reports = append(out.Reports, item)
	}
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	fmt.Printf("reports:\n")
	for _, item := range out.Reports {
		fmt.Printf("  - pid: %d\n", item.PID)
		fmt.Printf("    comm: %s\n", item.Comm)
		fmt.Printf("    status: %s\n", item.Status)
		if item.From != "" {
			fmt.Printf("    from: %s\n", item.From)
		}
		if item.To != "" {
			fmt.Printf("    to: %s\n", item.To)
		}
		if item.Error != "" {
			fmt.Printf("    error: %s\n", item.Error)
		}
	}
}

func summarizePIDAffinity(procRoot string, pid int, maxCPU int) (string, error) {
	tasks, err := listTasks(procRoot, pid)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "", fmt.Errorf("no tasks found for pid %d", pid)
	}
	values := map[string]struct{}{}
	for _, task := range tasks {
		mask, err := getAffinity(task.TID, maxCPU)
		if err != nil {
			if isProcRace(err) {
				continue
			}
			return "", err
		}
		values[mask.String()] = struct{}{}
	}
	if len(values) == 0 {
		return "", fmt.Errorf("no affinity data available for pid %d", pid)
	}
	if len(values) == 1 {
		for value := range values {
			return value, nil
		}
	}
	parts := make([]string, 0, len(values))
	for value := range values {
		parts = append(parts, value)
	}
	sort.Strings(parts)
	return "mixed(" + strings.Join(parts, " | ") + ")", nil
}

type topologyJSONOutput struct {
	CPUModel       string                `json:"cpu_model,omitempty"`
	Online         string                `json:"online"`
	ClusterTagNote string                `json:"cluster_tag_note,omitempty"`
	SpecialTags    []string              `json:"special_tags,omitempty"`
	UnassignedTags []string              `json:"unassigned_tags,omitempty"`
	Clusters       []topologyJSONCluster `json:"clusters"`
}

type topologyJSONCluster struct {
	CPUs                string   `json:"cpus"`
	Tags                []string `json:"tags,omitempty"`
	L3KiB               int64    `json:"l3_kib"`
	L3PerCoreKiB        float64  `json:"l3_per_core_kib"`
	AMDPstateMaxFreqMHz []int64  `json:"amd_pstate_max_freq_mhz,omitempty"`
	PhysicalCores       int      `json:"physical_cores"`
	AvgHighestPerf      float64  `json:"avg_highest_perf"`
	MinHighestPerf      *int     `json:"min_highest_perf,omitempty"`
	MaxHighestPerf      *int     `json:"max_highest_perf,omitempty"`
}

func writeTopologyJSON(topo *Topology, clusterTags ClusterTags) error {
	out := topologyJSONOutput{
		CPUModel:       topo.CPUModel,
		Online:         topo.Online.String(),
		SpecialTags:    clusterTags.Special,
		UnassignedTags: clusterTags.Unassigned,
	}
	if !clusterTags.HasAssignedClusterTags() {
		out.ClusterTagNote = "no unique cluster tags are available on this topology"
	}
	for _, cluster := range topo.Clusters {
		entry := topologyJSONCluster{
			CPUs:                cluster.CPUs.String(),
			Tags:                clusterTags.ByCPUSet[cluster.CPUs.String()],
			L3KiB:               cluster.L3SizeBytes / 1024,
			L3PerCoreKiB:        cluster.L3PerCore() / 1024.0,
			AMDPstateMaxFreqMHz: cluster.AMDPstateMaxFreqMHzList(),
			PhysicalCores:       cluster.PhysicalCoreCount,
			AvgHighestPerf:      cluster.AvgHighestPerf(),
		}
		if v, ok := cluster.MinHighestPerf(); ok {
			entry.MinHighestPerf = &v
		}
		if v, ok := cluster.MaxHighestPerf(); ok {
			entry.MaxHighestPerf = &v
		}
		out.Clusters = append(out.Clusters, entry)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeTopologyYAMLish(topo *Topology, clusterTags ClusterTags) error {
	if topo.CPUModel != "" {
		fmt.Printf("cpu_model: %s\n", topo.CPUModel)
	}
	fmt.Printf("online: %s\n", topo.Online.String())
	if !clusterTags.HasAssignedClusterTags() {
		fmt.Printf("cluster_tag_note: no unique cluster tags are available on this topology\n")
	}
	fmt.Printf("special_tags:\n")
	if len(clusterTags.Special) == 0 {
		fmt.Printf("  []\n")
	} else {
		for _, tag := range clusterTags.Special {
			fmt.Printf("  - %s\n", tag)
		}
	}
	fmt.Printf("unassigned_tags:\n")
	if len(clusterTags.Unassigned) == 0 {
		fmt.Printf("  []\n")
	} else {
		for _, tag := range clusterTags.Unassigned {
			fmt.Printf("  - %s\n", tag)
		}
	}
	fmt.Printf("clusters:\n")
	for _, cluster := range topo.Clusters {
		cpus := cluster.CPUs.String()
		minPerf, hasMin := cluster.MinHighestPerf()
		maxPerf, hasMax := cluster.MaxHighestPerf()
		fmt.Printf("  - cpus: %s\n", cpus)
		fmt.Printf("    tags:\n")
		tags := clusterTags.ByCPUSet[cpus]
		if len(tags) == 0 {
			fmt.Printf("      []\n")
		} else {
			for _, tag := range tags {
				fmt.Printf("      - %s\n", tag)
			}
		}
		fmt.Printf("    physical_cores: %d\n", cluster.PhysicalCoreCount)
		fmt.Printf("    logical_cpus: %d\n", cluster.CPUs.Count())
		fmt.Printf("    l3_kib: %d\n", cluster.L3SizeBytes/1024)
		fmt.Printf("    l3_per_core_kib: %.1f\n", cluster.L3PerCore()/1024.0)
		if freqs := cluster.AMDPstateMaxFreqMHzList(); len(freqs) != 0 {
			fmt.Printf("    amd_pstate_max_freq_mhz: %s\n", formatInt64List(freqs))
		}
		fmt.Printf("    avg_highest_perf: %.1f\n", cluster.AvgHighestPerf())
		if hasMin {
			fmt.Printf("    min_highest_perf: %d\n", minPerf)
		}
		if hasMax {
			fmt.Printf("    max_highest_perf: %d\n", maxPerf)
		}
	}
	return nil
}

func formatInt64List(values []int64) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(value, 10))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func newFlagSet(name string, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		fmt.Fprintln(os.Stdout, usage)
	}
	return fs
}

func resolveStringFlagAlias(name string, long string, short string) (string, error) {
	if long != "" && short != "" && long != short {
		return "", fmt.Errorf("--%s and -%s disagree", name, string(name[0]))
	}
	if long != "" {
		return long, nil
	}
	return short, nil
}

func resolveBoolFlagAlias(name string, long bool, short bool) (bool, error) {
	if long && short {
		return true, nil
	}
	return long || short, nil
}

func parsePIDList(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	seen := map[int]struct{}{}
	pids := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			return nil, fmt.Errorf("invalid pid %q", field)
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids, nil
}

func usageText() string {
	return strings.TrimSpace(`
usage:
  taskaffctl --tag TAG -- command...
  taskaffctl --tag TAG --pid PIDLIST
  taskaffctl --tag TAG --comm NAME
  taskaffctl --topology [--json]

options:
  --tag TAG, -t TAG   required for command launch and process updates; use all-cores for the full online mask
  --pid PIDLIST, -p PIDLIST
                     update affinity of one or more existing processes; accepts commas or whitespace
  --comm NAME, -c NAME
                     update affinity of all matching process names
  --descendants, -d  include current descendant processes of each selected process
  --topology, -T      print detected cluster topology
  --json, -j          print topology or process-update reports as JSON

examples:
  taskaffctl -t lowest-perf-cores -- make -j8
  taskaffctl -t highest-perf-cores -p 1234
  taskaffctl -t lowest-perf-cores -d -p "$MAINPID"
  taskaffctl -t all-cores -c syncthing
`)
}

func enforceDoubleDashLongFlags(args []string) error {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "-h" || arg == "--help" {
			continue
		}
		if len(arg) >= 3 && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			name := arg[1:]
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			if name != "" {
				return fmt.Errorf("long options must use --%s, not %s", name, arg)
			}
		}
	}
	return nil
}
