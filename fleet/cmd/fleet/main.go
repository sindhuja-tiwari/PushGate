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

func (p *proc) run(ctx context.Context, rng *rand.Rand, rngMu *sync.Mutex) {
	attempt := 0
	for ctx.Err() == nil {
		inst := p.snapshot()
		args := []string{"--id", inst.ID, "--port", strconv.Itoa(inst.Port), "--config", inst.ConfigPath}
		if inst.OnBadFile != "" {
			args = append(args, "--on-bad-file", inst.OnBadFile)
		}
		cmd := exec.Command(p.bin, args...)
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
			stopWatch := make(chan struct{})
			oom := make(chan string, 1)
			go p.watchMemory(cmd.Process.Pid, stopWatch, oom)
			select {
			case err := <-waitErr:
				close(stopWatch)
				p.mu.Lock()
				p.inst.PID = 0
				p.inst.RSSKB = 0
				p.inst.LastExit = exitText(err)
				select {
				case msg := <-oom:
					p.inst.LastExit = "killed: memory limit"
					p.inst.LastPanic = msg
				default:
					if s := registry.PanicSummary(p.tail); s != "" {
						p.inst.LastPanic = s
					}
				}
				p.mu.Unlock()
			case <-ctx.Done():
				close(stopWatch)
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

// memLimitKB is the resident memory an instance may use before the
// supervisor kills it, like a container's memory limit. 0 means no limit.
var memLimitKB int

// watchMemory samples the process's resident memory into the registry and,
// over the limit, kills it the way an out-of-memory killer would.
func (p *proc) watchMemory(pid int, stop <-chan struct{}, oom chan<- string) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		kb := residentKB(pid)
		p.mu.Lock()
		p.inst.RSSKB = kb
		p.mu.Unlock()
		if memLimitKB > 0 && kb > memLimitKB {
			msg := fmt.Sprintf("out of memory: resident memory %d MB over the %d MB limit", kb/1024, memLimitKB/1024)
			p.record("supervisor: killed: " + msg)
			oom <- msg
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return
		}
	}
}

func residentKB(pid int) int {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb int
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "VmRSS:")), "%d", &kb)
			return kb
		}
	}
	return 0
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
					Serving       *bool  `json:"serving"`
					LKG           *struct {
						From         string `json:"from"`
						LastRejected *struct {
							ConfigVersion string `json:"config_version"`
							Reason        string `json:"reason"`
						} `json:"last_rejected"`
					} `json:"lkg"`
				}
				if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&h) == nil {
					p.mu.Lock()
					p.inst.ConfigVersion = h.ConfigVersion
					p.inst.NotServing = h.Serving != nil && !*h.Serving
					p.inst.Rejected, p.inst.RejectReason, p.inst.ServingFrom = "", "", ""
					if h.LKG != nil {
						p.inst.ServingFrom = h.LKG.From
						if r := h.LKG.LastRejected; r != nil {
							p.inst.Rejected, p.inst.RejectReason = r.ConfigVersion, r.Reason
						}
					}
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
	seed := flag.Int64("seed", 0, "override the layout seed, which decides which release each instance runs (and so which releases land in each ring)")
	scale := flag.Float64("scale", 1, "multiply per_region counts, for quick local runs (e.g. 0.1)")
	memLimit := flag.Int("mem-limit-mb", 0, "kill an instance whose resident memory exceeds this many MB, like a container memory limit (0: no limit)")
	onBad := flag.String("on-bad-file", "", "run every consumer with the last-known-good library: keep_last_good or fail_closed, or off for the naive loader (default: each consumer's on_bad_file in the layout)")
	flag.Parse()
	memLimitKB = *memLimit * 1024

	cfg, err := registry.LoadFleetConfig(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if *seed != 0 {
		cfg.Seed = *seed
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
	switch *onBad {
	case "":
	case "off", "none", "naive":
		for i := range cfg.Consumers {
			cfg.Consumers[i].OnBadFile = ""
		}
	default:
		for i := range cfg.Consumers {
			cfg.Consumers[i].OnBadFile = *onBad
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
