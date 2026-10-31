package registry

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
)

// Plan expands a fleet layout into concrete instances. The result is
// deterministic for a given layout and seed: the same ids get the same
// region, version and port every run.
func Plan(c *FleetConfig) ([]Instance, error) {
	rng := rand.New(rand.NewSource(c.Seed))
	var out []Instance
	port := c.BasePort
	for _, cc := range c.Consumers {
		for _, region := range c.Regions {
			versions, err := apportion(cc.Versions, cc.PerRegion)
			if err != nil {
				return nil, fmt.Errorf("consumer %s: %w", cc.Name, err)
			}
			rng.Shuffle(len(versions), func(i, j int) { versions[i], versions[j] = versions[j], versions[i] })
			for i, v := range versions {
				id := fmt.Sprintf("%s-%s-%03d", short(cc.Name), region, i)
				out = append(out, Instance{
					ID:         id,
					Consumer:   cc.Name,
					Region:     region,
					Version:    v,
					Port:       port,
					ConfigPath: filepath.Join(c.StateDir, "instances", id, cc.ConfigFile),
					State:      StateStarting,
				})
				port++
			}
		}
	}
	return out, nil
}

func short(name string) string {
	switch name {
	case "bot-proxy":
		return "bp"
	case "go-policy":
		return "gp"
	}
	return name
}

// apportion splits n slots across versions by weight using the largest
// remainder method, so a 5% weight on 100 slots is exactly 5 instances.
func apportion(weights map[string]int, n int) ([]int, error) {
	type share struct {
		version int
		whole   int
		rem     float64
	}
	total := 0
	for _, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("negative weight")
		}
		total += w
	}
	if total == 0 {
		return nil, fmt.Errorf("version weights sum to zero")
	}
	var shares []share
	used := 0
	for _, k := range SortedKeys(weights) {
		v, err := strconv.Atoi(k)
		if err != nil {
			return nil, fmt.Errorf("version %q is not a number", k)
		}
		exact := float64(weights[k]) * float64(n) / float64(total)
		s := share{version: v, whole: int(exact), rem: exact - float64(int(exact))}
		used += s.whole
		shares = append(shares, s)
	}
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].rem > shares[j].rem })
	for i := 0; used < n; i++ {
		shares[i%len(shares)].whole++
		used++
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].version < shares[j].version })
	var out []int
	for _, s := range shares {
		for i := 0; i < s.whole; i++ {
			out = append(out, s.version)
		}
	}
	return out, nil
}

// ringHash places an instance on a stable ring. The same id always gets the
// same position, so a 1% ring is always a subset of the 10% ring.
func ringHash(id string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(id))
	return h.Sum32()
}

// RingMembers returns the first pct percent of instances in stable ring
// order, rounded up and never fewer than one (if there are any instances).
func RingMembers(insts []Instance, pct float64) []Instance {
	if len(insts) == 0 || pct <= 0 {
		return nil
	}
	sorted := append([]Instance(nil), insts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		hi, hj := ringHash(sorted[i].ID), ringHash(sorted[j].ID)
		if hi != hj {
			return hi < hj
		}
		return sorted[i].ID < sorted[j].ID
	})
	n := int(math.Ceil(pct / 100 * float64(len(sorted))))
	if n < 1 {
		n = 1
	}
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}
