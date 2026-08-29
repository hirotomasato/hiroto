// Package skills implements the skill format: a directory containing
// SKILL.md with YAML frontmatter (name, description). Skills are indexed into
// the system prompt; the agent loads the full SKILL.md on demand.
package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Path        string // path to SKILL.md
	Dir         string
	Content     string // full markdown body (loaded lazily for index; kept here)
}

// IndexLine renders the ~57-char description window Hiroto uses in its index.
func (s Skill) IndexLine() string {
	d := s.Description
	if len(d) > 57 {
		d = d[:57] + "..."
	}
	return s.Name + ": " + d
}

// Discover scans skill directories for */SKILL.md, sorted by name.
// Extra dirs come from config; ~/.hiroto/skills is always included.
func Discover(extraDirs []string) []Skill {
	seen := map[string]Skill{}
	var dirs []string
	home := homeDir()
	dirs = append(dirs, filepath.Join(home, "skills"))
	dirs = append(dirs, extraDirs...)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// Nested categories (like Hiroto's autonomous-ai-agents/hiroto-agent)
			sub := filepath.Join(d, e.Name())
			subEntries, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			hasSub := false
			for _, se := range subEntries {
				if se.IsDir() {
					hasSub = true
					break
				}
			}
			if hasSub {
				for _, se := range subEntries {
					if se.IsDir() {
						loadSkillDir(filepath.Join(sub, se.Name()), seen)
					}
				}
			}
			loadSkillDir(sub, seen)
		}
	}
	var out []Skill
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func loadSkillDir(dir string, seen map[string]Skill) {
	path := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	name, desc := parseFrontmatter(string(data))
	if name == "" {
		name = filepath.Base(dir)
	}
	if _, dup := seen[name]; dup {
		return // first wins
	}
	seen[name] = Skill{Name: name, Description: desc, Path: path, Dir: dir, Content: string(data)}
}

// parseFrontmatter extracts name/description from a leading --- YAML block.
func parseFrontmatter(text string) (name, desc string) {
	if !strings.HasPrefix(text, "---") {
		return "", ""
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	fm := text[3 : 3+end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), `"'`)
		} else if strings.HasPrefix(line, "description:") {
			desc = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), `"'`)
		}
	}
	return name, desc
}

func homeDir() string {
	if h := os.Getenv("HIROTO_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hiroto")
}
