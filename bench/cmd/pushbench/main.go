// Command pushbench runs incident scenarios against rollout strategies on the
// simulated fleet and measures the damage.
//
// For each scenario and strategy it: restores a good file everywhere, pushes
// the scenario's bad file under the strategy, watches the fleet for a fixed
// window, and records how many instances were hurt, for how long, and how
// many requests failed.
//
//	pushbench -strategies b0,b1,b2 -window 45s -bake 10s
//
// By default it starts its own fleet and loadgen. Week 5 extends this with
// the good-push corpus, more scenarios and repeated seeds.
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
	"strings"
	"syscall"
	"time"

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
	Name     string     `json:"name"`
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
	Scenario         string            `json:"scenario"`
	Incident         string            `json:"incident"`
	Strategy         string            `json:"strategy"`
	Consumer         string            `json:"consumer"`
	Fleet            int               `json:"fleet"`
	Vulnerable       int               `json:"vulnerable"` // instances whose release the bad file breaks
	Push             *rollout.Result   `json:"push"`
	WindowS          float64           `json:"window_s"`
	PeakFailing      int               `json:"peak_failing"`
	PeakFailingShare float64           `json:"peak_failing_share"`
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
	scenarioGlob := flag.String("scenarios", "bench/scenarios/*.json", "scenario files")
	strategies := flag.String("strategies", "b0,b1,b2", "comma-separated strategies")
	window := flag.Duration("window", 45*time.Second, "how long to watch the fleet after each push")
	bake := flag.Duration("bake", 10*time.Second, "b2 bake time per ring")
	rate := flag.Int("rate", 2000, "loadgen requests per second")
	scale := flag.Float64("scale", 1, "fleet size multiplier (1 = 500 instances)")
	py := flag.String("py", ".venv/bin/python", "python with duckdb, for the producer")
	work := flag.String("work", "state/bench", "scratch directory")
	out := flag.String("out", "bench/results/latest.json", "results JSON")
	md := flag.String("md", "bench/results/latest.md", "results table (Markdown)")
	manage := flag.Bool("manage", true, "start and stop the fleet and loadgen")
	flag.Parse()

	scenarios, err := loadScenarios(*scenarioGlob)
	if err != nil {
		log.Fatal(err)
	}
	var strats []rollout.Strategy
	for _, name := range strings.Split(*strategies, ",") {
		s, err := rollout.ByName(strings.TrimSpace(name), *bake)
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
	if *manage {
		stop, err := startFleet(*scale, *rate, jsonl)
		if err != nil {
			log.Fatal(err)
		}
		defer stop()
	}

	fleet := rollout.SimFleet{Registry: registryURL}
	var runs []Run
	for _, sc := range scenarios {
		for _, s := range strats {
			log.Printf("== %s / %s", sc.Name, s.Name)
			r, err := runOne(fleet, sc, s, files[sc.Name][0], files[sc.Name][1], *window, *work, jsonl)
			if err != nil {
				log.Fatalf("%s/%s: %v", sc.Name, s.Name, err)
			}
			r.Env = map[string]string{"cpus": fmt.Sprint(runtime.NumCPU()), "rate": fmt.Sprint(*rate), "bake": bake.String()}
			runs = append(runs, *r)
			log.Printf("   %s: exposed %d/%d, peak failing %d (%.0f%%), %.0f failed instance-seconds, %.1f%% requests failed",
				r.Push.Status, r.Push.Exposed, r.Fleet, r.PeakFailing, 100*r.PeakFailingShare, r.FailedInstSec, 100*r.ReqFailShare)
		}
	}

	b, _ := json.MarshalIndent(runs, "", "  ")
	if err := registry.WriteAtomic(*out, b); err != nil {
		log.Fatal(err)
	}
	table := markdown(runs, *window, *bake, *rate)
	if err := registry.WriteAtomic(*md, []byte(table)); err != nil {
		log.Fatal(err)
	}
	fmt.Print(table)
}

func loadScenarios(glob string) ([]Scenario, error) {
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
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

func startFleet(scale float64, rate int, jsonl string) (func(), error) {
	_ = os.Remove(jsonl)
	fl := exec.Command("bin/fleet", "-config", "deploy/fleet.json", "-fresh", "-scale", fmt.Sprint(scale))
	fl.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := fl.Start(); err != nil {
		return nil, err
	}
	if err := waitAllUp(90 * time.Second); err != nil {
		fl.Process.Kill()
		return nil, err
	}
	lg := exec.Command("bin/loadgen", "-rate", fmt.Sprint(rate), "-quiet", "-listen", loadgenAddr, "-jsonl", jsonl)
	lg.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := lg.Start(); err != nil {
		fl.Process.Kill()
		return nil, err
	}
	time.Sleep(3 * time.Second)
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

	// Sample until the window ends.
	tick := time.NewTicker(sampleEvery)
	for time.Since(start) < window {
		<-tick.C
		snap, err := fleet.Snapshot()
		if err != nil {
			continue
		}
		failing := 0
		for _, i := range snap {
			if i.Consumer != sc.Consumer {
				continue
			}
			if i.State != registry.StateUp || i.Restarts > prev[i.ID] {
				failing++
			}
			prev[i.ID] = i.Restarts
		}
		run.Samples = append(run.Samples, Sample{At: time.Since(start).Seconds(), Failing: failing})
	}
	tick.Stop()
	run.Push = <-done
	if run.Push == nil {
		return nil, fmt.Errorf("push returned no result")
	}
	run.ExposedByVersion = run.Push.ExposedByVer
	summarize(run, sampleEvery)
	run.ReqOK, run.ReqFail = requestsBetween(jsonl, sc.Consumer, start, start.Add(window))
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

func markdown(runs []Run, window, bake time.Duration, rate int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# PushBench results\n\n")
	fmt.Fprintf(&b, "Generated %s on %d CPUs. Traffic %d req/s. Each run watched the fleet for %s after the push. B2 bake time %s per ring (rings 1%%, 10%%, 100%%).\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 MST"), runtime.NumCPU(), rate, window, bake)
	b.WriteString("| Incident | Strategy | Outcome | Got the bad file | Peak failing | Failed instance-seconds | Failed requests | Alarm | Recovered |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
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
		fmt.Fprintf(&b, "| %s | %s | %s | %d of %d (%.0f%%) | %d (%.0f%%) | %.0f | %.1f%% | %s | %s |\n",
			r.Incident, strings.ToUpper(r.Strategy), outcome, r.Push.Exposed, r.Fleet, 100*r.Push.ExposedPeak,
			r.PeakFailing, 100*r.PeakFailingShare, r.FailedInstSec, 100*r.ReqFailShare, alarm, rec)
	}
	b.WriteString("\nB0 and B1 have no health gate, so nothing rolls them back inside the window. In the real incidents people did, hours later. Failed instance-seconds for B0 and B1 therefore grow with the window length; compare strategies within one window, not across windows.\n")
	return b.String()
}
