package main

import "testing"

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		glob, name string
		want       bool
	}{
		{"config/*.json", "config/limits.json", true},
		{"config/*.json", "config/sub/limits.json", false},
		{"config/**/*.json", "config/limits.json", true},
		{"config/**/*.json", "config/a/b/limits.json", true},
		{"**/features.json", "deploy/eu/features.json", true},
		{"config/**", "other/limits.json", false},
	}
	for _, c := range cases {
		if got := globMatch(c.glob, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.glob, c.name, got, c.want)
		}
	}
}

func TestSpecConfig(t *testing.T) {
	cs := ConsumerSpec{Name: "svc", Releases: []ReleaseSpec{{Version: "v7", Command: []string{"svc", "{port}"}}, {Version: "canary", Command: []string{"svc"}}}}
	cfg, err := cs.config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Releases[0].Version != 7 || cfg.Releases[0].Instances != 1 || cfg.Releases[1].Version != 2 || cfg.Replay != 1000 {
		t.Fatalf("unexpected config: %+v", cfg.Releases)
	}
	if _, err := (&ConsumerSpec{Name: "x"}).config(); err == nil {
		t.Fatal("a consumer with no releases should be an error")
	}
}
