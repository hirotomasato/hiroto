// Package session persists conversations to ~/.hiroto/sessions/<id>.json
// so they can be listed and resumed later (Hiroto-style --continue).
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// StoredToolCall is a persisted assistant tool call.
type StoredToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// StoredMessage is one persisted message (tool-call aware).
type StoredMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolName   string           `json:"tool_name,omitempty"`
	ToolCalls  []StoredToolCall `json:"tool_calls,omitempty"`
}

// Session is a saved conversation.
type Session struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Model    string          `json:"model,omitempty"`
	Created  time.Time       `json:"created"`
	Updated  time.Time       `json:"updated"`
	Messages []StoredMessage `json:"messages"`
}

// Store manages the sessions directory.
type Store struct {
	Dir string
}

func New() *Store {
	dir := filepath.Join(homeDir(), "sessions")
	_ = os.MkdirAll(dir, 0o755)
	return &Store{Dir: dir}
}

// NewAt builds a store at a custom directory (for tests).
func NewAt(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	return &Store{Dir: dir}
}

func (s *Store) file(id string) string { return filepath.Join(s.Dir, id+".json") }

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Save writes (or overwrites) a session file.
func (s *Store) Save(sess *Session) error {
	if !idRe.MatchString(sess.ID) {
		return os.ErrInvalid
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.file(sess.ID), data, 0o644)
}

// Load reads one session by id (ids are sanitized).
func (s *Store) Load(id string) (*Session, error) {
	if !idRe.MatchString(id) {
		return nil, os.ErrInvalid
	}
	data, err := os.ReadFile(s.file(id))
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// List returns all sessions, newest first.
func (s *Store) List() []Session {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var out []Session
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if sess, err := s.Load(e.Name()[:len(e.Name())-5]); err == nil {
			out = append(out, *sess)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// Search returns sessions whose title or message content contains the query.
func (s *Store) Search(query string) []Session {
	query = strings.ToLower(query)
	all := s.List()
	var out []Session
	for _, sess := range all {
		if strings.Contains(strings.ToLower(sess.Title), query) {
			out = append(out, sess)
			continue
		}
		for _, msg := range sess.Messages {
			if strings.Contains(strings.ToLower(msg.Content), query) {
				out = append(out, sess)
				break
			}
		}
	}
	return out
}

func homeDir() string {
	if h := os.Getenv("HIROTO_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hiroto")
}
