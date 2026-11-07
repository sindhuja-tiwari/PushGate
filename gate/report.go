package gate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Severities. Only "hold" stops a push. "warn" and "info" are reported and
// the push continues to the rings.
const (
	Hold = "hold"
	Warn = "warn"
	Info = "info"
)

// Finding is one thing a check noticed.
type Finding struct {
	Layer    string `json:"layer"` // "diff" or "sandbox"
	Check    string `json:"check"`
	Severity string `json:"severity"`
	Version  int    `json:"version,omitempty"` // consumer release, for sandbox findings
	Detail   string `json:"detail"`
	// Cause groups sandbox findings for the summary (e.g. the panic text).
	Cause string `json:"cause,omitempty"`
}

// VersionResult is what the sandbox saw for one consumer release.
type VersionResult struct {
	Version   int  `json:"version"`
	Instances int  `json:"instances"` // how many fleet instances run it
	Loaded    bool `json:"loaded"`
	Crashed   bool `json:"crashed"`
	// CrashedOn is the 1-based replay request that killed it (0: on load
	// or not crashed).
	CrashedOn  int     `json:"crashed_on_request,omitempty"`
	Request    string  `json:"request,omitempty"`
	Panic      string  `json:"panic,omitempty"`
	Requests   int     `json:"requests"`
	Errors     int     `json:"errors"`
	OutputDiff float64 `json:"output_diff_share"`
	P50MS      float64 `json:"p50_ms"`
	P99MS      float64 `json:"p99_ms"`
	LKGP99MS   float64 `json:"lkg_p99_ms"`
	RSSKB      int     `json:"rss_kb"`
	LKGRSSKB   int     `json:"lkg_rss_kb"`
	// Memory added while replaying, from serving to the last request.
	GrowthKB    int `json:"rss_growth_kb"`
	LKGGrowthKB int `json:"lkg_rss_growth_kb"`
}

// Report is the gate's full answer for one candidate file.
type Report struct {
	Consumer      string          `json:"consumer"`
	Candidate     string          `json:"candidate"`
	LastKnownGood string          `json:"last_known_good"`
	Layers        []string        `json:"layers"`
	Verdict       string          `json:"verdict"` // "pass" or "hold"
	Findings      []Finding       `json:"findings"`
	Versions      []VersionResult `json:"versions,omitempty"`
	DurationMS    int64           `json:"duration_ms"`
}

func (r *Report) add(f Finding) {
	r.Findings = append(r.Findings, f)
	if f.Severity == Hold {
		r.Verdict = "hold"
	}
}

// Summary is the one-line explanation a held push carries, in the order a
// person needs it: what crashed and where, then why the file looks wrong.
// Releases that failed the same way are grouped.
func (r *Report) Summary() string {
	inst := map[int]int{}
	total := 0
	for _, v := range r.Versions {
		inst[v.Version] = v.Instances
		total += v.Instances
	}
	var holds, warns []string
	type group struct {
		check    string
		versions []string
		n        int
	}
	var groups []*group
	byCause := map[string]*group{}
	for _, f := range r.Findings {
		if f.Layer != "sandbox" || f.Severity != Hold || f.Version == 0 {
			continue
		}
		key := f.Check + "|" + f.Cause
		if f.Cause == "" {
			key += "|" + f.Detail
		}
		g := byCause[key]
		if g == nil {
			g = &group{check: f.Check}
			if f.Cause != "" {
				g.check = f.Check + ": " + f.Cause
			} else {
				g.check = f.Detail
			}
			byCause[key] = g
			groups = append(groups, g)
		}
		g.versions = append(g.versions, fmt.Sprintf("v%d", f.Version))
		g.n += inst[f.Version]
	}
	for _, g := range groups {
		verb, cause, _ := strings.Cut(g.check, ": ")
		if what, ok := map[string]string{"load": "crashes loading the file", "replay": "crashes on replayed traffic", "memory-growth": "leaks memory"}[verb]; ok {
			holds = append(holds, fmt.Sprintf("%s (%d of %d instances) %s: %s", strings.Join(g.versions, ", "), g.n, total, what, cause))
		} else {
			holds = append(holds, g.check)
		}
	}
	for _, layer := range []string{"sandbox", "diff"} {
		for _, f := range r.Findings {
			if f.Layer != layer {
				continue
			}
			switch {
			case f.Severity == Hold && (layer == "diff" || f.Version == 0):
				holds = append(holds, f.Detail)
			case f.Severity == Warn:
				warns = append(warns, f.Detail)
			}
		}
	}
	if r.Verdict == "hold" {
		s := "Held: " + strings.Join(holds, "; ")
		if len(warns) > 0 {
			s += fmt.Sprintf(" (also %d warning(s): %s)", len(warns), strings.Join(warns, "; "))
		}
		return s
	}
	if len(warns) > 0 {
		return fmt.Sprintf("Passed with %d warning(s): %s", len(warns), strings.Join(warns, "; "))
	}
	return "Passed"
}

// sortFindings puts diff findings first, then sandbox findings by release,
// so reports from parallel runs read the same every time.
func (r *Report) sortFindings() {
	rank := map[string]int{"diff": 0, "sandbox": 1}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if rank[a.Layer] != rank[b.Layer] {
			return rank[a.Layer] < rank[b.Layer]
		}
		return a.Version < b.Version
	})
}

var (
	threadID = regexp.MustCompile(`thread '([^']*)' \(\d+\)`)
	selfName = regexp.MustCompile(`\S+ v\d+ sandbox(-\d+)?: `)
	tempCfg  = regexp.MustCompile(`"?/[^\s"]*pushgate-sandbox-[^\s"]*/config\.json"?`)
)

// cleanPanic drops what differs between runs (thread ids, temp paths) so
// identical failures group together.
func cleanPanic(s string) string {
	s = threadID.ReplaceAllString(s, "thread '$1'")
	s = tempCfg.ReplaceAllString(s, "<file>")
	s = selfName.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "thread '<unnamed>' ", "")
	s = strings.ReplaceAll(s, "thread 'main' ", "")
	return s
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// Text renders the report for people: verdict, findings, then a table of
// what each release did with the file.
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  (%s, candidate %s, last known good %s, %d ms)\n\n", strings.ToUpper(r.Verdict), r.Consumer, r.Candidate, r.LastKnownGood, r.DurationMS)
	for _, f := range r.Findings {
		v := ""
		if f.Version > 0 {
			v = fmt.Sprintf(" v%d", f.Version)
		}
		fmt.Fprintf(&b, "  %-5s %-8s %-14s%s  %s\n", f.Severity, f.Layer, f.Check, v, f.Detail)
	}
	if len(r.Versions) > 0 {
		b.WriteString("\n  release  instances  result            requests  errors  p99 ms (lkg)   output diff\n")
		for _, v := range r.Versions {
			res := "ok"
			switch {
			case v.Crashed && v.CrashedOn > 0:
				res = fmt.Sprintf("crashed on #%d", v.CrashedOn)
			case v.Crashed:
				res = "crashed on load"
			}
			fmt.Fprintf(&b, "  v%-7d %-10d %-17s %-9d %-7d %5.2f (%5.2f)   %5.1f%%\n",
				v.Version, v.Instances, res, v.Requests, v.Errors, v.P99MS, v.LKGP99MS, 100*v.OutputDiff)
		}
	}
	return b.String()
}
