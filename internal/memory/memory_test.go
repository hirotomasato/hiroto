package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestAddAndList(t *testing.T) {
	testHome(t)
	m := New()
	id := m.Add("memory", "hello world")
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	entries := m.ListEntries("memory")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != "hello world" {
		t.Errorf("content = %q, want %q", entries[0].Content, "hello world")
	}
}

func TestRemove(t *testing.T) {
	testHome(t)
	m := New()
	id := m.Add("memory", "temp")
	if !m.Remove("memory", id) {
		t.Error("Remove returned false")
	}
	if len(m.ListEntries("memory")) != 0 {
		t.Error("entries still present after Remove")
	}
}

func TestPersistence(t *testing.T) {
	testHome(t)
	m1 := New()
	m1.Add("memory", "persist me")

	// New instance reads same file.
	m2 := New()
	entries := m2.ListEntries("memory")
	if len(entries) == 0 {
		t.Fatal("no entries after reload")
	}
	if entries[0].Content != "persist me" {
		t.Errorf("content = %q, want %q", entries[0].Content, "persist me")
	}
}

func TestPromptBlock(t *testing.T) {
	testHome(t)
	m := New()
	m.Add("user", "name is Anton")
	m.Add("memory", "likes Go")
	block := m.PromptBlock()
	if block == "" {
		t.Fatal("PromptBlock empty")
	}
	if !strContains(block, "User profile") || !strContains(block, "Memory notes") {
		t.Errorf("PromptBlock missing sections: %q", block)
	}
}

func TestAddCreatesDir(t *testing.T) {
	home := testHome(t)
	m := New()
	m.Add("memory", "x")
	if _, err := os.Stat(filepath.Join(home, ".hiroto", "memory")); err != nil {
		t.Errorf("memory dir not created: %v", err)
	}
}

func strContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
