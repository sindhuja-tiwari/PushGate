// Command rollout delivers config files to the simulated fleet under one of
// the PushBench baseline strategies, and holds the kill switch.
//
//	rollout push -strategy b2 -consumer bot-proxy -file f.json [-bake 20s] [-rings 1,10,100]
//	rollout set-lkg -consumer bot-proxy -file f.json   mark a file as last known good
//	rollout rollback -consumer bot-proxy               deliver last known good everywhere
//	rollout freeze -consumer bot-proxy -reason "..."   stop all pushes for this file type
//	rollout unfreeze -consumer bot-proxy
//	rollout status                                     freezes and last known good, per consumer
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/sindhuja-tiwari/pushgate/rollout"
)

var consumers = []string{"bot-proxy", "go-policy"}

func main() {
	reg := flag.String("registry", "http://127.0.0.1:19000/registry", "fleet supervisor URL or registry.json path")
	stateDir := flag.String("state", "state/rollout", "rollout state directory")
	loadgen := flag.String("loadgen", "", "loadgen -listen URL for the error-rate alarm (e.g. http://127.0.0.1:19100)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: rollout [-registry URL] [-state DIR] [-loadgen URL] push|set-lkg|rollback|freeze|unfreeze|status [flags]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}
	rn := &rollout.Runner{Fleet: rollout.SimFleet{Registry: *reg}, Store: rollout.Store{Dir: *stateDir}}
	if *loadgen != "" {
		rn.Errors = rollout.LoadgenErrors{URL: *loadgen}
	}
	args := flag.Args()[1:]
	var err error
	switch flag.Arg(0) {
	case "push":
		err = push(rn, args)
	case "set-lkg":
		err = setLKG(rn, args)
	case "rollback":
		fs := flag.NewFlagSet("rollback", flag.ExitOnError)
		c := fs.String("consumer", "", "consumer type")
		_ = fs.Parse(args)
		var n int
		if n, err = rn.Rollback(*c); err == nil {
			fmt.Printf("delivered last known good to %d %s instances\n", n, *c)
		}
	case "freeze", "unfreeze":
		fs := flag.NewFlagSet(flag.Arg(0), flag.ExitOnError)
		c := fs.String("consumer", "", "consumer type")
		reason := fs.String("reason", "manual freeze", "why (shown to anyone whose push is stopped)")
		_ = fs.Parse(args)
		if *c == "" {
			err = fmt.Errorf("%s needs -consumer", flag.Arg(0))
			break
		}
		if flag.Arg(0) == "unfreeze" {
			*reason = ""
		}
		if err = rn.Store.SetFreeze(*c, *reason); err == nil {
			fmt.Printf("%s %sd\n", *c, flag.Arg(0))
		}
	case "status":
		err = status(rn)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rollout:", err)
		os.Exit(1)
	}
}

func push(rn *rollout.Runner, args []string) error {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	strategy := fs.String("strategy", "b2", "b0 (push-all), b1 (schema) or b2 (staged with alarms)")
	consumer := fs.String("consumer", "", "consumer type (bot-proxy or go-policy)")
	file := fs.String("file", "", "config file to push")
	bake := fs.Duration("bake", 20*time.Second, "b2: how long each ring must stay healthy")
	rings := fs.String("rings", "", "b2: cumulative ring percentages, e.g. 1,10,100")
	jsonOut := fs.Bool("json", false, "print the push record as JSON instead of a timeline")
	_ = fs.Parse(args)
	if *consumer == "" || *file == "" {
		return fmt.Errorf("push needs -consumer and -file")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	s, err := rollout.ByName(*strategy, *bake)
	if err != nil {
		return err
	}
	if *rings != "" {
		if s.Rings, err = rollout.ParseRings(*rings); err != nil {
			return err
		}
	}
	if !*jsonOut {
		rn.Log = func(r *rollout.Result, e rollout.Event) {
			line := fmt.Sprintf("%7.1fs  %-18s", time.Duration(e.At).Seconds(), e.Kind)
			if e.Ring > 0 {
				line += fmt.Sprintf(" ring %g%%", e.Ring)
			}
			if e.Count > 0 {
				line += fmt.Sprintf(" (%d)", e.Count)
			}
			if e.Detail != "" {
				line += "  " + e.Detail
			}
			fmt.Println(line)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	r, err := rn.Push(ctx, s, *consumer, data)
	if err != nil {
		return err
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("\n%s %s: %s. %d of %d instances got the file (%.1f%%).", r.Strategy, r.Consumer, r.Status, r.Exposed, r.Fleet, 100*r.ExposedPeak)
	if r.Reason != "" {
		fmt.Printf("\n  %s", r.Reason)
	}
	fmt.Printf("\n  record: %s/pushes/%s.json\n", rn.Store.Dir, r.ID)
	return nil
}

func setLKG(rn *rollout.Runner, args []string) error {
	fs := flag.NewFlagSet("set-lkg", flag.ExitOnError)
	consumer := fs.String("consumer", "", "consumer type")
	file := fs.String("file", "", "file to mark as last known good")
	_ = fs.Parse(args)
	if *consumer == "" || *file == "" {
		return fmt.Errorf("set-lkg needs -consumer and -file")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	if err := rn.Store.SetLKG(*consumer, data, "manual"); err != nil {
		return err
	}
	fmt.Printf("%s last known good set from %s\n", *consumer, *file)
	return nil
}

func status(rn *rollout.Runner) error {
	for _, c := range consumers {
		_, meta, err := rn.Store.LKG(c)
		if err != nil {
			return err
		}
		f, err := rn.Store.Frozen(c)
		if err != nil {
			return err
		}
		lkg := "none"
		if meta != nil {
			lkg = fmt.Sprintf("%s (since %s, push %s)", meta.ConfigVersion, meta.Since.Format(time.RFC3339), meta.PushID)
		}
		frozen := "no"
		if f != nil {
			frozen = fmt.Sprintf("yes, since %s: %s", f.Since.Format(time.RFC3339), f.Reason)
		}
		fmt.Printf("%s\n  last known good: %s\n  frozen: %s\n", c, lkg, frozen)
	}
	return nil
}
