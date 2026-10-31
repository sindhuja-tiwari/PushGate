package rollout

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

// fakeFleet crashes an instance (restart count +1, state backoff) as soon as
// it holds a file its release cannot load.
type fakeFleet struct {
	mu    sync.Mutex
	insts []registry.Instance
	files map[string][]byte
	// breaks reports whether data crashes a given release.
	breaks func(version int, data []byte) bool
}

func newFakeFleet(n int, versions []int, breaks func(int, []byte) bool) *fakeFleet {
	f := &fakeFleet{files: map[string][]byte{}, breaks: breaks}
	for i := 0; i < n; i++ {
		f.insts = append(f.insts, registry.Instance{
			ID: fmt.Sprintf("bp-x-%03d", i), Consumer: "bot-proxy", Version: versions[i%len(versions)], State: registry.StateUp,
		})
	}
	return f
}

func (f *fakeFleet) Snapshot() ([]registry.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.insts {
		if d := f.files[f.insts[i].ID]; d != nil && f.breaks(f.insts[i].Version, d) {
			f.insts[i].Restarts++
			f.insts[i].State = registry.StateBackoff
		} else {
			f.insts[i].State = registry.StateUp
		}
	}
	return append([]registry.Instance(nil), f.insts...), nil
}

func (f *fakeFleet) Deliver(inst registry.Instance, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[inst.ID] = data
	return nil
}

func (f *fakeFleet) holding(marker string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.files {
		if strings.Contains(string(d), marker) {
			n++
		}
	}
	return n
}

func featureFile(version string, n int) []byte {
	var feats []string
	for i := 0; i < n; i++ {
		feats = append(feats, fmt.Sprintf(`{"name":"f%d","weight":1,"fields":["curl","*"]}`, i))
	}
	return []byte(fmt.Sprintf(`{"schema":"bot-features/1","config_version":%q,"features":[%s]}`, version, strings.Join(feats, ",")))
}

func fastB2() Strategy {
	s := B2(150 * time.Millisecond)
	s.Alarms.Every = 10 * time.Millisecond
	s.Alarms.Grace = 0
	return s
}

func tooMany(_ int, data []byte) bool { return strings.Count(string(data), `"name"`) > 200 }

func newRunner(t *testing.T, f Fleet) *Runner {
	return &Runner{Fleet: f, Store: Store{Dir: t.TempDir()}}
}

func TestB0DeliversEverywhereWithoutHealthCheck(t *testing.T) {
	f := newFakeFleet(100, []int{44}, tooMany)
	rn := newRunner(t, f)
	r, err := rn.Push(context.Background(), B0(), "bot-proxy", featureFile("bad", 220))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusCompleted || r.Exposed != 100 || f.holding(`"bad"`) != 100 {
		t.Fatalf("B0 should push the bad file to all 100: %+v", r)
	}
	if lkg, _, _ := rn.Store.LKG("bot-proxy"); lkg != nil {
		t.Fatal("B0 must not mark an unverified file as last known good")
	}
}

func TestB1RejectsSchemaViolation(t *testing.T) {
	f := newFakeFleet(10, []int{44}, tooMany)
	rn := newRunner(t, f)
	bad := []byte(`{"schema":"bot-features/1","config_version":"x","features":[{"name":"a","weight":"heavy","fields":["*"]}]}`)
	r, _ := rn.Push(context.Background(), B1(), "bot-proxy", bad)
	if r.Status != StatusRejected || f.holding(`"x"`) != 0 {
		t.Fatalf("B1 should reject a string weight before delivery: %+v", r)
	}
	if r0, _ := rn.Push(context.Background(), B0(), "bot-proxy", bad); r0.Status != StatusCompleted {
		t.Fatalf("B0 has no schema check and should push it: %+v", r0)
	}
}

func TestSchemaAcceptsWhatTheFormatAllows(t *testing.T) {
	// 220 features and a 21-field rule are both legal in the format. Only
	// consumer releases limit them, which is the gap B1 leaves open.
	if err := (SchemaCheck{}).Validate("bot-proxy", featureFile("big", 220)); err != nil {
		t.Fatalf("schema should accept 220 features: %v", err)
	}
	blank := []byte(`{"schema":"quota-policy/1","config_version":"p","policies":[{"project":"p","api":"a","allow":true,"enforce_quota":true,"quota":null}]}`)
	if err := (SchemaCheck{}).Validate("go-policy", blank); err != nil {
		t.Fatalf("schema should accept a null quota: %v", err)
	}
}

func TestB2RollsBackOnRingZeroCrash(t *testing.T) {
	f := newFakeFleet(400, []int{41, 42, 43, 44}, tooMany)
	rn := newRunner(t, f)
	good := featureFile("good", 110)
	if err := rn.Store.SetLKG("bot-proxy", good, "seed"); err != nil {
		t.Fatal(err)
	}
	for _, i := range f.insts {
		_ = f.Deliver(i, good)
	}
	r, err := rn.Push(context.Background(), fastB2(), "bot-proxy", featureFile("bad", 220))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusRolledBack {
		t.Fatalf("want rolled_back, got %s (%s)", r.Status, r.Reason)
	}
	if r.Exposed != 4 {
		t.Fatalf("only the 1%% ring (4 of 400) should have been exposed, got %d", r.Exposed)
	}
	if n := f.holding(`"bad"`); n != 0 {
		t.Fatalf("%d instances still hold the bad file after rollback", n)
	}
}

func TestB2CompletesAndSetsLKG(t *testing.T) {
	f := newFakeFleet(50, []int{44}, tooMany)
	rn := newRunner(t, f)
	r, _ := rn.Push(context.Background(), fastB2(), "bot-proxy", featureFile("next", 120))
	if r.Status != StatusCompleted || r.Exposed != 50 {
		t.Fatalf("good push should complete: %+v", r)
	}
	if lkg, meta, _ := rn.Store.LKG("bot-proxy"); lkg == nil || meta.ConfigVersion != "next" {
		t.Fatalf("completed B2 push should become last known good: %v", meta)
	}
}

func TestB2HeldWithoutLKG(t *testing.T) {
	f := newFakeFleet(100, []int{44}, tooMany)
	rn := newRunner(t, f)
	r, _ := rn.Push(context.Background(), fastB2(), "bot-proxy", featureFile("bad", 220))
	if r.Status != StatusHeld {
		t.Fatalf("with no last known good file the push should be held, got %s", r.Status)
	}
}

func TestFreezeBlocksAndStops(t *testing.T) {
	f := newFakeFleet(100, []int{44}, tooMany)
	rn := newRunner(t, f)
	if err := rn.Store.SetFreeze("bot-proxy", "incident 42"); err != nil {
		t.Fatal(err)
	}
	r, _ := rn.Push(context.Background(), B0(), "bot-proxy", featureFile("x", 10))
	if r.Status != StatusFrozen || f.holding(`"x"`) != 0 {
		t.Fatalf("frozen consumer should get nothing: %+v", r)
	}

	// Freeze while a push is baking: it stops before the next ring.
	_ = rn.Store.SetFreeze("bot-proxy", "")
	s := fastB2()
	s.Bake = 300 * time.Millisecond
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = rn.Store.SetFreeze("bot-proxy", "stop now")
	}()
	r, _ = rn.Push(context.Background(), s, "bot-proxy", featureFile("y", 10))
	if r.Status != StatusFrozen || r.Exposed != 1 {
		t.Fatalf("freeze during ring 0 bake should stop at 1 instance: %+v", r)
	}
}

func TestManualRollback(t *testing.T) {
	f := newFakeFleet(20, []int{44}, tooMany)
	rn := newRunner(t, f)
	_ = rn.Store.SetLKG("bot-proxy", featureFile("good", 5), "seed")
	_, _ = rn.Push(context.Background(), B0(), "bot-proxy", featureFile("bad", 220))
	n, err := rn.Rollback("bot-proxy")
	if err != nil || n != 20 || f.holding(`"bad"`) != 0 {
		t.Fatalf("rollback: n=%d err=%v still bad=%d", n, err, f.holding(`"bad"`))
	}
}

func TestParseRings(t *testing.T) {
	if _, err := ParseRings("1,10,100"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"10,1,100", "1,10", "0,100", "a"} {
		if _, err := ParseRings(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
