package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, frontmatter string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	os.MkdirAll(skillDir, 0o755)
	content := "---\n" + frontmatter + "\n---\n\n# " + name + "\n"
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644)
}

func TestDiscoverFindsSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillsDir := filepath.Join(home, ".hiroto", "skills")
	os.MkdirAll(skillsDir, 0o755)

	writeSkill(t, skillsDir, "test-skill", "name: test-skill\ndescription: a test skill")

	found := Discover(nil)
	if len(found) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(found))
	}
	if found[0].Name != "test-skill" {
		t.Errorf("Name = %q, want %q", found[0].Name, "test-skill")
	}
}

func TestDiscoverNestedCategory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillsDir := filepath.Join(home, ".hiroto", "skills")
	catDir := filepath.Join(skillsDir, "category")
	os.MkdirAll(catDir, 0o755)

	writeSkill(t, catDir, "nested-skill", "name: nested-skill\ndescription: nested")

	found := Discover(nil)
	if len(found) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(found))
	}
	if found[0].Name != "nested-skill" {
		t.Errorf("Name = %q, want %q", found[0].Name, "nested-skill")
	}
}

func TestParseFrontmatter(t *testing.T) {
	name, desc := parseFrontmatter("---\nname: foo\ndescription: bar baz\n---\nbody")
	if name != "foo" {
		t.Errorf("name = %q, want %q", name, "foo")
	}
	if desc != "bar baz" {
		t.Errorf("desc = %q, want %q", desc, "bar baz")
	}
}

func TestParseFrontmatterNoBlock(t *testing.T) {
	name, desc := parseFrontmatter("just markdown\nno frontmatter")
	if name != "" || desc != "" {
		t.Errorf("expected empty, got %q %q", name, desc)
	}
}

func TestIndexLineTruncates(t *testing.T) {
	s := Skill{Name: "x", Description: string(make([]byte, 100))}
	line := s.IndexLine()
	if len(line) > 65 {
		t.Errorf("IndexLine too long: %d chars", len(line))
	}
}

func TestDiscoverEmptyDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".hiroto", "skills"), 0o755)
	if found := Discover(nil); len(found) != 0 {
		t.Errorf("expected 0 skills, got %d", len(found))
	}
}

func TestDiscoverDuplicateFirstWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillsDir := filepath.Join(home, ".hiroto", "skills")
	os.MkdirAll(skillsDir, 0o755)

	writeSkill(t, skillsDir, "dup-skill", "name: dup\ndescription: first")
	writeSkill(t, skillsDir, "dup-skill-2", "name: dup\ndescription: second")

	found := Discover(nil)
	count := 0
	for _, s := range found {
		if s.Name == "dup" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 'dup' skill, got %d", count)
	}
}
