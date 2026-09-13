package agent

import "testing"

func TestApproveTool(t *testing.T) {
	// Default (empty Ask) = permissive: every tool runs, no prompts (OMP).
	a := &Agent{}
	for _, name := range []string{"read_file", "patch", "write_file", "terminal", "execute_code"} {
		if !a.approveTool(name) {
			t.Errorf("default must approve %q (opt-in gate)", name)
		}
	}

	// Tools listed in Ask require approval (refused until /allow).
	a2 := &Agent{Ask: []string{"terminal", "write_file"}}
	if a2.approveTool("terminal") {
		t.Error("terminal in Ask must require approval")
	}
	if a2.approveTool("write_file") {
		t.Error("write_file in Ask must require approval")
	}
	// Not in Ask → still runs freely.
	if !a2.approveTool("patch") {
		t.Error("patch not in Ask must run")
	}

	// SkipPermissions ignores Ask entirely.
	a3 := &Agent{SkipPermissions: true, Ask: []string{"terminal"}}
	if !a3.approveTool("terminal") {
		t.Error("skip-permissions must approve even an Ask tool")
	}
}

func TestIsRisky(t *testing.T) {
	if !isRisky("patch") || !isRisky("write_file") || !isRisky("terminal") {
		t.Error("patch/write_file/terminal must be risky")
	}
	if isRisky("read_file") || isRisky("search_files") {
		t.Error("read-only tools must not be risky")
	}
}

func TestPlanModeReadOnly(t *testing.T) {
	// PlanMode must refuse every risky tool, even if skipped or Ask-listed.
	a := &Agent{PlanMode: true, SkipPermissions: true, Ask: []string{"patch"}}
	for name := range riskyTools {
		if a.approveTool(name) {
			t.Errorf("plan mode must refuse risky %q even with skip+allow", name)
		}
	}
	// Read-only still fine.
	for _, ro := range []string{"read_file", "search_files", "lsp", "web_search"} {
		if !a.approveTool(ro) {
			t.Errorf("plan mode must allow read-only %q", ro)
		}
	}
	// Without plan mode, skip bypasses again.
	a.PlanMode = false
	if !a.approveTool("patch") {
		t.Error("with plan mode off + skip, patch should be approved")
	}
}
