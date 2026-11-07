package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

// testdata holds files from the producer (week 1): a good file of each type
// and the three incident files, plus a small loadgen -record sample.

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func diffOnly(t *testing.T, consumer, cand, lkg string) *Report {
	t.Helper()
	cfg := Config{Diff: true, Budgets: DefaultBudgets(), LKG: read(t, lkg)}
	r, err := New(cfg).Check(context.Background(), consumer, read(t, cand))
	if r == nil {
		t.Fatal(err)
	}
	if (r.Verdict == "hold") != errors.Is(err, ErrHeld) {
		t.Fatalf("verdict %s but err %v", r.Verdict, err)
	}
	return r
}

func has(r *Report, layer, check, severity string) bool {
	for _, f := range r.Findings {
		if f.Layer == layer && f.Check == check && f.Severity == severity {
			return true
		}
	}
	return false
}

func TestPSI(t *testing.T) {
	base := make([]float64, 100)
	for i := range base {
		base[i] = float64(i)
	}
	if p, ok := PSI(base, base); !ok || p > 1e-9 {
		t.Fatalf("identical: psi %v ok %v", p, ok)
	}
	shifted := make([]float64, 100)
	for i := range shifted {
		shifted[i] = float64(i) + 50
	}
	if p, ok := PSI(base, shifted); !ok || p < 0.25 {
		t.Fatalf("shifted by half the range: psi %v, want > 0.25", p)
	}
	if _, ok := PSI([]float64{1, 2, 3}, base); ok {
		t.Fatal("three values should be too few to bin")
	}
	if _, ok := PSI(make([]float64, 50), base); ok {
		t.Fatal("a constant baseline cannot be binned")
	}
}

// The diff alone should hold the Cloudflare file (it doubles and repeats
// every name) but only warn on the other two: a 21-field rule and a blank
// quota block are sometimes legitimate, and only running the consumers
// shows whether they are fatal.
func TestDiffCloudflareHolds(t *testing.T) {
	r := diffOnly(t, "bot-proxy", "feature-dup.json", "feature-good.json")
	if r.Verdict != "hold" || !has(r, "diff", "duplicates", Hold) || !has(r, "diff", "row-count", Hold) {
		t.Fatalf("want hold on duplicates and row count:\n%s", r.Text())
	}
}

func TestDiffCrowdStrikeWarns(t *testing.T) {
	r := diffOnly(t, "bot-proxy", "feature-21field.json", "feature-good.json")
	if r.Verdict != "pass" || !has(r, "diff", "new-shape", Warn) {
		t.Fatalf("want pass with a new-shape warning:\n%s", r.Text())
	}
}

func TestDiffGoogleWarns(t *testing.T) {
	r := diffOnly(t, "go-policy", "policy-blank.json", "policy-good.json")
	if r.Verdict != "pass" || !has(r, "diff", "nulls", Warn) {
		t.Fatalf("want pass with a nulls warning:\n%s", r.Text())
	}
}

func TestDiffIdenticalPasses(t *testing.T) {
	for _, c := range [][2]string{{"bot-proxy", "feature-good.json"}, {"go-policy", "policy-good.json"}} {
		r := diffOnly(t, c[0], c[1], c[1])
		if r.Verdict != "pass" || strings.Contains(r.Summary(), "warning") {
			t.Fatalf("%s: identical file should pass clean:\n%s", c[0], r.Text())
		}
	}
}

// grow returns the good feature file with n new, distinct features, which
// is ordinary day-to-day growth.
func grow(t *testing.T, n int) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(read(t, "feature-good.json"), &m); err != nil {
		t.Fatal(err)
	}
	fs := m["features"].([]any)
	for i := 0; i < n; i++ {
		f := map[string]any{}
		for k, v := range fs[i].(map[string]any) {
			f[k] = v
		}
		f["name"] = fmt.Sprintf("feat_new_%03d", i)
		fs = append(fs, f)
	}
	m["features"] = fs
	m["config_version"] = "feat-grown"
	b, _ := json.Marshal(m)
	return b
}

func TestDiffTenPercentGrowthPasses(t *testing.T) {
	cfg := Config{Diff: true, Budgets: DefaultBudgets(), LKG: read(t, "feature-good.json")}
	r, err := New(cfg).Check(context.Background(), "bot-proxy", grow(t, 11))
	if err != nil || r.Verdict != "pass" {
		t.Fatalf("10%% growth should pass: %v\n%s", err, r.Text())
	}
}

func TestDiffWithoutBaselineSkips(t *testing.T) {
	r, err := New(Config{Diff: true, Budgets: DefaultBudgets()}).Check(context.Background(), "bot-proxy", read(t, "feature-dup.json"))
	if err != nil || r.Verdict != "pass" || !has(r, "diff", "baseline", Info) {
		t.Fatalf("no baseline: want pass with info, got %v\n%s", err, r.Text())
	}
}

func TestCleanPanic(t *testing.T) {
	in := `thread 'main' (6155) panicked at src/main.rs:186:29: bot-proxy v44 sandbox: failed to load "/tmp/pushgate-sandbox-1411647037/config.json": too many`
	want := `panicked at src/main.rs:186:29: failed to load <file>: too many`
	if got := cleanPanic(in); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	// Each sandbox run has its own id, so /healthz can prove it is ours.
	if got := cleanPanic(strings.Replace(in, "v44 sandbox:", "v44 sandbox-17:", 1)); got != want {
		t.Fatalf("numbered id: got %q", got)
	}
}

func TestBindFailed(t *testing.T) {
	for _, s := range []string{
		"bot-proxy: cannot bind port 42317: Address already in use (os error 98)",
		"2026/11/05 17:33:28 listen tcp 127.0.0.1:39669: bind: address already in use",
	} {
		if !bindFailed(s) {
			t.Errorf("not recognised as a bind failure: %s", s)
		}
	}
	if bindFailed("panicked at src/main.rs:108:20: index out of bounds") {
		t.Error("a real panic is not a bind failure")
	}
}

func TestSummaryGroupsReleases(t *testing.T) {
	r := &Report{Verdict: "pass", Versions: []VersionResult{{Version: 41, Instances: 20}, {Version: 42, Instances: 60}, {Version: 44, Instances: 200}}}
	r.add(Finding{Layer: "sandbox", Check: "replay", Severity: Hold, Version: 41, Cause: "index out of bounds"})
	r.add(Finding{Layer: "sandbox", Check: "replay", Severity: Hold, Version: 42, Cause: "index out of bounds"})
	r.add(Finding{Layer: "diff", Check: "new-shape", Severity: Warn, Detail: "5 rules use a 21-field template"})
	want := "Held: v41, v42 (80 of 280 instances) crashes on replayed traffic: index out of bounds (also 1 warning(s): 5 rules use a 21-field template)"
	if got := r.Summary(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// fixedFleet is a registry snapshot with a given number of instances per
// release. The sandbox only reads it.
type fixedFleet struct{ insts []registry.Instance }

func newFixedFleet(consumer string, perVersion map[int]int) fixedFleet {
	var f fixedFleet
	for v, n := range perVersion {
		for i := 0; i < n; i++ {
			f.insts = append(f.insts, registry.Instance{ID: fmt.Sprintf("%s-%d-%d", consumer, v, i), Consumer: consumer, Version: v, State: registry.StateUp})
		}
	}
	return f
}

func (f fixedFleet) Snapshot() ([]registry.Instance, error)  { return f.insts, nil }
func (f fixedFleet) Deliver(registry.Instance, []byte) error { return nil }

// sandboxGate runs the real consumer binaries from ../bin (make build).
func sandboxGate(t *testing.T, consumer, lkg string, diff bool) *Gate {
	t.Helper()
	bin, _ := filepath.Abs("../bin")
	for v := 41; v <= 44; v++ {
		if _, err := os.Stat(filepath.Join(bin, consumer, fmt.Sprintf("v%d", v))); err != nil {
			t.Skipf("no %s v%d binary; run make build first", consumer, v)
		}
	}
	return New(Config{
		Fleet:       newFixedFleet(consumer, map[int]int{41: 5, 42: 15, 43: 30, 44: 50}),
		BinDir:      bin,
		TrafficFile: "testdata/traffic.txt",
		Replay:      1000,
		Diff:        diff,
		Sandbox:     true,
		Budgets:     DefaultBudgets(),
		LKG:         read(t, lkg),
	})
}

func crashed(r *Report) map[int]bool {
	out := map[int]bool{}
	for _, v := range r.Versions {
		if v.Crashed {
			out[v.Version] = true
		}
	}
	return out
}

func TestSandbox(t *testing.T) {
	cases := []struct {
		name, consumer, file, lkg string
		data                      []byte
		crash                     []int
		check                     string
	}{
		{"good feature file", "bot-proxy", "feature-good.json", "feature-good.json", nil, nil, ""},
		{"10% more features", "bot-proxy", "", "feature-good.json", grow(t, 11), nil, ""},
		{"cloudflare duplicate rows", "bot-proxy", "feature-dup.json", "feature-good.json", nil, []int{41, 42, 43, 44}, "load"},
		{"crowdstrike 21st field", "bot-proxy", "feature-21field.json", "feature-good.json", nil, []int{41, 42, 43}, "replay"},
		{"good policy file", "go-policy", "policy-good.json", "policy-good.json", nil, nil, ""},
		{"google blank quota", "go-policy", "policy-blank.json", "policy-good.json", nil, []int{43, 44}, "load"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := sandboxGate(t, c.consumer, c.lkg, false)
			data := c.data
			if data == nil {
				data = read(t, c.file)
			}
			start := time.Now()
			r, err := g.Check(context.Background(), c.consumer, data)
			if r == nil {
				t.Fatal(err)
			}
			if el := time.Since(start); el > time.Minute {
				t.Errorf("gate took %v, budget is 1 minute", el)
			}
			got := crashed(r)
			if len(got) != len(c.crash) {
				t.Fatalf("crashed releases %v, want %v\n%s", got, c.crash, r.Text())
			}
			for _, v := range c.crash {
				if !got[v] {
					t.Fatalf("v%d should crash\n%s", v, r.Text())
				}
			}
			if len(c.crash) == 0 {
				if err != nil || r.Verdict != "pass" {
					t.Fatalf("want pass, got %v\n%s", err, r.Text())
				}
				return
			}
			if !errors.Is(err, ErrHeld) || !has(r, "sandbox", c.check, Hold) {
				t.Fatalf("want a sandbox %s hold, got %v\n%s", c.check, err, r.Text())
			}
		})
	}
}
