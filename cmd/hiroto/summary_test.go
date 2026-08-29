package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hirotomasato/hiroto/internal/llm"
)

func TestCountStats(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "hai"},
		{Role: llm.RoleAssistant, Content: "halo"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1"}, {ID: "2"}}},
		{Role: llm.RoleTool, ToolCallID: "1"},
		{Role: llm.RoleUser, Content: "lagi"},
	}
	users, toolCalls := countStats(msgs)
	if users != 2 {
		t.Fatalf("users = %d, want 2", users)
	}
	if toolCalls != 2 {
		t.Fatalf("toolCalls = %d, want 2", toolCalls)
	}
}

func TestCountStatsEmpty(t *testing.T) {
	users, toolCalls := countStats(nil)
	if users != 0 || toolCalls != 0 {
		t.Fatalf("got %d/%d, want 0/0", users, toolCalls)
	}
}

func TestPrintExitSummary(t *testing.T) {
	exitSummary = exitInfo{
		set:       true,
		id:        "20260829_174120_39daaf",
		title:     "Perbaiki error push",
		dur:       18*time.Minute + 24*time.Second,
		total:     81,
		users:     2,
		toolCalls: 77,
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printExitSummary()
	w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	for _, want := range []string{
		"Resume this session with:",
		"hiroto --resume 20260829_174120_39daaf",
		`hiroto -c "Perbaiki error push"`,
		"Session:",
		"18m 24s",
		"81 (2 user, 77 tool calls)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\ngot: %s", want, out)
		}
	}
	// empty session => no output
	exitSummary = exitInfo{}
	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	printExitSummary()
	w2.Close()
	os.Stdout = old
	n2, _ := r2.Read(buf)
	if n2 != 0 {
		t.Errorf("empty summary should print nothing, got %q", string(buf[:n2]))
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{18*time.Minute + 24*time.Second, "18m 24s"},
		{2*time.Hour + 5*time.Minute + 3*time.Second, "2h 5m 3s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
