# taskaffctl

`taskaffctl` steers work onto the cluster of cores that you actually want on CPUs where not all cores are equal.

This likely makes the most sense on:

- mobile Ryzen CPUs with a mix of full and compact cores, such as Ryzen AI 300 and 400 series parts, where you may want to keep something like Syncthing on the lower-power cores so a laptop does not ramp up hard every time it scans directories
- desktop Ryzen CPUs with two CCDs where only one of them has the extra X3D cache like Ryzen 9950x3D, where you may want to keep a game or another cache-sensitive workload on the cache-rich cluster

In my testing on Zen5 systems, firmware does not expose this cleanly. CPUs with two CCDs are reported as having the same `cluster_id`. That is not a serious problem when both CCDs are equivalent, but it becomes a real limitation on CPUs that mix normal and compact cores such as Zen5 and Zen5c, because there is then no simple firmware-exposed way to pin work to the lower-power cores.

This tool therefore mostly figures clusters out from which CPUs share the same L3 cache. On HX 370, the Zen5 cluster (`0-3,12-15`) has `16384K` of L3 shared by 4 physical cores, while the Zen5c cluster (`4-11,16-23`) has `8192K` of L3 shared by 8 physical cores. On the same kind of system, `topology/cluster_id` is reported as `65535` across the sampled CPUs, while the real split is visible in `cache/index3/shared_cpu_list`.

The current implementation depends on ACPI CPPC being exposed by firmware and is centered around Ryzen CPUs. It has only been tested on Zen5-family CPUs so far. My understanding is that Intel systems tend to expose the efficiency and performance core split more clearly through firmware, but I do not have access to such a system, so there is no Intel-specific handling here and Intel CPUs would go through the same code path.


## Topology samples

Mobile Zen5+Zen5c

```
% taskaffctl --topology
cpu_model: AMD Ryzen AI 9 HX 370 w/ Radeon 890M
online: 0-23
special_tags:
  - all-cores
unassigned_tags:
  []
clusters:
  - cpus: 0-3,12-15
    tags:
      - highest-perf-cores
      - least-cores
      - most-cache
      - most-cache-per-core
    physical_cores: 4
    logical_cpus: 8
    l3_kib: 16384
    l3_per_core_kib: 4096.0
    amd_pstate_max_freq_mhz: [5157]
    avg_highest_perf: 203.5
    min_highest_perf: 196
    max_highest_perf: 208
  - cpus: 4-11,16-23
    tags:
      - least-cache
      - least-cache-per-core
      - lowest-perf-cores
      - most-cores
    physical_cores: 8
    logical_cpus: 16
    l3_kib: 8192
    l3_per_core_kib: 1024.0
    amd_pstate_max_freq_mhz: [3289]
    avg_highest_perf: 125.0
    min_highest_perf: 125
    max_highest_perf: 125
```

Dual CCD Zen5 CPU

```
% taskaffctl --topology
cpu_model: AMD EPYC 4545P 16-Core Processor
online: 0-31
special_tags:
  - all-cores
unassigned_tags:
  - most-cache
  - least-cache
  - most-cache-per-core
  - least-cache-per-core
  - most-cores
  - least-cores
clusters:
  - cpus: 0-7,16-23
    tags:
      - highest-perf-cores
    physical_cores: 8
    logical_cpus: 16
    l3_kib: 32768
    l3_per_core_kib: 4096.0
    amd_pstate_max_freq_mhz: [5472]
    avg_highest_perf: 222.9
    min_highest_perf: 206
    max_highest_perf: 236
  - cpus: 8-15,24-31
    tags:
      - lowest-perf-cores
    physical_cores: 8
    logical_cpus: 16
    l3_kib: 32768
    l3_per_core_kib: 4096.0
    amd_pstate_max_freq_mhz: [5472]
    avg_highest_perf: 183.5
    min_highest_perf: 166
    max_highest_perf: 201
```

## Usage
```
usage:
  taskaffctl --tag TAG -- command...
  taskaffctl --tag TAG --pid PID
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
  taskaffctl -t most-cache -- mangohud %command%
  taskaffctl -t lowest-perf-cores -d -p "$MAINPID"
  taskaffctl -t all-cores -c syncthing
```

## How It Works

- CPUs are grouped by shared L3 and treated as clusters.
- Tags such as `lowest-perf-cores` and `highest-perf-cores` are assigned at cluster level.
- Performance ranking is based on ACPI CPPC `highest_perf`.
- `amd_pstate_max_freq_mhz` is shown in topology output when available, but it is informational only and does not participate in tag assignment.
- Cache-related tags such as `most-cache` and `most-cache-per-core` are exposed separately.
- `all-cores` is a special tag meaning the full online CPU mask, not a single cluster.

On the Zen5 samples I tested, `amd_pstate_max_freq` was the only frequency-related field that reflected the hardware maximum even when the active cpufreq policy was capping the current maximum. Fields such as `scaling_max_freq`, and in those samples also `cpuinfo_max_freq`, reflected the active cap instead. For that reason the tool shows `amd_pstate_max_freq_mhz` when it is exposed, but performance tags still rely only on ACPI CPPC.

## Tags

Cluster tags:

- `lowest-perf-cores`
- `highest-perf-cores`
- `most-cache`
- `least-cache`
- `most-cache-per-core`
- `least-cache-per-core`
- `most-cores`
- `least-cores`

Special tag:

- `all-cores`

Every tag except `all-cores` is unique. If a tag would apply to more than one cluster, it is assigned to neither of them. In that situation the tag is effectively unusable on that topology.

## Examples

Launch a command on a selected cluster:

```
taskaffctl -t lowest-perf-cores -- make -j8
taskaffctl -t most-cache -- mangohud %command%
```

Retag existing processes by PID:

```
taskaffctl -t lowest-perf-cores -p 1234
taskaffctl -t all-cores -p "1234 5678"
taskaffctl -t highest-perf-cores -d -p "$MAINPID"
```

Retag existing processes by name:

```
taskaffctl -t lowest-perf-cores -c syncthing
taskaffctl -t all-cores -c syncthing
taskaffctl -t most-cache -d -c steam
```

Inspect detected topology:

```
taskaffctl --topology
taskaffctl --topology --json
```

`--pid` applies to the selected process and all of its existing threads. `--descendants` extends the target set to current child processes recursively. `--comm` matches either `/proc/<pid>/comm` or the executable basename from `/proc/<pid>/exe`.

Example systemd integration for `syncthing@.service`:

```
systemctl edit syncthing@${USER}.service
```

Then add:

```
[Service]
ExecStart=
ExecStart=/usr/local/bin/taskaffctl -t lowest-perf-cores -- /usr/bin/syncthing serve --no-browser --no-restart
```

The empty `ExecStart=` line clears the original command first. Without it, systemd treats the new line as an additional `ExecStart` instead of an override.

This is just an example. The same pattern should work for most other services too. Copy the original `ExecStart` from `systemctl cat ...` and prepend `taskaffctl`.

This is the cleaner integration because the service starts already pinned, instead of being adjusted after startup.

## Output

- `--topology` prints YAML-like output by default and JSON with `--json`.
- `--pid` and `--comm` print per-process update reports in YAML-like form by default and JSON with `--json`.
- Command launch mode does not print YAML or JSON.
- `amd_pstate_max_freq_mhz` is optional and appears only on systems that expose `amd_pstate_max_freq`.

## Build

```
make
```

The binary is built into `build/bin/host/taskaffctl`. Go caches and temporary files stay under `build/`.

Install into `/usr/local/bin` as root or `~/.local/bin` as a normal user:

```
make install
```
