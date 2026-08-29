package gateway

import (
	"strings"
	"testing"
)

// clipLive must cap bodies at Telegram's message limit with an ellipsis.
func TestClipLive(t *testing.T) {
	short := "hello"
	if got := clipLive(short); got != short {
		t.Errorf("short body changed: %q", got)
	}
	long := strings.Repeat("x", maxLiveLen+500)
	got := clipLive(long)
	if len(got) > maxLiveLen+len("…") {
		t.Errorf("clipLive did not cap: len=%d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clipped body should end with ellipsis")
	}
}

// render composes tool breadcrumbs above the streamed body.
func TestLiveRender(t *testing.T) {
	l := &live{}
	if l.render() != "" {
		t.Errorf("empty live should render empty")
	}
	l.tools = []string{"▸ Reading main.go"}
	if l.render() != "▸ Reading main.go" {
		t.Errorf("tools-only render mismatch: %q", l.render())
	}
	l.body = "done"
	want := "▸ Reading main.go\n\ndone"
	if l.render() != want {
		t.Errorf("render mismatch: %q, want %q", l.render(), want)
	}
}
