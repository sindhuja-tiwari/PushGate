package main

import (
	"os"
	"path/filepath"
	"testing"
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
