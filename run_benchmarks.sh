#!/usr/bin/env bash
set -e

# Benchmark runner script for SCALIBR extractors.
# Measures execution time, heap allocations, OS peak RAM RSS, and CPU utilization.

if [ ! -f ./bench_scalibr ]; then
    echo "Building bench_scalibr binary..."
    go build -o bench_scalibr ./cmd/bench_scalibr
else
    echo "Using existing bench_scalibr binary..."
fi

TIME_CMD="/usr/bin/time -v"
TIME_LOG="/tmp/scalibr_time.log"

run_benchmark_case() {
    local label="$1"
    local extractors="$2"
    local runs="${3:-3}"

    # Run bench_scalibr under /usr/bin/time -v
    $TIME_CMD ./bench_scalibr -extractors="$extractors" -runs="$runs" 2> "$TIME_LOG" > /tmp/bench_out.txt

    local bench_out
    bench_out=$(cat /tmp/bench_out.txt)

    # Parse /usr/bin/time metrics
    local max_rss_kb user_cpu cpu_percent
    max_rss_kb=$(grep "Maximum resident set size" "$TIME_LOG" | awk '{print $NF}')
    user_cpu=$(grep "User time" "$TIME_LOG" | awk '{print $NF}')
    cpu_percent=$(grep "Percent of CPU" "$TIME_LOG" | awk '{print $NF}')

    local max_rss_mb
    if [ -n "$max_rss_kb" ]; then
        max_rss_mb=$(awk "BEGIN {printf \"%.2f\", $max_rss_kb/1024}")
    else
        max_rss_mb="N/A"
    fi

    # Output formatted Markdown row
    echo "| \`$label\` | $bench_out | ${max_rss_mb} MB | ${user_cpu}s | ${cpu_percent} |"
}

echo ""
echo "========================================================================="
echo "### SCALIBR Extractors Benchmark Results"
echo "========================================================================="
echo ""
echo "| Configuration / Extractor | Extractors List | Avg Scan Time | Avg Heap Alloc | Pkgs Found | Peak RAM RSS | CPU User Time | CPU Usage |"
echo "| --- | --- | --- | --- | --- | --- | --- | --- |"

# 1. Base OS Baseline
run_benchmark_case "Base OS Only" "os/dpkg,os/rpm,os/cos" 3

# 2. Individual Extractor Tests
EXTRACTORS=(
  "python/wheelegg"
  "python/uvlock"
  "ruby/gemspec"
  "javascript/packagejson"
  "java/archive"
  "java/pomxml"
  "java/gradlelockfile"
  "go/binary"
  "go/gomod"
  "rust/cargolock"
  "rust/cargoauditable"
  "php/composerlock"
  "swift/packageresolved"
  "dart/pubspec"
)

for ext in "${EXTRACTORS[@]}"; do
  run_benchmark_case "+ $ext" "os/dpkg,os/rpm,os/cos,$ext" 3
done

# 3. All Selected Language Extractors Combined
ALL_EXTS=$(IFS=,; echo "${EXTRACTORS[*]}")
run_benchmark_case "All Selected Combined" "os/dpkg,os/rpm,os/cos,$ALL_EXTS" 3

rm -f /tmp/scalibr_time.log /tmp/bench_out.txt
echo ""
echo "Benchmark run complete!"
