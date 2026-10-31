// Package registry holds the types shared by the fleet supervisor, pgctl and
// loadgen: the fleet layout file and the live instance registry.
package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FleetConfig is the fleet layout, read from a JSON file (see deploy/fleet.json).
type FleetConfig struct {
	StateDir  string           `json:"state_dir"`
	BinDir    string           `json:"bin_dir"`
	BasePort  int              `json:"base_port"`
	Seed      int64            `json:"seed"`
	Regions   []string         `json:"regions"`
	Consumers []ConsumerConfig `json:"consumers"`
}

// ConsumerConfig describes one consumer type and its version mix.
type ConsumerConfig struct {
	Name       string `json:"name"`
	PerRegion  int    `json:"per_region"`
	ConfigFile string `json:"config_file"`
	SeedFile   string `json:"seed_file"`
	// Versions maps a release number to its relative weight in the fleet,
	// for example {"41": 5, "44": 50}.
	Versions map[string]int `json:"versions"`
}

// LoadFleetConfig reads and checks a fleet layout file.
func LoadFleetConfig(path string) (*FleetConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c FleetConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.StateDir == "" {
		c.StateDir = "state"
	}
	if c.BinDir == "" {
		c.BinDir = "bin"
	}
	if c.BasePort == 0 {
		c.BasePort = 20000
	}
	if len(c.Regions) == 0 || len(c.Consumers) == 0 {
		return nil, fmt.Errorf("%s: need at least one region and one consumer", path)
	}
	for _, cc := range c.Consumers {
		if len(cc.Versions) == 0 || cc.PerRegion <= 0 || cc.ConfigFile == "" {
			return nil, fmt.Errorf("%s: consumer %q needs versions, per_region and config_file", path, cc.Name)
		}
	}
	return &c, nil
}

// Instance states.
const (
	StateStarting = "starting" // process spawned, health check not yet passed
	StateUp       = "up"       // health check passing
	StateBackoff  = "backoff"  // exited, waiting to restart
	StateStopped  = "stopped"  // supervisor shutting down
)

// Instance is one consumer process in the fleet.
type Instance struct {
	ID            string    `json:"id"`
	Consumer      string    `json:"consumer"`
	Region        string    `json:"region"`
	Version       int       `json:"version"`
	Port          int       `json:"port"`
	ConfigPath    string    `json:"config_path"`
	State         string    `json:"state"`
	PID           int       `json:"pid"`
	Restarts      int       `json:"restarts"`
	ConfigVersion string    `json:"config_version"`
	LastExit      string    `json:"last_exit,omitempty"`
	LastPanic     string    `json:"last_panic,omitempty"`
	StateSince    time.Time `json:"state_since"`
}

// Snapshot is what the supervisor publishes at /registry and in
// state/registry.json.
type Snapshot struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Instances   []Instance `json:"instances"`
}

// Fetch reads a snapshot from a supervisor URL or a registry.json path.
func Fetch(src string) (*Snapshot, error) {
	var s Snapshot
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			return nil, err
		}
		return &s, nil
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteAtomic writes data to path through a temp file and rename, so a
// reader never sees a half-written file.
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	// CreateTemp makes the file 0600. Other readers (Prometheus in its own
	// container, consumers running as another user) need to read it.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SortedKeys returns map keys in order, for stable output.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
