package gateway

import "testing"

// isAllowed denies everyone when the allowlist is empty (safe default) and
// permits only listed IDs otherwise.
func TestIsAllowed(t *testing.T) {
	// Empty allowlist → deny all, including id 0 (missing From).
	empty := &gw{allowed: map[int64]bool{}}
	if empty.isAllowed(12345) {
		t.Error("empty allowlist must deny known-looking IDs")
	}
	if empty.isAllowed(0) {
		t.Error("empty allowlist must deny id 0")
	}

	// Populated allowlist → only listed IDs pass.
	g := &gw{allowed: map[int64]bool{111: true, 222: true}}
	if !g.isAllowed(111) || !g.isAllowed(222) {
		t.Error("allowlisted IDs must pass")
	}
	if g.isAllowed(333) {
		t.Error("non-allowlisted ID must be denied")
	}
	if g.isAllowed(0) {
		t.Error("id 0 (missing From) must be denied even with a populated list")
	}
}
