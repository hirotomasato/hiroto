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
