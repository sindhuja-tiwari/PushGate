// Command loadgen sends steady traffic to every instance in the fleet and
// reports, each interval, what share of requests succeeded, split by consumer,
// release and region. This is the fleet's health signal: B2's alarms and the
// PushBench blast-radius numbers are computed from it.
//
//	loadgen -rate 2000 -duration 60s -jsonl state/load.jsonl
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sindhuja-tiwari/pushgate/fleet/internal/registry"
)

// Request vocabulary. The producer draws feature templates from the same
// lists, so some requests match some rules.
var (
	userAgents = []string{"chrome", "safari", "firefox", "curl", "python-requests", "go-http-client", "headless-chrome", "bot"}
	methods    = []string{"GET", "POST", "HEAD"}
	countries  = []string{"US", "IN", "DE", "BR", "JP", "NG"}
	asnClasses = []string{"residential", "mobile", "hosting", "university"}
	apis       = []string{"storage.read", "storage.write", "compute.list", "iam.get"}
)

type counter struct{ ok, fail atomic.Int64 }

type stats struct {
	mu   sync.Mutex
	keys map[string]*counter
}

func (s *stats) get(k string) *counter {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.keys[k]
	if c == nil {
		c = &counter{}
		s.keys[k] = c
	}
	return c
}

// swap returns the counts for the interval and starts a new one.
func (s *stats) swap() map[string][2]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][2]int64{}
	for k, c := range s.keys {
		out[k] = [2]int64{c.ok.Swap(0), c.fail.Swap(0)}
	}
	return out
}

type Report struct {
	Time    time.Time                   `json:"time"`
	OK      int64                       `json:"ok"`
	Fail    int64                       `json:"fail"`
	Dropped int64                       `json:"dropped"`
	Groups  map[string]map[string]int64 `json:"groups"`
}

func buildURL(rng *rand.Rand, i registry.Instance) string {
	if i.Consumer == "go-policy" {
		return fmt.Sprintf("http://127.0.0.1:%d/check?project=p-%03d&api=%s", i.Port, rng.Intn(50), apis[rng.Intn(len(apis))])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "http://127.0.0.1:%d/score?f0=%s&f1=%s&f2=%s&f3=%s", i.Port,
		userAgents[rng.Intn(len(userAgents))], methods[rng.Intn(len(methods))],
		countries[rng.Intn(len(countries))], asnClasses[rng.Intn(len(asnClasses))])
	// Slots 4..20 carry low-cardinality values so that 20- and 21-field
	// templates can fully match some requests.
	for s := 4; s <= 20; s++ {
		fmt.Fprintf(&b, "&f%d=v%d", s, rng.Intn(2))
	}
	return b.String()
}

func main() {
	reg := flag.String("registry", "http://127.0.0.1:19000/registry", "supervisor URL or registry.json path")
	rate := flag.Int("rate", 1000, "requests per second across the fleet")
	duration := flag.Duration("duration", 0, "stop after this long (0 = until Ctrl-C)")
	interval := flag.Duration("interval", time.Second, "report interval")
	jsonl := flag.String("jsonl", "", "also append one JSON report per interval to this file")
	workers := flag.Int("workers", 256, "concurrent request workers")
	quiet := flag.Bool("quiet", false, "only print the final summary")
	flag.Parse()

	snap, err := registry.Fetch(*reg)
	if err != nil {
		log.Fatalf("loadgen: %v", err)
	}
	insts := snap.Instances
	if len(insts) == 0 {
		log.Fatal("loadgen: registry is empty")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	var out *os.File
	if *jsonl != "" {
		out, err = os.OpenFile(*jsonl, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
	}

	client := &http.Client{
		Timeout:   500 * time.Millisecond,
		Transport: &http.Transport{MaxIdleConns: 2048, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second},
	}
	st := &stats{keys: map[string]*counter{}}
	var dropped atomic.Int64
	jobs := make(chan registry.Instance, *workers*4)

	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for i := range jobs {
				ok := false
				resp, err := client.Get(buildURL(rng, i))
				if err == nil {
					ok = resp.StatusCode == 200
					resp.Body.Close()
				}
				for _, k := range []string{
					"consumer=" + i.Consumer,
					"version=" + i.Consumer + "/v" + strconv.Itoa(i.Version),
					"region=" + i.Region,
				} {
					c := st.get(k)
					if ok {
						c.ok.Add(1)
					} else {
						c.fail.Add(1)
					}
				}
			}
		}(int64(w) + 1)
	}

	// Pace requests in 10 ms batches; pick instances uniformly, like a
	// load balancer.
	go func() {
		defer close(jobs)
		rng := rand.New(rand.NewSource(42))
		const step = 10 * time.Millisecond
		tick := time.NewTicker(step)
		defer tick.Stop()
		perStep := float64(*rate) * step.Seconds()
		owed := 0.0
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				owed += perStep
				for ; owed >= 1; owed-- {
					select {
					case jobs <- insts[rng.Intn(len(insts))]:
					default:
						// Workers are all busy. Count it, so slow
						// failures (timeouts) are not hidden by a
						// lower request count.
						dropped.Add(1)
					}
				}
			}
		}
	}()

	var totOK, totFail int64
	minRate := 1.0
	report := func() {
		g := st.swap()
		r := Report{Time: time.Now().UTC(), Dropped: dropped.Swap(0), Groups: map[string]map[string]int64{}}
		for k, v := range g {
			if strings.HasPrefix(k, "consumer=") {
				r.OK += v[0]
				r.Fail += v[1]
			}
			r.Groups[k] = map[string]int64{"ok": v[0], "fail": v[1]}
		}
		if r.OK+r.Fail == 0 {
			return
		}
		totOK += r.OK
		totFail += r.Fail
		rate := float64(r.OK) / float64(r.OK+r.Fail)
		if rate < minRate {
			minRate = rate
		}
		if out != nil {
			b, _ := json.Marshal(r)
			out.Write(append(b, '\n'))
		}
		if !*quiet {
			var parts []string
			for _, k := range registry.SortedKeys(g) {
				if !strings.HasPrefix(k, "version=") {
					continue
				}
				v := g[k]
				parts = append(parts, fmt.Sprintf("%s %.0f%%", strings.TrimPrefix(k, "version="), 100*float64(v[0])/float64(max(v[0]+v[1], 1))))
			}
			drop := ""
			if r.Dropped > 0 {
				drop = fmt.Sprintf("  dropped %d", r.Dropped)
			}
			fmt.Printf("%s  %6d req  ok %5.1f%%%s  | %s\n", r.Time.Format("15:04:05"), r.OK+r.Fail, 100*rate, drop, strings.Join(parts, "  "))
		}
	}

	t := time.NewTicker(*interval)
	defer t.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-t.C:
			report()
		}
	}
	wg.Wait()
	report()
	fmt.Printf("summary: %d requests, %.2f%% ok, worst interval %.1f%% ok\n", totOK+totFail, 100*float64(totOK)/float64(max(totOK+totFail, 1)), 100*minRate)
}
