package main

import (
	"testing"
	"time"
)

func TestPerInstanceWindow(t *testing.T) {
	p := newPerInstance()
	now := time.Unix(1000, 0)
	p.record("a", true, now.Add(-10*time.Second)) // outside a 3 s window
	p.record("a", true, now.Add(-2*time.Second))
	p.record("a", false, now)
	p.record("b", false, now.Add(-1*time.Second))
	w := p.window(3, now)
	if w["a"] != [2]int64{1, 1} {
		t.Fatalf("a: got %v want [1 1]", w["a"])
	}
	if w["b"] != [2]int64{0, 1} {
		t.Fatalf("b: got %v want [0 1]", w["b"])
	}
}
