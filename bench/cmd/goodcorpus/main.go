// Command goodcorpus measures how often the gate holds a push that is safe.
//
// It generates a corpus of good files from the seed files, each one a
// routine change a team ships every day (new rules, removed rules, retuned
// weights, new projects, quota changes), and runs every file through B1's
// schema and the gate's layers. Every file is safe for every release by
// construction: at most 128 features, no rule reads past input slot 20, and
// every policy that enforces a quota has one. So any hold is a false hold.
//
//	goodcorpus -n 500 -out bench/results/week5-good.json -md bench/results/week5-good.md
//
// The sandbox runs the real consumer binaries from bin/ on a fixed fleet
// with deploy/fleet.json's release mix, replaying gate/testdata/traffic.txt.
// No live fleet is needed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sindhuja-tiwari/pushgate/gate"
	"github.com/sindhuja-tiwari/pushgate/internal/registry"
	"github.com/sindhuja-tiwari/pushgate/rollout"
)

// Limits every release can handle (see docs/versions.md). The corpus stays
// inside them.
const (
	safeFeatures = 128 // v41's capacity, the smallest
	safeFields   = 20  // v41 to v43's input slots
)

type feature struct {
	Name   string   `json:"name"`
	Weight float64  `json:"weight"`
	Fields []string `json:"fields"`
}

type featureFile struct {
	Schema        string    `json:"schema"`
	ConfigVersion string    `json:"config_version"`
	Features      []feature `json:"features"`
}

type quota struct {
	LimitPerMin int `json:"limit_per_min"`
	Burst       int `json:"burst"`
}

type policy struct {
	Project      string `json:"project"`
	API          string `json:"api"`
	Allow        bool   `json:"allow"`
	EnforceQuota bool   `json:"enforce_quota"`
	Quota        *quota `json:"quota"`
}

type policyFile struct {
	Schema        string   `json:"schema"`
	ConfigVersion string   `json:"config_version"`
	Policies      []policy `json:"policies"`
}

// Item is one corpus file and what each check said about it.
type Item struct {
	ID       string   `json:"id"`
	Consumer string   `json:"consumer"`
	Changes  []string `json:"changes"`
	Rows     int      `json:"rows"`
	BaseRows int      `json:"base_rows"`

	SchemaOK bool `json:"schema_ok"`
	// Per gate layer set: "diff", "sandbox", "both".
	Verdict  map[string]string   `json:"verdict"`
	Warnings map[string][]string `json:"warnings"`
	Holds    map[string][]string `json:"holds"`
	MS       map[string]int64    `json:"duration_ms"`
	// CPUSeconds is the user+system CPU the full gate's sandbox processes
	// used for this file.
	CPUSeconds float64 `json:"cpu_seconds"`
}

func main() {
	n := flag.Int("n", 500, "corpus size (split evenly between the two consumers)")
	seed := flag.Int64("seed", 1, "corpus seed")
	binDir := flag.String("bin", "bin", "consumer binaries")
	traffic := flag.String("traffic", "gate/testdata/traffic.txt", "request sample to replay")
	layout := flag.String("fleet", "deploy/fleet.json", "fleet layout, for the release mix")
	out := flag.String("out", "bench/results/latest-good.json", "per-file results")
	md := flag.String("md", "bench/results/latest-good.md", "summary (Markdown)")
	keep := flag.String("keep", "", "if set, also write each corpus file to this directory")
	flag.Parse()

	cfg, err := registry.LoadFleetConfig(*layout)
	if err != nil {
		log.Fatal(err)
	}
	plan, err := registry.Plan(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fleet := fixedFleet(plan)

	var baseF featureFile
	var baseP policyFile
	mustRead("seed/feature.json", &baseF)
	mustRead("seed/policy.json", &baseP)
	rawF, _ := json.Marshal(baseF)
	rawP, _ := json.Marshal(baseP)
	lkg := map[string][]byte{"bot-proxy": rawF, "go-policy": rawP}

	gates := map[string]*gate.Gate{}
	for _, layers := range []string{"diff", "sandbox", "both"} {
		gc := gate.DefaultConfig(fleet, rollout.Store{})
		gc.BinDir, gc.TrafficFile = *binDir, *traffic
		gc.Diff = layers != "sandbox"
		gc.Sandbox = layers != "diff"
		gates[layers] = gate.New(gc)
	}

	rng := rand.New(rand.NewSource(*seed))
	var items []Item
	start := time.Now()
	for i := 0; i < *n; i++ {
		var it Item
		var data []byte
		if i%2 == 0 {
			f, changes := mutateFeatures(rng, baseF, i)
			data, _ = json.Marshal(f)
			it = Item{ID: f.ConfigVersion, Consumer: "bot-proxy", Changes: changes, Rows: len(f.Features), BaseRows: len(baseF.Features)}
		} else {
			p, changes := mutatePolicies(rng, baseP, i)
			data, _ = json.Marshal(p)
			it = Item{ID: p.ConfigVersion, Consumer: "go-policy", Changes: changes, Rows: len(p.Policies), BaseRows: len(baseP.Policies)}
		}
		if *keep != "" {
			_ = registry.WriteAtomic(filepath.Join(*keep, it.ID+".json"), data)
		}
		it.SchemaOK = (rollout.SchemaCheck{}).Validate(it.Consumer, data) == nil
		it.Verdict, it.Warnings, it.Holds, it.MS = map[string]string{}, map[string][]string{}, map[string][]string{}, map[string]int64{}
		for _, layers := range []string{"diff", "sandbox", "both"} {
			g := gates[layers]
			g.Cfg.LKG = lkg[it.Consumer]
			before := childCPU()
			r, err := g.Check(context.Background(), it.Consumer, data)
			if r == nil {
				log.Fatalf("%s: %v", it.ID, err)
			}
			if layers == "both" {
				it.CPUSeconds = childCPU() - before
			}
			it.Verdict[layers], it.MS[layers] = r.Verdict, r.DurationMS
			for _, f := range r.Findings {
				switch f.Severity {
				case gate.Warn:
					it.Warnings[layers] = append(it.Warnings[layers], f.Layer+"/"+f.Check)
				case gate.Hold:
					it.Holds[layers] = append(it.Holds[layers], f.Layer+"/"+f.Check+": "+f.Detail)
				}
			}
		}
		items = append(items, it)
		if (i+1)%50 == 0 {
			log.Printf("%d/%d checked (%s)", i+1, *n, time.Since(start).Round(time.Second))
		}
	}

	b, _ := json.MarshalIndent(items, "", " ")
	if err := registry.WriteAtomic(*out, b); err != nil {
		log.Fatal(err)
	}
	table := summary(items, *seed)
	if err := registry.WriteAtomic(*md, []byte(table)); err != nil {
		log.Fatal(err)
	}
	fmt.Print(table)
}

func mustRead(path string, v any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
}

// Request vocabulary, as in loadgen and the producer.
var (
	userAgents = []string{"chrome", "safari", "firefox", "curl", "python-requests", "go-http-client", "headless-chrome", "bot"}
	methods    = []string{"GET", "POST", "HEAD"}
	countries  = []string{"US", "IN", "DE", "BR", "JP", "NG"}
	asns       = []string{"residential", "mobile", "hosting", "university"}
)

func newTemplate(rng *rand.Rand) []string {
	f := make([]string, safeFields)
	for i := range f {
		f[i] = "*"
	}
	f[0] = userAgents[rng.Intn(len(userAgents))]
	if rng.Float64() < 0.5 {
		f[1] = methods[rng.Intn(len(methods))]
	}
	if rng.Float64() < 0.4 {
		f[2] = countries[rng.Intn(len(countries))]
	}
	if rng.Float64() < 0.4 {
		f[3] = asns[rng.Intn(len(asns))]
	}
	return f
}

// mutateFeatures applies one to three routine changes.
func mutateFeatures(rng *rand.Rand, base featureFile, i int) (featureFile, []string) {
	f := featureFile{Schema: base.Schema}
	for _, x := range base.Features {
		f.Features = append(f.Features, feature{x.Name, x.Weight, append([]string(nil), x.Fields...)})
	}
	var changes []string
	ops := rng.Perm(6)[:1+rng.Intn(3)]
	sort.Ints(ops)
	for _, op := range ops {
		switch op {
		case 0: // new rules, staying under every release's capacity
			room := safeFeatures - len(f.Features)
			if room <= 0 {
				continue
			}
			k := 1 + rng.Intn(min(room, 18))
			for j := 0; j < k; j++ {
				f.Features = append(f.Features, feature{fmt.Sprintf("feat_g%03d_%02d", i, j), round3(rng.Float64()*3 - 1), newTemplate(rng)})
			}
			changes = append(changes, fmt.Sprintf("add %d rules", k))
		case 1: // retire rules
			k := 1 + rng.Intn(len(f.Features)/5)
			for j := 0; j < k; j++ {
				x := rng.Intn(len(f.Features))
				f.Features = append(f.Features[:x], f.Features[x+1:]...)
			}
			changes = append(changes, fmt.Sprintf("remove %d rules", k))
		case 2: // retune weights
			spread := []float64{0.1, 0.3, 0.5}[rng.Intn(3)]
			share := []float64{0.2, 0.5, 1}[rng.Intn(3)]
			for j := range f.Features {
				if rng.Float64() < share {
					f.Features[j].Weight = round3(f.Features[j].Weight * (1 + spread*(2*rng.Float64()-1)))
				}
			}
			changes = append(changes, fmt.Sprintf("retune %.0f%% of weights by up to ±%.0f%%", 100*share, 100*spread))
		case 3: // replace some rules with new ones
			k := 1 + rng.Intn(len(f.Features)/10)
			for j := 0; j < k; j++ {
				x := rng.Intn(len(f.Features))
				f.Features[x] = feature{fmt.Sprintf("feat_r%03d_%02d", i, j), round3(rng.Float64()*3 - 1), newTemplate(rng)}
			}
			changes = append(changes, fmt.Sprintf("replace %d rules", k))
		case 4: // edit match fields of some rules
			k := 1 + rng.Intn(10)
			for j := 0; j < k; j++ {
				x := rng.Intn(len(f.Features))
				f.Features[x].Fields[2] = countries[rng.Intn(len(countries))]
			}
			changes = append(changes, fmt.Sprintf("edit country on %d rules", k))
		case 5: // a wider template whose 21st field is a wildcard: legal for every release
			k := 1 + rng.Intn(5)
			for j := 0; j < k; j++ {
				x := rng.Intn(len(f.Features))
				if len(f.Features[x].Fields) == safeFields {
					f.Features[x].Fields = append(f.Features[x].Fields, "*")
				}
			}
			changes = append(changes, fmt.Sprintf("widen %d rules with a wildcard 21st field", k))
		}
	}
	f.ConfigVersion = fmt.Sprintf("feat-good-%03d", i)
	return f, changes
}

func mutatePolicies(rng *rand.Rand, base policyFile, i int) (policyFile, []string) {
	p := policyFile{Schema: base.Schema}
	for _, x := range base.Policies {
		y := x
		if x.Quota != nil {
			q := *x.Quota
			y.Quota = &q
		}
		p.Policies = append(p.Policies, y)
	}
	apis := []string{"storage.read", "storage.write", "compute.list", "iam.get"}
	limits := []int{600, 1200, 6000}
	var changes []string
	ops := rng.Perm(5)[:1+rng.Intn(3)]
	sort.Ints(ops)
	for _, op := range ops {
		switch op {
		case 0: // onboard projects
			k := 1 + rng.Intn(10)
			for j := 0; j < k; j++ {
				for _, api := range apis {
					pol := policy{Project: fmt.Sprintf("p-n%03d-%02d", i, j), API: api, Allow: rng.Float64() < 0.9}
					if rng.Float64() < 0.3 {
						pol.EnforceQuota, pol.Quota = true, &quota{limits[rng.Intn(3)], 50}
					}
					p.Policies = append(p.Policies, pol)
				}
			}
			changes = append(changes, fmt.Sprintf("add %d projects", k))
		case 1: // offboard projects
			k := 1 + rng.Intn(8)
			for j := 0; j < k; j++ {
				victim := p.Policies[rng.Intn(len(p.Policies))].Project
				kept := p.Policies[:0]
				for _, x := range p.Policies {
					if x.Project != victim {
						kept = append(kept, x)
					}
				}
				p.Policies = kept
			}
			changes = append(changes, fmt.Sprintf("remove %d projects", k))
		case 2: // grant or revoke access
			k := 1 + rng.Intn(len(p.Policies)/10)
			for j := 0; j < k; j++ {
				x := rng.Intn(len(p.Policies))
				p.Policies[x].Allow = !p.Policies[x].Allow
			}
			changes = append(changes, fmt.Sprintf("flip allow on %d policies", k))
		case 3: // change quota limits
			n := 0
			for j := range p.Policies {
				if p.Policies[j].Quota != nil && rng.Float64() < 0.3 {
					p.Policies[j].Quota.LimitPerMin = limits[rng.Intn(3)]
					n++
				}
			}
			changes = append(changes, fmt.Sprintf("change %d quota limits", n))
		case 4: // start enforcing quotas, with a complete quota block
			k := 1 + rng.Intn(15)
			for j := 0; j < k; j++ {
				x := rng.Intn(len(p.Policies))
				p.Policies[x].EnforceQuota = true
				if p.Policies[x].Quota == nil {
					p.Policies[x].Quota = &quota{limits[rng.Intn(3)], 50}
				}
			}
			changes = append(changes, fmt.Sprintf("enforce quota on %d policies", k))
		}
	}
	p.ConfigVersion = fmt.Sprintf("pol-good-%03d", i)
	return p, changes
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

type staticFleet []registry.Instance

func fixedFleet(plan []registry.Instance) staticFleet {
	out := make(staticFleet, len(plan))
	for i, inst := range plan {
		inst.State = registry.StateUp
		out[i] = inst
	}
	return out
}

func (f staticFleet) Snapshot() ([]registry.Instance, error)  { return f, nil }
func (f staticFleet) Deliver(registry.Instance, []byte) error { return nil }

// childCPU is the user plus system CPU time of waited-for child processes.
func childCPU() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &ru); err != nil {
		return 0
	}
	tv := func(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
	return tv(ru.Utime) + tv(ru.Stime)
}

// Wilson returns the 95% Wilson score interval for k successes in n trials.
func Wilson(k, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.96
	p := float64(k) / float64(n)
	d := 1 + z*z/float64(n)
	c := p + z*z/(2*float64(n))
	m := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n)*float64(n)))
	return math.Max(0, (c-m)/d), math.Min(1, (c+m)/d)
}

func pct(xs []int64, p float64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[int(p*float64(len(s)-1))]
}

func summary(items []Item, seed int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Good-push corpus\n\nGenerated %s. %d safe files (seed %d), half feature files and half policy files, each with one to three routine changes. Every file is within every release's limits, so every hold is a false hold.\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 MST"), len(items), seed)
	b.WriteString("| Check | Held | False-hold rate (95% CI) | Passed with a warning | Gate time p50 / p95 |\n|---|---|---|---|---|\n")
	n := len(items)
	schemaFail := 0
	for _, it := range items {
		if !it.SchemaOK {
			schemaFail++
		}
	}
	lo, hi := Wilson(schemaFail, n)
	fmt.Fprintf(&b, "| B1 schema | %d of %d | %.1f%% (%.1f to %.1f%%) | – | – |\n", schemaFail, n, 100*float64(schemaFail)/float64(n), 100*lo, 100*hi)
	names := map[string]string{"diff": "Gate: semantic diff only", "sandbox": "Gate: sandbox only", "both": "Gate: both layers (B3)"}
	for _, layers := range []string{"diff", "sandbox", "both"} {
		held, warned := 0, 0
		var ms []int64
		for _, it := range items {
			if it.Verdict[layers] == "hold" {
				held++
			} else if len(it.Warnings[layers]) > 0 {
				warned++
			}
			ms = append(ms, it.MS[layers])
		}
		lo, hi := Wilson(held, n)
		fmt.Fprintf(&b, "| %s | %d of %d | %.1f%% (%.1f to %.1f%%) | %d (%.0f%%) | %d / %d ms |\n", names[layers], held, n, 100*float64(held)/float64(n), 100*lo, 100*hi, warned, 100*float64(warned)/float64(n), pct(ms, 0.5), pct(ms, 0.95))
	}

	// Why things were held or warned.
	reasons := map[string]int{}
	warnings := map[string]int{}
	for _, it := range items {
		for _, h := range it.Holds["both"] {
			k, _, _ := strings.Cut(h, ":")
			reasons[k]++
		}
		seen := map[string]bool{}
		for _, w := range it.Warnings["both"] {
			if !seen[w] {
				warnings[w]++
				seen[w] = true
			}
		}
	}
	if len(reasons) > 0 {
		b.WriteString("\nFalse holds by check (both layers):\n\n")
		for _, k := range registry.SortedKeys(reasons) {
			fmt.Fprintf(&b, "- `%s`: %d\n", k, reasons[k])
		}
	}
	if len(warnings) > 0 {
		b.WriteString("\nWarnings by check (both layers; a file can have several):\n\n")
		for _, k := range registry.SortedKeys(warnings) {
			fmt.Fprintf(&b, "- `%s`: %d files\n", k, warnings[k])
		}
	}
	var cpu []float64
	for _, it := range items {
		cpu = append(cpu, it.CPUSeconds)
	}
	sort.Float64s(cpu)
	if len(cpu) > 0 {
		sum := 0.0
		for _, c := range cpu {
			sum += c
		}
		fmt.Fprintf(&b, "\nGate cost: %.2f CPU-seconds per push on average (p95 %.2f), for 8 sandboxed processes per check (4 releases, candidate and last known good).\n",
			sum/float64(len(cpu)), cpu[int(0.95*float64(len(cpu)-1))])
	}
	return b.String()
}
