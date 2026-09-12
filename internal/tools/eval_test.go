package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestEvalPyStatePersists(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Call 1: define state.
	r1 := evalExec(ctx, map[string]any{"language": "python", "code": "x = 42"})
	if r1.IsError {
		t.Fatalf("call 1 failed: %s", r1.Output)
	}
	// Call 2: use state — proves persistence.
	r2 := evalExec(ctx, map[string]any{"language": "python", "code": "print(x * 2)"})
	if r2.IsError {
		t.Fatalf("call 2 failed: %s", r2.Output)
	}
	if strings.TrimSpace(r2.Output) != "84" {
		t.Errorf("expected '84' from persistent state, got %q", r2.Output)
	}
	// Call 3: error path returns non-zero IsError.
	r3 := evalExec(ctx, map[string]any{"language": "python", "code": "raise ValueError('boom')"})
	if !r3.IsError {
		t.Errorf("expected error for raise, got ok with %q", r3.Output)
	}
}

func TestEvalJSPersists(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r1 := evalExec(ctx, map[string]any{"language": "javascript", "code": "const y = 10;"})
	if r1.IsError {
		t.Fatalf("call 1 failed: %s", r1.Output)
	}
	r2 := evalExec(ctx, map[string]any{"language": "javascript", "code": "display(y + 5);"})
	if r2.IsError {
		t.Fatalf("call 2 failed: %s", r2.Output)
	}
	if r2.Output != "15" {
		t.Errorf("expected '15' from persistent state, got %q", r2.Output)
	}
}
