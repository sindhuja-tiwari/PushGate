// Command pushbench runs incident scenarios against rollout strategies on the
// simulated fleet and measures the damage.
//
// For each scenario and strategy it: restores a good file everywhere, pushes
// the scenario's bad file under the strategy, watches the fleet for a fixed
// window, and records how many instances were hurt, for how long, and how
// many requests failed.
//
//	pushbench -strategies b0,b1,b2,b3 -window 45s -bake 10s
//
// B3 is PushGate (B2 with the pre-exposure gate in front); b3-diff and
// b3-sandbox run one gate layer each. By default pushbench starts its own
// fleet and loadgen, and loadgen records the traffic sample the gate's
// sandbox replays. Week 5 extends this with the good-push corpus, more
// scenarios and repeated seeds.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sindhuja-tiwari/pushgate/gate"
	"github.com/sindhuja-tiwari/pushgate/internal/registry"
	"github.com/sindhuja-tiwari/pushgate/rollout"
)

const (
	registryURL = "http://127.0.0.1:19000/registry"
	loadgenAddr = "127.0.0.1:19100"
	sampleEvery = 500 * time.Millisecond
)

// Scenario is one bench/scenarios/*.json file.
type Scenario struct {
	Name string `json:"name"`
	// Kind is "bad" (an incident shape) or "good" (a safe push, which
	// measures false rollbacks and push delay).
	Kind     string     `json:"kind"`
	Incident string     `json:"incident"`
	Consumer string     `json:"consumer"`
	Summary  string     `json:"summary"`
	Breaks   []string   `json:"breaks"`
	Good     [][]string `json:"good"`
	Bad      [][]string `json:"bad"`
	path     string
}

// Sample is one look at the consumer's instances during a run.
type Sample struct {
	At      float64 `json:"at_s"` // seconds since the push started
	Failing int     `json:"failing"`
}

// Run is the measured outcome of one scenario under one strategy.
type Run struct {
	Scenario         string          `json:"scenario"`
	Kind             string          `json:"kind"`
	Seed             int64           `json:"seed"`
	Incident         string          `json:"incident"`
	Strategy         string          `json:"strategy"`
	Consumer         string          `json:"consumer"`
	Fleet            int             `json:"fleet"`
	Vulnerable       int             `json:"vulnerable"` // instances whose release the bad file breaks
	Push             *rollout.Result `json:"push"`
	WindowS          float64         `json:"window_s"`
	PeakFailing      int             `json:"peak_failing"`
	PeakFailingShare float64         `json:"peak_failing_share"`
	// PeakRefused is the most instances at once whose last-known-good
	// library refused the bad file (and kept serving, unless fail-closed).
	PeakRefused      int               `json:"peak_refused"`
	OnBadFile        string            `json:"on_bad_file,omitempty"`
	FailedInstSec    float64           `json:"failed_instance_seconds"`
	ReqOK            int64             `json:"requests_ok"`
	ReqFail          int64             `json:"requests_failed"`
	ReqFailShare     float64           `json:"requests_failed_share"`
	RecoveredAtS     *float64          `json:"recovered_at_s"` // nil: still failing at end of window
	Samples          []Sample          `json:"samples"`
	ExposedByVersion map[string]int    `json:"exposed_by_version"`
	Env              map[string]string `json:"env,omitempty"`
}

func main() {
	scenarioGlob := flag.String("scenarios", "bench/scenarios/*.json", "scenario files: comma-separated globs or scenario names")
	strategies := flag.String("strategies", "b0,b1,b2,b3", "comma-separated strategies (b0, b1, b2, b3, b3-diff, b3-sandbox)")
	window := flag.Duration("window", 45*time.Second, "how long to watch the fleet after each push")
	bake := flag.Duration("bake", 10*time.Second, "b2 bake time per ring")
	rate := flag.Int("rate", 2000, "loadgen requests per second")
	scale := flag.Float64("scale", 1, "fleet size multiplier (1 = 500 instances)")
	py := flag.String("py", ".venv/bin/python", "python with duckdb, for the producer")
	work := flag.String("work", "state/bench", "scratch directory")
	out := flag.String("out", "bench/results/latest.json", "results JSON")
	md := flag.String("md", "bench/results/latest.md", "results table (Markdown)")
	manage := flag.Bool("manage", true, "start and stop the fleet and loadgen")
	seedList := flag.String("seeds", "0", "comma-separated fleet layout seeds; each restarts the fleet with a different release-to-instance assignment (0: the layout's own seed)")
	replay := flag.Int("replay", 1000, "b3: recorded requests the sandbox replays per release")
	onBad := flag.String("on-bad-file", "", "run consumers with the last-known-good library: keep_last_good or fail_closed (default: the naive loaders)")
	flag.Parse()

	scenarios, err := loadScenarios(*scenarioGlob)
	if err != nil {
		log.Fatal(err)
	}
	fleet := rollout.SimFleet{Registry: registryURL}
	traffic := filepath.Join(*work, "traffic.txt")
	gcfg := gate.DefaultConfig(fleet, rollout.Store{Dir: filepath.Join(*work, "rollout")})
	gcfg.TrafficFile, gcfg.Replay = traffic, *replay
	var strats []rollout.Strategy
	for _, name := range strings.Split(*strategies, ",") {
		s, err := gate.StrategyByName(strings.TrimSpace(name), *bake, gcfg)
		if err != nil {
			log.Fatal(err)
		}
		strats = append(strats, s)
	}
	if err := os.MkdirAll(*work, 0o755); err != nil {
		log.Fatal(err)
	}

	// Build every scenario's files up front, so producer time never lands
	// inside a measurement window.
	files := map[string][2][]byte{}
	for _, sc := range scenarios {
		dir := filepath.Join(*work, sc.Name)
		good, bad := filepath.Join(dir, "good.json"), filepath.Join(dir, "bad.json")
		vars := map[string]string{"{py}": *py, "{work}": dir, "{good}": good, "{bad}": bad}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal(err)
		}
		for _, cmd := range append(append([][]string{}, sc.Good...), sc.Bad...) {
			if err := runCmd(cmd, vars); err != nil {
				log.Fatalf("%s: %v", sc.Name, err)
			}
		}
		g, err1 := os.ReadFile(good)
		b, err2 := os.ReadFile(bad)
		if err1 != nil || err2 != nil {
			log.Fatalf("%s: missing built files: %v %v", sc.Name, err1, err2)
		}
		files[sc.Name] = [2][]byte{g, b}
	}

	jsonl := filepath.Join(*work, "load.jsonl")
	var seeds []int64
	for _, f := range strings.Split(*seedList, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
		if err != nil {
			log.Fatalf("bad -seeds: %v", err)
		}
		seeds = append(seeds, n)
	}

	var runs []Run
	for _, seed := range seeds {
		var stop func()
		if *manage {
			log.Printf("== fleet seed %d", seed)
			if stop, err = startFleet(*scale, *rate, seed, *onBad, jsonl, traffic); err != nil {
				log.Fatal(err)
			}
		}
		for _, sc := range scenarios {
			for _, s := range strats {
				log.Printf("== seed %d / %s / %s", seed, sc.Name, s.Name)
				r, err := runOne(fleet, sc, s, files[sc.Name][0], files[sc.Name][1], *window, *work, jsonl)
				if err != nil {
					log.Fatalf("%s/%s: %v", sc.Name, s.Name, err)
				}
				r.Seed, r.Kind = seed, sc.Kind
				r.OnBadFile = *onBad
				r.Env = map[string]string{"cpus": fmt.Sprint(runtime.NumCPU()), "rate": fmt.Sprint(*rate), "bake": bake.String()}
				runs = append(runs, *r)
				log.Printf("   %s: exposed %d/%d, peak failing %d (%.0f%%), %.0f failed instance-seconds, %.1f%% requests failed",
					r.Push.Status, r.Push.Exposed, r.Fleet, r.PeakFailing, 100*r.PeakFailingShare, r.FailedInstSec, 100*r.ReqFailShare)
			}
		}
		if stop != nil {
			stop()
		}
	}

	b, _ := json.MarshalIndent(runs, "", "  ")
	if err := registry.WriteAtomic(*out, b); err != nil {
		log.Fatal(err)
	}
	table := markdown(runs, *window, *bake, *rate, *onBad)
	if err := registry.WriteAtomic(*md, []byte(table)); err != nil {
		log.Fatal(err)
	}
	fmt.Print(table)
}

// loadScenarios takes a comma-separated list of globs or bare scenario names
// (cloudflare-duplicate-rows means bench/scenarios/cloudflare-duplicate-rows.json).
func loadScenarios(glob string) ([]Scenario, error) {
	var paths []string
	for _, g := range strings.Split(glob, ",") {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if !strings.ContainsAny(g, "/*?[") {
			g = filepath.Join("bench", "scenarios", g+".json")
		}
		m, err := filepath.Glob(g)
		if err != nil {
			return nil, err
		}
		paths = append(paths, m...)
	}
	sort.Strings(paths)
	var out []Scenario
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var sc Scenario
		if err := json.Unmarshal(raw, &sc); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		sc.path = p
		out = append(out, sc)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no scenarios match %s", glob)
	}
	return out, nil
}

func runCmd(argv []string, vars map[string]string) error {
	args := make([]string, len(argv))
	for i, a := range argv {
		for k, v := range vars {
			a = strings.ReplaceAll(a, k, v)
		}
		args[i] = a
	}
	cmd := exec.Command(args[0], args[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func startFleet(scale float64, rate int, seed int64, onBad, jsonl, traffic string) (func(), error) {
	_ = os.Remove(jsonl)
	_ = os.Remove(traffic)
	args := []string{"-config", "deploy/fleet.json", "-fresh", "-scale", fmt.Sprint(scale)}
	if onBad != "" {
		args = append(args, "-on-bad-file", onBad)
	}
	if seed != 0 {
		args = append(args, "-seed", fmt.Sprint(seed))
	}
	fl := exec.Command("bin/fleet", args...)
	fl.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := fl.Start(); err != nil {
		return nil, err
	}
	if err := waitAllUp(90 * time.Second); err != nil {
		fl.Process.Kill()
		return nil, err
	}
	lg := exec.Command("bin/loadgen", "-rate", fmt.Sprint(rate), "-quiet", "-listen", loadgenAddr, "-jsonl", jsonl, "-record", traffic)
	lg.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := lg.Start(); err != nil {
		fl.Process.Kill()
		return nil, err
	}
	// loadgen writes its traffic sample every 5 s; B3 needs it.
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		if st, err := os.Stat(traffic); err == nil && st.Size() > 0 {
			break
		}
	}
	time.Sleep(time.Second)
	return func() {
		_ = lg.Process.Signal(os.Interrupt)
		_, _ = lg.Process.Wait()
		_ = fl.Process.Signal(os.Interrupt)
		_, _ = fl.Process.Wait()
	}, nil
}

func waitAllUp(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		s, err := registry.Fetch(registryURL)
		if err == nil && len(s.Instances) > 0 {
			up := 0
			for _, i := range s.Instances {
				if i.State == registry.StateUp {
					up++
				}
			}
			if up == len(s.Instances) {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("only %d of %d instances up after %s", up, len(s.Instances), timeout)
			}
		} else if time.Now().After(deadline) {
			return fmt.Errorf("fleet not reachable: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// restore puts the good file on every instance of the consumer and makes it
// the last known good, then waits for the fleet to be healthy and quiet.
func restore(fleet rollout.SimFleet, store rollout.Store, consumer string, good []byte) error {
	snap, err := fleet.Snapshot()
	if err != nil {
		return err
	}
	for _, i := range snap {
		if i.Consumer == consumer {
			if err := fleet.Deliver(i, good); err != nil {
				return err
			}
		}
	}
	if err := store.SetLKG(consumer, good, "pushbench-restore"); err != nil {
		return err
	}
	if err := store.SetFreeze(consumer, ""); err != nil {
		return err
	}
	if err := waitAllUp(90 * time.Second); err != nil {
		return err
	}
	time.Sleep(3 * time.Second) // let restarts settle and the error windows drain
	return waitAllUp(30 * time.Second)
}

func runOne(fleet rollout.SimFleet, sc Scenario, s rollout.Strategy, good, bad []byte, window time.Duration, work, jsonl string) (*Run, error) {
	store := rollout.Store{Dir: filepath.Join(work, "rollout")}
	if err := restore(fleet, store, sc.Consumer, good); err != nil {
		return nil, fmt.Errorf("restore before run: %w", err)
	}
	snap, err := fleet.Snapshot()
	if err != nil {
		return nil, err
	}
	breaks := map[string]bool{}
	for _, v := range sc.Breaks {
		breaks[v] = true
	}
	run := &Run{Scenario: sc.Name, Incident: sc.Incident, Strategy: s.Name, Consumer: sc.Consumer, WindowS: window.Seconds()}
	prev := map[string]int{}
	for _, i := range snap {
		if i.Consumer == sc.Consumer {
			run.Fleet++
			prev[i.ID] = i.Restarts
			if breaks[fmt.Sprintf("v%d", i.Version)] {
				run.Vulnerable++
			}
		}
	}

	rn := &rollout.Runner{Fleet: fleet, Errors: rollout.LoadgenErrors{URL: "http://" + loadgenAddr}, Store: store}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	done := make(chan *rollout.Result, 1)
	go func() {
		r, err := rn.Push(ctx, s, sc.Consumer, bad)
		if err != nil {
			log.Printf("push error: %v", err)
		}
		done <- r
	}()

	badVersion := configVersionOf(bad)
	// Sample until the window ends.
	// A push refused before any delivery cannot hurt the fleet, so the run
	// ends as soon as that is known instead of watching an idle fleet.
	var pushed *rollout.Result
	tick := time.NewTicker(sampleEvery)
sampling:
	for time.Since(start) < window {
		select {
		case pushed = <-done:
			done = nil
			if pushed != nil && pushed.Exposed == 0 {
				break sampling
			}
			continue
		case <-tick.C:
		}
		snap, err := fleet.Snapshot()
		if err != nil {
			continue
		}
		failing, refused := 0, 0
		for _, i := range snap {
			if i.Consumer != sc.Consumer {
				continue
			}
			if i.State != registry.StateUp || i.Restarts > prev[i.ID] || i.NotServing {
				failing++
			}
			if i.Rejected != "" && i.Rejected == badVersion {
				refused++
			}
			prev[i.ID] = i.Restarts
		}
		if refused > run.PeakRefused {
			run.PeakRefused = refused
		}
		run.Samples = append(run.Samples, Sample{At: time.Since(start).Seconds(), Failing: failing})
	}
	tick.Stop()
	end := time.Now()
	if done != nil {
		pushed = <-done
	}
	run.Push = pushed
	if run.Push == nil {
		return nil, fmt.Errorf("push returned no result")
	}
	run.ExposedByVersion = run.Push.ExposedByVer
	summarize(run, sampleEvery)
	run.ReqOK, run.ReqFail = requestsBetween(jsonl, sc.Consumer, start, end)
	if t := run.ReqOK + run.ReqFail; t > 0 {
		run.ReqFailShare = float64(run.ReqFail) / float64(t)
	}

	// Put the fleet back for the next run.
	if err := restore(fleet, store, sc.Consumer, good); err != nil {
		return nil, fmt.Errorf("restore after run: %w", err)
	}
	return run, nil
}

// summarize fills in peak, failed instance-seconds and recovery time from
// the samples.
func summarize(run *Run, every time.Duration) {
	lastFailing := -1
	for idx, s := range run.Samples {
		if s.Failing > run.PeakFailing {
			run.PeakFailing = s.Failing
		}
		run.FailedInstSec += float64(s.Failing) * every.Seconds()
		if s.Failing > 0 {
			lastFailing = idx
		}
	}
	if run.Fleet > 0 {
		run.PeakFailingShare = float64(run.PeakFailing) / float64(run.Fleet)
	}
	switch {
	case lastFailing == -1:
		zero := 0.0
		run.RecoveredAtS = &zero
	case lastFailing < len(run.Samples)-1:
		t := run.Samples[lastFailing+1].At
		run.RecoveredAtS = &t
	}
}

// requestsBetween sums the consumer's request outcomes from loadgen's
// per-interval JSON lines within [from, to].
func requestsBetween(path, consumer string, from, to time.Time) (ok, fail int64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	key := "consumer=" + consumer
	for sc.Scan() {
		var line struct {
			Time   time.Time                   `json:"time"`
			Groups map[string]map[string]int64 `json:"groups"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		// Each line covers the second before its timestamp.
		if line.Time.Before(from.Add(time.Second)) || line.Time.After(to.Add(time.Second)) {
			continue
		}
		g := line.Groups[key]
		ok += g["ok"]
		fail += g["fail"]
	}
	return ok, fail
}

func markdown(runs []Run, window, bake time.Duration, rate int, onBad string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# PushBench results\n\n")
	fmt.Fprintf(&b, "Generated %s on %d CPUs. Traffic %d req/s. Each run watched the fleet for %s after the push. B2 bake time %s per ring (rings 1%%, 10%%, 100%%).\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 MST"), runtime.NumCPU(), rate, window, bake)
	if onBad == "" {
		b.WriteString("Consumers: original naive loaders (no last-known-good library).\n\n")
	} else {
		fmt.Fprintf(&b, "Consumers: last-known-good library on, mode %s.\n\n", onBad)
	}
	b.WriteString("| Incident | Strategy | Outcome | Gate | Got the bad file | Refused it | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range runs {
		alarm := "–"
		if r.Push.TimeToAlarm > 0 {
			alarm = fmt.Sprintf("%.1f s", time.Duration(r.Push.TimeToAlarm).Seconds())
		}
		rec := fmt.Sprintf("not within %s", window)
		if r.RecoveredAtS != nil {
			if *r.RecoveredAtS == 0 {
				rec = "never failed"
			} else {
				rec = fmt.Sprintf("%.1f s", *r.RecoveredAtS)
			}
		}
		outcome := r.Push.Status
		if r.Push.Status == rollout.StatusRejected {
			outcome = "rejected before delivery"
		}
		refused := "–"
		if r.OnBadFile != "" {
			refused = fmt.Sprint(r.PeakRefused)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d of %d (%.0f%%) | %s | %d (%.0f%%) | %.0f | %.1f%% | %s | %s |\n",
			r.Incident, strings.ToUpper(r.Strategy), outcome, gateCell(r.Push), r.Push.Exposed, r.Fleet, 100*r.Push.ExposedPeak,
			refused, r.PeakFailing, 100*r.PeakFailingShare, r.FailedInstSec, 100*r.ReqFailShare, alarm, rec)
	}
	b.WriteString("\nRefused it: the most instances at once whose last-known-good library rejected the bad file. With keep_last_good they keep serving the old file, so they are not counted as failing; with fail_closed they stop answering and are.\n")
	b.WriteString("\nGate: what B3's pre-exposure gate decided, which layer held the file, and how long the gate took. B3-DIFF and B3-SANDBOX run one layer each.\n")
	b.WriteString("\nB0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.\n")
	return b.String()
}

// gateCell summarises the gate's report in a push record: verdict, the
// layers that held the file, and how long the gate took.
func gateCell(r *rollout.Result) string {
	for _, name := range []string{"gate", "gate-diff", "gate-sandbox"} {
		rep, ok := r.Checks[name].(*gate.Report)
		if !ok {
			continue
		}
		held := map[string]bool{}
		for _, f := range rep.Findings {
			if f.Severity == gate.Hold {
				held[f.Layer] = true
			}
		}
		var by []string
		for _, l := range []string{"diff", "sandbox"} {
			if held[l] {
				by = append(by, l)
			}
		}
		secs := float64(rep.DurationMS) / 1000
		if len(by) > 0 {
			return fmt.Sprintf("held by %s, %.1f s", strings.Join(by, " + "), secs)
		}
		return fmt.Sprintf("passed, %.1f s", secs)
	}
	return "–"
}

func configVersionOf(data []byte) string {
	var head struct {
		ConfigVersion string `json:"config_version"`
	}
	_ = json.Unmarshal(data, &head)
	return head.ConfigVersion
}
