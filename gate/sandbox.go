package gate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

const (
	startTimeout   = 5 * time.Second
	requestTimeout = time.Second
)

// runResult is what one sandboxed consumer process did with one file.
type runResult struct {
	loaded    bool
	crashed   bool
	crashedOn int // 1-based request index; 0 = on load
	request   string
	panic     string
	errors    int
	latencies []time.Duration
	outputs   []string
	rssKB     int
	startKB   int // resident memory once serving, before the replay
}

// sandboxRun starts bin with data as its config file, waits for it to serve,
// replays requests one at a time and reports what happened. One process,
// one file, nothing shared with the fleet.
//
// The port is picked free and then released before the process binds it, so
// another process can take it in between. A run that fails to bind is
// retried on a new port, and /healthz must answer with this run's own id,
// so the gate never replays traffic against someone else's process.
//
// argv is the command line, with {port}, {config} and {id} filled in per
// run. When argv has no {id}, the consumer cannot echo it, so /healthz only
// needs to answer 200.
func sandboxRun(ctx context.Context, argv []string, health string, data []byte, requests []string, decision func([]byte) string) (*runResult, error) {
	for attempt := 1; ; attempt++ {
		res, err := sandboxOnce(ctx, argv, health, data, requests, decision)
		if err != nil || attempt == bindAttempts || !res.crashed || res.loaded || !bindFailed(res.panic) {
			return res, err
		}
	}
}

const bindAttempts = 5

var runSeq atomic.Int64

func bindFailed(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "address already in use")
}

// consumerArgv is the command line for PushBench's own consumers.
func consumerArgv(bin string) []string {
	return []string{bin, "--id", "{id}", "--port", "{port}", "--config", "{config}", "--poll-ms", "3600000"}
}

func sandboxOnce(ctx context.Context, argv []string, health string, data []byte, requests []string, decision func([]byte) string) (*runResult, error) {
	dir, err := os.MkdirTemp("", "pushgate-sandbox-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, data, 0o644); err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}

	id := fmt.Sprintf("sandbox-%d", runSeq.Add(1))
	args, checkID := make([]string, len(argv)), false
	for i, a := range argv {
		checkID = checkID || strings.Contains(a, "{id}")
		args[i] = strings.NewReplacer("{id}", id, "{port}", strconv.Itoa(port), "{config}", cfg).Replace(a)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "RUST_BACKTRACE=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	var stderrMu sync.Mutex
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	readDone := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pipe)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			stderrMu.Lock()
			stderr.WriteString(sc.Text() + "\n")
			stderrMu.Unlock()
		}
		close(readDone)
	}()
	exited := make(chan struct{})
	go func() { <-readDone; _ = cmd.Wait(); close(exited) }()
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-exited
	}()

	panicText := func() string {
		<-exited
		stderrMu.Lock()
		defer stderrMu.Unlock()
		lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		if s := registry.PanicSummary(lines); s != "" {
			return s
		}
		if len(lines) > 0 {
			return lines[len(lines)-1]
		}
		return "exited"
	}

	res := &runResult{}
	client := &http.Client{Timeout: requestTimeout, Transport: &http.Transport{MaxIdleConnsPerHost: 2}}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait until it serves, or dies trying.
	deadline := time.Now().Add(startTimeout)
	for {
		select {
		case <-exited:
			res.crashed, res.panic = true, panicText()
			return res, nil
		default:
		}
		if resp, err := client.Get(base + health); err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 && (!checkID || bytes.Contains(body, []byte(`"id":"`+id+`"`))) {
				break
			}
		}
		if time.Now().After(deadline) {
			res.panic = "did not start serving within " + startTimeout.String()
			res.crashed = true
			return res, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	res.loaded = true
	res.startKB = rss(cmd.Process.Pid)

	for i, path := range requests {
		t0 := time.Now()
		resp, err := client.Get(base + path)
		lat := time.Since(t0)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			res.latencies = append(res.latencies, lat)
			if resp.StatusCode != 200 {
				res.errors++
				res.outputs = append(res.outputs, "status "+strconv.Itoa(resp.StatusCode))
			} else {
				res.outputs = append(res.outputs, decision(body))
			}
			continue
		}
		// Did the request kill it?
		select {
		case <-exited:
		case <-time.After(200 * time.Millisecond):
		}
		select {
		case <-exited:
			res.crashed, res.crashedOn, res.request, res.panic = true, i+1, path, panicText()
			return res, nil
		default:
			res.errors++
			res.outputs = append(res.outputs, "error")
		}
	}
	res.rssKB = rss(cmd.Process.Pid)
	return res, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func rss(pid int) int {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				return n
			}
		}
	}
	return 0
}

// decision extracts the field that matters from a response, so outputs from
// the candidate and the last known good can be compared.
// decisionFor reads the field of a JSON answer that two runs are compared
// on: a field name, or one of PushBench's consumers.
func decisionFor(field string) func([]byte) string {
	key := field
	if k, ok := map[string]string{"bot-proxy": "bot", "go-policy": "allow"}[field]; ok {
		key = k
	}
	return func(body []byte) string {
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return "unparseable"
		}
		return fmt.Sprint(m[key])
	}
}

// LoadTraffic reads a loadgen -record sample ("consumer<TAB>/path" lines, or
// bare "/path" lines) and returns up to n request paths for consumer, in a
// fixed shuffled order.
func LoadTraffic(path, consumer string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		c, p, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			// A bare path belongs to whichever consumer reads this file.
			c, p, ok = consumer, c, true
		}
		if ok && c == consumer && strings.HasPrefix(p, "/") {
			out = append(out, p)
		}
	}
	rand.New(rand.NewSource(1)).Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	if len(out) > n {
		out = out[:n]
	}
	return out, sc.Err()
}

func percentile(d []time.Duration, p float64) float64 {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(p * float64(len(s)-1))
	return float64(s[idx].Microseconds()) / 1000
}

// sandbox runs the candidate (and the last known good, for comparison)
// through every consumer release live in the fleet, in parallel.
func sandbox(ctx context.Context, r *Report, cfg Config, consumer string, cand, lkg []byte) {
	if len(cfg.Releases) > 0 {
		sandboxReleases(ctx, r, cfg, consumer, cand, lkg)
		return
	}
	snap, err := cfg.Fleet.Snapshot()
	if err != nil {
		r.add(Finding{Layer: "sandbox", Check: "fleet", Severity: Hold, Detail: "cannot read fleet registry: " + err.Error()})
		return
	}
	counts := map[int]int{}
	for _, i := range snap {
		if i.Consumer == consumer {
			counts[i.Version]++
		}
	}
	if len(counts) == 0 {
		r.add(Finding{Layer: "sandbox", Check: "fleet", Severity: Hold, Detail: "no live instances of " + consumer})
		return
	}
	var versions []int
	for v := range counts {
		versions = append(versions, v)
	}
	sort.Ints(versions)
	var rels []Release
	for _, v := range versions {
		bin := filepath.Join(cfg.BinDir, consumer, fmt.Sprintf("v%d", v))
		if _, err := os.Stat(bin); err != nil {
			r.add(Finding{Layer: "sandbox", Check: "binary", Severity: Hold, Version: v, Detail: fmt.Sprintf("no binary for live release v%d at %s", v, bin)})
			continue
		}
		rels = append(rels, Release{Version: v, Instances: counts[v], Command: consumerArgv(bin)})
	}
	runReleases(ctx, r, cfg, consumer, cand, lkg, rels)
}

// Release is one consumer release the sandbox runs: its version, how many
// instances run it (to size the blast radius), and its command line with
// {port} and {config} (and optionally {id}) placeholders.
type Release struct {
	Version   int      `json:"version"`
	Instances int      `json:"instances"`
	Command   []string `json:"command"`
}

// sandboxReleases runs releases listed in the config (a pushgate.json
// spec) instead of those live in a fleet registry.
func sandboxReleases(ctx context.Context, r *Report, cfg Config, consumer string, cand, lkg []byte) {
	runReleases(ctx, r, cfg, consumer, cand, lkg, cfg.Releases)
}

func runReleases(ctx context.Context, r *Report, cfg Config, consumer string, cand, lkg []byte, rels []Release) {
	requests, err := LoadTraffic(cfg.TrafficFile, consumer, cfg.Replay)
	if err != nil || len(requests) == 0 {
		r.add(Finding{Layer: "sandbox", Check: "traffic", Severity: Warn, Detail: "no traffic sample for " + consumer + "; checked loading only"})
		requests = nil
	}
	// Requests built to reach every new or changed entry go after the
	// sample. They count for crashes, errors and latency, but not for the
	// share of changed answers: a changed rule is meant to answer differently.
	sampled := len(requests)
	if !cfg.NoTargeted {
		if t := targeted(cfg.kind(consumer), cand, lkg, requests); len(t) > 0 {
			requests = append(requests[:sampled:sampled], t...)
			r.add(Finding{Layer: "sandbox", Check: "targeted", Severity: Info,
				Detail: fmt.Sprintf("%d requests built to reach new or changed entries, replayed after %d sampled", len(t), sampled)})
		}
	}
	dec := decisionFor(cfg.answer(consumer))
	health := cfg.Health
	if health == "" {
		health = "/healthz"
	}

	results := make([]VersionResult, len(rels))
	var wg sync.WaitGroup
	for idx, rel := range rels {
		wg.Add(1)
		go func(idx int, rel Release) {
			defer wg.Done()
			v := rel.Version
			vr := VersionResult{Version: v, Instances: rel.Instances}
			var cr, lr *runResult
			var cerr, lerr error
			var inner sync.WaitGroup
			inner.Add(1)
			go func() { defer inner.Done(); cr, cerr = sandboxRun(ctx, rel.Command, health, cand, requests, dec) }()
			if lkg != nil {
				inner.Add(1)
				go func() { defer inner.Done(); lr, lerr = sandboxRun(ctx, rel.Command, health, lkg, requests, dec) }()
			}
			inner.Wait()
			if cerr != nil {
				r.addSafe(Finding{Layer: "sandbox", Check: "run", Severity: Hold, Version: v, Detail: fmt.Sprintf("v%d: sandbox failed to run: %v", v, cerr)})
				results[idx] = vr
				return
			}
			vr.Loaded, vr.Crashed, vr.CrashedOn, vr.Request, vr.Panic = cr.loaded, cr.crashed, cr.crashedOn, cr.request, cleanPanic(cr.panic)
			vr.Requests, vr.Errors, vr.RSSKB, vr.GrowthKB = len(cr.latencies), cr.errors, cr.rssKB, cr.rssKB-cr.startKB
			vr.P50MS, vr.P99MS = percentile(cr.latencies, 0.5), percentile(cr.latencies, 0.99)
			if lerr == nil && lr != nil && !lr.crashed {
				vr.LKGP99MS, vr.LKGRSSKB, vr.LKGGrowthKB = percentile(lr.latencies, 0.99), lr.rssKB, lr.rssKB-lr.startKB
				n, diff := min(len(cr.outputs), len(lr.outputs), sampled), 0
				for i := 0; i < n; i++ {
					if cr.outputs[i] != lr.outputs[i] {
						diff++
					}
				}
				if n > 0 {
					vr.OutputDiff = float64(diff) / float64(n)
				}
			}
			results[idx] = vr
			judge(r, cfg.Budgets, vr, rel.Instances, sampled, len(requests))
		}(idx, rel)
	}
	wg.Wait()
	r.Versions = results
}

func judge(r *Report, b Budgets, v VersionResult, instances, sampled, planned int) {
	who := fmt.Sprintf("v%d (%d instances)", v.Version, instances)
	switch {
	case v.Crashed && !v.Loaded:
		r.addSafe(Finding{Layer: "sandbox", Check: "load", Severity: Hold, Version: v.Version, Cause: v.Panic,
			Detail: fmt.Sprintf("%s crashed loading the file: %s", who, v.Panic)})
		return
	case v.Crashed && v.CrashedOn > sampled:
		r.addSafe(Finding{Layer: "sandbox", Check: "replay", Severity: Hold, Version: v.Version, Cause: v.Panic,
			Detail: fmt.Sprintf("%s crashed on a request built for a changed entry (%d of %d after the sample: %s): %s", who, v.CrashedOn-sampled, planned-sampled, shorten(v.Request, 60), v.Panic)})
		return
	case v.Crashed:
		r.addSafe(Finding{Layer: "sandbox", Check: "replay", Severity: Hold, Version: v.Version, Cause: v.Panic,
			Detail: fmt.Sprintf("%s crashed on replayed request %d of %d (%s): %s", who, v.CrashedOn, planned, shorten(v.Request, 60), v.Panic)})
		return
	}
	if v.Requests > 0 {
		if share := float64(v.Errors) / float64(v.Requests); share > b.ErrorHold {
			r.addSafe(Finding{Layer: "sandbox", Check: "errors", Severity: Hold, Version: v.Version, Detail: fmt.Sprintf("%s failed %.1f%% of replayed requests", who, 100*share)})
		}
	}
	if v.LKGP99MS > 0 && v.P99MS > b.LatencyHoldX*v.LKGP99MS && v.P99MS-v.LKGP99MS > b.LatencyHoldMS {
		r.addSafe(Finding{Layer: "sandbox", Check: "latency", Severity: Hold, Version: v.Version, Detail: fmt.Sprintf("%s p99 %.1f ms vs %.1f ms with last known good", who, v.P99MS, v.LKGP99MS)})
	}
	if v.OutputDiff > b.OutputWarn {
		r.addSafe(Finding{Layer: "sandbox", Check: "outputs", Severity: Warn, Version: v.Version, Detail: fmt.Sprintf("%s answers %.0f%% of replayed requests differently", who, 100*v.OutputDiff)})
	}
	// A leak per request shows in a short replay: 1,000 requests is minutes
	// of one instance's traffic. Hold when memory grows steadily with the
	// candidate and not with the last known good file.
	if v.Requests >= 100 {
		perReq := float64(v.GrowthKB) / float64(v.Requests)
		if perReq > b.GrowthHoldKB && v.GrowthKB > 2048 && v.GrowthKB > 4*v.LKGGrowthKB {
			r.addSafe(Finding{Layer: "sandbox", Check: "memory-growth", Severity: Hold, Version: v.Version, Cause: "memory grows with every replayed request",
				Detail: fmt.Sprintf("%s memory grew %.1f MB over %d replayed requests (%.1f KB per request) vs %.1f MB with last known good; it will keep growing under fleet traffic",
					who, float64(v.GrowthKB)/1024, v.Requests, perReq, float64(v.LKGGrowthKB)/1024)})
		}
	}
	if v.LKGRSSKB > 0 && float64(v.RSSKB) > b.RSSWarnX*float64(v.LKGRSSKB) {
		r.addSafe(Finding{Layer: "sandbox", Check: "memory", Severity: Warn, Version: v.Version, Detail: fmt.Sprintf("%s uses %d KB vs %d KB", who, v.RSSKB, v.LKGRSSKB)})
	}
}
