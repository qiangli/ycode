package pipeline

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// State is a typed, versioned set of pipeline slots. A version belongs to the
// root slot; nested field updates advance that version too.
type State struct {
	mu    sync.RWMutex
	slots map[string]slot
}

type slot struct {
	typ     string
	value   any
	version uint64
	present bool
}

func NewState() *State { return &State{slots: make(map[string]slot)} }

func (s *State) Declare(name, typ string) error {
	if name == "" || typ == "" {
		return fmt.Errorf("pipeline: state declaration requires name and type")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.slots[name]; ok && old.typ != typ {
		return fmt.Errorf("pipeline: state %q already has type %q", name, old.typ)
	}
	if _, ok := s.slots[name]; !ok {
		s.slots[name] = slot{typ: typ}
	}
	return nil
}

func (s *State) SetTyped(name, typ string, value any) error {
	if err := s.Declare(name, typ); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.slots[name]
	v.value, v.present, v.version = cloneValue(value), true, v.version+1
	s.slots[name] = v
	return nil
}

// CompareAndSwap atomically replaces a root slot when its version matches.
func (s *State) CompareAndSwap(name string, expected uint64, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.slots[name]
	if !ok {
		return fmt.Errorf("pipeline: state %q is not declared", name)
	}
	if v.version != expected {
		return fmt.Errorf("pipeline: state %q version conflict: have %d, want %d", name, v.version, expected)
	}
	v.value, v.present, v.version = cloneValue(value), true, v.version+1
	s.slots[name] = v
	return nil
}

func (s *State) Read(path string) (any, string, uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parts := strings.Split(path, ".")
	v, ok := s.slots[parts[0]]
	if !ok || !v.present {
		return nil, "", 0, false
	}
	value := v.value
	for _, part := range parts[1:] {
		var found bool
		value, found = field(value, part)
		if !found {
			return nil, v.typ, v.version, false
		}
	}
	return cloneValue(value), v.typ, v.version, true
}

func (s *State) Version(name string) (uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.slots[name]
	return v.version, ok
}

// apply commits all node outputs atomically and increments each affected root
// exactly once. Compiler-enforced single-writer/merge rules make this the only
// mutation seam the interpreter needs.
func (s *State) apply(writes map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := make(map[string]slot)
	for path, value := range writes {
		parts := strings.Split(path, ".")
		root := parts[0]
		v, ok := changed[root]
		if !ok {
			v, ok = s.slots[root]
		}
		if !ok {
			return fmt.Errorf("pipeline: output targets undeclared state %q", root)
		}
		if len(parts) == 1 {
			v.value, v.present = cloneValue(value), true
		} else {
			base := map[string]any{}
			if v.present {
				var valid bool
				base, valid = cloneValue(v.value).(map[string]any)
				if !valid {
					return fmt.Errorf("pipeline: nested output %q targets non-object state", path)
				}
			}
			setMapPath(base, parts[1:], cloneValue(value))
			v.value, v.present = base, true
		}
		changed[root] = v
	}
	for root, v := range changed {
		v.version++
		s.slots[root] = v
	}
	return nil
}

func field(value any, name string) (any, bool) {
	if m, ok := value.(map[string]any); ok {
		v, found := m[name]
		return v, found
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.Kind() == reflect.Struct {
		rt := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			f := rt.Field(i)
			if f.Name == name || strings.Split(f.Tag.Get("json"), ",")[0] == name {
				return rv.Field(i).Interface(), true
			}
		}
	}
	return nil, false
}

func setMapPath(m map[string]any, parts []string, value any) {
	if len(parts) == 1 {
		m[parts[0]] = value
		return
	}
	next, _ := m[parts[0]].(map[string]any)
	if next == nil {
		next = map[string]any{}
		m[parts[0]] = next
	}
	setMapPath(next, parts[1:], value)
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = cloneValue(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = cloneValue(v)
		}
		return out
	default:
		return v
	}
}
