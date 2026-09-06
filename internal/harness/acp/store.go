// Package acp persists ACP session identity and fork lineage without owning an
// agent loop. Turns remain the responsibility of the YAML Harness controller.
package acp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Session struct {
	ID           string    `json:"id"`
	Cwd          string    `json:"cwd"`
	ParentID     string    `json:"parent_id,omitempty"`
	ForkSequence uint64    `json:"fork_sequence,omitempty"`
	HeadSequence uint64    `json:"head_sequence,omitempty"`
	NextTurn     uint64    `json:"next_turn"`
	UpdatedAt    time.Time `json:"updated_at"`
	Closed       bool      `json:"closed,omitempty"`
}

type LineageEvent struct {
	Sequence       uint64    `json:"sequence"`
	Type           string    `json:"type"`
	SessionID      string    `json:"session_id"`
	ParentID       string    `json:"parent_id,omitempty"`
	AtSequence     uint64    `json:"at_sequence,omitempty"`
	Time           time.Time `json:"time"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	Digest         string    `json:"digest"`
}

type diskState struct {
	Version  int                `json:"version"`
	Sessions map[string]Session `json:"sessions"`
	Lineage  []LineageEvent     `json:"lineage"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	state diskState
}

func NewMemory() *Store {
	return &Store{state: diskState{Version: 1, Sessions: make(map[string]Session)}}
}

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s := &Store{path: abs, state: diskState{Version: 1, Sessions: make(map[string]Session)}}
	data, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("acp session store: %w", err)
	}
	if s.state.Version != 1 || s.state.Sessions == nil {
		return nil, errors.New("acp session store: unsupported state")
	}
	if err := validateLineage(s.state.Lineage); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Create(cwd string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cwd == "" || !filepath.IsAbs(cwd) {
		return Session{}, errors.New("acp session cwd must be absolute")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	value := Session{ID: "ycode-" + hex.EncodeToString(idBytes), Cwd: filepath.Clean(cwd), NextTurn: 1, UpdatedAt: now}
	s.state.Sessions[value.ID] = value
	s.appendLineage("created", value.ID, "", 0, now)
	return value, s.save()
}

func (s *Store) Resume(id, cwd string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Sessions[id]
	if !ok || value.Closed {
		return Session{}, errors.New("acp session is unknown or closed")
	}
	if filepath.Clean(cwd) != value.Cwd {
		return Session{}, errors.New("acp session cwd mismatch")
	}
	value.UpdatedAt = time.Now().UTC()
	s.state.Sessions[id] = value
	s.appendLineage("resumed", id, value.ParentID, value.HeadSequence, value.UpdatedAt)
	return value, s.save()
}

func (s *Store) Fork(parentID, cwd string, at uint64) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parent, ok := s.state.Sessions[parentID]
	if !ok || parent.Closed {
		return Session{}, errors.New("acp fork parent is unknown or closed")
	}
	if filepath.Clean(cwd) != parent.Cwd || at > parent.HeadSequence {
		return Session{}, errors.New("acp fork cwd or sequence is invalid")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	value := Session{ID: "ycode-" + hex.EncodeToString(idBytes), Cwd: parent.Cwd, ParentID: parentID, ForkSequence: at, HeadSequence: at, NextTurn: parent.NextTurn, UpdatedAt: now}
	s.state.Sessions[value.ID] = value
	s.appendLineage("forked", value.ID, parentID, at, now)
	return value, s.save()
}

func (s *Store) NextTurn(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Sessions[id]
	if !ok || value.Closed {
		return "", errors.New("acp session is unknown or closed")
	}
	runID := fmt.Sprintf("%s-%08d", id, value.NextTurn)
	value.NextTurn++
	value.UpdatedAt = time.Now().UTC()
	s.state.Sessions[id] = value
	return runID, s.save()
}

func (s *Store) Advance(id string, sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Sessions[id]
	if !ok || value.Closed {
		return errors.New("acp session is unknown or closed")
	}
	if sequence < value.HeadSequence {
		return errors.New("acp session sequence regressed")
	}
	value.HeadSequence, value.UpdatedAt = sequence, time.Now().UTC()
	s.state.Sessions[id] = value
	return s.save()
}

func (s *Store) Close(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.state.Sessions[id]
	if !ok {
		return errors.New("acp session is unknown")
	}
	value.Closed, value.UpdatedAt = true, time.Now().UTC()
	s.state.Sessions[id] = value
	s.appendLineage("closed", id, value.ParentID, value.HeadSequence, value.UpdatedAt)
	return s.save()
}

func (s *Store) List(cwd string) []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.state.Sessions))
	for _, value := range s.state.Sessions {
		if !value.Closed && (cwd == "" || filepath.Clean(cwd) == value.Cwd) {
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) Lineage() []LineageEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LineageEvent(nil), s.state.Lineage...)
}

func (s *Store) appendLineage(kind, id, parent string, at uint64, now time.Time) {
	previous := ""
	if len(s.state.Lineage) > 0 {
		previous = s.state.Lineage[len(s.state.Lineage)-1].Digest
	}
	e := LineageEvent{Sequence: uint64(len(s.state.Lineage) + 1), Type: kind, SessionID: id, ParentID: parent, AtSequence: at, Time: now, PreviousDigest: previous}
	e.Digest = lineageDigest(e)
	s.state.Lineage = append(s.state.Lineage, e)
}

func lineageDigest(value LineageEvent) string {
	value.Digest = ""
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func validateLineage(values []LineageEvent) error {
	previous := ""
	for i, value := range values {
		if value.Sequence != uint64(i+1) || value.PreviousDigest != previous || value.Digest != lineageDigest(value) {
			return errors.New("acp session store: invalid lineage chain")
		}
		previous = value.Digest
	}
	return nil
}
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".acp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0o600)
	_, err = tmp.Write(raw)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
