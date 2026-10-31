// Command fleet runs the simulated PushBench fleet: hundreds of consumer
// processes across regions, at mixed release versions, each watching its own
// copy of a config file.
//
// It restarts processes that exit (with randomized exponential backoff),
// health-checks each one, records the last panic message, and publishes the
// live registry at /registry, state/registry.json and, for Prometheus,
// state/prom_targets.json.
//
// Config files are delivered by writing to each instance's config_path
// (see pgctl push). The supervisor itself never touches them after seeding.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

const (
	backoffBase  = 250 * time.Millisecond
	backoffMax   = 8 * time.Second
	stableAfter  = 30 * time.Second // reset backoff after this long up
	healthEvery  = 2 * time.Second
	publishEvery = 1 * time.Second
	tailLines    = 30
)

type proc struct {
	mu   sync.Mutex
	inst registry.Instance
	bin  string
	env  []string
	tail []string // last stderr lines
	log  *os.File
}

func (p *proc) snapshot() registry.Instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inst
}

func (p *proc) setState(s string) {
	p.mu.Lock()
	if p.inst.State != s {
		p.inst.State = s
		p.inst.StateSince = time.Now()
	}
	p.mu.Unlock()
}

// record keeps a bounded tail of stderr and remembers panic messages.
func (p *proc) record(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tail = append(p.tail, line)
	if len(p.tail) > tailLines {
		p.tail = p.tail[len(p.tail)-tailLines:]
	}
	if p.log != nil {
		fmt.Fprintf(p.log, "%s %s\n", time.Now().Format(time.RFC3339Nano), line)
	}
}

// panicSummary picks the useful lines out of a crash. Rust prints
// "thread 'x' panicked at file:line:col:" then the message. Go prints
// "panic: msg" and later "\tfile.go:line".
func panicSummary(tail []string) string {
	for i, l := range tail {
		if strings.Contains(l, "panicked at") {
			msg := strings.TrimSpace(l)
			if i+1 < len(tail) {
				msg += " " + strings.TrimSpace(tail[i+1])
			}
			return msg
		}
		if strings.HasPrefix(l, "panic: ") {
			msg := strings.TrimSpace(l)
			// The first stack frame outside the Go runtime is the culprit.
			for _, m := range tail[i+1:] {
				if !strings.HasPrefix(m, "\t") || !strings.Contains(m, ".go:") || strings.Contains(m, "/runtime/") {
					continue
				}
				msg += " at " + filepath.Base(strings.Fields(m)[0])
				break
			}
			return msg
		}
	}
	return ""
}

func (p *proc) run(ctx context.Context, rng *rand.Rand, rngMu *sync.Mutex) {
	attempt := 0
	for ctx.Err() == nil {
		inst := p.snapshot()
		cmd := exec.Command(p.bin,
			"--id", inst.ID,
			"--port", strconv.Itoa(inst.Port),
			"--config", inst.ConfigPath,
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
		cmd.Env = p.env
		stderr, _ := cmd.StderrPipe()
		cmd.Stdout = io.Discard
		p.mu.Lock()
		p.tail = nil
		p.mu.Unlock()
		p.setState(registry.StateStarting)
		started := time.Now()
		if err := cmd.Start(); err != nil {
			p.record("supervisor: start failed: " + err.Error())
			p.mu.Lock()
			p.inst.LastExit = "start failed: " + err.Error()
			p.mu.Unlock()
		} else {
			p.mu.Lock()
			p.inst.PID = cmd.Process.Pid
			p.mu.Unlock()
			done := make(chan struct{})
			go func() {
				sc := bufio.NewScanner(stderr)
				sc.Buffer(make([]byte, 64*1024), 1024*1024)
				for sc.Scan() {
					p.record(sc.Text())
				}
				close(done)
			}()
			waitErr := make(chan error, 1)
			go func() { <-done; waitErr <- cmd.Wait() }()
			select {
			case err := <-waitErr:
				p.mu.Lock()
				p.inst.PID = 0
				p.inst.LastExit = exitText(err)
				if s := panicSummary(p.tail); s != "" {
					p.inst.LastPanic = s
				}
				p.mu.Unlock()
			case <-ctx.Done():
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				select {
				case <-waitErr:
				case <-time.After(2 * time.Second):
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					<-waitErr
				}
				p.setState(registry.StateStopped)
				return
			}
		}
		if ctx.Err() != nil {
			break
		}
		if time.Since(started) > stableAfter {
			attempt = 0
		}
		d := backoffBase << attempt
		if d > backoffMax || d <= 0 {
			d = backoffMax
		} else {
			attempt++
		}
		// Full jitter, so a fleet-wide crash does not restart in lockstep.
		rngMu.Lock()
		d = time.Duration(rng.Int63n(int64(d)) + 1)
		rngMu.Unlock()
		p.mu.Lock()
		p.inst.Restarts++
		p.mu.Unlock()
		p.setState(registry.StateBackoff)
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
	p.setState(registry.StateStopped)
}

func exitText(err error) string {
	if err == nil {
		return "exit 0"
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return "signal " + ws.Signal().String()
		}
		return fmt.Sprintf("exit %d", ee.ExitCode())
	}
	return err.Error()
}

// healthLoop polls /healthz and records the config version each instance
// is actually serving.
func healthLoop(ctx context.Context, procs []*proc) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	sem := make(chan struct{}, 64)
	for ctx.Err() == nil {
		var wg sync.WaitGroup
		for _, p := range procs {
			inst := p.snapshot()
			if inst.State != registry.StateStarting && inst.State != registry.StateUp {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(p *proc, port int) {
				defer wg.Done()
				defer func() { <-sem }()
				resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
				if err != nil {
					if p.snapshot().State == registry.StateUp {
						p.setState(registry.StateStarting)
					}
					return
				}
				defer resp.Body.Close()
				var h struct {
					ConfigVersion string `json:"config_version"`
				}
				if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&h) == nil {
					p.mu.Lock()
					p.inst.ConfigVersion = h.ConfigVersion
					p.mu.Unlock()
					// Only promote if the process did not exit meanwhile.
					if p.snapshot().State == registry.StateStarting {
						p.setState(registry.StateUp)
					}
				}
			}(p, inst.Port)
		}
		wg.Wait()
		select {
		case <-time.After(healthEvery):
		case <-ctx.Done():
		}
	}
}

func snapshot(procs []*proc) registry.Snapshot {
	s := registry.Snapshot{GeneratedAt: time.Now().UTC()}
	for _, p := range procs {
		s.Instances = append(s.Instances, p.snapshot())
	}
	return s
}

func writeMetrics(w io.Writer, procs []*proc) {
	type key struct{ consumer, region, version, state string }
	counts := map[key]int{}
	fmt.Fprintln(w, "# TYPE pg_instance_restarts_total counter")
	for _, p := range procs {
		i := p.snapshot()
		v := strconv.Itoa(i.Version)
		counts[key{i.Consumer, i.Region, v, i.State}]++
		fmt.Fprintf(w, "pg_instance_restarts_total{id=%q,consumer=%q,region=%q,version=%q} %d\n", i.ID, i.Consumer, i.Region, v, i.Restarts)
	}
	fmt.Fprintln(w, "# TYPE pg_fleet_instances gauge")
	for k, n := range counts {
		fmt.Fprintf(w, "pg_fleet_instances{consumer=%q,region=%q,version=%q,state=%q} %d\n", k.consumer, k.region, k.version, k.state, n)
	}
}

func seedConfig(inst registry.Instance, seed string, fresh bool) error {
	if !fresh {
		if _, err := os.Stat(inst.ConfigPath); err == nil {
			return nil
		}
	}
	data, err := os.ReadFile(seed)
	if err != nil {
		return fmt.Errorf("seed file for %s: %w", inst.Consumer, err)
	}
	return registry.WriteAtomic(inst.ConfigPath, data)
}

func main() {
	cfgPath := flag.String("config", "deploy/fleet.json", "fleet layout file")
	listen := flag.String("listen", "127.0.0.1:19000", "address for /registry and /metrics")
	fresh := flag.Bool("fresh", false, "reset every instance's config file to the seed file")
	backtraces := flag.Bool("backtraces", false, "let Rust consumers print backtraces on panic. Costly in a crash loop: with 400 instances crash-looping, symbolizing backtraces alone can saturate a 4-core machine, much like the core dumps in Cloudflare's postmortem")
	scale := flag.Float64("scale", 1, "multiply per_region counts, for quick local runs (e.g. 0.1)")
	flag.Parse()

	cfg, err := registry.LoadFleetConfig(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if *scale != 1 {
		for i := range cfg.Consumers {
			n := int(float64(cfg.Consumers[i].PerRegion)**scale + 0.5)
			if n < 1 {
				n = 1
			}
			cfg.Consumers[i].PerRegion = n
		}
	}
	plan, err := registry.Plan(cfg)
	if err != nil {
		log.Fatal(err)
	}

	seeds := map[string]string{}
	for _, cc := range cfg.Consumers {
		seeds[cc.Name] = cc.SeedFile
	}
	logDir := filepath.Join(cfg.StateDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Fatal(err)
	}

	env := os.Environ()
	if *backtraces {
		env = append(env, "RUST_BACKTRACE=1")
	} else {
		env = append(env, "RUST_BACKTRACE=0")
	}

	var procs []*proc
	for _, inst := range plan {
		bin := filepath.Join(cfg.BinDir, inst.Consumer, fmt.Sprintf("v%d", inst.Version))
		if _, err := os.Stat(bin); err != nil {
			log.Fatalf("missing binary %s (run `make build`)", bin)
		}
		if err := seedConfig(inst, seeds[inst.Consumer], *fresh); err != nil {
			log.Fatal(err)
		}
		lf, err := os.OpenFile(filepath.Join(logDir, inst.ID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		inst.StateSince = time.Now()
		procs = append(procs, &proc{inst: inst, bin: bin, env: env, log: lf})
	}

	// Prometheus file_sd targets: one entry per instance with its labels.
	type target struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}
	var targets []target
	for _, p := range procs {
		i := p.inst
		targets = append(targets, target{
			Targets: []string{fmt.Sprintf("127.0.0.1:%d", i.Port)},
			Labels:  map[string]string{"instance_id": i.ID, "consumer": i.Consumer, "region": i.Region, "release": strconv.Itoa(i.Version)},
		})
	}
	tb, _ := json.MarshalIndent(targets, "", "  ")
	if err := registry.WriteAtomic(filepath.Join(cfg.StateDir, "prom_targets.json"), tb); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rng := rand.New(rand.NewSource(cfg.Seed))
	var rngMu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p *proc) { defer wg.Done(); p.run(ctx, rng, &rngMu) }(p)
	}
	go healthLoop(ctx, procs)

	regPath := filepath.Join(cfg.StateDir, "registry.json")
	go func() {
		for ctx.Err() == nil {
			b, _ := json.MarshalIndent(snapshot(procs), "", "  ")
			_ = registry.WriteAtomic(regPath, b)
			select {
			case <-time.After(publishEvery):
			case <-ctx.Done():
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/registry", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot(procs))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writeMetrics(w, procs)
	})
	mux.HandleFunc("/logs/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/logs/")
		for _, p := range procs {
			if p.inst.ID == id {
				p.mu.Lock()
				out := strings.Join(p.tail, "\n")
				p.mu.Unlock()
				fmt.Fprintln(w, out)
				return
			}
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	log.Printf("fleet: %d instances across %d regions; registry at http://%s/registry", len(procs), len(cfg.Regions), *listen)

	<-ctx.Done()
	log.Printf("fleet: stopping %d instances", len(procs))
	shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shut)
	wg.Wait()
	b, _ := json.MarshalIndent(snapshot(procs), "", "  ")
	_ = registry.WriteAtomic(regPath, b)
	for _, p := range procs {
		p.log.Close()
	}
}
