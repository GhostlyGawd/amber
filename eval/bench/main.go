// Command bench measures recall latency against the §7 target:
// p50 < 50ms at 50k memories on a laptop. Results go into
// docs/benchmarks.md — real numbers, whatever they are.
//
//	go run ./eval/bench -n 50000 -queries 200
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

var subjects = []string{"the api gateway", "the billing service", "the deploy pipeline", "the auth layer",
	"the mobile app", "the data warehouse", "the search index", "the admin console", "the cron runner",
	"the webhook relay", "the feature flags", "the design system", "the metrics stack", "the edge cache"}
var verbs = []string{"uses", "runs on", "deploys to", "is written in", "stores data in", "targets",
	"requires", "defaults to", "is owned by", "reports errors to"}
var objects = []string{"postgres 16", "redis 7", "kubernetes", "fly.io", "aws us-east-1", "grpc", "graphql",
	"typescript strict mode", "pnpm workspaces", "github actions", "terraform", "vault", "launchdarkly",
	"sqs with a dlq", "loki", "grafana", "sentry", "stripe", "postmark", "airflow at 02:00 utc"}

func main() {
	n := flag.Int("n", 50000, "memories to plant")
	queries := flag.Int("queries", 200, "queries to time")
	dims := flag.Int("dims", 256, "hash embedder dims")
	dir := flag.String("dir", "", "store dir (default temp; kept if given)")
	flag.Parse()

	tmp := *dir
	if tmp == "" {
		var err error
		tmp, err = os.MkdirTemp("", "amber-bench-*")
		must(err)
		defer os.RemoveAll(tmp)
	}
	dbPath := filepath.Join(tmp, "amber.db")
	fresh := true
	if _, err := os.Stat(dbPath); err == nil {
		fresh = false
	}
	s, err := store.Create(tmp)
	must(err)
	defer s.Close()
	e := embed.NewHash(*dims)
	w := &writer.Writer{Store: s, Config: config.Default(), Embedder: e}

	rng := rand.New(rand.NewSource(42)) // fixed seed: reproducible corpus

	if fresh {
		fmt.Printf("planting %d memories (hash-%d vectors)…\n", *n, *dims)
		t0 := time.Now()
		// Bulk insert without the belief pass (Insert, not Write): the
		// bench measures RECALL, and 50k adjudications would swamp setup.
		for i := 0; i < *n; i++ {
			content := fmt.Sprintf("%s %s %s (variant %d)",
				subjects[rng.Intn(len(subjects))], verbs[rng.Intn(len(verbs))],
				objects[rng.Intn(len(objects))], i)
			v, _ := e.Embed(content)
			m := &store.Memory{
				Content: content, Type: "fact", Trust: trust.T2, Confidence: 0.8,
				Importance: 1 + rng.Intn(5), Embedding: v,
			}
			must(s.Insert(m, nil, nil))
		}
		fmt.Printf("planted in %s (%.0f/s)\n", time.Since(t0).Round(time.Millisecond),
			float64(*n)/time.Since(t0).Seconds())
	}
	_ = w

	var times []float64
	for i := 0; i < *queries; i++ {
		q := fmt.Sprintf("%s %s", subjects[rng.Intn(len(subjects))], objects[rng.Intn(len(objects))])
		t0 := time.Now()
		_, err := search.Recall(s, e, search.Request{Query: q, Limit: 8})
		must(err)
		times = append(times, float64(time.Since(t0).Microseconds())/1000)
	}
	sort.Float64s(times)
	fmt.Printf("\nrecall over %d memories, %d queries:\n", *n, *queries)
	fmt.Printf("  p50 %.1fms · p90 %.1fms · p95 %.1fms · p99 %.1fms · max %.1fms\n",
		pct(times, .50), pct(times, .90), pct(times, .95), pct(times, .99), times[len(times)-1])
	target := 50.0
	verdict := "PASS"
	if pct(times, .50) >= target {
		verdict = "MISS"
	}
	fmt.Printf("  §7 target p50 < %.0fms: %s\n", target, verdict)
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}
