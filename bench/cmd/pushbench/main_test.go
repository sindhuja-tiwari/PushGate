package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	r := &Run{Fleet: 100, Samples: []Sample{{0.5, 0}, {1, 4}, {1.5, 4}, {2, 0}, {2.5, 0}}}
	summarize(r, 500*time.Millisecond)
	if r.PeakFailing != 4 || r.PeakFailingShare != 0.04 {
		t.Fatalf("peak: %d %.2f", r.PeakFailing, r.PeakFailingShare)
	}
	if r.FailedInstSec != 4 {
		t.Fatalf("failed instance-seconds: got %.1f want 4", r.FailedInstSec)
	}
	if r.RecoveredAtS == nil || *r.RecoveredAtS != 2 {
		t.Fatalf("recovered: %v", r.RecoveredAtS)
	}
}

func TestSummarizeStillFailing(t *testing.T) {
	r := &Run{Fleet: 10, Samples: []Sample{{0.5, 10}, {1, 10}}}
	summarize(r, 500*time.Millisecond)
	if r.RecoveredAtS != nil {
		t.Fatal("a run failing at the end of the window has not recovered")
	}
}

func TestSummarizeNeverFailed(t *testing.T) {
	r := &Run{Fleet: 10, Samples: []Sample{{0.5, 0}, {1, 0}}}
	summarize(r, 500*time.Millisecond)
	if r.RecoveredAtS == nil || *r.RecoveredAtS != 0 {
		t.Fatal("a run with no failures should report 0")
	}
}

func TestScenarioFilesParse(t *testing.T) {
	sc, err := loadScenarios(filepath.Join("..", "..", "scenarios", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sc) < 3 {
		t.Fatalf("want at least 3 scenarios, got %d", len(sc))
	}
	for _, s := range sc {
		if s.Consumer == "" || len(s.Good) == 0 || len(s.Bad) == 0 || len(s.Breaks) == 0 {
			t.Errorf("%s is incomplete", s.Name)
		}
	}
}

func TestRequestsBetween(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l.jsonl")
	base := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)
	lines := `{"time":"2026-01-01T00:00:05Z","groups":{"consumer=bot-proxy":{"ok":100,"fail":0}}}
{"time":"2026-01-01T00:00:12Z","groups":{"consumer=bot-proxy":{"ok":10,"fail":90}}}
{"time":"2026-01-01T00:00:13Z","groups":{"consumer=bot-proxy":{"ok":20,"fail":80},"consumer=go-policy":{"ok":5,"fail":0}}}
`
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, fail := requestsBetween(p, "bot-proxy", base, base.Add(5*time.Second))
	if ok != 30 || fail != 170 {
		t.Fatalf("got ok=%d fail=%d", ok, fail)
	}
}
