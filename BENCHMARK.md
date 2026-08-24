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
sudo -E $(which go) test -v -tags=benchmark -run=TestScalibrBenchmark ./packages/ 2>&1 | grep ' | '
```

---

## 📊 Collected Metrics

The benchmark suite measures both Go runtime metrics and OS-level system resource consumption:

| Metric Name | Source | Description |
| --- | --- | --- |
| **Avg Scan Time** | Go `time.Since()` | Average execution duration of SCALIBR scan |
| **Avg Heap Alloc** | `runtime.ReadMemStats` | Average RAM memory allocated in Go heap per scan run |
| **Peak RAM RSS** | `utiltrace.TraceMemory` | Maximum Resident Set Size (peak physical memory used by process) |
| **Peak CPU %** | `/proc/self/stat` | Peak CPU utilization percentage during execution |
| **Mean CPU %** | `/proc/self/stat` | Average CPU utilization percentage during execution |
| **Pkgs Found** | `packages.Packages` | Total number of installed software packages detected |

---

## 📂 Benchmark Suite Files

- **`packages/scalibr_bench_test.go`**: Go benchmark test suite (isolated from CI via `//go:build benchmark`).
- **`install_packages.sh`**: Debian 12 environment installer generating real package files for scanning.
- **`BENCHMARK.md`**: Documentation and usage instructions.
