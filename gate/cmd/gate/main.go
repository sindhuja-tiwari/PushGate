// Command gate runs PushGate's pre-exposure check on one candidate file and
// prints why it would pass or hold. It never delivers anything.
//
//	gate check -consumer bot-proxy -file f.json [-lkg old.json] [-layers diff,sandbox] [-json]
//
// Without -lkg it uses the last known good file from the rollout store. The
// sandbox runs every consumer release that is live in the fleet registry,
// replaying requests from a loadgen -record sample.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sindhuja-tiwari/pushgate/gate"
	"github.com/sindhuja-tiwari/pushgate/rollout"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "check" {
		fmt.Fprintln(os.Stderr, "usage: gate check -consumer C -file F [flags]   (gate check -h for flags)")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	consumer := fs.String("consumer", "", "consumer type (bot-proxy or go-policy)")
	file := fs.String("file", "", "candidate config file")
	lkgFile := fs.String("lkg", "", "last known good file (default: the rollout store's)")
	reg := fs.String("registry", "http://127.0.0.1:19000/registry", "fleet supervisor URL or registry.json path")
	stateDir := fs.String("state", "state/rollout", "rollout state directory (for last known good)")
	binDir := fs.String("bin", "bin", "directory holding bin/<consumer>/v<NN>")
	traffic := fs.String("traffic", "state/traffic.txt", "loadgen -record sample to replay")
	replay := fs.Int("replay", 1000, "requests to replay per release")
	layers := fs.String("layers", "diff,sandbox", "which layers to run")
	jsonOut := fs.Bool("json", false, "print the report as JSON")
	_ = fs.Parse(os.Args[2:])
	if *consumer == "" || *file == "" {
		fmt.Fprintln(os.Stderr, "gate check needs -consumer and -file")
		os.Exit(2)
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		fail(err)
	}
	cfg := gate.DefaultConfig(rollout.SimFleet{Registry: *reg}, rollout.Store{Dir: *stateDir})
	cfg.BinDir, cfg.TrafficFile, cfg.Replay = *binDir, *traffic, *replay
	cfg.Diff = strings.Contains(*layers, "diff")
	cfg.Sandbox = strings.Contains(*layers, "sandbox")
	if *lkgFile != "" {
		if cfg.LKG, err = os.ReadFile(*lkgFile); err != nil {
			fail(err)
		}
	}
	r, err := gate.New(cfg).Check(context.Background(), *consumer, data)
	if r == nil {
		fail(err)
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Print(r.Text())
		fmt.Println("\n" + r.Summary())
	}
	if errors.Is(err, gate.ErrHeld) {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gate:", err)
	os.Exit(2)
}
