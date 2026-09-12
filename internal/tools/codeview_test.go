package tools

import "testing"

// codeStruct must surface declarations and elide bodies across languages.
func TestCodeStruct(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		src  string
		want []string // substrings that must appear, in order
	}{
		{
			name: "go",
			ext:  ".go",
			src:  "package tools\n\nimport \"fmt\"\n\nfunc Foo(x int) string {\n\treturn \"\"\n}\n\ntype Bar struct {\n\tN int\n}\n\nvar X = 1\nconst Y = 2\n",
			want: []string{"func Foo(x int) string { … }", "type Bar struct { … }", "var X = 1", "const Y = 2"},
		},
		{
			name: "python",
			ext:  ".py",
			src:  "import os\n\n\ndef foo():\n    pass\n\n\nclass Bar:\n    pass\n\nasync def baz():\n    pass\n",
			want: []string{"def foo(): …", "class Bar: …", "async def baz(): …"},
		},
		{
			name: "typescript",
			ext:  ".ts",
			src:  "export interface User {\n  id: number\n}\n\nexport function getUser(): User {\n  return {} as User\n}\n\nclass Impl {}\n",
			want: []string{"export interface User { … }", "export function getUser(): User { … }", "class Impl { … }"},
		},
		{
			name: "rust",
			ext:  ".rs",
			src:  "pub fn main() {\n}\n\nstruct Point {\n    x: i32,\n}\n\nimpl Point {\n}\n\npub enum Kind { A, B }\n",
			want: []string{"pub fn main() { … }", "struct Point { … }", "impl Point { … }", "pub enum Kind { A, B }"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := codeStruct([]byte(c.src), c.ext)
			if !ok {
				t.Fatalf("codeStruct returned ok=false")
			}
			for _, want := range c.want {
				if !hasStr(got, want) {
					t.Errorf("missing %q in summary:\n%s", want, got)
				}
			}
		})
	}
}

// Unknown extension → no structural view.
func TestCodeStructUnknownExt(t *testing.T) {
	if _, ok := codeStruct([]byte("func f() {}\n"), ".txt"); ok {
		t.Fatal("expected ok=false for .txt")
	}
}

func hasStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || strIdx(s, sub) >= 0)
}

func strIdx(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
