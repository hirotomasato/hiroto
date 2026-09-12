package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLSPserverFor(t *testing.T) {
	cases := []struct {
		ext string
		bin string
		ok  bool
	}{
		{".go", "gopls", true},
		{".py", "", true}, // pyright or pylsp, both ok
		{".ts", "typescript-language-server", true},
		{".rs", "rust-analyzer", true},
		{".txt", "", false},
		{".md", "", false},
	}
	for _, c := range cases {
		bin, _, ok := lspServerFor(c.ext)
		if ok != c.ok {
			t.Errorf("%s: ok=%v want %v", c.ext, ok, c.ok)
		}
		if c.bin != "" && bin != c.bin {
			t.Errorf("%s: bin=%q want %q", c.ext, bin, c.bin)
		}
	}
}

func TestLSPposition(t *testing.T) {
	text := "func Foo() {}\nvar x = 1\n"
	// line 1, symbol "Foo" → char at index of "Foo"
	pos := lspPosition(text, "Foo", 1)
	if pos["line"] != 0 || pos["character"] != 5 {
		t.Errorf("got %v, want line=0 char=5", pos)
	}
	// no symbol → char 0
	pos = lspPosition(text, "", 1)
	if pos["character"] != 0 {
		t.Errorf("no symbol: got char=%v, want 0", pos["character"])
	}
}

func TestFormatLocs(t *testing.T) {
	raw := json.RawMessage(`[{"uri":"file:///a/b.go","range":{"start":{"line":9,"character":2}}}]`)
	out, err := formatLocs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out != "/a/b.go:10:3" {
		t.Errorf("got %q, want %q", out, "/a/b.go:10:3")
	}
}

func TestApplyRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.go")
	_ = os.WriteFile(path, []byte("func Old() {}\n"), 0o644)
	raw := json.RawMessage(`{"changes":{"file://` + path + `":[{"range":{"start":{"line":0,"character":5},"end":{"line":0,"character":8}},"newText":"New"}]}}`)
	out, err := applyRename(raw, "New")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "func New() {}\n" {
		t.Errorf("file content = %q, want %q", string(data), "func New() {}\n")
	}
	if out == "" {
		t.Error("expected a non-empty summary")
	}
}
