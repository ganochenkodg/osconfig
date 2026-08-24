# SCALIBR Extractor Performance Benchmark Suite

This benchmark suite measures the performance impact of OSV-SCALIBR extractors within `google-osconfig-agent`.

---

## 🚀 Quick Start Guide

### Step 1: Install Language Environments & Sample Packages (Debian 12)
Run the environment preparation script with `sudo` to install Go, Python, Node.js, Ruby, PHP, Rust, Java, Swift, Dart, and generate sample lockfiles:

```bash
sudo ./install_packages.sh
```

### Step 2: Run Automated Benchmark Test
Run the Go benchmark test suite (isolated from standard CI pipelines using build tag `-tags=benchmark`):

```bash
go test -v -tags=benchmark -run=TestScalibrBenchmark ./packages/
```

---

## 📊 Collected Metrics

The benchmark suite measures both Go runtime metrics and OS-level system resource consumption:

| Metric Name | Source | Description |
| --- | --- | --- |
| **Avg Scan Time** | Go `time.Since()` | Average execution duration of SCALIBR scan |
| **Avg Heap Alloc** | `runtime.ReadMemStats` | Average RAM memory allocated in Go heap per scan run |
| **Pkgs Found** | `packages.Packages` | Total number of installed software packages detected |
| **Peak RAM RSS** | OS `/usr/bin/time -v` | Maximum Resident Set Size (peak physical memory used by process) |
| **CPU User Time** | OS `/usr/bin/time -v` | Total CPU time spent in user-space execution |
| **CPU Usage** | OS `/usr/bin/time -v` | Percentage of CPU utilization during execution |

---

## 📂 Benchmark Suite Files

- **`cmd/bench_scalibr/main.go`**: Standalone Go benchmark tool that executes `RunScalibrScanForBenchmark`.
- **`run_benchmarks.sh`**: Automated runner script executing baseline, single-extractor, and combined test cases.
- **`install_packages.sh`**: Debian 12 environment installer generating real package files for scanning.
- **`BENCHMARK.md`**: Documentation and usage instructions.
