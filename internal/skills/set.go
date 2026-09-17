package skills

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Set is a concurrency-safe collection of active skills for a session
// (D-2026-09-16-38). It keeps insertion order for deterministic prompt
// rendering and tracks whether any skill ships scripts, which is what enables
// the skill_run tool. allowed_tools never grants anything on its own: the
// union is only used to RESTRICT the base toolbox (intersection, D-38).
type Set struct {
	mu        sync.RWMutex
	order     []string
	byName    map[string]*Skill
	anyScript bool
}

// NewSet returns an empty skill set.
func NewSet() *Set {
	return &Set{byName: map[string]*Skill{}}
}

// Add inserts a skill, refusing nil duplicates. A skill with an empty name
// (only possible through manual construction) is rejected fail-closed.
func (s *Set) Add(sk *Skill) error {
	if sk == nil {
		return errors.New("skills: cannot add a nil skill")
	}
	if sk.Name == "" {
		return errors.New("skills: cannot add a skill with empty name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[sk.Name]; ok {
		return fmt.Errorf("skills: skill %q ja carregada", sk.Name)
	}
	s.byName[sk.Name] = sk
	s.order = append(s.order, sk.Name)
	if len(sk.ScriptNames) > 0 {
		s.anyScript = true
	}
	return nil
}

// Get returns the skill and whether it is present.
func (s *Set) Get(name string) (*Skill, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sk, ok := s.byName[name]
	return sk, ok
}

// Names returns the loaded skill names in deterministic (sorted) order.
func (s *Set) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.byName))
	for name := range s.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of loaded skills.
func (s *Set) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byName)
}

// HasScripts reports whether any loaded skill ships a scripts/ directory.
func (s *Set) HasScripts() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.anyScript
}

// UnionAllowed returns the union of allowed_tools across all loaded skills.
// Empty when there are no skills; used to intersect the base toolbox (D-38).
func (s *Set) UnionAllowed() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	for _, name := range s.byName {
		for _, t := range name.AllowedTools {
			out[t] = true
		}
	}
	return out
}

// Remove drops a skill from the session set.
func (s *Set) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[name]; !ok {
		return
	}
	delete(s.byName, name)
	idx := -1
	for i, n := range s.order {
		if n == name {
			idx = i
			break
		}
	}
	if idx >= 0 {
		s.order = append(s.order[:idx], s.order[idx+1:]...)
	}
	s.anyScript = false
	for _, sk := range s.byName {
		if len(sk.ScriptNames) > 0 {
			s.anyScript = true
			break
		}
	}
}
