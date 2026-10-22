package main

// Layout state (order, groups, nicknames, overrides, settings) is owned by the
// frontend; the backend just stores it as one JSON document.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	path string
	mu   sync.Mutex
	doc  json.RawMessage
	rev  int64
}

func NewStore(dir string) *Store {
	s := &Store{path: filepath.Join(dir, "layout.json"), doc: json.RawMessage(`{}`)}
	if b, err := os.ReadFile(s.path); err == nil && json.Valid(b) {
		var env struct {
			Rev    int64           `json:"rev"`
			Layout json.RawMessage `json:"layout"`
		}
		if json.Unmarshal(b, &env) == nil && env.Layout != nil {
			s.doc, s.rev = env.Layout, env.Rev
		}
	}
	return s
}

func (s *Store) Get() (json.RawMessage, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doc, s.rev
}

var errConflict = errors.New("layout changed elsewhere")

// Put saves the layout. baseRev guards against two browsers overwriting each
// other; pass -1 to force.
func (s *Store) Put(doc json.RawMessage, baseRev int64) (int64, error) {
	if !json.Valid(doc) || len(doc) > 2<<20 {
		return 0, errors.New("invalid layout")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if baseRev >= 0 && baseRev != s.rev {
		return s.rev, errConflict
	}
	rev := s.rev + 1
	b, _ := json.Marshal(map[string]any{"rev": rev, "layout": doc})
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return 0, err
	}
	s.doc, s.rev = doc, rev
	return rev, nil
}
