package lkg

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cfg is a toy config: a list of numbers with a declared limit on length.
type cfg struct {
	ConfigVersion string `json:"config_version"`
	Items         []int  `json:"items"`
	Ptr           *int   `json:"ptr"`
}

const maxItems = 3

func parse(raw []byte) (cfg, error) {
	var c cfg
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if len(c.Items) > maxItems {
		return c, errors.New("too many items")
	}
	if c.ConfigVersion == "boom" {
		_ = *c.Ptr // nil dereference, like go-policy's blank quota
	}
	return c, nil
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make sure the mtime or size changes between writes.
	future := time.Now().Add(time.Duration(len(body)) * time.Millisecond)
	_ = os.Chtimes(path, future, future)
}

func open(t *testing.T, mode Mode, body string) (*Loader[cfg], string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.json")
	if body != "" {
		write(t, p, body)
	}
	return Open(Options{Path: p, Mode: mode, Poll: 5 * time.Millisecond, Jitter: 5 * time.Millisecond, BackoffBase: time.Millisecond, Seed: 1}, parse), p
}

// watch runs l.Watch until the test ends, and waits for it to stop before
// the temp dir is removed.
func watch(t *testing.T, l *Loader[cfg]) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Watch(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

const good = `{"config_version":"g1","items":[1,2]}`
const good2 = `{"config_version":"g2","items":[1,2,3]}`
const tooMany = `{"config_version":"bad","items":[1,2,3,4]}`
const panics = `{"config_version":"boom","items":[]}`

func TestGoodFileServes(t *testing.T) {
	l, p := open(t, KeepLastGood, good)
	c, ok := l.Current()
	if !ok || c.ConfigVersion != "g1" {
		t.Fatalf("got %v %v", c, ok)
	}
	if s := l.Status(); s.From != "file" || s.Loads != 1 || s.Rejections != 0 {
		t.Fatalf("status %+v", s)
	}
	if b, _ := os.ReadFile(CachePath(p)); string(b) != good {
		t.Fatalf("cache not written: %q", b)
	}
}

func TestReloadRejectsAndKeepsLastGood(t *testing.T) {
	l, p := open(t, KeepLastGood, good)
	watch(t, l)

	write(t, p, tooMany)
	waitFor(t, "rejection", func() bool { return l.Status().Rejections == 1 })
	c, ok := l.Current()
	if !ok || c.ConfigVersion != "g1" {
		t.Fatalf("should still serve g1, got %v %v", c, ok)
	}
	r := l.Status().LastRejected
	if r == nil || r.ConfigVersion != "bad" || !strings.Contains(r.Reason, "too many items") {
		t.Fatalf("rejection %+v", r)
	}

	write(t, p, good2)
	waitFor(t, "good reload", func() bool { c, _ := l.Current(); return c.ConfigVersion == "g2" })
	if l.Status().LastRejected != nil {
		t.Fatal("a good file should clear the last rejection")
	}
}

func TestPanicIsCaught(t *testing.T) {
	l, p := open(t, KeepLastGood, good)
	watch(t, l)
	write(t, p, panics)
	waitFor(t, "rejection", func() bool { return l.Status().Rejections == 1 })
	r := l.Status().LastRejected
	if !strings.Contains(r.Reason, "nil pointer") || !strings.Contains(r.Reason, "lkg_test.go:") {
		t.Fatalf("reason should name the panic and where: %q", r.Reason)
	}
	if c, ok := l.Current(); !ok || c.ConfigVersion != "g1" {
		t.Fatalf("should still serve g1, got %v %v", c, ok)
	}
}

func TestRestartWithBadFileUsesCache(t *testing.T) {
	_, p := open(t, KeepLastGood, good) // caches g1
	write(t, p, tooMany)
	l := Open(Options{Path: p}, parse)
	c, ok := l.Current()
	if !ok || c.ConfigVersion != "g1" || l.Status().From != "cache" {
		t.Fatalf("restart should serve the cache: %v %v %+v", c, ok, l.Status())
	}
	if l.Status().LastRejected.ConfigVersion != "bad" {
		t.Fatalf("restart should report the rejection: %+v", l.Status())
	}
}

func TestNoGoodFileAtAll(t *testing.T) {
	l, _ := open(t, KeepLastGood, tooMany)
	if _, ok := l.Current(); ok {
		t.Fatal("nothing good to serve")
	}
	l2, _ := open(t, KeepLastGood, "")
	if _, ok := l2.Current(); ok || !strings.HasPrefix(l2.Status().LastRejected.Reason, "read:") {
		t.Fatalf("missing file: %+v", l2.Status())
	}
}

func TestFailClosed(t *testing.T) {
	l, p := open(t, FailClosed, good)
	watch(t, l)
	write(t, p, tooMany)
	waitFor(t, "stop serving", func() bool { _, ok := l.Current(); return !ok })
	write(t, p, good2)
	waitFor(t, "serve again", func() bool { c, ok := l.Current(); return ok && c.ConfigVersion == "g2" })

	// Fail closed does not fall back to the cache on restart.
	write(t, p, tooMany)
	l2 := Open(Options{Path: p, Mode: FailClosed}, parse)
	if _, ok := l2.Current(); ok {
		t.Fatal("fail closed should not serve the cache")
	}
}

func TestReadErrorRetries(t *testing.T) {
	l, p := open(t, KeepLastGood, good)
	watch(t, l)
	// Replace the file with an unreadable directory entry, then fix it.
	os.Remove(p)
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if c, ok := l.Current(); !ok || c.ConfigVersion != "g1" {
		t.Fatal("a read error must not drop the current file")
	}
	os.Remove(p)
	write(t, p, good2)
	waitFor(t, "recovery after read errors", func() bool { c, _ := l.Current(); return c.ConfigVersion == "g2" })
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"keep": KeepLastGood, "keep_last_good": KeepLastGood, "fail_closed": FailClosed, "closed": FailClosed} {
		if m, err := ParseMode(in); err != nil || m != want {
			t.Fatalf("%s: %v %v", in, m, err)
		}
	}
	if _, err := ParseMode("maybe"); err == nil {
		t.Fatal("want error")
	}
}

func TestStatusJSONShape(t *testing.T) {
	l, _ := open(t, KeepLastGood, good)
	var m map[string]any
	if err := json.Unmarshal(l.StatusJSON(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"mode", "serving", "config_version", "from", "loads", "rejections"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("status JSON missing %q: %s", k, l.StatusJSON())
		}
	}
}
