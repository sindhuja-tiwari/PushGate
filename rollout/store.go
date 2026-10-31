package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sindhuja-tiwari/pushgate/internal/registry"
)

// Store keeps rollout state on disk under one directory:
//
//	<dir>/<consumer>/lkg.json       last known good file (exact bytes)
//	<dir>/<consumer>/lkg.meta.json  when and by which push it became good
//	<dir>/<consumer>/freeze.json    present while pushes are frozen
//	<dir>/pushes/<id>.json          one record per push
type Store struct {
	Dir string
}

type LKGMeta struct {
	ConfigVersion string    `json:"config_version"`
	PushID        string    `json:"push_id"`
	Since         time.Time `json:"since"`
}

type Freeze struct {
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
}

func (s Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.Dir}, parts...)...)
}

// LKG returns the last known good file for a consumer, or nil if none.
func (s Store) LKG(consumer string) ([]byte, *LKGMeta, error) {
	data, err := os.ReadFile(s.path(consumer, "lkg.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var meta LKGMeta
	if raw, err := os.ReadFile(s.path(consumer, "lkg.meta.json")); err == nil {
		_ = json.Unmarshal(raw, &meta)
	}
	return data, &meta, nil
}

// SetLKG records data as the consumer's last known good file.
func (s Store) SetLKG(consumer string, data []byte, pushID string) error {
	if err := registry.WriteAtomic(s.path(consumer, "lkg.json"), data); err != nil {
		return err
	}
	meta := LKGMeta{ConfigVersion: configVersion(data), PushID: pushID, Since: time.Now().UTC()}
	b, _ := json.MarshalIndent(meta, "", "  ")
	return registry.WriteAtomic(s.path(consumer, "lkg.meta.json"), b)
}

// Frozen reports whether pushes for a consumer are frozen.
func (s Store) Frozen(consumer string) (*Freeze, error) {
	raw, err := os.ReadFile(s.path(consumer, "freeze.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f Freeze
	if err := json.Unmarshal(raw, &f); err != nil {
		return &Freeze{Reason: "unreadable freeze file"}, nil
	}
	return &f, nil
}

// SetFreeze freezes (reason != "") or unfreezes (reason == "") a consumer.
func (s Store) SetFreeze(consumer, reason string) error {
	p := s.path(consumer, "freeze.json")
	if reason == "" {
		err := os.Remove(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, _ := json.MarshalIndent(Freeze{Reason: reason, Since: time.Now().UTC()}, "", "  ")
	return registry.WriteAtomic(p, b)
}

// SaveResult writes a push record.
func (s Store) SaveResult(r *Result) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return registry.WriteAtomic(s.path("pushes", r.ID+".json"), b)
}

func configVersion(data []byte) string {
	var head struct {
		ConfigVersion string `json:"config_version"`
	}
	if json.Unmarshal(data, &head) != nil || head.ConfigVersion == "" {
		return "unknown"
	}
	return head.ConfigVersion
}

func newID(consumer string) string {
	return fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405.000Z"), consumer)
}
