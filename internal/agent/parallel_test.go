package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hirotomasato/hiroto/internal/llm"
	"github.com/hirotomasato/hiroto/internal/tools"
)

// call builds a ToolCall with the given id/name (args empty).
func call(id, name string) llm.ToolCall {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = "{}"
	return tc
}

// executeToolCalls must return results in the SAME order as the calls, so each
// tool-result message lines up with its tool_call_id — even when the tools
// finish out of order.
func TestExecuteToolCallsPreservesOrder(t *testing.T) {
	reg := tools.NewRegistry()
	// "slow" sleeps so it would finish LAST if we relied on completion order.
	reg.Register(&tools.Tool{
		Name: "slow",
		Exec: func(ctx context.Context, args map[string]any) tools.Result {
			time.Sleep(40 * time.Millisecond)
			return tools.Result{Output: "SLOW"}
		},
	})
	reg.Register(&tools.Tool{
		Name: "fast",
		Exec: func(ctx context.Context, args map[string]any) tools.Result {
			return tools.Result{Output: "FAST"}
		},
	})
	a := &Agent{Reg: reg}
	calls := []llm.ToolCall{call("a", "slow"), call("b", "fast")}
	out := a.executeToolCalls(context.Background(), calls)

	if len(out) != 2 {
		t.Fatalf("got %d results, want 2", len(out))
	}
	if out[0].ToolCallID != "a" || out[0].Content != "SLOW" {
		t.Errorf("result[0] = {%s, %v}, want {a, SLOW}", out[0].ToolCallID, out[0].Content)
	}
	if out[1].ToolCallID != "b" || out[1].Content != "FAST" {
		t.Errorf("result[1] = {%s, %v}, want {b, FAST}", out[1].ToolCallID, out[1].Content)
	}
}

// Multiple independent calls must run concurrently: two 40ms tools should
// finish in well under their 80ms serial sum.
func TestExecuteToolCallsRunsConcurrently(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.Tool{
		Name: "sleep",
		Exec: func(ctx context.Context, args map[string]any) tools.Result {
			time.Sleep(40 * time.Millisecond)
			return tools.Result{Output: "ok"}
		},
	})
	a := &Agent{Reg: reg}
	calls := []llm.ToolCall{call("1", "sleep"), call("2", "sleep"), call("3", "sleep")}

	start := time.Now()
	out := a.executeToolCalls(context.Background(), calls)
	elapsed := time.Since(start)

	if len(out) != 3 {
		t.Fatalf("got %d results, want 3", len(out))
	}
	// Serial would be ~120ms; concurrent should be ~40ms. Allow generous slack.
	if elapsed > 90*time.Millisecond {
		t.Errorf("elapsed %v, expected concurrent (<90ms)", elapsed)
	}
}

// An unknown tool must yield an error result, not panic, and keep its slot.
func TestExecuteToolCallsUnknownTool(t *testing.T) {
	a := &Agent{Reg: tools.NewRegistry()}
	out := a.executeToolCalls(context.Background(), []llm.ToolCall{call("x", "nope")})
	if len(out) != 1 || out[0].ToolCallID != "x" {
		t.Fatalf("bad result slot: %+v", out)
	}
	if got := out[0].Content.(string); got == "" || got[:5] != "ERROR" {
		t.Errorf("unknown tool content = %q, want ERROR prefix", got)
	}
}

// Every call must emit exactly one tool_start and one tool_end event.
func TestExecuteToolCallsEmitsEvents(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.Tool{
		Name: "noop",
		Exec: func(ctx context.Context, args map[string]any) tools.Result {
			return tools.Result{Output: "ok"}
		},
	})
	var starts, ends int32
	a := &Agent{Reg: reg, Emit: func(e Event) {
		switch e.Type {
		case "tool_start":
			atomic.AddInt32(&starts, 1)
		case "tool_end":
			atomic.AddInt32(&ends, 1)
		}
	}}
	a.executeToolCalls(context.Background(), []llm.ToolCall{call("1", "noop"), call("2", "noop")})
	if starts != 2 || ends != 2 {
		t.Errorf("events: starts=%d ends=%d, want 2/2", starts, ends)
	}
}
