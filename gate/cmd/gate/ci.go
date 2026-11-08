package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/sindhuja-tiwari/pushgate/gate"
)

// Spec is pushgate.json: which files in the repository are config pushes,
// and which consumer releases load them.
type Spec struct {
	Consumers []ConsumerSpec `json:"consumers"`
}

type ConsumerSpec struct {
	Name string `json:"name"`
	// Files are the config files this consumer loads, as path globs
	// relative to the repository root ("config/*.json"; "**" matches any
	// number of directories).
	Files    []string      `json:"files"`
	Releases []ReleaseSpec `json:"releases"`
	// Traffic is a sample of requests to replay, one per line: a path such
	// as /score?f0=chrome, optionally after "<consumer>\t".
	Traffic string   `json:"traffic"`
	Replay  int      `json:"replay"`
	Health  string   `json:"health"`
	Answer  string   `json:"answer"`
	Format  string   `json:"format"`
	Rows    string   `json:"rows"`
	Key     []string `json:"key"`
}

type ReleaseSpec struct {
	Version   string   `json:"version"`
	Instances int      `json:"instances"`
	Command   []string `json:"command"`
}

func ci(args []string) {
	fs := flag.NewFlagSet("ci", flag.ExitOnError)
	specPath := fs.String("spec", "pushgate.json", "the spec: config files and the consumer releases that load them")
	base := fs.String("base", "origin/main", "git ref holding the last known good version of each file")
	files := fs.String("files", "", "comma-separated files to check (default: files changed since -base that the spec covers)")
	summary := fs.String("summary", os.Getenv("GITHUB_STEP_SUMMARY"), "append a Markdown report to this file (GitHub sets GITHUB_STEP_SUMMARY)")
	only := fs.String("consumer", "", "check -files as this consumer from the spec, whatever their paths (for local runs and tests)")
	lkgFile := fs.String("lkg", "", "with -consumer: last known good file to compare against, instead of the version at -base")
	annotate := fs.Bool("annotate", os.Getenv("GITHUB_ACTIONS") == "true", "print GitHub annotations for holds and warnings")
	_ = fs.Parse(args)

	raw, err := os.ReadFile(*specPath)
	if err != nil {
		fail(err)
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		fail(fmt.Errorf("%s: %w", *specPath, err))
	}

	var candidates []string
	if *files != "" {
		candidates = strings.Split(*files, ",")
	} else {
		out, err := exec.Command("git", "diff", "--name-only", "--diff-filter=AM", *base+"...HEAD").Output()
		if err != nil {
			fail(fmt.Errorf("git diff against %s: %w (fetch the base branch, e.g. actions/checkout with fetch-depth: 0)", *base, err))
		}
		candidates = strings.Fields(string(out))
	}

	var md strings.Builder
	held, checked := 0, 0
	for _, f := range candidates {
		cs := spec.match(f)
		if *only != "" {
			cs = spec.named(*only)
			if cs == nil {
				fail(fmt.Errorf("no consumer %q in %s", *only, *specPath))
			}
		}
		if cs == nil {
			continue
		}
		checked++
		data, err := os.ReadFile(f)
		if err != nil {
			fail(err)
		}
		cfg, err := cs.config()
		if err != nil {
			fail(fmt.Errorf("%s: %w", cs.Name, err))
		}
		if *lkgFile != "" {
			if cfg.LKG, err = os.ReadFile(*lkgFile); err != nil {
				fail(err)
			}
		} else if lkg, err := exec.Command("git", "show", *base+":"+f).Output(); err == nil {
			cfg.LKG = lkg
		}
		r, err := gate.New(cfg).Check(context.Background(), cs.Name, data)
		if r == nil {
			fail(err)
		}
		fmt.Printf("== %s\n%s\n%s\n\n", f, r.Text(), r.Summary())
		md.WriteString(r.Markdown(f))
		if errors.Is(err, gate.ErrHeld) {
			held++
		}
		if *annotate {
			for _, x := range r.Findings {
				if x.Severity == gate.Warn {
					fmt.Printf("::warning file=%s,title=PushGate %s/%s::%s\n", f, x.Layer, x.Check, escape(x.Detail))
				}
			}
			if r.Verdict == "hold" {
				fmt.Printf("::error file=%s,title=PushGate held this file::%s\n", f, escape(r.Summary()))
			}
		}
	}
	if checked == 0 {
		fmt.Println("PushGate: no changed file is covered by", *specPath)
		md.WriteString("PushGate: no changed config file to check.\n")
	}
	if *summary != "" {
		if fh, err := os.OpenFile(*summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = fh.WriteString(md.String())
			_ = fh.Close()
		}
	}
	fmt.Printf("PushGate: %d file(s) checked, %d held\n", checked, held)
	if held > 0 {
		os.Exit(1)
	}
}

// escape keeps a message on one annotation line.
func escape(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func (s Spec) named(name string) *ConsumerSpec {
	for i := range s.Consumers {
		if s.Consumers[i].Name == name {
			return &s.Consumers[i]
		}
	}
	return nil
}

func (s Spec) match(file string) *ConsumerSpec {
	for i := range s.Consumers {
		for _, g := range s.Consumers[i].Files {
			if globMatch(g, file) {
				return &s.Consumers[i]
			}
		}
	}
	return nil
}

// globMatch is path.Match plus "**" for any number of directories.
func globMatch(glob, name string) bool {
	if !strings.Contains(glob, "**") {
		ok, _ := path.Match(glob, name)
		return ok
	}
	pre, rest, _ := strings.Cut(glob, "**")
	if !strings.HasPrefix(name, pre) {
		return false
	}
	rest = strings.TrimPrefix(rest, "/")
	tail := strings.TrimPrefix(name, pre)
	for {
		if globMatch(rest, tail) {
			return true
		}
		i := strings.Index(tail, "/")
		if i < 0 {
			return false
		}
		tail = tail[i+1:]
	}
}

func (cs *ConsumerSpec) config() (gate.Config, error) {
	if len(cs.Releases) == 0 {
		return gate.Config{}, errors.New("no releases listed")
	}
	cfg := gate.Config{
		Diff: true, Sandbox: true, Budgets: gate.DefaultBudgets(),
		TrafficFile: cs.Traffic, Replay: cs.Replay,
		Health: cs.Health, Answer: cs.Answer, Format: cs.Format, Rows: cs.Rows, Key: cs.Key,
	}
	if cfg.Replay == 0 {
		cfg.Replay = 1000
	}
	for i, r := range cs.Releases {
		if len(r.Command) == 0 {
			return cfg, fmt.Errorf("release %s has no command", r.Version)
		}
		v, err := strconv.Atoi(strings.TrimLeft(r.Version, "vV"))
		if err != nil {
			v = i + 1
		}
		n := r.Instances
		if n == 0 {
			n = 1
		}
		cfg.Releases = append(cfg.Releases, gate.Release{Version: v, Instances: n, Command: r.Command})
	}
	return cfg, nil
}
