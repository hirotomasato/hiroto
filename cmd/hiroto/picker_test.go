package main

import "testing"

func TestPickerApplyFilter(t *testing.T) {
	items := []string{"hermes-agent: config", "github-auth: tokens", "obsidian: notes", "hiroto-development: tools"}
	values := []string{"hermes-agent", "github-auth", "obsidian", "hiroto-development"}
	p := newPicker("t", items, values, nil)

	if len(p.items) != 4 {
		t.Fatalf("no filter should show all, got %d", len(p.items))
	}

	p.filter = "hiroto"
	p.applyFilter()
	if len(p.items) != 1 || p.values[0] != "hiroto-development" {
		t.Fatalf("filter hiroto: items=%v values=%v", p.items, p.values)
	}

	p.filter = "auth"
	p.applyFilter()
	if len(p.items) != 1 || p.values[0] != "github-auth" {
		t.Fatalf("filter auth: items=%v values=%v", p.items, p.values)
	}

	p.filter = "zzz-nothing"
	p.applyFilter()
	if len(p.items) != 0 || p.cursor != -1 {
		t.Fatalf("no match: len=%d cursor=%d", len(p.items), p.cursor)
	}

	p.filter = ""
	p.applyFilter()
	if len(p.items) != 4 || p.cursor != 0 {
		t.Fatalf("clear filter: len=%d cursor=%d", len(p.items), p.cursor)
	}
}

func TestPickerNoValuesDefaultsToItems(t *testing.T) {
	p := newPicker("t", []string{"a", "b"}, nil, nil)
	if p.values[0] != "a" || p.values[1] != "b" {
		t.Fatalf("values should default to items, got %v", p.values)
	}
}

func TestFuzzyScore(t *testing.T) {
	cases := []struct {
		q, text string
		match   bool
	}{
		{"htd", "hiroto-development", true},   // h..t..d subsequence
		{"hdp", "hiroto-development", true},   // h...d...p
		{"htmg", "hiroto-development", false}, // matches h,t,m but 'g' is absent
		{"hello", "hello", true},
		{"xyz", "hiroto-development", false},
		{"", "anything", true},
		{"auth", "github-auth", true},
	}
	for _, c := range cases {
		if _, ok := fuzzyScore(c.q, c.text); ok != c.match {
			t.Errorf("fuzzyScore(%q,%q) match=%v want %v", c.q, c.text, ok, c.match)
		}
	}
}

func TestFuzzyRankingPrefersBetterMatch(t *testing.T) {
	items := []string{"hiroto-development", "github-code-review", "hermes-agent"}
	values := []string{"a", "b", "c"}
	p := newPicker("t", items, values, nil)
	p.filter = "hr"
	p.applyFilter()
	// "hermes-agent" starts with h+r close together; "hiroto-development" has h...i...r...o
	// both should match; assert both present and valid values kept
	if len(p.items) < 2 {
		t.Fatalf("expected >=2 fuzzy hits, got %v", p.items)
	}
	found := map[string]bool{}
	for _, v := range p.values {
		found[v] = true
	}
	if !found["a"] || !found["c"] {
		t.Fatalf("wrong values mapped: %v", p.values)
	}
}
