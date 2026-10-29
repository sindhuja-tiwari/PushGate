// Command go-policy is the Go test consumer for PushBench. It answers
// "may this project call this API?" from a quota policy file, in the shape of
// Google Cloud's Service Control.
//
// The Google Cloud June 2025 failure shape is built in on purpose. Releases
// v43 and later add quota enforcement. When they index the policy file, they
// read policy.Quota without a nil check. A policy with enforce_quota=true and
// a blank (null) quota block crashes the process. It crashes again on restart,
// because the same file is still on disk. Releases v41 and v42 never read that
// field, so the same file is harmless to them.
//
// The release number is set at build time:
//
//	go build -ldflags "-X main.buildVersion=43" ./consumers/go-policy
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// buildVersion is overwritten with -ldflags at build time.
var buildVersion = "44"

// quotaEnforcementSince is the first release with the quota code path.
const quotaEnforcementSince = 43

type Quota struct {
	LimitPerMin int `json:"limit_per_min"`
	Burst       int `json:"burst"`
}

type Policy struct {
	Project      string `json:"project"`
	API          string `json:"api"`
	Allow        bool   `json:"allow"`
	EnforceQuota bool   `json:"enforce_quota"`
	Quota        *Quota `json:"quota"`
}

type PolicyFile struct {
	Schema        string   `json:"schema"`
	ConfigVersion string   `json:"config_version"`
	Policies      []Policy `json:"policies"`
}

type index struct {
	configVersion string
	allow         map[string]bool
	quota         map[string]int
	count         int
}

func version() int {
	v, err := strconv.Atoi(buildVersion)
	if err != nil {
		log.Fatalf("bad buildVersion %q", buildVersion)
	}
	return v
}

func load(path string) (*index, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f PolicyFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if f.Schema != "quota-policy/1" {
		return nil, fmt.Errorf("unexpected schema %q", f.Schema)
	}
	idx := &index{configVersion: f.ConfigVersion, allow: map[string]bool{}, quota: map[string]int{}, count: len(f.Policies)}
	for _, p := range f.Policies {
		key := p.Project + "|" + p.API
		idx.allow[key] = p.Allow
		if version() >= quotaEnforcementSince && p.EnforceQuota {
			// Deliberately unchecked, as in the incident: a null quota
			// block dereferences a nil pointer and the process dies.
			idx.quota[key] = p.Quota.LimitPerMin
		}
	}
	return idx, nil
}

func main() {
	id := flag.String("id", "local", "instance id")
	port := flag.Int("port", 8081, "listen port on 127.0.0.1")
	cfg := flag.String("config", "policy.json", "policy file to load and watch")
	pollMS := flag.Int("poll-ms", 500, "how often to check the file for changes")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-policy v%s quota_enforcement=%v\n", buildVersion, version() >= quotaEnforcementSince)
		return
	}

	var cur atomic.Pointer[index]
	var loads, requests atomic.Uint64
	started := time.Now()

	idx, err := load(*cfg)
	if err != nil {
		log.Fatalf("go-policy v%s %s: failed to load %s: %v", buildVersion, *id, *cfg, err)
	}
	cur.Store(idx)
	loads.Add(1)
	log.Printf("go-policy v%s %s loaded %d policies (config %s)", buildVersion, *id, idx.count, idx.configVersion)

	go func() {
		var last time.Time
		if st, err := os.Stat(*cfg); err == nil {
			last = st.ModTime()
		}
		for {
			time.Sleep(time.Duration(*pollMS) * time.Millisecond)
			st, err := os.Stat(*cfg)
			if err != nil || st.ModTime().Equal(last) {
				continue
			}
			last = st.ModTime()
			fresh, err := load(*cfg)
			if err != nil {
				// A parse error keeps the old policy. Only the nil
				// dereference above takes the process down.
				log.Printf("go-policy v%s %s: reload rejected: %v", buildVersion, *id, err)
				continue
			}
			cur.Store(fresh)
			loads.Add(1)
			log.Printf("go-policy v%s %s reloaded %d policies (config %s)", buildVersion, *id, fresh.count, fresh.configVersion)
		}
	}()

	// Per-minute request counts for quota checks.
	var mu sync.Mutex
	window := time.Now().Truncate(time.Minute)
	counts := map[string]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		key := r.URL.Query().Get("project") + "|" + r.URL.Query().Get("api")
		ix := cur.Load()
		allowed := ix.allow[key]
		reason := "policy"
		if limit, ok := ix.quota[key]; ok && allowed {
			mu.Lock()
			if now := time.Now().Truncate(time.Minute); now != window {
				window, counts = now, map[string]int{}
			}
			counts[key]++
			if counts[key] > limit {
				allowed, reason = false, "quota"
			}
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"allow":%v,"reason":%q,"config_version":%q,"version":%s}`, allowed, reason, ix.configVersion, buildVersion)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ix := cur.Load()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"id":%q,"version":%s,"config_version":%q,"policies":%d}`, *id, buildVersion, ix.configVersion, ix.count)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		ix := cur.Load()
		l := fmt.Sprintf(`consumer="go-policy",id=%q,version=%q`, *id, buildVersion)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE pg_requests_total counter\npg_requests_total{%s} %d\n", l, requests.Load())
		fmt.Fprintf(w, "# TYPE pg_config_loads_total counter\npg_config_loads_total{%s} %d\n", l, loads.Load())
		fmt.Fprintf(w, "# TYPE pg_policies_loaded gauge\npg_policies_loaded{%s} %d\n", l, ix.count)
		fmt.Fprintf(w, "# TYPE pg_config_info gauge\npg_config_info{%s,config_version=%q} 1\n", l, ix.configVersion)
		fmt.Fprintf(w, "# TYPE pg_uptime_seconds gauge\npg_uptime_seconds{%s} %.1f\n", l, time.Since(started).Seconds())
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
