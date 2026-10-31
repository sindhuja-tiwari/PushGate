package rollout

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

// Fleet is how a rollout sees and changes the fleet.
type Fleet interface {
	// Snapshot returns every instance with its live state and restart count.
	Snapshot() ([]registry.Instance, error)
	// Deliver writes a config file to one instance.
	Deliver(inst registry.Instance, data []byte) error
}

// SimFleet talks to the simulated fleet: it reads the supervisor's registry
// and delivers by writing each instance's config file.
type SimFleet struct {
	Registry string // supervisor URL or path to state/registry.json
}

func (f SimFleet) Snapshot() ([]registry.Instance, error) {
	s, err := registry.Fetch(f.Registry)
	if err != nil {
		return nil, err
	}
	return s.Instances, nil
}

func (f SimFleet) Deliver(inst registry.Instance, data []byte) error {
	return registry.WriteAtomic(inst.ConfigPath, data)
}

// ErrorSource reports request outcomes per instance over a recent window,
// the way a load balancer would. loadgen serves this at /instances.
type ErrorSource interface {
	InstanceErrors(window time.Duration) (map[string]Counts, error)
}

// Counts is ok/failed requests for one instance.
type Counts struct {
	OK   int64 `json:"ok"`
	Fail int64 `json:"fail"`
}

// LoadgenErrors reads per-instance counts from loadgen's -listen endpoint.
type LoadgenErrors struct {
	URL string // e.g. http://127.0.0.1:19100
}

func (l LoadgenErrors) InstanceErrors(window time.Duration) (map[string]Counts, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("%s/instances?window=%d", strings.TrimRight(l.URL, "/"), int(window.Seconds()))
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Instances map[string]Counts `json:"instances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Instances, nil
}
