package python

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
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			ix, err := code.Load(code.LoadOptions{
				Root:      filepath.Join("testdata", e.Name()),
				Frontends: []code.Frontend{New()},
				Resolve:   true,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range ix.Errors {
				t.Errorf("load error: %v", err)
			}
			got := code.Dump(ix) + dumpComments(ix)
			golden := filepath.Join("testdata", e.Name()+".golden")
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
		})
	}
}

// dumpComments lists the comments and docstrings, since code.Dump leaves
// them out.
func dumpComments(ix *code.Index) string {
	var b strings.Builder
	kinds := map[code.CommentKind]string{code.LineComment: "line", code.BlockComment: "block", code.DocComment: "doc"}
	for _, f := range ix.Files {
		for _, c := range f.Comments {
			p := f.Pos(c.Span.Start)
			b.WriteString("comment " + f.Path + " L" + strconv.Itoa(p.Line) + ":" + strconv.Itoa(p.Col) + " " + kinds[c.Kind] + " " + strconv.Quote(f.CommentText(c)) + "\n")
		}
	}
	return b.String()
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

// lexDump renders the parser tokens of src. Synthetic tokens show up by
// name, the others as kind:text, with f-string fields in braces.
func lexDump(src string) string {
	l := lex([]byte(src), 0, len(src), true, false)
	var parts []string
	for _, t := range l.toks {
		text := src[t.span.Start:t.span.End]
		switch t.kind {
		case tNewline:
			parts = append(parts, "NL")
		case tIndent:
			parts = append(parts, "IN")
		case tDedent:
			parts = append(parts, "DE")
		case tEOF:
		case tString:
			var s strings.Builder
			s.WriteString("str:" + text)
			for _, f := range t.fields {
				s.WriteString(" {" + src[f.Start:f.End] + "}")
			}
			parts = append(parts, s.String())
		case tName:
			parts = append(parts, "name:"+text)
		case tKeyword:
			parts = append(parts, "kw:"+text)
		case tNumber:
			parts = append(parts, "num:"+text)
		case tOp:
			parts = append(parts, "op:"+text)
		}
	}
	return strings.Join(parts, " | ")
}

func TestLexer(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"prefixes", `rb'\x00' Rb"a" u'x' BR'y' br`, `str:rb'\x00' | str:Rb"a" | str:u'x' | str:BR'y' | name:br | NL`},
		{"not a prefix", `ur'x'`, `name:ur | str:'x' | NL`},
		{"escaped quotes", `'it\'s' "a\\" x`, `str:'it\'s' | str:"a\\" | name:x | NL`},
		{"raw string keeps escaped quote", `r'\'' x`, `str:r'\'' | name:x | NL`},
		{"triple quotes", `"""a"b""c""" x`, `str:"""a"b""c""" | name:x | NL`},
		{"f-string conversion and spec", `f'{x!r:>{w}.{p}}'`, `str:f'{x!r:>{w}.{p}}' {x} {w} {p} | NL`},
		{"f-string nested same quotes", `f"{"a" + d["k"]}" + x`, `str:f"{"a" + d["k"]}" {"a" + d["k"]} | op:+ | name:x | NL`},
		{"f-string escaped braces", `f"{{lit}} {v}"`, `str:f"{{lit}} {v}" {v} | NL`},
		{"f-string not equal", `f"{a != b}"`, `str:f"{a != b}" {a != b} | NL`},
		{"f-string self documenting", `f"{x=}"`, `str:f"{x=}" {x=} | NL`},
		{"f-string dict and nested f-string", `f"{ {'k': 1}['k'] } {f'{y}'}"`, `str:f"{ {'k': 1}['k'] } {f'{y}'}" { {'k': 1}['k'] } {f'{y}'} | NL`},
		{"f-string named escape", `f"\N{BULLET} {x}"`, `str:f"\N{BULLET} {x}" {x} | NL`},
		{"raw f-string", `rf"\d{n}"`, `str:rf"\d{n}" {n} | NL`},
		{"t-string", `t"{name}"`, `str:t"{name}" {name} | NL`},
		{"multiline f-string", "f'''a\n{x\n + y}'''", "str:f'''a\n{x\n + y}''' {x\n + y} | NL"},
		{"unterminated string", "s = 'abc\nx", `name:s | op:= | str:'abc | NL | name:x | NL`},
		{"numbers", `1_000.5e-3j 0x_FF .5 1e5 0o17 1if`, `num:1_000.5e-3j | num:0x_FF | num:.5 | num:1e5 | num:0o17 | num:1 | kw:if | NL`},
		{"operators", `a **= b // c -> d := e ... @`, `name:a | op:**= | name:b | op:// | name:c | op:-> | name:d | op::= | name:e | op:... | op:@ | NL`},
		{"indentation", "if a:\n    b\n\n    # c\n    d\ne", `kw:if | name:a | op:: | NL | IN | name:b | NL | name:d | NL | DE | name:e | NL`},
		{"implicit and explicit joining", "x = (1,\n  2) + \\\n  3\ny", `name:x | op:= | op:( | num:1 | op:, | num:2 | op:) | op:+ | num:3 | NL | name:y | NL`},
		{"unbalanced bracket recovery", "x = (1,\ndef f():\n    pass", `name:x | op:= | op:( | num:1 | op:, | NL | kw:def | name:f | op:( | op:) | op:: | NL | IN | kw:pass | NL | DE`},
		{"unicode identifiers", "caf\u00e9 = \u03c0", "name:caf\u00e9 | op:= | name:\u03c0 | NL"},
		{"soft keywords are names", "match = type", `name:match | op:= | name:type | NL`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lexDump(tt.src); got != tt.want {
				t.Errorf("lex(%q)\n got: %s\nwant: %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestLexerComments(t *testing.T) {
	src := "x = 1  # one\r\n# two\ny = '#not'\n"
	l := lex([]byte(src), 0, len(src), true, false)
	var got []string
	for _, c := range l.comments {
		got = append(got, src[c.Span.Start:c.Span.End])
	}
	if want := []string{"# one", "# two"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("comments = %q; want %q", got, want)
	}
}

func TestSoftKeywords(t *testing.T) {
	src := "match x:\n    case [a]:\n        pass\ntype T = int\nmatch(x)\n"
	f := code.NewFile("m.py", "/m.py", code.Python, []byte(src))
	parseFile(f)
	var got []string
	for _, tok := range f.Tokens {
		if text := f.TokenText(tok); text == "match" || text == "case" || text == "type" {
			got = append(got, text+":"+tok.Kind.String())
		}
	}
	want := "match:keyword case:keyword type:keyword match:ident"
	if strings.Join(got, " ") != want {
		t.Errorf("soft keyword kinds = %q; want %q", strings.Join(got, " "), want)
	}
}

func TestModuleName(t *testing.T) {
	tests := []struct {
		rel, want string
		pkg       bool
	}{
		{"calculator.py", "calculator", false},
		{"pkg/sub/mod.py", "pkg.sub.mod", false},
		{"src/pkg/__init__.py", "pkg", true},
		{"pkg/types.pyi", "pkg.types", false},
		{"__init__.py", "proj", true},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			name, pkg := moduleName(tt.rel, "/work/proj/"+tt.rel)
			if name != tt.want || pkg != tt.pkg {
				t.Errorf("moduleName(%q) = %q, %v; want %q, %v", tt.rel, name, pkg, tt.want, tt.pkg)
			}
		})
	}
}

func TestSpans(t *testing.T) {
	src := "@log\n@other(1)\ndef f(a,\n      b=2):\n    \"\"\"Doc.\"\"\"\n    return a  # trailing\n\n\nclass C: pass\nx: int = (\n    1)\n"
	f := code.NewFile("m.py", "/m.py", code.Python, []byte(src))
	parseFile(f)
	tests := []struct {
		fqn, span, body string
	}{
		{"m.f", "@log\n@other(1)\ndef f(a,\n      b=2):\n    \"\"\"Doc.\"\"\"\n    return a", "\"\"\"Doc.\"\"\"\n    return a"},
		{"m.f->b", "b=2", ""},
		{"m.C", "class C: pass", "pass"},
		{"m.x", "x: int = (\n    1)", ""},
	}
	for _, tt := range tests {
		t.Run(tt.fqn, func(t *testing.T) {
			for _, d := range f.Decls {
				if d.FQN() != tt.fqn {
					continue
				}
				if got := f.Text(d.Span); got != tt.span {
					t.Errorf("Span = %q; want %q", got, tt.span)
				}
				if got := f.Text(d.Body); got != tt.body {
					t.Errorf("Body = %q; want %q", got, tt.body)
				}
				return
			}
			t.Errorf("no declaration %s", tt.fqn)
		})
	}
}
