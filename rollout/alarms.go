package rollout

import (
	"fmt"
	"math"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

// AlarmConfig sets the health gate used during bake time, modelled on
// metric alarms in tools like AWS AppConfig with CloudWatch: a threshold on
// a fleet signal that must be breached for several evaluations in a row.
type AlarmConfig struct {
	// Every is how often alarms are evaluated.
	Every time.Duration
	// Datapoints is how many breaching evaluations in a row fire the alarm.
	Datapoints int
	// Grace is how long after delivery an instance is ignored, so a normal
	// reload is not counted as unhealthy.
	Grace time.Duration
	// CrashShare fires when this share of exposed instances crashed or are
	// not serving (at least one instance).
	CrashShare float64
	// ErrorRate fires when exposed instances fail more than this share of
	// requests over ErrorWindow, given at least MinRequests requests.
	ErrorRate   float64
	ErrorWindow time.Duration
	MinRequests int64
}

// DefaultAlarms are the settings PushBench uses unless a run overrides them.
func DefaultAlarms() AlarmConfig {
	return AlarmConfig{
		Every:       time.Second,
		Datapoints:  2,
		Grace:       time.Second,
		CrashShare:  0.05,
		ErrorRate:   0.05,
		ErrorWindow: 3 * time.Second,
		MinRequests: 20,
	}
}

// exposure is one instance that has received the candidate file.
type exposure struct {
	inst        registry.Instance
	deliveredAt time.Time
	baseline    int // restart count when the push started
}

// signal is one alarm evaluation.
type signal struct {
	Breach bool
	Reason string
}

// evaluate checks the exposed instances once. It is a breach if either the
// crash signal or the error-rate signal is over its threshold.
func evaluate(cfg AlarmConfig, fleet Fleet, errs ErrorSource, exposed map[string]*exposure, now time.Time) (signal, error) {
	snap, err := fleet.Snapshot()
	if err != nil {
		return signal{}, err
	}
	live := make(map[string]registry.Instance, len(snap))
	for _, i := range snap {
		live[i.ID] = i
	}

	considered, unhealthy := 0, 0
	var example string
	for id, e := range exposed {
		if now.Sub(e.deliveredAt) < cfg.Grace {
			continue
		}
		considered++
		i, ok := live[id]
		if !ok {
			continue
		}
		if i.Restarts > e.baseline || i.State != registry.StateUp {
			unhealthy++
			if example == "" {
				example = describe(i)
			}
		}
	}
	if considered > 0 {
		need := int(math.Ceil(cfg.CrashShare * float64(considered)))
		if need < 1 {
			need = 1
		}
		if unhealthy >= need {
			return signal{Breach: true, Reason: fmt.Sprintf("crash alarm: %d of %d exposed instances crashed or not serving (e.g. %s)", unhealthy, considered, example)}, nil
		}
	}

	if errs != nil && cfg.ErrorRate > 0 {
		counts, err := errs.InstanceErrors(cfg.ErrorWindow)
		if err == nil {
			var ok, fail int64
			for id := range exposed {
				c := counts[id]
				ok += c.OK
				fail += c.Fail
			}
			if total := ok + fail; total >= cfg.MinRequests {
				if rate := float64(fail) / float64(total); rate > cfg.ErrorRate {
					return signal{Breach: true, Reason: fmt.Sprintf("error-rate alarm: %.1f%% of %d requests to exposed instances failed in the last %s", 100*rate, total, cfg.ErrorWindow)}, nil
				}
			}
		}
		// An unreachable error source is not a breach: the crash signal
		// still runs, and a missing metric should not roll back a push.
	}
	return signal{}, nil
}

func describe(i registry.Instance) string {
	s := fmt.Sprintf("%s v%d %s", i.ID, i.Version, i.State)
	if i.LastPanic != "" {
		p := i.LastPanic
		if len(p) > 160 {
			p = p[:160] + "…"
		}
		s += ": " + p
	}
	return s
}
