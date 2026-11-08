package gate

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Budgets are the semantic-diff thresholds. The rule of thumb behind the
// defaults: hold only on things that are almost never legitimate (duplicate
// keys, the file doubling or halving), and warn on things that are sometimes
// legitimate (a new rule shape, new nulls, drift). Warnings leave the final
// call to the sandbox, which runs the real consumers.
type Budgets struct {
	HoldGrowth    float64 // |row count change| above this holds (0.5 = ±50%)
	WarnGrowth    float64 // above this warns
	PSIWarn       float64 // population stability index above this warns
	ChurnWarn     float64 // share of keys not in last known good above this warns
	FlipWarn      float64 // share of policies whose allow flipped above this warns
	OutputWarn    float64 // sandbox: share of replayed responses that differ
	ErrorHold     float64 // sandbox: share of replayed requests that error
	LatencyHoldX  float64 // sandbox: p99 above this multiple of last known good holds...
	LatencyHoldMS float64 // ...if also this many ms above it
	RSSWarnX      float64 // sandbox: memory above this multiple warns
	GrowthHoldKB  float64 // sandbox: memory growth per replayed request above this holds
}

func DefaultBudgets() Budgets {
	return Budgets{
		HoldGrowth: 0.5, WarnGrowth: 0.2, PSIWarn: 0.25, ChurnWarn: 0.5, FlipWarn: 0.2,
		OutputWarn: 0.1, ErrorHold: 0.01, LatencyHoldX: 3, LatencyHoldMS: 20, RSSWarnX: 2, GrowthHoldKB: 4,
	}
}

type feature struct {
	Name   string   `json:"name"`
	Weight float64  `json:"weight"`
	Fields []string `json:"fields"`
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

type doc struct {
	ConfigVersion string    `json:"config_version"`
	Features      []feature `json:"features"`
	Policies      []policy  `json:"policies"`
}

func parse(data []byte) (*doc, error) {
	var d doc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// diff compares a candidate with the last known good file and adds findings.
func diff(r *Report, kind string, cand, lkg []byte, cfg Config) {
	b := cfg.Budgets
	if kind != "bot-proxy" && kind != "go-policy" {
		diffGeneric(r, kind, cand, lkg, cfg)
		return
	}
	c, err := parse(cand)
	if err != nil {
		r.add(Finding{Layer: "diff", Check: "parse", Severity: Hold, Detail: "candidate does not parse: " + err.Error()})
		return
	}
	if lkg == nil {
		r.add(Finding{Layer: "diff", Check: "baseline", Severity: Info, Detail: "no last known good file; semantic diff skipped"})
		return
	}
	l, err := parse(lkg)
	if err != nil {
		r.add(Finding{Layer: "diff", Check: "baseline", Severity: Warn, Detail: "last known good does not parse; semantic diff skipped"})
		return
	}
	if kind == "bot-proxy" {
		diffFeatures(r, c.Features, l.Features, b)
	} else {
		diffPolicies(r, c.Policies, l.Policies, b)
	}
}

// diffGeneric is the semantic diff for a JSON file PushGate has no
// built-in checks for: row count against the last known good file, and
// duplicate keys, on the top-level array cfg.Rows identified by cfg.Key.
func diffGeneric(r *Report, kind string, cand, lkg []byte, cfg Config) {
	var c map[string]json.RawMessage
	if err := json.Unmarshal(cand, &c); err != nil {
		r.add(Finding{Layer: "diff", Check: "parse", Severity: Hold, Detail: "candidate is not a JSON object: " + err.Error()})
		return
	}
	if cfg.Rows == "" {
		r.add(Finding{Layer: "diff", Check: "consumer", Severity: Info, Detail: "no semantic diff for " + kind + " (set rows and key in the spec)"})
		return
	}
	rows := func(doc map[string]json.RawMessage) ([]map[string]any, error) {
		var out []map[string]any
		raw, ok := doc[cfg.Rows]
		if !ok {
			return nil, fmt.Errorf("no %q array", cfg.Rows)
		}
		return out, json.Unmarshal(raw, &out)
	}
	cr, err := rows(c)
	if err != nil {
		r.add(Finding{Layer: "diff", Check: "parse", Severity: Hold, Detail: "candidate: " + err.Error()})
		return
	}
	key := func(row map[string]any) string {
		parts := make([]string, len(cfg.Key))
		for i, k := range cfg.Key {
			parts[i] = fmt.Sprint(row[k])
		}
		return strings.Join(parts, "/")
	}
	var lr []map[string]any
	if lkg != nil {
		var l map[string]json.RawMessage
		if json.Unmarshal(lkg, &l) == nil {
			lr, _ = rows(l)
		}
	}
	if lr == nil {
		r.add(Finding{Layer: "diff", Check: "baseline", Severity: Info, Detail: "no usable last known good file; row count not compared"})
	} else {
		growth(r, cfg.Rows+" count", len(cr), len(lr), cfg.Budgets)
	}
	if len(cfg.Key) == 0 {
		return
	}
	ck := make([]string, len(cr))
	for i, row := range cr {
		ck[i] = key(row)
	}
	nc, ex := duplicates(ck)
	nl := 0
	if lr != nil {
		lk := make([]string, len(lr))
		for i, row := range lr {
			lk[i] = key(row)
		}
		nl, _ = duplicates(lk)
	}
	if nc > nl {
		r.add(Finding{Layer: "diff", Check: "duplicates", Severity: Hold,
			Detail: fmt.Sprintf("%d %s share a %s with another row (e.g. %s); last known good had %d", nc, cfg.Rows, strings.Join(cfg.Key, "+"), ex, nl)})
	}
}

func growth(r *Report, what string, nc, nl int, b Budgets) {
	if nl == 0 {
		return
	}
	ch := float64(nc-nl) / float64(nl)
	d := fmt.Sprintf("%s %d vs %d in last known good (%+.0f%%)", what, nc, nl, 100*ch)
	switch {
	case math.Abs(ch) > b.HoldGrowth:
		r.add(Finding{Layer: "diff", Check: "row-count", Severity: Hold, Detail: d})
	case math.Abs(ch) > b.WarnGrowth:
		r.add(Finding{Layer: "diff", Check: "row-count", Severity: Warn, Detail: d})
	default:
		r.add(Finding{Layer: "diff", Check: "row-count", Severity: Info, Detail: d})
	}
}

func duplicates(keys []string) (int, string) {
	seen := map[string]int{}
	for _, k := range keys {
		seen[k]++
	}
	n, example := 0, ""
	for _, k := range keys {
		if seen[k] > 1 {
			n++
			if example == "" {
				example = k
			}
		}
	}
	return n, example
}

func diffFeatures(r *Report, c, l []feature, b Budgets) {
	growth(r, "feature count", len(c), len(l), b)

	names := func(fs []feature) []string {
		out := make([]string, len(fs))
		for i, f := range fs {
			out[i] = f.Name
		}
		return out
	}
	dc, ex := duplicates(names(c))
	dl, _ := duplicates(names(l))
	if dc > dl {
		r.add(Finding{Layer: "diff", Check: "duplicates", Severity: Hold,
			Detail: fmt.Sprintf("%d rows share a feature name with another row (e.g. %s); last known good had %d", dc, ex, dl)})
	}

	arity := func(fs []feature) map[int]int {
		m := map[int]int{}
		for _, f := range fs {
			m[len(f.Fields)]++
		}
		return m
	}
	ac, al := arity(c), arity(l)
	var known []int
	for n := range al {
		known = append(known, n)
	}
	sort.Ints(known)
	for n, cnt := range ac {
		if al[n] == 0 {
			r.add(Finding{Layer: "diff", Check: "new-shape", Severity: Warn,
				Detail: fmt.Sprintf("%d rules use a %d-field template; last known good only has %v-field templates", cnt, n, known)})
		}
	}

	weights := func(fs []feature) []float64 {
		out := make([]float64, len(fs))
		for i, f := range fs {
			out[i] = f.Weight
		}
		return out
	}
	if psi, ok := PSI(weights(l), weights(c)); ok {
		sev := Info
		if psi > b.PSIWarn {
			sev = Warn
		}
		r.add(Finding{Layer: "diff", Check: "drift", Severity: sev, Detail: fmt.Sprintf("weight distribution PSI %.3f", psi)})
	}

	old := map[string]bool{}
	for _, f := range l {
		old[f.Name] = true
	}
	fresh := 0
	for _, f := range c {
		if !old[f.Name] {
			fresh++
		}
	}
	if len(c) > 0 {
		if share := float64(fresh) / float64(len(c)); share > b.ChurnWarn {
			r.add(Finding{Layer: "diff", Check: "churn", Severity: Warn, Detail: fmt.Sprintf("%.0f%% of features are new", 100*share)})
		}
	}
}

func diffPolicies(r *Report, c, l []policy, b Budgets) {
	growth(r, "policy count", len(c), len(l), b)

	keys := func(ps []policy) []string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.Project + "|" + p.API
		}
		return out
	}
	dc, ex := duplicates(keys(c))
	dl, _ := duplicates(keys(l))
	if dc > dl {
		r.add(Finding{Layer: "diff", Check: "duplicates", Severity: Hold,
			Detail: fmt.Sprintf("%d policies share a project and API with another (e.g. %s); last known good had %d", dc, ex, dl)})
	}

	blank := func(ps []policy) int {
		n := 0
		for _, p := range ps {
			if p.EnforceQuota && p.Quota == nil {
				n++
			}
		}
		return n
	}
	if bc, bl := blank(c), blank(l); bc > bl {
		r.add(Finding{Layer: "diff", Check: "nulls", Severity: Warn,
			Detail: fmt.Sprintf("%d policies enforce quota with a blank quota block; last known good had %d", bc, bl)})
	}

	limits := func(ps []policy) []float64 {
		var out []float64
		for _, p := range ps {
			if p.Quota != nil {
				out = append(out, float64(p.Quota.LimitPerMin))
			}
		}
		return out
	}
	if psi, ok := PSI(limits(l), limits(c)); ok {
		sev := Info
		if psi > b.PSIWarn {
			sev = Warn
		}
		r.add(Finding{Layer: "diff", Check: "drift", Severity: sev, Detail: fmt.Sprintf("quota limit distribution PSI %.3f", psi)})
	}

	old := map[string]bool{}
	for _, p := range l {
		old[p.Project+"|"+p.API] = p.Allow
	}
	flips, both := 0, 0
	for _, p := range c {
		if a, ok := old[p.Project+"|"+p.API]; ok {
			both++
			if a != p.Allow {
				flips++
			}
		}
	}
	if both > 0 {
		if share := float64(flips) / float64(both); share > b.FlipWarn {
			r.add(Finding{Layer: "diff", Check: "allow-flips", Severity: Warn, Detail: fmt.Sprintf("%.0f%% of policies changed allow/deny", 100*share)})
		}
	}
}

// PSI is the population stability index of cur against base, with up to 10
// bins at base's deciles. ok is false when base has too few distinct values
// to bin.
func PSI(base, cur []float64) (float64, bool) {
	if len(base) < 10 || len(cur) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), base...)
	sort.Float64s(sorted)
	var edges []float64
	for q := 1; q < 10; q++ {
		e := sorted[q*len(sorted)/10]
		if len(edges) == 0 || e > edges[len(edges)-1] {
			edges = append(edges, e)
		}
	}
	if len(edges) < 2 {
		return 0, false
	}
	bin := func(v float64) int { return sort.SearchFloat64s(edges, v) }
	pb := make([]float64, len(edges)+1)
	pc := make([]float64, len(edges)+1)
	for _, v := range base {
		pb[bin(v)]++
	}
	for _, v := range cur {
		pc[bin(v)]++
	}
	const eps = 1e-4
	psi := 0.0
	for i := range pb {
		b := math.Max(pb[i]/float64(len(base)), eps)
		c := math.Max(pc[i]/float64(len(cur)), eps)
		psi += (c - b) * math.Log(c/b)
	}
	return psi, true
}
