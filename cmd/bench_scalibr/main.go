package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/osconfig/osinfo"
	"github.com/GoogleCloudPlatform/osconfig/packages"
)

func main() {
	extractorsFlag := flag.String("extractors", "os/dpkg,os/rpm,os/cos", "Comma-separated list of SCALIBR extractors")
	runsFlag := flag.Int("runs", 3, "Number of benchmark iterations")
	flag.Parse()

	var extractors []string
	for _, item := range strings.Split(*extractorsFlag, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			extractors = append(extractors, item)
		}
	}

	ctx := context.Background()
	osinfoProvider := osinfo.NewProvider()

	// Warmup run
	_, _ = packages.RunScalibrScanForBenchmark(ctx, extractors, osinfoProvider)

	var totalDuration time.Duration
	var totalAllocMB float64
	var lastPkgs packages.Packages

	for i := 0; i < *runsFlag; i++ {
		runtime.GC()
		var memBefore, memAfter runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		start := time.Now()
		pkgs, err := packages.RunScalibrScanForBenchmark(ctx, extractors, osinfoProvider)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&memAfter)

		if err != nil {
			fmt.Fprintf(os.Stderr, "Benchmark run failed: %v\n", err)
			os.Exit(1)
		}

		allocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024
		totalDuration += elapsed
		totalAllocMB += allocMB
		lastPkgs = pkgs
	}

	avgDuration := totalDuration / time.Duration(*runsFlag)
	avgAllocMB := totalAllocMB / float64(*runsFlag)
	totalCount := countTotalPackages(lastPkgs)

	// Format output line for table reporting
	fmt.Printf("%s | %v | %.2f MB | %d\n",
		strings.Join(extractors, ","),
		avgDuration,
		avgAllocMB,
		totalCount,
	)
}

func countTotalPackages(pkgs packages.Packages) int {
	return len(pkgs.Deb) + len(pkgs.Rpm) + len(pkgs.COS) +
		len(pkgs.Pip) + len(pkgs.Gem) + len(pkgs.Npm) +
		len(pkgs.Maven) + len(pkgs.Go) + len(pkgs.Cargo) +
		len(pkgs.Composer) + len(pkgs.Swift) + len(pkgs.Pub)
}
