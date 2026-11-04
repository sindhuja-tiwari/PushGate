// Package lkg is PushGate's consumer-side safety net for config files: a
// loader that never lets a bad file take the process down.
//
//   - Last known good. A file that fails to parse, breaks a declared limit or
//     panics while loading is rejected, and the process keeps serving the
//     last file that loaded. Every good file is also cached next to the
//     config (<path>.lkg), so a restart with a bad file on disk starts from
//     the cache instead of crash-looping.
//   - Declared limits. The consumer's parse function checks the limits its
//     code depends on (table sizes, field counts, required fields) and
//     returns an error instead of failing later. The limits are published in
//     Status so operators and the gate can see them.
//   - Fail open or closed, per file. KeepLastGood keeps serving the old file
//     (availability first). FailClosed stops serving until a good file
//     arrives, for files where stale rules are worse than no answer.
//   - Randomized reload. A changed file is read after a random delay, so a
//     fleet does not reload in lockstep, and read errors are retried with
//     jittered exponential backoff.
//   - Reports. Status says what is served, from where, and the last
//     rejection with its reason. Consumers expose it on /healthz.
//
// The Rust crate in lib/rust/lkg implements the same behavior and status
// format.
package lkg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Mode is what happens when a new file is rejected.
type Mode string

const (
	// KeepLastGood keeps serving the last file that loaded.
	KeepLastGood Mode = "keep_last_good"
	// FailClosed stops serving until a good file arrives.
	FailClosed Mode = "fail_closed"
)

// ParseMode accepts "keep_last_good" (or "keep", "open") and "fail_closed"
// (or "closed").
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(s) {
	case "keep_last_good", "keep", "open", "fail_open":
		return KeepLastGood, nil
	case "fail_closed", "closed":
		return FailClosed, nil
	}
	return "", fmt.Errorf("unknown mode %q (want keep_last_good or fail_closed)", s)
}

// Options configure a Loader. Only Path is required.
type Options struct {
	Path string
	Mode Mode // default KeepLastGood
	// Poll is how often the file is checked for changes (default 500 ms).
	Poll time.Duration
	// Jitter is the most a changed file waits before it is read (default:
	// Poll). Each reload waits a random time in [0, Jitter).
	Jitter time.Duration
	// BackoffBase and BackoffMax bound retries after a read error (defaults
	// 250 ms and 30 s), with full jitter.
	BackoffBase, BackoffMax time.Duration
	// Limits are the consumer's declared limits, published in Status.
	Limits map[string]any
	// Seed seeds the jitter (default: time and pid).
	Seed int64
	// Logf receives one line per load or rejection (default: discard).
	Logf func(format string, args ...any)
}

// Rejection is a file the loader refused.
type Rejection struct {
	ConfigVersion string    `json:"config_version"`
	Reason        string    `json:"reason"`
	At            time.Time `json:"at"`
}

// Status is what a consumer reports about its config. Its JSON form is
// shared with the Rust crate.
type Status struct {
	Mode          Mode   `json:"mode"`
	Serving       bool   `json:"serving"`
	ConfigVersion string `json:"config_version"` // the file being served
	// From is "file" (the config path), "cache" (<path>.lkg, after a bad
	// file at startup) or "" when nothing is served.
	From         string         `json:"from"`
	Loads        int            `json:"loads"`
	Rejections   int            `json:"rejections"`
	LastRejected *Rejection     `json:"last_rejected,omitempty"`
	Limits       map[string]any `json:"limits,omitempty"`
}

// Loader holds the current config of type T.
type Loader[T any] struct {
	opts  Options
	parse func([]byte) (T, error)

	mu     sync.RWMutex
	cur    T
	status Status

	rngMu sync.Mutex
	rng   *rand.Rand

	opened stamp // the file as Open saw it, so Watch starts from there
}

// CachePath is where the last good file is kept.
func CachePath(path string) string { return path + ".lkg" }

// Open loads the config file. It never fails because of the file's
// contents: a bad file is rejected and, in KeepLastGood mode, the cached last
// good file is served instead. If no good file is available the loader
// starts without one and Current reports false until a good file arrives.
func Open[T any](opts Options, parse func([]byte) (T, error)) *Loader[T] {
	if opts.Mode == "" {
		opts.Mode = KeepLastGood
	}
	if opts.Poll <= 0 {
		opts.Poll = 500 * time.Millisecond
	}
	if opts.Jitter < 0 {
		opts.Jitter = 0
	} else if opts.Jitter == 0 {
		opts.Jitter = opts.Poll
	}
	if opts.BackoffBase <= 0 {
		opts.BackoffBase = 250 * time.Millisecond
	}
	if opts.BackoffMax <= 0 {
		opts.BackoffMax = 30 * time.Second
	}
	if opts.Seed == 0 {
		opts.Seed = time.Now().UnixNano() ^ int64(os.Getpid())<<20
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	l := &Loader[T]{opts: opts, parse: parse, rng: rand.New(rand.NewSource(opts.Seed))}
	l.status = Status{Mode: opts.Mode, Limits: opts.Limits}

	l.opened, _ = statOf(opts.Path)
	raw, err := os.ReadFile(opts.Path)
	if err == nil {
		if l.accept(raw, "file") {
			return l
		}
	} else {
		l.reject(nil, "read: "+err.Error())
	}
	if opts.Mode == KeepLastGood {
		if raw, err := os.ReadFile(CachePath(opts.Path)); err == nil {
			v, perr := l.safeParse(raw)
			if perr == nil {
				l.mu.Lock()
				l.cur = v
				l.status.Serving, l.status.From, l.status.ConfigVersion = true, "cache", configVersion(raw)
				l.status.Loads++
				l.mu.Unlock()
				opts.Logf("lkg: serving cached last known good %s", configVersion(raw))
			}
		}
	}
	return l
}

// Current returns the config to serve. ok is false when there is nothing to
// serve: no good file yet, or FailClosed after a rejection.
func (l *Loader[T]) Current() (v T, ok bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.status.Serving {
		return v, false
	}
	return l.cur, true
}

// Status returns a copy of the loader's status.
func (l *Loader[T]) Status() Status {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := l.status
	if s.LastRejected != nil {
		r := *s.LastRejected
		s.LastRejected = &r
	}
	return s
}

// StatusJSON is Status as JSON, for /healthz.
func (l *Loader[T]) StatusJSON() []byte {
	b, _ := json.Marshal(l.Status())
	return b
}

type stamp struct {
	mod  time.Time
	size int64
}

func statOf(path string) (stamp, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return stamp{}, false
	}
	return stamp{st.ModTime(), st.Size()}, true
}

// Watch polls the file and reloads it when it changes, until ctx ends.
func (l *Loader[T]) Watch(ctx context.Context) {
	last := l.opened
	for {
		if !sleep(ctx, l.opts.Poll) {
			return
		}
		now, ok := statOf(l.opts.Path)
		if !ok || now == last {
			continue
		}
		// Spread reloads across the fleet.
		if !sleep(ctx, l.randDur(l.opts.Jitter)) {
			return
		}
		attempt := 0
		for {
			now, _ = statOf(l.opts.Path)
			raw, err := os.ReadFile(l.opts.Path)
			if err == nil {
				l.accept(raw, "file")
				break
			}
			// A read error is often transient (a file mid-replace). Retry
			// with backoff until it reads or the file changes again.
			d := l.opts.BackoffBase << attempt
			if d > l.opts.BackoffMax || d <= 0 {
				d = l.opts.BackoffMax
			} else {
				attempt++
			}
			l.opts.Logf("lkg: read failed, retrying: %v", err)
			if !sleep(ctx, l.randDur(d)+1) {
				return
			}
		}
		last = now
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (l *Loader[T]) randDur(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	l.rngMu.Lock()
	defer l.rngMu.Unlock()
	return time.Duration(l.rng.Int63n(int64(max)))
}

// accept parses raw and, if it is good, makes it current and caches it.
func (l *Loader[T]) accept(raw []byte, from string) bool {
	v, err := l.safeParse(raw)
	if err != nil {
		l.reject(raw, err.Error())
		return false
	}
	cv := configVersion(raw)
	l.mu.Lock()
	l.cur = v
	l.status.Serving, l.status.From, l.status.ConfigVersion = true, from, cv
	l.status.Loads++
	l.status.LastRejected = nil
	l.mu.Unlock()
	if err := writeAtomic(CachePath(l.opts.Path), raw); err != nil {
		l.opts.Logf("lkg: caching last known good: %v", err)
	}
	l.opts.Logf("lkg: loaded %s", cv)
	return true
}

func (l *Loader[T]) reject(raw []byte, reason string) {
	cv := "unknown"
	if raw != nil {
		cv = configVersion(raw)
	}
	l.mu.Lock()
	l.status.Rejections++
	l.status.LastRejected = &Rejection{ConfigVersion: cv, Reason: reason, At: time.Now().UTC()}
	if l.opts.Mode == FailClosed {
		l.status.Serving = false
	}
	serving := l.status.ConfigVersion
	l.mu.Unlock()
	if l.opts.Mode == FailClosed || serving == "" {
		l.opts.Logf("lkg: rejected %s (%s); not serving", cv, reason)
	} else {
		l.opts.Logf("lkg: rejected %s (%s); still serving %s", cv, reason, serving)
	}
}

// safeParse runs the consumer's parse function and turns a panic into an
// error naming where it happened.
func (l *Loader[T]) safeParse(raw []byte) (v T, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic while loading: %v%s", p, panicSite())
		}
	}()
	return l.parse(raw)
}

// panicSite finds the first frame outside the runtime and this package,
// which is the consumer code that panicked.
func panicSite() string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if !strings.HasPrefix(f.Function, "runtime.") && !strings.Contains(f.Function, "lkg.(*Loader") && f.File != "" {
			return fmt.Sprintf(" at %s:%d", filepath.Base(f.File), f.Line)
		}
		if !more {
			return ""
		}
	}
}

func configVersion(raw []byte) string {
	var head struct {
		ConfigVersion string `json:"config_version"`
	}
	if json.Unmarshal(raw, &head) != nil || head.ConfigVersion == "" {
		return "unknown"
	}
	return head.ConfigVersion
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lkg-tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o644)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
