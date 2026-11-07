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
}

// sandboxRun starts bin with data as its config file, waits for it to serve,
// replays requests one at a time and reports what happened. One process,
// one file, nothing shared with the fleet.
//
// The port is picked free and then released before the process binds it, so
// another process can take it in between. A run that fails to bind is
// retried on a new port, and /healthz must answer with this run's own id,
// so the gate never replays traffic against someone else's process.
func sandboxRun(ctx context.Context, bin string, data []byte, requests []string, decision func([]byte) string) (*runResult, error) {
	for attempt := 1; ; attempt++ {
		res, err := sandboxOnce(ctx, bin, data, requests, decision)
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

func sandboxOnce(ctx context.Context, bin string, data []byte, requests []string, decision func([]byte) string) (*runResult, error) {
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
	cmd := exec.CommandContext(ctx, bin, "--id", id, "--port", strconv.Itoa(port), "--config", cfg, "--poll-ms", "3600000")
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
		if resp, err := client.Get(base + "/healthz"); err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 && bytes.Contains(body, []byte(`"id":"`+id+`"`)) {
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
func decisionFor(consumer string) func([]byte) string {
	key := map[string]string{"bot-proxy": "bot", "go-policy": "allow"}[consumer]
	return func(body []byte) string {
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return "unparseable"
		}
		return fmt.Sprint(m[key])
	}
}

// LoadTraffic reads a loadgen -record sample and returns up to n request
// paths for consumer, in a fixed shuffled order.
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
		if t := targeted(consumer, cand, lkg, requests); len(t) > 0 {
			requests = append(requests[:sampled:sampled], t...)
			r.add(Finding{Layer: "sandbox", Check: "targeted", Severity: Info,
				Detail: fmt.Sprintf("%d requests built to reach new or changed entries, replayed after %d sampled", len(t), sampled)})
		}
	}
	dec := decisionFor(consumer)

	results := make([]VersionResult, len(versions))
	var wg sync.WaitGroup
	for idx, v := range versions {
		wg.Add(1)
		go func(idx, v int) {
			defer wg.Done()
			vr := VersionResult{Version: v, Instances: counts[v]}
			bin := filepath.Join(cfg.BinDir, consumer, fmt.Sprintf("v%d", v))
			if _, err := os.Stat(bin); err != nil {
				r.addSafe(Finding{Layer: "sandbox", Check: "binary", Severity: Hold, Version: v, Detail: fmt.Sprintf("no binary for live release v%d at %s", v, bin)})
				results[idx] = vr
				return
			}
			var cr, lr *runResult
			var cerr, lerr error
			var inner sync.WaitGroup
			inner.Add(1)
			go func() { defer inner.Done(); cr, cerr = sandboxRun(ctx, bin, cand, requests, dec) }()
			if lkg != nil {
				inner.Add(1)
				go func() { defer inner.Done(); lr, lerr = sandboxRun(ctx, bin, lkg, requests, dec) }()
			}
			inner.Wait()
			if cerr != nil {
				r.addSafe(Finding{Layer: "sandbox", Check: "run", Severity: Hold, Version: v, Detail: fmt.Sprintf("v%d: sandbox failed to run: %v", v, cerr)})
				results[idx] = vr
				return
			}
			vr.Loaded, vr.Crashed, vr.CrashedOn, vr.Request, vr.Panic = cr.loaded, cr.crashed, cr.crashedOn, cr.request, cleanPanic(cr.panic)
			vr.Requests, vr.Errors, vr.RSSKB = len(cr.latencies), cr.errors, cr.rssKB
			vr.P50MS, vr.P99MS = percentile(cr.latencies, 0.5), percentile(cr.latencies, 0.99)
			if lerr == nil && lr != nil && !lr.crashed {
				vr.LKGP99MS, vr.LKGRSSKB = percentile(lr.latencies, 0.99), lr.rssKB
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
			judge(r, cfg.Budgets, vr, counts[v], sampled, len(requests))
		}(idx, v)
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
	if v.LKGRSSKB > 0 && float64(v.RSSKB) > b.RSSWarnX*float64(v.LKGRSSKB) {
		r.addSafe(Finding{Layer: "sandbox", Check: "memory", Severity: Warn, Version: v.Version, Detail: fmt.Sprintf("%s uses %d KB vs %d KB", who, v.RSSKB, v.LKGRSSKB)})
	}
}
