// Command report turns PushBench runs (several fleet seeds) and the
// good-push corpus into the week 5 results: tables with 95% confidence
// intervals and two SVG charts.
//
//	report -runs bench/results/week5.json -good bench/results/week5-good.json -out bench/results/week5
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// run is the part of a pushbench Run this report reads.
type run struct {
	Scenario         string  `json:"scenario"`
	Kind             string  `json:"kind"`
	Seed             int64   `json:"seed"`
	Incident         string  `json:"incident"`
	Strategy         string  `json:"strategy"`
	Consumer         string  `json:"consumer"`
	Fleet            int     `json:"fleet"`
	Vulnerable       int     `json:"vulnerable"`
	WindowS          float64 `json:"window_s"`
	PeakFailing      int     `json:"peak_failing"`
	PeakFailingShare float64 `json:"peak_failing_share"`
	FailedInstSec    float64 `json:"failed_instance_seconds"`
	ReqFailShare     float64 `json:"requests_failed_share"`
	Samples          []struct {
		At      float64 `json:"at_s"`
		Failing int     `json:"failing"`
	} `json:"samples"`
	Push struct {
		Status      string  `json:"status"`
		Reason      string  `json:"reason"`
		Exposed     int     `json:"exposed"`
		ExposedPeak float64 `json:"exposed_share"`
		TimeToFull  int64   `json:"time_to_full_ms"`
		TimeToAlarm int64   `json:"time_to_alarm_ms"`
		Events      []struct {
			At   int64  `json:"at_ms"`
			Kind string `json:"kind"`
		} `json:"events"`
		Checks map[string]struct {
			Verdict    string `json:"verdict"`
			DurationMS int64  `json:"duration_ms"`
			Findings   []struct {
				Layer    string `json:"layer"`
				Severity string `json:"severity"`
			} `json:"findings"`
		} `json:"checks"`
	} `json:"push"`
}

type goodItem struct {
	Verdict    map[string]string `json:"verdict"`
	SchemaOK   bool              `json:"schema_ok"`
	CPUSeconds float64           `json:"cpu_seconds"`
	MS         map[string]int64  `json:"duration_ms"`
}

func main() {
	runsPath := flag.String("runs", "bench/results/week5.json", "pushbench runs JSON")
	goodPath := flag.String("good", "bench/results/week5-good.json", "goodcorpus JSON")
	outDir := flag.String("out", "bench/results/week5", "directory for charts")
	md := flag.String("md", "bench/results/week5.md", "report (Markdown)")
	flag.Parse()

	var runs []run
	readJSON(*runsPath, &runs)
	var good []goodItem
	if *goodPath != "" {
		readJSON(*goodPath, &good)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	rel, _ := filepath.Rel(filepath.Dir(*md), *outDir)

	var b strings.Builder
	writeReport(&b, runs, good, rel)
	exposureChart(filepath.Join(*outDir, "exposure.svg"), runs)
	timelineChart(filepath.Join(*outDir, "timeline.svg"), runs)
	if err := os.WriteFile(*md, []byte(b.String()), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Print(b.String())
}

func readJSON(path string, v any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
}

// --- statistics ---

// tCrit is the two-sided 95% Student t critical value for df degrees of
// freedom (df >= 1).
func tCrit(df int) float64 {
	table := []float64{12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228}
	if df <= 0 {
		return math.NaN()
	}
	if df <= len(table) {
		return table[df-1]
	}
	return 1.96
}

// meanCI returns the mean and the half-width of its 95% confidence interval.
func meanCI(xs []float64) (mean, half float64) {
	n := len(xs)
	if n == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(n)
	if n == 1 {
		return mean, 0
	}
	ss := 0.0
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	sd := math.Sqrt(ss / float64(n-1))
	return mean, tCrit(n-1) * sd / math.Sqrt(float64(n))
}

// wilson is the 95% Wilson score interval for k of n.
func wilson(k, n int) (lo, hi float64) {
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

func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(q*float64(len(s)-1)+0.5)]
}

func fmtCI(mean, half float64, unit string, digits int) string {
	f := "%." + fmt.Sprint(digits) + "f"
	// Hide an interval that rounds to zero at this precision.
	if fmt.Sprintf(f, half) == fmt.Sprintf(f, 0.0) {
		return fmt.Sprintf(f+"%s", mean, unit)
	}
	return fmt.Sprintf(f+" ± "+f+"%s", mean, half, unit)
}

// --- grouping ---

var strategyOrder = []string{"b0", "b1", "b2", "b3"}

func strategies(runs []run) []string {
	seen := map[string]bool{}
	for _, r := range runs {
		seen[r.Strategy] = true
	}
	var out []string
	for _, s := range strategyOrder {
		if seen[s] {
			out = append(out, s)
			delete(seen, s)
		}
	}
	var rest []string
	for s := range seen {
		rest = append(rest, s)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

type scenario struct{ name, incident, kind string }

func scenarios(runs []run, kind string) []scenario {
	seen := map[string]bool{}
	var out []scenario
	for _, r := range runs {
		if r.Kind == kind && !seen[r.Scenario] {
			seen[r.Scenario] = true
			out = append(out, scenario{r.Scenario, r.Incident, r.Kind})
		}
	}
	return out
}

func pick(runs []run, scenario, strategy string) []run {
	var out []run
	for _, r := range runs {
		if r.Scenario == scenario && r.Strategy == strategy {
			out = append(out, r)
		}
	}
	return out
}

func caught(r run) bool { return r.Push.Exposed == 0 }

func seedsOf(runs []run) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, r := range runs {
		if !seen[r.Seed] {
			seen[r.Seed] = true
			out = append(out, r.Seed)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// --- report ---

func writeReport(b *strings.Builder, runs []run, good []goodItem, chartDir string) {
	strats := strategies(runs)
	seeds := seedsOf(runs)
	bad := scenarios(runs, "bad")
	fleets, window := map[string]int{}, 0.0
	for _, r := range runs {
		fleets[r.Consumer] = r.Fleet
		window = r.WindowS
	}

	fmt.Fprintf(b, "# PushBench week 5 results\n\nGenerated %s. %d bad scenarios and %d good pushes, each run under %s on %d fleet layouts (seeds %s). A seed changes which release each instance runs, and so which releases land in each ring. The fleet has %d bot-proxy and %d go-policy instances. Each run watched the fleet for %.0f s after the push. Intervals are 95%%: Wilson for proportions, Student t over seeds for means.\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 MST"), len(bad), len(scenarios(runs, "good")), upper(strats), len(seeds), joinSeeds(seeds), fleets["bot-proxy"], fleets["go-policy"], window)

	// Headline: per strategy over all bad runs.
	b.WriteString("## Overall, bad pushes\n\n")
	b.WriteString("| Strategy | Held before any exposure | Mean share of fleet that got the file | Mean failed instance-seconds per push | Runs where any instance failed |\n|---|---|---|---|---|\n")
	for _, s := range strats {
		var k, n, hurt int
		var exp, fis []float64
		for _, sc := range bad {
			for _, r := range pick(runs, sc.name, s) {
				n++
				if caught(r) {
					k++
				}
				if r.PeakFailing > 0 {
					hurt++
				}
				exp = append(exp, 100*r.Push.ExposedPeak)
				fis = append(fis, r.FailedInstSec)
			}
		}
		lo, hi := wilson(k, n)
		em, eh := meanCI(exp)
		fm, fh := meanCI(fis)
		hl, hh := wilson(hurt, n)
		fmt.Fprintf(b, "| %s | %d of %d, %.0f%% (%.0f to %.0f%%) | %s | %s | %d of %d, %.0f%% (%.0f to %.0f%%) |\n",
			strings.ToUpper(s), k, n, 100*float64(k)/float64(max(n, 1)), 100*lo, 100*hi, fmtCI(em, eh, "%", 1), fmtCI(fm, fh, "", 0),
			hurt, n, 100*float64(hurt)/float64(max(n, 1)), 100*hl, 100*hh)
	}
	fmt.Fprintf(b, "\n![Share of the fleet that got each bad file](%s/exposure.svg)\n\n", chartDir)

	// Per scenario.
	b.WriteString("## Per scenario\n\nHeld: runs where no instance got the file. Got the file and failed instance-seconds are means over seeds, with the 95% interval.\n\n")
	b.WriteString("| Scenario | Breaks | Strategy | Held | Got the file | Peak failing | Failed instance-seconds | Failed requests |\n|---|---|---|---|---|---|---|---|\n")
	for _, sc := range bad {
		for i, s := range strats {
			rs := pick(runs, sc.name, s)
			if len(rs) == 0 {
				continue
			}
			k := 0
			var exp, peak, fis, req []float64
			vuln := 0
			for _, r := range rs {
				if caught(r) {
					k++
				}
				exp = append(exp, 100*r.Push.ExposedPeak)
				peak = append(peak, 100*r.PeakFailingShare)
				fis = append(fis, r.FailedInstSec)
				req = append(req, 100*r.ReqFailShare)
				vuln = r.Vulnerable * 100 / max(r.Fleet, 1)
			}
			name, breaks := "", ""
			if i == 0 {
				name, breaks = sc.incident+" (`"+sc.name+"`)", fmt.Sprintf("%d%% of fleet", vuln)
			}
			em, eh := meanCI(exp)
			pm, ph := meanCI(peak)
			fm, fh := meanCI(fis)
			rm, rh := meanCI(req)
			fmt.Fprintf(b, "| %s | %s | %s | %d of %d | %s | %s | %s | %s |\n", name, breaks, strings.ToUpper(s), k, len(rs),
				fmtCI(em, eh, "%", 0), fmtCI(pm, ph, "%", 0), fmtCI(fm, fh, "", 0), fmtCI(rm, rh, "%", 1))
		}
	}

	// Good pushes: false rollbacks and push delay.
	if gs := scenarios(runs, "good"); len(gs) > 0 {
		b.WriteString("\n## Good pushes through the fleet\n\nA good push should complete. Push delay is the time from the push to every instance having the file.\n\n")
		b.WriteString("| Strategy | Completed | Held or rolled back | Push delay p50 / max | Gate time p50 |\n|---|---|---|---|---|\n")
		for _, s := range strats {
			var done, stopped int
			var delay, gateMS []float64
			for _, sc := range gs {
				for _, r := range pick(runs, sc.name, s) {
					if r.Push.Status == "completed" {
						done++
						delay = append(delay, float64(r.Push.TimeToFull)/1000)
					} else {
						stopped++
					}
					if c, ok := r.Push.Checks["gate"]; ok {
						gateMS = append(gateMS, float64(c.DurationMS))
					}
				}
			}
			gt := "–"
			if len(gateMS) > 0 {
				gt = fmt.Sprintf("%.0f ms", quantile(gateMS, 0.5))
			}
			fmt.Fprintf(b, "| %s | %d of %d | %d | %.1f s / %.1f s | %s |\n", strings.ToUpper(s), done, done+stopped, stopped, quantile(delay, 0.5), quantile(delay, 1), gt)
		}
	}

	if len(good) > 0 {
		n := len(good)
		count := func(f func(goodItem) bool) int {
			k := 0
			for _, g := range good {
				if f(g) {
					k++
				}
			}
			return k
		}
		fmt.Fprintf(b, "\n## Good-push corpus (gate only)\n\n%d safe files, run through the checks without a fleet ([details](week5-good.md)).\n\n", n)
		b.WriteString("| Check | False holds | Rate (95% CI) |\n|---|---|---|\n")
		row := func(name string, k int) {
			lo, hi := wilson(k, n)
			fmt.Fprintf(b, "| %s | %d of %d | %.1f%% (%.1f to %.1f%%) |\n", name, k, n, 100*float64(k)/float64(n), 100*lo, 100*hi)
		}
		row("B1 schema", count(func(g goodItem) bool { return !g.SchemaOK }))
		row("Gate, diff only", count(func(g goodItem) bool { return g.Verdict["diff"] == "hold" }))
		row("Gate, sandbox only", count(func(g goodItem) bool { return g.Verdict["sandbox"] == "hold" }))
		row("Gate, both layers (B3)", count(func(g goodItem) bool { return g.Verdict["both"] == "hold" }))
		var cpu, ms []float64
		for _, g := range good {
			cpu = append(cpu, g.CPUSeconds)
			ms = append(ms, float64(g.MS["both"]))
		}
		cm, _ := meanCI(cpu)
		fmt.Fprintf(b, "\nGate cost: %.2f CPU-seconds per push on average, %.0f ms wall time at p50 and %.0f ms at p95.\n", cm, quantile(ms, 0.5), quantile(ms, 0.95))
	}

	// Where B2 depended on the layout.
	b.WriteString("\n## Where ring 0 luck decided B2\n\n")
	any := false
	for _, sc := range bad {
		rs := pick(runs, sc.name, "b2")
		var exp []string
		lo, hi := 101.0, -1.0
		for _, r := range rs {
			e := 100 * r.Push.ExposedPeak
			exp = append(exp, fmt.Sprintf("seed %d: %.0f%%", r.Seed, e))
			lo, hi = math.Min(lo, e), math.Max(hi, e)
		}
		if hi-lo >= 5 {
			any = true
			fmt.Fprintf(b, "- **%s**: B2 exposed between %.0f%% and %.0f%% of the fleet depending on the layout (%s).\n", sc.incident, lo, hi, strings.Join(exp, ", "))
		}
	}
	if !any {
		b.WriteString("No scenario's B2 exposure varied by more than 5 points across seeds.\n")
	}
	fmt.Fprintf(b, "\n![Failing instances over time](%s/timeline.svg)\n", chartDir)
}

func upper(ss []string) string {
	var out []string
	for _, s := range ss {
		out = append(out, strings.ToUpper(s))
	}
	return strings.Join(out, ", ")
}

func joinSeeds(seeds []int64) string {
	var out []string
	for _, s := range seeds {
		out = append(out, fmt.Sprint(s))
	}
	return strings.Join(out, ", ")
}

// --- charts ---
//
// Static SVGs for the README and GitHub, so no hover layer: values are
// direct-labelled instead. Colors are the first three slots of the
// reference categorical palette (validated all-pairs), on a fixed light
// surface so the chart reads the same in light and dark page themes.

const (
	surface   = "#fcfcfb"
	ink       = "#0b0b0b"
	ink2      = "#52514e"
	gridColor = "#e4e3df"
	font      = "font-family=\"system-ui, -apple-system, Segoe UI, sans-serif\""
)

var seriesColor = map[string]string{"b0": "#2a78d6", "b2": "#eb6834", "b3": "#1baf7a"}
var seriesName = map[string]string{"b0": "B0 push-all", "b2": "B2 staged + alarms", "b3": "B3 PushGate"}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}

// exposureChart: for each bad scenario, the share of the fleet that got the
// file under B0, B2 and B3 (mean over seeds, with the min to max range).
func exposureChart(path string, runs []run) {
	bad := scenarios(runs, "bad")
	strats := []string{"b0", "b2", "b3"}
	const (
		w, left, right, top = 760, 230, 60, 70
		barH, gap, groupGap = 8, 2, 16
	)
	groupH := len(strats)*(barH+gap) - gap
	h := top + len(bad)*(groupH+groupGap) + 40
	plotW := float64(w - left - right)
	x := func(pct float64) float64 { return float64(left) + plotW*pct/100 }

	var s strings.Builder
	fmt.Fprintf(&s, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" %s>`+"\n", w, h, w, h, font)
	fmt.Fprintf(&s, `<rect width="100%%" height="100%%" fill="%s"/>`+"\n", surface)
	fmt.Fprintf(&s, `<text x="16" y="26" font-size="15" font-weight="600" fill="%s">Share of the fleet that got the bad file</text>`+"\n", ink)
	fmt.Fprintf(&s, `<text x="16" y="44" font-size="12" fill="%s">Mean over fleet seeds; whisker shows the range. 0%% means the file was held before any instance got it.</text>`+"\n", ink2)
	// Legend.
	lx := 16.0
	for _, st := range strats {
		fmt.Fprintf(&s, `<rect x="%.0f" y="54" width="10" height="10" rx="2" fill="%s"/><text x="%.0f" y="63" font-size="12" fill="%s">%s</text>`+"\n", lx, seriesColor[st], lx+14, ink2, seriesName[st])
		lx += 14 + float64(len(seriesName[st]))*6.6 + 18
	}
	// Grid.
	for _, g := range []float64{0, 25, 50, 75, 100} {
		fmt.Fprintf(&s, `<line x1="%.1f" x2="%.1f" y1="%d" y2="%d" stroke="%s" stroke-width="1"/>`+"\n", x(g), x(g), top-4, h-34, gridColor)
		fmt.Fprintf(&s, `<text x="%.1f" y="%d" font-size="11" fill="%s" text-anchor="middle">%.0f%%</text>`+"\n", x(g), h-18, ink2, g)
	}
	y := top
	for _, sc := range bad {
		fmt.Fprintf(&s, `<text x="%d" y="%d" font-size="12" fill="%s" text-anchor="end">%s</text>`+"\n", left-10, y+groupH/2+4, ink, esc(shortName(sc)))
		for _, st := range strats {
			rs := pick(runs, sc.name, st)
			var vals []float64
			for _, r := range rs {
				vals = append(vals, 100*r.Push.ExposedPeak)
			}
			m, _ := meanCI(vals)
			lo, hi := quantile(vals, 0), quantile(vals, 1)
			wbar := x(m) - float64(left)
			if wbar > 0 {
				// Rounded at the data end only: a square-ended path anchored at the baseline.
				r := math.Min(4, wbar)
				fmt.Fprintf(&s, `<path d="M%d,%d h%.1f a%.1f,%.1f 0 0 1 %.1f,%.1f v%.1f a%.1f,%.1f 0 0 1 -%.1f,%.1f h-%.1f z" fill="%s"><title>%s, %s: %.0f%% (range %.0f to %.0f%%)</title></path>`+"\n",
					left, y, wbar-r, r, r, r, r, float64(barH)-2*r, r, r, r, r, wbar-r, seriesColor[st], esc(sc.incident), seriesName[st], m, lo, hi)
			} else {
				fmt.Fprintf(&s, `<line x1="%d" x2="%d" y1="%d" y2="%d" stroke="%s" stroke-width="2"/>`+"\n", left, left, y, y+barH, seriesColor[st])
			}
			if hi-lo > 0.5 {
				cy := float64(y) + float64(barH)/2
				fmt.Fprintf(&s, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="1.5"/>`+"\n", x(lo), x(hi), cy, cy, ink2)
				for _, e := range []float64{lo, hi} {
					fmt.Fprintf(&s, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="%s" stroke-width="1.5"/>`+"\n", x(e), x(e), cy-3, cy+3, ink2)
				}
			}
			label := fmt.Sprintf("%.0f%%", m)
			fmt.Fprintf(&s, `<text x="%.1f" y="%d" font-size="10" fill="%s">%s</text>`+"\n", math.Max(x(m), x(hi))+5, y+barH, ink2, label)
			y += barH + gap
		}
		y += groupGap - gap
	}
	s.WriteString("</svg>\n")
	if err := os.WriteFile(path, []byte(s.String()), 0o644); err != nil {
		log.Fatal(err)
	}
}

func shortName(sc scenario) string {
	names := map[string]string{
		"cloudflare-duplicate-rows":  "Cloudflare: duplicate rows",
		"cloudflare-partial-replica": "Cloudflare: partial duplicates",
		"crowdstrike-extra-field":    "CrowdStrike: 21st field",
		"crowdstrike-rare-request":   "CrowdStrike: rare request",
		"google-blank-quota":         "Google: blank quota (3 rows)",
		"google-single-row":          "Google: blank quota (1 row)",
		"feature-explosion":          "Growth to 210 features",
		"growth-past-v41":            "Growth to 130 (v41 only)",
		"truncated-file":             "Half-written file",
		"wrong-type-weight":          "Weight as a string",
	}
	if n, ok := names[sc.name]; ok {
		return n
	}
	return sc.name
}

// timelineChart: failing instances over time for the scenario and seed
// where B2 exposed the most, with B0 and B3 on the same seed.
func timelineChart(path string, runs []run) {
	var worst *run
	for i, r := range runs {
		if r.Strategy == "b2" && r.Kind == "bad" && (worst == nil || r.FailedInstSec > worst.FailedInstSec) {
			worst = &runs[i]
		}
	}
	if worst == nil {
		return
	}
	strats := []string{"b0", "b2", "b3"}
	series := map[string]run{}
	for _, r := range runs {
		if r.Scenario == worst.Scenario && r.Seed == worst.Seed {
			series[r.Strategy] = r
		}
	}
	const w, h, left, right, top, bottom = 760, 320, 56, 150, 64, 40
	pw, ph := float64(w-left-right), float64(h-top-bottom)
	maxY := float64(worst.Fleet)
	x := func(t float64) float64 { return float64(left) + pw*t/worst.WindowS }
	y := func(v float64) float64 { return float64(top) + ph*(1-v/maxY) }

	var s strings.Builder
	fmt.Fprintf(&s, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" %s>`+"\n", w, h, w, h, font)
	fmt.Fprintf(&s, `<rect width="100%%" height="100%%" fill="%s"/>`+"\n", surface)
	fmt.Fprintf(&s, `<text x="16" y="26" font-size="15" font-weight="600" fill="%s">Failing %s instances after the push: %s</text>`+"\n", ink, worst.Consumer, esc(worst.Incident))
	fmt.Fprintf(&s, `<text x="16" y="44" font-size="12" fill="%s">Fleet seed %d, the layout where B2 did worst. %d instances; crashed, restarting or not serving counts as failing.</text>`+"\n", ink2, worst.Seed, worst.Fleet)
	for _, f := range []float64{0, 0.25, 0.5, 0.75, 1} {
		v := f * maxY
		fmt.Fprintf(&s, `<line x1="%d" x2="%.0f" y1="%.1f" y2="%.1f" stroke="%s"/>`+"\n", left, float64(left)+pw, y(v), y(v), gridColor)
		fmt.Fprintf(&s, `<text x="%d" y="%.1f" font-size="11" fill="%s" text-anchor="end">%.0f</text>`+"\n", left-6, y(v)+4, ink2, v)
	}
	for t := 0.0; t <= worst.WindowS+0.01; t += 5 {
		fmt.Fprintf(&s, `<text x="%.1f" y="%d" font-size="11" fill="%s" text-anchor="middle">%.0f s</text>`+"\n", x(t), h-bottom+18, ink2, t)
	}
	for _, st := range strats {
		r, ok := series[st]
		if !ok {
			continue
		}
		var pts []string
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(0), y(0)))
		last := 0.0
		for _, p := range r.Samples {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(p.At), y(float64(p.Failing))))
			last = float64(p.Failing)
		}
		end := worst.WindowS
		if len(r.Samples) == 0 {
			// Held before delivery: nothing ever failed.
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(end), y(0)))
		}
		fmt.Fprintf(&s, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round"/>`+"\n", strings.Join(pts, " "), seriesColor[st])
		lastX := x(end)
		if len(r.Samples) > 0 {
			lastX = x(r.Samples[len(r.Samples)-1].At)
		}
		label := seriesName[st]
		if st == "b3" && r.Push.Exposed == 0 {
			label += ": held, 0"
		}
		fmt.Fprintf(&s, `<circle cx="%.1f" cy="%.1f" r="4" fill="%s" stroke="%s" stroke-width="2"/>`+"\n", lastX, y(last), seriesColor[st], surface)
		fmt.Fprintf(&s, `<text x="%.1f" y="%.1f" font-size="12" fill="%s">%s</text>`+"\n", float64(left)+pw+10, labelY(st, y(last)), ink, esc(label))
	}
	s.WriteString("</svg>\n")
	if err := os.WriteFile(path, []byte(s.String()), 0o644); err != nil {
		log.Fatal(err)
	}
}

// labelY nudges end labels apart when lines finish at the same height. It
// moves a label up, so labels on a line at zero stay clear of the x axis.
var usedY []float64

func labelY(_ string, want float64) float64 {
	y := want + 4
	for moved := true; moved; {
		moved = false
		for _, u := range usedY {
			if math.Abs(u-y) < 14 {
				y, moved = u-14, true
			}
		}
	}
	usedY = append(usedY, y)
	return y
}
