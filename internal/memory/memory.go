// Package memory provides persistent user profile + notes, Hiroto-style:
// entries injected into every session's system prompt.
package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID      string    `json:"id"`
	Target  string    `json:"target"` // "user" | "memory"
	Content string    `json:"content"`
	Created time.Time `json:"created"`
}

// Store keeps entries as one JSON file per target (simple, greppable, portable).
type Store struct {
	Dir string
	mu  sync.Mutex
}

func New() *Store {
	dir := filepath.Join(homeDir(), "memory")
	_ = os.MkdirAll(dir, 0o755)
	return &Store{Dir: dir}
}

func (s *Store) file(target string) string { return filepath.Join(s.Dir, target+".json") }

func (s *Store) List(target string) []string {
	var out []string
	for _, e := range s.ListEntries(target) {
		out = append(out, e.ID+"| "+e.Content)
	}
	return out
}

func (s *Store) ListEntries(target string) []Entry {
	data, err := os.ReadFile(s.file(target))
	var out []Entry
	if err == nil {
		decodeJSON(data, &out)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

func (s *Store) Add(target, content string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.ListEntries(target)
	e := Entry{ID: nextID(entries), Target: target, Content: strings.TrimSpace(content), Created: time.Now()}
	entries = append(entries, e)
	writeJSON(s.file(target), entries)
	return e.ID
}

func (s *Store) Remove(target, idOrText string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.ListEntries(target)
	kept := entries[:0]
	removed := false
	for _, e := range entries {
		if e.ID == idOrText || strings.Contains(e.Content, idOrText) {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if removed {
		writeJSON(s.file(target), kept)
	}
	return removed
}

// PromptBlock renders all entries for system-prompt injection (Hiroto-style).
func (s *Store) PromptBlock() string {
	var b strings.Builder
	if u := s.List("user"); len(u) > 0 {
		b.WriteString("## User profile\n")
		for _, e := range u {
			b.WriteString("- " + e + "\n")
		}
	}
	if m := s.List("memory"); len(m) > 0 {
		b.WriteString("\n## Memory notes\n")
		for _, e := range m {
			b.WriteString("- " + e + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}
