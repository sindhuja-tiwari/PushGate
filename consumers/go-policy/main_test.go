package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sindhuja-tiwari/pushgate/lib/go/lkg"
)

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const blankQuota = `{"schema":"quota-policy/1","config_version":"t","policies":[
 {"project":"p1","api":"storage.read","allow":true,"enforce_quota":true,"quota":null}]}`

func TestOldReleaseIgnoresBlankQuota(t *testing.T) {
	old := buildVersion
	defer func() { buildVersion = old }()
	buildVersion = "42"
	idx, err := load(writePolicy(t, blankQuota))
	if err != nil || idx.count != 1 {
		t.Fatalf("v42 should load the file: idx=%v err=%v", idx, err)
	}
}

func TestNewReleaseCrashesOnBlankQuota(t *testing.T) {
	old := buildVersion
	defer func() { buildVersion = old }()
	buildVersion = "43"
	defer func() {
		if recover() == nil {
			t.Fatal("v43 should panic on a nil quota block")
		}
	}()
	_, _ = load(writePolicy(t, blankQuota))
}

func TestQuotaIndexed(t *testing.T) {
	buildVersion = "44"
	idx, err := load(writePolicy(t, `{"schema":"quota-policy/1","config_version":"t","policies":[
	 {"project":"p1","api":"a","allow":true,"enforce_quota":true,"quota":{"limit_per_min":5,"burst":1}}]}`))
	if err != nil || idx.quota["p1|a"] != 5 {
		t.Fatalf("quota not indexed: %v %v", idx, err)
	}
}

// With the last-known-good library, the same blank-quota file is rejected
// with the panic's location instead of killing the process.
func TestSafeLoaderRejectsBlankQuota(t *testing.T) {
	old := buildVersion
	defer func() { buildVersion = old }()
	buildVersion = "43"
	good := `{"schema":"quota-policy/1","config_version":"good","policies":[
	 {"project":"p1","api":"a","allow":true,"enforce_quota":true,"quota":{"limit_per_min":5,"burst":1}}]}`
	p := writePolicy(t, good)
	l := lkg.Open(lkg.Options{Path: p}, parse)
	if err := os.WriteFile(p, []byte(blankQuota), 0o644); err != nil {
		t.Fatal(err)
	}
	l2 := lkg.Open(lkg.Options{Path: p}, parse) // a restart with the bad file on disk
	ix, ok := l2.Current()
	if !ok || ix.configVersion != "good" {
		t.Fatalf("restart should serve the cached good file, got %v %v", ix, ok)
	}
	r := l2.Status().LastRejected
	if r == nil || !strings.Contains(r.Reason, "nil pointer") || !strings.Contains(r.Reason, "main.go:") {
		t.Fatalf("rejection should name the panic and its line: %+v", r)
	}
	_ = l
}
