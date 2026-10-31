// Command pgctl inspects the simulated fleet and delivers config files to it.
//
//	pgctl status                         counts by consumer, state, version, region
//	pgctl push -consumer bot-proxy -file f.json [-region r] [-version 41] [-percent 10]
//	pgctl wait -timeout 60s              wait until every instance is up
//	pgctl crashes                        list instances that crashed, with the panic
//
// In week 1, `pgctl push` with no filters is baseline B0 (push-all): the file
// is written to every matching instance at once, with no checks.
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pgctl [-registry URL|path] status|push|wait|crashes [flags]")
	os.Exit(2)
}

func main() {
	reg := flag.String("registry", "http://127.0.0.1:19000/registry", "supervisor URL or path to state/registry.json")
	flag.Parse()
	if flag.NArg() < 1 {
		usage()
	}
	args := flag.Args()[1:]
	var err error
	switch flag.Arg(0) {
	case "status":
		err = status(*reg)
	case "push":
		err = push(*reg, args)
	case "wait":
		err = wait(*reg, args)
	case "crashes":
		err = crashes(*reg)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgctl:", err)
		os.Exit(1)
	}
}

func status(src string) error {
	s, err := registry.Fetch(src)
	if err != nil {
		return err
	}
	type row struct {
		total, up int
		configs   map[string]int
	}
	byKey := map[string]*row{}
	for _, i := range s.Instances {
		k := fmt.Sprintf("%s\tv%d\t%s", i.Consumer, i.Version, i.Region)
		r := byKey[k]
		if r == nil {
			r = &row{configs: map[string]int{}}
			byKey[k] = r
		}
		r.total++
		if i.State == registry.StateUp {
			r.up++
		}
		r.configs[i.ConfigVersion]++
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CONSUMER\tVERSION\tREGION\tUP\tTOTAL\tSERVING CONFIG")
	up, total := 0, 0
	for _, k := range registry.SortedKeys(byKey) {
		r := byKey[k]
		up += r.up
		total += r.total
		var cs []string
		for _, c := range registry.SortedKeys(r.configs) {
			name := c
			if name == "" {
				name = "(none)"
			}
			cs = append(cs, fmt.Sprintf("%s×%d", name, r.configs[c]))
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\n", k, r.up, r.total, strings.Join(cs, " "))
	}
	tw.Flush()
	fmt.Printf("\nfleet: %d of %d instances up (%.1f%%)\n", up, total, 100*float64(up)/float64(max(total, 1)))
	return nil
}

func push(src string, args []string) error {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	consumer := fs.String("consumer", "", "consumer type (bot-proxy or go-policy)")
	file := fs.String("file", "", "config file to deliver")
	region := fs.String("region", "", "only this region")
	version := fs.Int("version", 0, "only this release")
	percent := fs.Float64("percent", 100, "only this share of matching instances, in stable ring order (same rings as rollout)")
	dry := fs.Bool("dry-run", false, "list targets without writing")
	_ = fs.Parse(args)
	if *consumer == "" || *file == "" {
		return fmt.Errorf("push needs -consumer and -file")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	s, err := registry.Fetch(src)
	if err != nil {
		return err
	}
	var matching []registry.Instance
	for _, i := range s.Instances {
		if i.Consumer != *consumer || (*region != "" && i.Region != *region) || (*version != 0 && i.Version != *version) {
			continue
		}
		matching = append(matching, i)
	}
	targets := matching
	if *percent < 100 {
		targets = registry.RingMembers(matching, *percent)
	}
	if len(targets) == 0 {
		return fmt.Errorf("no instances match")
	}
	start := time.Now()
	if !*dry {
		for _, t := range targets {
			if err := registry.WriteAtomic(t.ConfigPath, data); err != nil {
				return fmt.Errorf("%s: %w", t.ID, err)
			}
		}
	}
	verb := "pushed"
	if *dry {
		verb = "would push"
	}
	fmt.Printf("%s %s to %d instances in %s\n", verb, *file, len(targets), time.Since(start).Round(time.Millisecond))
	return nil
}

func wait(src string, args []string) error {
	fs := flag.NewFlagSet("wait", flag.ExitOnError)
	timeout := fs.Duration("timeout", 60*time.Second, "give up after this long")
	_ = fs.Parse(args)
	deadline := time.Now().Add(*timeout)
	for {
		s, err := registry.Fetch(src)
		if err == nil && len(s.Instances) > 0 {
			up := 0
			for _, i := range s.Instances {
				if i.State == registry.StateUp {
					up++
				}
			}
			if up == len(s.Instances) {
				fmt.Printf("all %d instances up\n", up)
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out: %d of %d up", up, len(s.Instances))
			}
		} else if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for supervisor: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

var threadID = regexp.MustCompile(`\(\d+\)`)

// normalizePanic removes per-instance details (instance id, thread id) so
// identical crashes across the fleet group into one line.
func normalizePanic(i registry.Instance) string {
	m := strings.ReplaceAll(i.LastPanic, i.ID, "<id>")
	m = threadID.ReplaceAllString(m, "")
	return strings.Join(strings.Fields(m), " ")
}

func crashes(src string) error {
	s, err := registry.Fetch(src)
	if err != nil {
		return err
	}
	var hit []registry.Instance
	for _, i := range s.Instances {
		if i.Restarts > 0 {
			hit = append(hit, i)
		}
	}
	sort.Slice(hit, func(a, b int) bool { return hit[a].ID < hit[b].ID })
	if len(hit) == 0 {
		fmt.Println("no crashes")
		return nil
	}
	// Group identical panics so 400 crash-looping instances print as one line.
	groups := map[string][]registry.Instance{}
	for _, i := range hit {
		k := fmt.Sprintf("%s v%d | %s", i.Consumer, i.Version, normalizePanic(i))
		groups[k] = append(groups[k], i)
	}
	for _, k := range registry.SortedKeys(groups) {
		g := groups[k]
		restarts := 0
		for _, i := range g {
			restarts += i.Restarts
		}
		fmt.Printf("%4d instances, %5d restarts  %s\n", len(g), restarts, k)
	}
	return nil
}
