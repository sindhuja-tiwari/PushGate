package registry

import "testing"

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
