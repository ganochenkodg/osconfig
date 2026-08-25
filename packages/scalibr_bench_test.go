//  Copyright 2026 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

//go:build benchmark

package packages

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/osconfig/osinfo"
	"github.com/GoogleCloudPlatform/osconfig/util/utiltrace"
)

type cpuSample struct {
	totalTicks uint64
	time       time.Time
}

func readProcStatTicks() (uint64, error) {
	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("CPU metrics only supported on linux")
	}
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, fmt.Errorf("read /proc/self/stat: %w", err)
	}
	statStr := string(data)
	lastParen := strings.LastIndex(statStr, ")")
	if lastParen == -1 || lastParen+2 >= len(statStr) {
		return 0, fmt.Errorf("invalid stat format")
	}
	fields := strings.Fields(statStr[lastParen+2:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("invalid stat fields count: %d", len(fields))
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil {
		return 0, fmt.Errorf("error parsing utime: %w", err1)
	}
	if err2 != nil {
		return 0, fmt.Errorf("error parsing stime: %w", err2)
	}
	return utime + stime, nil
}

type traceMetricsResult struct {
	utiltrace.TraceMemoryResult
	CPUPeakPercent float64
	CPUMeanPercent float64
}

func traceMetrics(ctx context.Context, interval time.Duration, resultChan chan<- traceMetricsResult) {
	memChan := make(chan utiltrace.TraceMemoryResult, 1)
	ctxMemory, cancelMem := context.WithCancel(ctx)
	go utiltrace.TraceMemory(ctxMemory, interval, memChan)

	var lastSample cpuSample
	if ticks, err := readProcStatTicks(); err == nil {
		lastSample = cpuSample{totalTicks: ticks, time: time.Now()}
	}

	var peakCPU, runningAverageCPU float64
	sampleCount := 0
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentTicks, err := readProcStatTicks()
			now := time.Now()
			if err == nil && !lastSample.time.IsZero() {
				timeDelta := now.Sub(lastSample.time).Seconds()
				ticksDelta := float64(currentTicks - lastSample.totalTicks)
				if timeDelta > 0 {
					// 100.0 is standard SC_CLK_TCK ticks per second on Linux
					cpuPercent := (ticksDelta / 100.0) / timeDelta * 100.0
					sampleCount++
					runningAverageCPU += (cpuPercent - runningAverageCPU) / float64(sampleCount)
					if cpuPercent > peakCPU {
						peakCPU = cpuPercent
					}
				}
				lastSample = cpuSample{totalTicks: currentTicks, time: now}
			}
		case <-ctx.Done():
			cancelMem()
			memResult := <-memChan
			resultChan <- traceMetricsResult{
				TraceMemoryResult: memResult,
				CPUPeakPercent:    peakCPU,
				CPUMeanPercent:    runningAverageCPU,
			}
			return
		}
	}
}

type benchResult struct {
	duration  time.Duration
	allocMB   float64
	memPeakMB float64
	cpuPeak   float64
	cpuMean   float64
	pkgsCount int
}

// runSingleBenchmark runs a single iteration of installed packages extraction with metrics tracing.
func runSingleBenchmark(ctx context.Context, osinfoProvider osinfo.Provider, extractors []string) (benchResult, error) {
	runtime.GC()

	traceCtx, cancelTrace := context.WithCancel(ctx)
	resChan := make(chan traceMetricsResult, 1)
	go traceMetrics(traceCtx, 20*time.Millisecond, resChan)

	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	provider := &scalibrInstalledPackagesProvider{
		extractors:     extractors,
		osinfoProvider: osinfoProvider,
	}
	pkgs, err := provider.GetInstalledPackages(ctx)
	elapsed := time.Since(start)

	runtime.ReadMemStats(&memAfter)
	cancelTrace()

	metrics := <-resChan
	if err != nil {
		return benchResult{}, fmt.Errorf("GetInstalledPackages error: %w", err)
	}

	pkgsCount := len(pkgs.Deb) + len(pkgs.Rpm) + len(pkgs.COS)
	allocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024

	return benchResult{
		duration:  elapsed,
		allocMB:   allocMB,
		memPeakMB: metrics.MemPeakMB,
		cpuPeak:   metrics.CPUPeakPercent,
		cpuMean:   metrics.CPUMeanPercent,
		pkgsCount: pkgsCount,
	}, nil
}

// logBenchResult calculates averages over runs and outputs the formatted benchmark table row.
func logBenchResult(t *testing.T, name string, res benchResult, runs int) {
	if runs <= 0 {
		return
	}
	r := float64(runs)
	avgDuration := res.duration / time.Duration(runs)
	avgAllocMB := res.allocMB / r
	avgPeakRAM := res.memPeakMB / r
	avgPeakCPU := res.cpuPeak / r
	avgMeanCPU := res.cpuMean / r

	t.Logf("| `%s` | %v | %.2f MB | %.2f MB | %.1f%% | %.1f%% | %d |",
		name,
		avgDuration,
		avgAllocMB,
		avgPeakRAM,
		avgPeakCPU,
		avgMeanCPU,
		res.pkgsCount,
	)
}

func TestScalibrBenchmark(t *testing.T) {
	benchmarks := []struct {
		name       string
		extractors []string
	}{
		{name: "All os extractors", extractors: []string{"os/dpkg", "os/rpm", "os/cos"}},
		{name: "os/dpkg", extractors: []string{"os/dpkg"}},
		{name: "os/rpm", extractors: []string{"os/rpm"}},
		{name: "os/cos", extractors: []string{"os/cos"}},
	}

	ctx := context.Background()
	osinfoProvider := osinfo.NewProvider()

	t.Log("\n=========================================================================")
	t.Log("### SCALIBR Extractors Benchmark Results")
	t.Log("=========================================================================")
	t.Log("| Scenario | Avg Scan Time | Avg Heap Alloc | Peak RAM RSS | Peak CPU % | Mean CPU % | Pkgs Found |")
	t.Log("| --- | --- | --- | --- | --- | --- | --- |")

	runs := 3
	for _, bm := range benchmarks {
		var total benchResult
		for i := 0; i < runs; i++ {
			res, err := runSingleBenchmark(ctx, osinfoProvider, bm.extractors)
			if err != nil {
				t.Fatalf("Benchmark scenario %s failed: %v", bm.name, err)
			}
			total.duration += res.duration
			total.allocMB += res.allocMB
			total.memPeakMB += res.memPeakMB
			total.cpuPeak += res.cpuPeak
			total.cpuMean += res.cpuMean
			total.pkgsCount = res.pkgsCount
		}

		logBenchResult(t, bm.name, total, runs)
	}
}
