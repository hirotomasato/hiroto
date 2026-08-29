package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DelegateTask runs a sub-agent via `hiroto -q` in a subprocess.
func registerDelegateTask(r *Registry) {
	r.Register(&Tool{
		Name:        "delegate_task",
		Description: "Spawn a sub-agent to handle a task independently. The sub-agent runs `hiroto -q` with the given goal and returns its final answer. Use for parallel work or when you want to offload a subtask.",
		Parameters:  mustJSON(`{"type":"object","properties":{"goal":{"type":"string","description":"The task for the sub-agent (passed as -q query)"},"timeout":{"type":"integer","description":"Max seconds to wait (default 120)"}},"required":["goal"]}`),
		Exec:        delegateExec,
	})
}

func delegateExec(ctx context.Context, args map[string]any) Result {
	goal, _ := args["goal"].(string)
	if goal == "" {
		return Result{Output: "missing goal", IsError: true}
	}
	timeout := 120
	if t, ok := toInt(args["timeout"]); ok && t > 0 {
		timeout = t
	}
	ctx2, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx2, "hiroto", "-q", goal)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		if result == "" {
			result = fmt.Sprintf("delegate error: %v", err)
		}
		return Result{Output: result, IsError: true}
	}
	if len(result) > 10000 {
		result = result[:10000] + "\n... (truncated)"
	}
	return Result{Output: result}
}

// Cron job registry.
type cronEntry struct {
	ID       string
	Schedule string // cron expression or interval
	Command  string
	NextRun  time.Time
	mu       sync.Mutex
}

var (
	cronMu   sync.Mutex
	cronJobs = make(map[string]*cronEntry)
	cronNext int
)

func registerCronjob(r *Registry) {
	r.Register(&Tool{
		Name:        "cronjob",
		Description: "Schedule a recurring or one-shot task. Actions: schedule (interval or cron), list, cancel. Use schedule to create a task that runs `hiroto -q` with the given prompt.",
		Parameters:  mustJSON(`{"type":"object","properties":{"action":{"type":"string","description":"One of: schedule, list, cancel"},"id":{"type":"string","description":"Job ID (required for cancel)"},"prompt":{"type":"string","description":"Prompt to run (required for schedule)"},"interval":{"type":"string","description":"Interval like '30m', '1h', or cron '0 9 * * *' (required for schedule)"}},"required":["action"]}`),
		Exec:        cronExec,
	})
}

func cronExec(ctx context.Context, args map[string]any) Result {
	action, _ := args["action"].(string)
	switch action {
	case "schedule":
		return cronSchedule(args)
	case "list":
		return cronList()
	case "cancel":
		return cronCancel(args)
	default:
		return Result{Output: fmt.Sprintf("unknown action: %s", action), IsError: true}
	}
}

func cronSchedule(args map[string]any) Result {
	prompt, _ := args["prompt"].(string)
	interval, _ := args["interval"].(string)
	if prompt == "" || interval == "" {
		return Result{Output: "missing prompt or interval", IsError: true}
	}
	dur, err := parseInterval(interval)
	if err != nil {
		return Result{Output: fmt.Sprintf("invalid interval: %v (use 30m, 1h, etc.)", err), IsError: true}
	}
	cronMu.Lock()
	cronNext++
	id := fmt.Sprintf("cron_%d", cronNext)
	entry := &cronEntry{
		ID:       id,
		Schedule: interval,
		Command:  prompt,
		NextRun:  time.Now().Add(dur),
	}
	cronJobs[id] = entry
	cronMu.Unlock()

	go func() {
		time.Sleep(time.Until(entry.NextRun))
		cmd := exec.Command("hiroto", "-q", entry.Command)
		cmd.Env = os.Environ()
		cmd.Run()
	}()

	return Result{Output: fmt.Sprintf("scheduled %s (runs in %s): %s", id, dur, prompt)}
}

func cronList() Result {
	cronMu.Lock()
	defer cronMu.Unlock()
	if len(cronJobs) == 0 {
		return Result{Output: "(no scheduled jobs)"}
	}
	var b strings.Builder
	for _, e := range cronJobs {
		fmt.Fprintf(&b, "%s  %s  %s\n", e.ID, e.NextRun.Format("15:04"), e.Command)
	}
	return Result{Output: strings.TrimRight(b.String(), "\n")}
}

func cronCancel(args map[string]any) Result {
	id, _ := args["id"].(string)
	cronMu.Lock()
	defer cronMu.Unlock()
	if _, ok := cronJobs[id]; !ok {
		return Result{Output: fmt.Sprintf("job not found: %s", id), IsError: true}
	}
	delete(cronJobs, id)
	return Result{Output: fmt.Sprintf("cancelled %s", id)}
}

func parseInterval(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
