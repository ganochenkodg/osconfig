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
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, fmt.Errorf("invalid stat fields count: %d", len(fields))
	}
	utime, err1 := strconv.ParseUint(fields[13], 10, 64)
	stime, err2 := strconv.ParseUint(fields[14], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("error parsing cpu ticks")
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

func TestScalibrBenchmark(t *testing.T) {
	benchmarks := []struct {
		label      string
		extractors []string
	}{
		{label: "os/dpkg", extractors: []string{"os/dpkg"}},
		{label: "os/rpm", extractors: []string{"os/rpm"}},
		{label: "os/cos", extractors: []string{"os/cos"}},
		{label: "All os extractors", extractors: []string{"os/dpkg", "os/rpm", "os/cos"}},
		},
	}

	ctx := context.Background()
	osinfoProvider := osinfo.NewProvider()

	t.Log("\n=========================================================================")
	t.Log("### SCALIBR Extractors Benchmark Results")
	t.Log("=========================================================================")
	t.Log("| Scenario | Avg Scan Time | Avg Heap Alloc | Peak RAM RSS | Peak CPU % | Mean CPU % | Pkgs Found |")
	t.Log("| --- | --- | --- | --- | --- | --- | --- |")

	for _, bm := range benchmarks {
		runs := 3
		var totalDuration time.Duration
		var totalAllocMB float64
		var lastPeakRAM, lastPeakCPU, lastMeanCPU float64
		var totalPkgs int

		for i := 0; i < runs; i++ {
			runtime.GC()

			traceCtx, cancelTrace := context.WithCancel(ctx)
			resChan := make(chan traceMetricsResult, 1)
			go traceMetrics(traceCtx, 20*time.Millisecond, resChan)

			var memBefore, memAfter runtime.MemStats
			runtime.ReadMemStats(&memBefore)

			start := time.Now()
			provider := &scalibrInstalledPackagesProvider{
				extractors:     bm.extractors,
				osinfoProvider: osinfoProvider,
			}
			pkgs, err := provider.GetInstalledPackages(ctx)
			elapsed := time.Since(start)

			runtime.ReadMemStats(&memAfter)
			cancelTrace()

			metrics := <-resChan
			if err != nil {
				t.Fatalf("Benchmark scenario %s failed: %v", bm.label, err)
			}

			totalDuration += elapsed
			totalAllocMB += float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024
			lastPeakRAM = metrics.MemPeakMB
			lastPeakCPU = metrics.CPUPeakPercent
			lastMeanCPU = metrics.CPUMeanPercent
			totalPkgs = len(pkgs.Deb) + len(pkgs.Rpm) + len(pkgs.COS) +
				len(pkgs.Pip) + len(pkgs.Gem) + len(pkgs.Npm) +
				len(pkgs.Maven) + len(pkgs.Go) + len(pkgs.Cargo) +
				len(pkgs.Composer) + len(pkgs.Swift) + len(pkgs.Pub)
		}

		avgDuration := totalDuration / time.Duration(runs)
		avgAllocMB := totalAllocMB / float64(runs)

		t.Logf("| `%s` | %v | %.2f MB | %.2f MB | %.1f%% | %.1f%% | %d |",
			bm.label,
			avgDuration,
			avgAllocMB,
			lastPeakRAM,
			lastPeakCPU,
			lastMeanCPU,
			totalPkgs,
		)
	}
}
