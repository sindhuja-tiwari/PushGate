// Package rollout delivers a config file to the fleet under a strategy, and
// records what happened. It implements the PushBench baselines:
//
//	B0 push-all   format check, then 100% of instances at once
//	B1 schema     format and JSON Schema checks, then 100% at once
//	B2 staged     B1's checks, then rings (1%, 10%, 100%) with bake times,
//	              alarm-driven rollback to the last known good file
//
// PushGate itself (B3) adds checks in front of B2 in later weeks.
package rollout

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

// Strategy is a rollout policy.
type Strategy struct {
	Name       string
	Validators []Validator
	// Rings are cumulative percentages of the consumer's instances, in
	// order. The last must be 100.
	Rings []float64
	// Bake is how long each ring must stay healthy before the next one.
	// Zero means no health gate at all.
	Bake   time.Duration
	Alarms AlarmConfig
}

// Gated reports whether the strategy watches health and can roll back.
func (s Strategy) Gated() bool { return s.Bake > 0 }

// B0 is push-all: check the format, then deliver everywhere at once.
func B0() Strategy {
	return Strategy{Name: "b0", Validators: []Validator{FormatCheck{}}, Rings: []float64{100}}
}

// B1 adds a JSON Schema check, then delivers everywhere at once.
func B1() Strategy {
	return Strategy{Name: "b1", Validators: []Validator{FormatCheck{}, SchemaCheck{}}, Rings: []float64{100}}
}

// B2 is a staged rollout with alarms, like AWS AppConfig with CloudWatch
// alarms: B1's checks, then rings with bake times and automatic rollback.
func B2(bake time.Duration) Strategy {
	return Strategy{
		Name:       "b2",
		Validators: []Validator{FormatCheck{}, SchemaCheck{}},
		Rings:      []float64{1, 10, 100},
		Bake:       bake,
		Alarms:     DefaultAlarms(),
	}
}

// ByName returns a baseline strategy by name.
func ByName(name string, bake time.Duration) (Strategy, error) {
	switch strings.ToLower(name) {
	case "b0", "push-all":
		return B0(), nil
	case "b1", "schema":
		return B1(), nil
	case "b2", "staged":
		return B2(bake), nil
	}
	return Strategy{}, fmt.Errorf("unknown strategy %q (want b0, b1 or b2)", name)
}

// ParseRings parses "1,10,100".
func ParseRings(s string) ([]float64, error) {
	var out []float64
	for _, p := range strings.Split(s, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || v <= 0 || v > 100 {
			return nil, fmt.Errorf("bad ring %q", p)
		}
		if len(out) > 0 && v <= out[len(out)-1] {
			return nil, fmt.Errorf("rings must increase: %s", s)
		}
		out = append(out, v)
	}
	if len(out) == 0 || out[len(out)-1] != 100 {
		return nil, fmt.Errorf("last ring must be 100: %s", s)
	}
	return out, nil
}

// Outcomes of a push.
const (
	StatusCompleted  = "completed"   // every ring delivered (and healthy, if gated)
	StatusRejected   = "rejected"    // a validator refused the file; nothing delivered
	StatusRolledBack = "rolled_back" // an alarm fired; exposed instances got the last known good file
	StatusHeld       = "held"        // an alarm fired but there was no last known good file
	StatusFrozen     = "frozen"      // the kill switch stopped it
	StatusFailed     = "failed"      // the rollout itself errored
)

// Event is one step in a push's timeline. At is time since the push started.
type Event struct {
	At     Millis  `json:"at_ms"`
	Kind   string  `json:"kind"`
	Ring   float64 `json:"ring,omitempty"`
	Count  int     `json:"count,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

// Millis marshals a duration as whole milliseconds.
type Millis time.Duration

func (m Millis) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(time.Duration(m).Milliseconds(), 10)), nil
}

func (m *Millis) UnmarshalJSON(b []byte) error {
	v, err := strconv.ParseInt(string(b), 10, 64)
	*m = Millis(time.Duration(v) * time.Millisecond)
	return err
}

// Result is the record of one push.
type Result struct {
	ID            string         `json:"id"`
	Consumer      string         `json:"consumer"`
	Strategy      string         `json:"strategy"`
	ConfigVersion string         `json:"config_version"`
	Started       time.Time      `json:"started"`
	Status        string         `json:"status"`
	Reason        string         `json:"reason,omitempty"`
	Fleet         int            `json:"fleet"`             // instances of this consumer
	Exposed       int            `json:"exposed"`           // instances that received the candidate file
	ExposedPeak   float64        `json:"exposed_share"`     // Exposed / Fleet
	TimeToFull    Millis         `json:"time_to_full_ms"`   // push start to 100% delivered (0 if never)
	TimeToAlarm   Millis         `json:"time_to_alarm_ms"`  // push start to alarm (0 if none)
	TimeToRevert  Millis         `json:"time_to_revert_ms"` // push start to rollback delivered (0 if none)
	Events        []Event        `json:"events"`
	ExposedByVer  map[string]int `json:"exposed_by_version"`
}

// Runner executes pushes against a fleet.
type Runner struct {
	Fleet  Fleet
	Errors ErrorSource // optional; nil disables the error-rate alarm
	Store  Store
	// Log, if set, receives each event as it happens.
	Log func(r *Result, e Event)
}

// Push runs one push of data to every instance of consumer under s.
func (rn *Runner) Push(ctx context.Context, s Strategy, consumer string, data []byte) (*Result, error) {
	start := time.Now()
	r := &Result{
		ID:            newID(consumer),
		Consumer:      consumer,
		Strategy:      s.Name,
		ConfigVersion: configVersion(data),
		Started:       start.UTC(),
		ExposedByVer:  map[string]int{},
	}
	emit := func(kind string, ring float64, count int, detail string) {
		e := Event{At: Millis(time.Since(start)), Kind: kind, Ring: ring, Count: count, Detail: detail}
		r.Events = append(r.Events, e)
		if rn.Log != nil {
			rn.Log(r, e)
		}
	}
	finish := func(status, reason string) (*Result, error) {
		r.Status, r.Reason = status, reason
		emit("finish", 0, 0, status)
		if err := rn.Store.SaveResult(r); err != nil {
			return r, err
		}
		return r, nil
	}

	if len(s.Rings) == 0 || s.Rings[len(s.Rings)-1] != 100 {
		return nil, errors.New("strategy rings must end at 100")
	}
	if f, err := rn.Store.Frozen(consumer); err != nil {
		return nil, err
	} else if f != nil {
		return finish(StatusFrozen, "pushes are frozen: "+f.Reason)
	}

	for _, v := range s.Validators {
		if err := v.Validate(consumer, data); err != nil {
			emit("validate_fail", 0, 0, v.Name()+": "+err.Error())
			return finish(StatusRejected, v.Name()+" check failed: "+err.Error())
		}
		emit("validate_ok", 0, 0, v.Name())
	}

	snap, err := rn.Fleet.Snapshot()
	if err != nil {
		return finish(StatusFailed, "cannot read fleet: "+err.Error())
	}
	var targets []registry.Instance
	for _, i := range snap {
		if i.Consumer == consumer {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return finish(StatusFailed, "no instances of "+consumer)
	}
	r.Fleet = len(targets)

	exposed := map[string]*exposure{}
	lkg, _, err := rn.Store.LKG(consumer)
	if err != nil {
		return finish(StatusFailed, "cannot read last known good: "+err.Error())
	}

	rollback := func(reason string) (*Result, error) {
		r.TimeToAlarm = Millis(time.Since(start))
		emit("alarm", 0, 0, reason)
		if lkg == nil {
			return finish(StatusHeld, reason+"; no last known good file to roll back to")
		}
		ids := make([]string, 0, len(exposed))
		for id := range exposed {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if err := rn.Fleet.Deliver(exposed[id].inst, lkg); err != nil {
				emit("rollback_error", 0, 0, id+": "+err.Error())
			}
		}
		r.TimeToRevert = Millis(time.Since(start))
		emit("rollback_delivered", 0, len(ids), "last known good "+configVersion(lkg))
		return finish(StatusRolledBack, reason)
	}

	for _, pct := range s.Rings {
		if f, _ := rn.Store.Frozen(consumer); f != nil {
			emit("frozen", pct, 0, f.Reason)
			return finish(StatusFrozen, "frozen during rollout: "+f.Reason)
		}
		members := registry.RingMembers(targets, pct)
		var fresh []registry.Instance
		for _, m := range members {
			if _, done := exposed[m.ID]; !done {
				fresh = append(fresh, m)
			}
		}
		// Baselines come from the live registry so restarts that happened
		// before delivery are not blamed on this file.
		live := map[string]registry.Instance{}
		if snap, err := rn.Fleet.Snapshot(); err == nil {
			for _, i := range snap {
				live[i.ID] = i
			}
		}
		for _, m := range fresh {
			if err := rn.Fleet.Deliver(m, data); err != nil {
				emit("deliver_error", pct, 0, m.ID+": "+err.Error())
				continue
			}
			base := m.Restarts
			if l, ok := live[m.ID]; ok {
				base = l.Restarts
			}
			exposed[m.ID] = &exposure{inst: m, deliveredAt: time.Now(), baseline: base}
			r.ExposedByVer[fmt.Sprintf("v%d", m.Version)]++
		}
		r.Exposed = len(exposed)
		r.ExposedPeak = float64(r.Exposed) / float64(r.Fleet)
		emit("ring_delivered", pct, len(fresh), fmt.Sprintf("%d of %d instances now have the file", r.Exposed, r.Fleet))
		if pct == 100 {
			r.TimeToFull = Millis(time.Since(start))
		}

		if !s.Gated() {
			continue
		}
		// Bake: the ring must stay healthy for s.Bake.
		breaches := 0
		bakeEnd := time.Now().Add(s.Bake)
		tick := time.NewTicker(s.Alarms.Every)
		for time.Now().Before(bakeEnd) {
			select {
			case <-ctx.Done():
				tick.Stop()
				return finish(StatusFailed, "cancelled")
			case <-tick.C:
			}
			if f, _ := rn.Store.Frozen(consumer); f != nil {
				tick.Stop()
				emit("frozen", pct, 0, f.Reason)
				return finish(StatusFrozen, "frozen during bake: "+f.Reason)
			}
			sig, err := evaluate(s.Alarms, rn.Fleet, rn.Errors, exposed, time.Now())
			if err != nil {
				emit("alarm_eval_error", pct, 0, err.Error())
				continue
			}
			if sig.Breach {
				breaches++
				if breaches >= s.Alarms.Datapoints {
					tick.Stop()
					return rollback(sig.Reason)
				}
			} else {
				breaches = 0
			}
		}
		tick.Stop()
		emit("bake_ok", pct, 0, fmt.Sprintf("healthy for %s", s.Bake))
	}

	if s.Gated() {
		if err := rn.Store.SetLKG(consumer, data, r.ID); err != nil {
			emit("lkg_error", 0, 0, err.Error())
		} else {
			emit("lkg_set", 0, 0, r.ConfigVersion)
		}
	}
	return finish(StatusCompleted, "")
}

// Rollback delivers the last known good file to every instance of a
// consumer. It is the manual "red button".
func (rn *Runner) Rollback(consumer string) (int, error) {
	lkg, _, err := rn.Store.LKG(consumer)
	if err != nil {
		return 0, err
	}
	if lkg == nil {
		return 0, fmt.Errorf("no last known good file for %s", consumer)
	}
	snap, err := rn.Fleet.Snapshot()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, i := range snap {
		if i.Consumer != consumer {
			continue
		}
		if err := rn.Fleet.Deliver(i, lkg); err != nil {
			return n, fmt.Errorf("%s: %w", i.ID, err)
		}
		n++
	}
	return n, nil
}
