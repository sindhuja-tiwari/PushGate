package registry

import (
	"fmt"
	"testing"
)

func TestApportionExact(t *testing.T) {
	got, err := apportion(map[string]int{"41": 5, "42": 15, "43": 30, "44": 50}, 100)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for _, v := range got {
		counts[v]++
	}
	want := map[int]int{41: 5, 42: 15, 43: 30, 44: 50}
	for v, n := range want {
		if counts[v] != n {
			t.Errorf("v%d: got %d want %d", v, counts[v], n)
		}
	}
}

func TestApportionRemainders(t *testing.T) {
	got, _ := apportion(map[string]int{"41": 1, "42": 1, "43": 1}, 25)
	if len(got) != 25 {
		t.Fatalf("got %d slots, want 25", len(got))
	}
}

func TestPlanDeterministic(t *testing.T) {
	c := &FleetConfig{StateDir: "s", BasePort: 1000, Seed: 3, Regions: []string{"a", "b"},
		Consumers: []ConsumerConfig{{Name: "bot-proxy", PerRegion: 10, ConfigFile: "f.json", Versions: map[string]int{"41": 1, "44": 1}}}}
	p1, _ := Plan(c)
	p2, _ := Plan(c)
	if len(p1) != 20 {
		t.Fatalf("got %d instances", len(p1))
	}
	for i := range p1 {
		if p1[i].ID != p2[i].ID || p1[i].Version != p2[i].Version || p1[i].Port != p2[i].Port {
			t.Fatalf("plan differs at %d", i)
		}
	}
}

func TestRingMembersNested(t *testing.T) {
	var insts []Instance
	for i := 0; i < 400; i++ {
		insts = append(insts, Instance{ID: fmt.Sprintf("bp-r-%03d", i)})
	}
	r1, r10 := RingMembers(insts, 1), RingMembers(insts, 10)
	if len(r1) != 4 || len(r10) != 40 {
		t.Fatalf("sizes %d %d", len(r1), len(r10))
	}
	in10 := map[string]bool{}
	for _, i := range r10 {
		in10[i.ID] = true
	}
	for _, i := range r1 {
		if !in10[i.ID] {
			t.Fatalf("%s in 1%% ring but not 10%%", i.ID)
		}
	}
	if got := RingMembers(insts[:10], 1); len(got) != 1 {
		t.Fatalf("small fleet ring should have 1 member, got %d", len(got))
	}
}
