package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"rinha-2026/internal/config"
	"rinha-2026/internal/ivfsearch"
	"rinha-2026/internal/vectorizer"
)

func main() {
	cfg := config.Load()

	if err := ivfsearch.LoadIndex(cfg.IndexPath); err != nil {
		panic(err)
	}
	ivfsearch.SetParams(cfg.IvfNprobe, cfg.IvfFullNprobe, cfg.Candidates)

	body := []byte(`{"transaction":{"amount":1000,"installments":1,"requested_at":"2024-01-15T10:30:00Z"},"customer":{"avg_amount":500,"tx_count_24h":2,"known_merchants":[]},"merchant":{"id":"merch123","mcc":"5411","avg_amount":800},"terminal":{"is_online":true,"card_present":true,"km_from_home":5}}`)

	const warmupN = 500
	const benchN = 100000

	fmt.Printf("Warming up %d queries...\n", warmupN)
	q, _ := vectorizer.Build(body)
	for i := 0; i < warmupN; i++ {
		ivfsearch.Search(q)
	}

	fmt.Printf("Running %d queries...\n", benchN)
	latencies := make([]time.Duration, benchN)
	for i := 0; i < benchN; i++ {
		start := time.Now()
		ivfsearch.Search(q)
		latencies[i] = time.Since(start)
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var total time.Duration
	for _, l := range latencies {
		total += l
	}

	percentile := func(p float64) time.Duration {
		idx := int(math.Ceil(float64(benchN)*p/100.0)) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= benchN {
			idx = benchN - 1
		}
		return latencies[idx]
	}

	fmt.Printf("\n=== Latency (n=%d) ===\n", benchN)
	fmt.Printf("avg:  %8v\n", total/time.Duration(benchN))
	fmt.Printf("p50:  %8v\n", percentile(50))
	fmt.Printf("p90:  %8v\n", percentile(90))
	fmt.Printf("p99:  %8v\n", percentile(99))
	fmt.Printf("p999: %8v\n", percentile(99.9))
	fmt.Printf("max:  %8v\n", latencies[benchN-1])
}
