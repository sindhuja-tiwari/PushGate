// Package gate is PushGate's pre-exposure check (strategy B3). Before any
// instance sees a candidate file it runs two layers:
//
//  1. Semantic diff against the last known good file: row counts,
//     duplicate keys, new rule shapes, new nulls, distribution drift.
//  2. Consumer-in-the-loop sandbox: for every consumer release live in the
//     fleet, start that exact binary with the candidate file, replay a
//     sample of recorded traffic, and compare crashes, errors, latency,
//     memory and answers with the same release running the last known good.
//
// Any "hold" finding stops the push with an explanation. Otherwise the push
// continues into B2's rings, bake times and alarms.
package gate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sindhuja-tiwari/pushgate/rollout"
)

// Config tells the gate where things are and which layers to run.
type Config struct {
	Fleet       rollout.Fleet
	Store       rollout.Store
	BinDir      string // bin/<consumer>/v<NN>
	TrafficFile string // loadgen -record output
	Replay      int    // requests to replay per release
	Diff        bool
	Sandbox     bool
	// NoTargeted turns off the requests built for new or changed entries,
	// leaving only the sampled replay (the week 5 behavior).
	NoTargeted bool
	Budgets    Budgets
	// LKG, if set, is used instead of the store's last known good file.
	LKG []byte

	// For consumers outside PushBench (gate ci, from a pushgate.json spec):
	// Releases replaces the fleet registry and bin/; Health is the path that
	// answers 200 once the consumer serves (default /healthz); Answer is the
	// JSON field compared between candidate and last known good; Format
	// picks a built-in semantic diff (bot-features, quota-policy), and Rows
	// and Key give a generic one: the top-level array of rows and the fields
	// that identify a row.
	Releases []Release
	Health   string
	Answer   string
	Format   string
	Rows     string
	Key      []string
}

// kind maps a consumer to the built-in checks it gets.
func (c Config) kind(consumer string) string {
	switch c.Format {
	case "bot-features":
		return "bot-proxy"
	case "quota-policy":
		return "go-policy"
	case "":
		return consumer
	}
	return c.Format
}

func (c Config) answer(consumer string) string {
	if c.Answer != "" {
		return c.Answer
	}
	return consumer
}

// DefaultConfig runs both layers with the default budgets.
func DefaultConfig(fleet rollout.Fleet, store rollout.Store) Config {
	return Config{
		Fleet: fleet, Store: store, BinDir: "bin", TrafficFile: "state/traffic.txt",
		Replay: 1000, Diff: true, Sandbox: true, Budgets: DefaultBudgets(),
	}
}

// Gate is a rollout.Validator (and rollout.Reporter).
type Gate struct {
	Cfg  Config
	name string
	mu   sync.Mutex
}

// New returns a gate named for the layers it runs.
func New(cfg Config) *Gate {
	name := "gate"
	switch {
	case cfg.Diff && !cfg.Sandbox:
		name = "gate-diff"
	case cfg.Sandbox && !cfg.Diff:
		name = "gate-sandbox"
	}
	return &Gate{Cfg: cfg, name: name}
}

func (g *Gate) Name() string { return g.name }

// Validate runs the gate and returns an error when it holds the push.
func (g *Gate) Validate(consumer string, data []byte) error {
	_, err := g.ValidateReport(consumer, data)
	return err
}

// ValidateReport runs the gate and returns its full report.
func (g *Gate) ValidateReport(consumer string, data []byte) (any, error) {
	r, err := g.Check(context.Background(), consumer, data)
	if r == nil {
		return nil, err
	}
	return r, err
}

// ErrHeld matches (with errors.Is) the error Check returns for a held file.
var ErrHeld = errors.New("held by gate")

type heldError struct{ summary string }

func (e heldError) Error() string        { return e.summary }
func (e heldError) Is(target error) bool { return target == ErrHeld }

// Check runs the configured layers on one candidate file.
func (g *Gate) Check(ctx context.Context, consumer string, data []byte) (*Report, error) {
	start := time.Now()
	lkg := g.Cfg.LKG
	if lkg == nil && g.Cfg.Store.Dir != "" {
		var err error
		if lkg, _, err = g.Cfg.Store.LKG(consumer); err != nil {
			return nil, err
		}
	}
	r := &Report{Consumer: consumer, Candidate: version(data), LastKnownGood: "none", Verdict: "pass"}
	if lkg != nil {
		r.LastKnownGood = version(lkg)
	}
	if g.Cfg.Diff {
		r.Layers = append(r.Layers, "diff")
		diff(r, g.Cfg.kind(consumer), data, lkg, g.Cfg)
	}
	if g.Cfg.Sandbox {
		r.Layers = append(r.Layers, "sandbox")
		sctx, cancel := context.WithTimeout(ctx, time.Minute)
		sandbox(sctx, r, g.Cfg, consumer, data, lkg)
		cancel()
	}
	r.sortFindings()
	r.DurationMS = time.Since(start).Milliseconds()
	if r.Verdict == "hold" {
		return r, heldError{r.Summary()}
	}
	return r, nil
}

func version(data []byte) string {
	d, err := parse(data)
	if err != nil || d.ConfigVersion == "" {
		return "unknown"
	}
	return d.ConfigVersion
}

// addSafe is add for findings reported from parallel sandbox runs.
var reportMu sync.Mutex

func (r *Report) addSafe(f Finding) {
	reportMu.Lock()
	defer reportMu.Unlock()
	r.add(f)
}

// B3 is PushGate: B2's checks, rings and alarms, with the gate in front.
func B3(bake time.Duration, cfg Config) rollout.Strategy {
	s := rollout.B2(bake)
	s.Name = "b3"
	switch {
	case cfg.Diff && !cfg.Sandbox:
		s.Name = "b3-diff"
	case cfg.Sandbox && !cfg.Diff:
		s.Name = "b3-sandbox"
	}
	s.Validators = append(s.Validators, New(cfg))
	return s
}

// UrgentBake is the urgent lane's bake at ring 0: long enough for the crash
// alarm, which fires within about 2 s of a crash in PushBench.
const UrgentBake = 3 * time.Second

// Urgent is the fast lane for content that cannot wait for a full staged
// rollout, such as a rule against an attack in progress. The whole gate still
// runs (it takes well under a second), then ring 0 bakes for UrgentBake with
// the usual alarms and rollback, then the file goes to everyone. A push in this
// lane must carry a reason, which is recorded with it. The kill switch still
// applies.
func Urgent(cfg Config) rollout.Strategy {
	cfg.Diff, cfg.Sandbox = true, true
	s := B3(UrgentBake, cfg)
	s.Name = "b3-urgent"
	s.Rings = []float64{1, 100}
	s.NeedsNote = true
	return s
}

// StrategyByName resolves b0, b1, b2, b3, the urgent lane b3-urgent, and the
// ablations b3-diff and b3-sandbox (one gate layer each).
func StrategyByName(name string, bake time.Duration, cfg Config) (rollout.Strategy, error) {
	switch strings.ToLower(name) {
	case "b3", "pushgate":
		cfg.Diff, cfg.Sandbox = true, true
		return B3(bake, cfg), nil
	case "b3-diff":
		cfg.Diff, cfg.Sandbox = true, false
		return B3(bake, cfg), nil
	case "b3-sandbox":
		cfg.Diff, cfg.Sandbox = false, true
		return B3(bake, cfg), nil
	case "b3-urgent", "urgent":
		return Urgent(cfg), nil
	}
	return rollout.ByName(name, bake)
}
