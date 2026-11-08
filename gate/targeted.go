package gate

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
)

// maxTargeted caps the requests built for changed entries, so a file that
// changes everything still replays in well under the time budget.
const maxTargeted = 200

// targeted builds requests that exercise every entry the candidate adds or
// changes relative to the last known good file. A sampled replay reaches a
// changed rule only if the sample happens to hold a request it matches, and
// a rule pinned to a rare request shape is easy to miss (week 5 caught one
// in 1 of 5 runs). These requests reach each changed rule directly.
//
// bot-proxy: one /score request that matches each new or changed feature
// rule, with wildcard slots filled from a sampled request.
// go-policy: one /check request for each new or changed project and API.
func targeted(consumer string, cand, lkg []byte, sample []string) []string {
	c, err := parse(cand)
	if err != nil {
		return nil
	}
	var l *doc
	if lkg != nil {
		if l, err = parse(lkg); err != nil {
			l = nil
		}
	}
	var out []string
	switch consumer {
	case "bot-proxy":
		old := map[string]string{}
		if l != nil {
			for _, f := range l.Features {
				old[f.Name] = strings.Join(f.Fields, "\x00")
			}
		}
		rng := rand.New(rand.NewSource(1))
		for _, f := range c.Features {
			if prev, ok := old[f.Name]; ok && prev == strings.Join(f.Fields, "\x00") {
				continue
			}
			out = append(out, matchFeature(f.Fields, sample, rng))
			if len(out) == maxTargeted {
				break
			}
		}
	case "go-policy":
		old := map[string]bool{}
		if l != nil {
			for _, p := range l.Policies {
				old[policyKey(p)] = true
			}
		}
		seen := map[string]bool{}
		for _, p := range c.Policies {
			k := p.Project + "/" + p.API
			if old[policyKey(p)] || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, fmt.Sprintf("/check?project=%s&api=%s", url.QueryEscape(p.Project), url.QueryEscape(p.API)))
			if len(out) == maxTargeted {
				break
			}
		}
	}
	return out
}

// policyKey identifies a policy with everything the consumer reads.
func policyKey(p policy) string {
	q := "nil"
	if p.Quota != nil {
		q = fmt.Sprintf("%d/%d", p.Quota.LimitPerMin, p.Quota.Burst)
	}
	return fmt.Sprintf("%s/%s/%v/%v/%s", p.Project, p.API, p.Allow, p.EnforceQuota, q)
}

// matchFeature returns a /score request whose slot i equals fields[i]
// wherever the rule is not a wildcard. Wildcard slots, and slots the rule
// does not mention, take their values from a random sampled request, so
// the request looks like real traffic everywhere else.
func matchFeature(fields []string, sample []string, rng *rand.Rand) string {
	base := url.Values{}
	if len(sample) > 0 {
		if _, q, ok := strings.Cut(sample[rng.Intn(len(sample))], "?"); ok {
			base, _ = url.ParseQuery(q)
		}
	}
	slots := len(fields)
	for k := range base {
		var i int
		if _, err := fmt.Sscanf(k, "f%d", &i); err == nil && i+1 > slots {
			slots = i + 1
		}
	}
	var b strings.Builder
	b.WriteString("/score?")
	for i := 0; i < slots; i++ {
		v := base.Get(fmt.Sprintf("f%d", i))
		if i < len(fields) && fields[i] != "*" {
			v = fields[i]
		}
		if v == "" {
			v = "x"
		}
		if i > 0 {
			b.WriteByte('&')
		}
		fmt.Fprintf(&b, "f%d=%s", i, url.QueryEscape(v))
	}
	return b.String()
}
