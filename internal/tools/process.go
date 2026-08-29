package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process registry for background tasks.
var (
	procMu     sync.Mutex
	procMap    = make(map[string]*proc)
	procNextID int
)

type proc struct {
	ID     string
	Cmd    *exec.Cmd
	Output strings.Builder
	Done   bool
	Err    error
	mu     sync.Mutex
}

func registerProcess(r *Registry) {
	r.Register(&Tool{
		Name:        "process",
		Description: "Manage background processes: start a command, list running processes, poll output, kill, or wait for completion. Actions: start, list, poll, kill, wait.",
		Parameters: mustJSON(`{"type":"object","properties":{"action":{"type":"string","description":"One of: start, list, poll, kill, wait"},"id":{"type":"string","description":"Process ID (required for poll/kill/wait)"},"command":{"type":"string","description":"Shell command to run (required for start)"},"timeout":{"type":"integer","description":"Max seconds to wait (for wait action, default 30)"}},"required":["action"]}`),
		Exec: processExec,
	})
}

func processExec(ctx context.Context, args map[string]any) Result {
	action, _ := args["action"].(string)
	switch action {
	case "start":
		return processStart(args)
	case "list":
		return processList()
	case "poll":
		return processPoll(args)
	case "kill":
		return processKill(args)
	case "wait":
		return processWait(ctx, args)
	default:
		return Result{Output: fmt.Sprintf("unknown action: %s (use start/list/poll/kill/wait)", action), IsError: true}
	}
}

func processStart(args map[string]any) Result {
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return Result{Output: "missing command", IsError: true}
	}
	procMu.Lock()
	procNextID++
	id := fmt.Sprintf("proc_%d", procNextID)
	cmd := exec.Command("bash", "-c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var outBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	p := &proc{ID: id, Cmd: cmd}
	if err := cmd.Start(); err != nil {
		procMu.Unlock()
		return Result{Output: fmt.Sprintf("start failed: %v", err), IsError: true}
	}
	procMap[id] = p
	procMu.Unlock()

	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.Done = true
		p.Err = err
		p.Output.WriteString(outBuf.String())
		p.mu.Unlock()
	}()
	return Result{Output: fmt.Sprintf("started %s", id)}
}

func processList() Result {
	procMu.Lock()
	defer procMu.Unlock()
	if len(procMap) == 0 {
		return Result{Output: "(no background processes)"}
	}
	var out strings.Builder
	for _, p := range procMap {
		status := "running"
		if p.Done {
			if p.Err != nil {
				status = "failed: " + p.Err.Error()
			} else {
				status = "done"
			}
		}
		fmt.Fprintf(&out, "%s  %s\n", p.ID, status)
	}
	return Result{Output: strings.TrimRight(out.String(), "\n")}
}

func processPoll(args map[string]any) Result {
	id, _ := args["id"].(string)
	p := getProc(id)
	if p == nil {
		return Result{Output: "process not found: " + id, IsError: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	status := "running"
	if p.Done {
		status = "done"
	}
	out := p.Output.String()
	if len(out) > 5000 {
		out = out[len(out)-5000:]
	}
	return Result{Output: fmt.Sprintf("%s (%s)\n%s", id, status, out)}
}

func processKill(args map[string]any) Result {
	id, _ := args["id"].(string)
	p := getProc(id)
	if p == nil {
		return Result{Output: "process not found: " + id, IsError: true}
	}
	if p.Done {
		return Result{Output: id + " already finished"}
	}
	if err := syscall.Kill(-p.Cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = p.Cmd.Process.Kill()
	}
	return Result{Output: id + " killed"}
}

func processWait(ctx context.Context, args map[string]any) Result {
	id, _ := args["id"].(string)
	p := getProc(id)
	if p == nil {
		return Result{Output: "process not found: " + id, IsError: true}
	}
	timeout := 30
	if t, ok := toInt(args["timeout"]); ok && t > 0 {
		timeout = t
	}
	done := make(chan struct{})
	go func() {
		_ = p.Cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		p.mu.Lock()
		out := p.Output.String()
		p.mu.Unlock()
		return Result{Output: id + " finished\n" + out}
	case <-ctx.Done():
		return Result{Output: id + " wait cancelled", IsError: true}
	case <-time.After(time.Duration(timeout) * time.Second):
		return Result{Output: id + " still running after " + fmt.Sprintf("%d", timeout) + "s", IsError: true}
	}
}

func getProc(id string) *proc {
	procMu.Lock()
	defer procMu.Unlock()
	return procMap[id]
}