package golang

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestGolden(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Run(e.Name(), func(t *testing.T) {
				checkGolden(t, filepath.Join("testdata", e.Name()), filepath.Join("testdata", e.Name()+".golden"))
			})
		}
	}
}

// TestMalformed loads files with syntax errors. They are written at test
// time so the repository stays gofmt-clean.
func TestMalformed(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"broken.go": "package broken\n\nfunc Good() int { return 1 }\n\nfunc Bad( {\n\tx := \"unterminated\n\treturn Good(\n}\n\n/* unterminated comment\n",
		"ok.go":     "package broken\n\nfunc Fine() int { return Good() }\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checkGolden(t, root, filepath.Join("testdata", "malformed.golden"))
}

func checkGolden(t *testing.T, root, golden string) {
	t.Helper()
	ix, err := code.Load(code.LoadOptions{Root: root, Frontends: []code.Frontend{New()}, Resolve: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range ix.Errors {
		t.Errorf("load error: %v", err)
	}
	got := code.Dump(ix)
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("Dump() differs from %s; run go test -update and review the diff\n%s", golden, firstDiff(got, string(want)))
	}
}

func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(g), len(w)) {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return "line " + strconv.Itoa(i+1) + ":\n got: " + gl + "\nwant: " + wl
		}
	}
	return ""
}

func TestScan(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"raw string with carriage return", "x := `a\r\nb`", []string{"ident x", "punct :=", "string `a\r\nb`"}},
		{"line comment before crlf", "a // c\r\nb", []string{"ident a", "comment // c", "ident b"}},
		{"block comment", "/* a\r\n*/ b", []string{"comment /* a\r\n*/", "ident b"}},
		{"unterminated block comment", "a /* b", []string{"ident a", "comment /* b"}},
		{"unterminated raw string", "a `b", []string{"ident a", "string `b"}},
		{"runes and numbers", `'\'' 0x1p-2 1_000i`, []string{`string '\''`, "number 0x1p-2", "number 1_000i"}},
		{"keywords and operators", "func(a ...int) <-chan", []string{"keyword func", "punct (", "ident a", "punct ...", "ident int", "punct )", "punct <-", "keyword chan"}},
		{"explicit semicolons only", "a; b\n", []string{"ident a", "punct ;", "ident b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, _ := scan([]byte(tt.src))
			var got []string
			for _, tok := range toks {
				got = append(got, tok.Kind.String()+" "+tt.src[tok.Span.Start:tok.Span.End])
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("scan(%q) = %q; want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestGuessName(t *testing.T) {
	tests := map[string]string{
		"fmt":                        "fmt",
		"github.com/acme/money":      "money",
		"github.com/acme/money/v2":   "money",
		"gopkg.in/yaml.v3":           "yaml",
		"github.com/acme/go-toolkit": "toolkit",
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			if got := guessName(path); got != want {
				t.Errorf("guessName(%q) = %q; want %q", path, got, want)
			}
		})
	}
}

func TestCommentsAndSpans(t *testing.T) {
	src := "// Package p is documented.\npackage p\n\n// T is documented.\ntype T struct {\n\tA int // trailing\n}\n\nvar (\n\tx = 1\n)\n\n/* block */\nfunc f() {}\n"
	f := code.NewFile("p.go", "/nonexistent/p.go", code.Go, []byte(src))
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, c := range f.Comments {
		kinds = append(kinds, strconv.Itoa(int(c.Kind))+" "+f.Text(c.Span))
	}
	wantKinds := []string{
		strconv.Itoa(int(code.DocComment)) + " // Package p is documented.",
		strconv.Itoa(int(code.DocComment)) + " // T is documented.",
		strconv.Itoa(int(code.LineComment)) + " // trailing",
		strconv.Itoa(int(code.DocComment)) + " /* block */",
	}
	if strings.Join(kinds, "|") != strings.Join(wantKinds, "|") {
		t.Errorf("comments = %q; want %q", kinds, wantKinds)
	}
	spans := map[string]string{}
	for _, d := range f.Decls {
		spans[d.FQN()] = f.Text(d.Span) + "|" + f.Text(d.Body)
	}
	wantSpans := map[string]string{
		"p.T":   "type T struct {\n\tA int // trailing\n}|{\n\tA int // trailing\n}",
		"p.T.A": "A int|",
		"p.x":   "x = 1|",
		"p.f":   "func f() {}|{}",
	}
	for fqn, want := range wantSpans {
		if got := spans[fqn]; got != want {
			t.Errorf("%s span|body = %q; want %q", fqn, got, want)
		}
	}
}
