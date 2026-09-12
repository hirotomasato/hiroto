package agent

import "testing"

func TestApproveTool(t *testing.T) {
	a := &Agent{}

	// Read-only tools always pass.
	for _, ro := range []string{"read_file", "search_files", "web_search", "lsp"} {
		if !a.approveTool(ro) {
			t.Errorf("read-only %q should always be approved", ro)
		}
	}

	// Risky + empty allow + no skip → refused.
	if a.approveTool("patch") {
		t.Error("risky patch with no allow/skip should be refused")
	}

	// Risky + allowlisted → approved.
	a2 := &Agent{Allow: []string{"patch", "terminal"}}
	for _, r := range []string{"patch", "terminal"} {
		if !a2.approveTool(r) {
			t.Errorf("allowlisted %q should be approved", r)
		}
	}
	// Still refused if not in the list.
	if a2.approveTool("write_file") {
		t.Error("write_file not in allow should be refused")
	}

	// SkipPermissions bypasses everything.
	a3 := &Agent{SkipPermissions: true}
	if !a3.approveTool("write_file") {
		t.Error("skip-permissions should approve everything")
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
