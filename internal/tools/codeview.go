package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// codeStruct returns a structural summary of a code file: declaration
// signatures with line numbers, bodies elided. Mirrors OMP's `read`
// structural view so the model sees the file shape before diving into a body.
// ok=false means the extension isn't a recognized language.
func codeStruct(data []byte, ext string) (string, bool) {
	re := declRe(ext)
	if re == nil {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var b strings.Builder
	count := 0
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		count++
		b.WriteString(fmt.Sprintf("%d|%s\n", i+1, elideBody(line)))
	}
	if count == 0 {
		return "", false
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// elideBody trims a declaration signature to its header, replacing the block
// with a "…" marker so the structural view stays a compact index.
func elideBody(line string) string {
	t := strings.TrimRight(line, " ")
	if i := strings.Index(t, "{}"); i >= 0 {
		return t[:i+1] + " … }"
	}
	switch {
	case strings.HasSuffix(t, "{"):
		return t + " … }"
	case strings.HasSuffix(t, ":"):
		return t + " …"
	default:
		return t
	}
}

// declRe maps a file extension to the regexp that matches top-level
// declarations for that language. Heuristic, not a parser — good enough to
// surface the file's shape (functions, types, classes).
func declRe(ext string) *regexp.Regexp {
	switch ext {
	case ".go":
		return goDecl
	case ".py", ".pyi":
		return pyDecl
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return tsDecl
	case ".rs":
		return rsDecl
	default:
		return nil
	}
}

var goDecl = regexp.MustCompile(`^(func|type|var|const)\s+`)

var pyDecl = regexp.MustCompile(`^(async\s+def|def|class)\s+`)

var tsDecl = regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?(function|class|interface|type|enum|const|let|var)\s+`)

var rsDecl = regexp.MustCompile(`^(pub(\s+\([^)]*\))?\s+)?(fn|struct|enum|trait|impl|mod|type|const|static)\s+`)
