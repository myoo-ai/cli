package rust

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
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

// dumpComments lists the comments, which code.Dump omits.
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

func lexDump(src string) string {
	l := lex([]byte(src))
	var parts []string
	for _, t := range l.toks {
		parts = append(parts, t.kind.String()+":"+src[t.s:t.e])
	}
	return strings.Join(parts, " | ")
}

func TestLexer(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"nested block comment", "/* a /* b */ c */ x", "comment:/* a /* b */ c */ | ident:x"},
		{"doc comments", "/// d\n//! i\n//// plain\n/** d */ /*! i */ /**/ /*** p */", "comment:/// d | comment://! i | comment://// plain | comment:/** d */ | comment:/*! i */ | comment:/**/ | comment:/*** p */"},
		{"raw strings", `r"a\" r#"b"c"# r##"x"#y"## br#"z"# cr"w"`, `string:r"a\" | string:r#"b"c"# | string:r##"x"#y"## | string:br#"z"# | string:cr"w"`},
		{"byte and c strings", `b"a" c"b" b'x' b'\''`, `string:b"a" | string:c"b" | string:b'x' | string:b'\''`},
		{"chars and lifetimes", `'a' '\n' '\u{1F600}' 'a 'static 'outer: '🦀'`, `string:'a' | string:'\n' | string:'\u{1F600}' | ident:'a | ident:'static | ident:'outer | punct:: | string:'🦀'`},
		{"raw identifier", `r#type r#fn r`, `ident:r#type | ident:r#fn | ident:r`},
		{"numbers", `1_000u32 0x_FFi64 1e-3_f64 2.5 1..2 1.max(2) 0b1010 1.`, `number:1_000u32 | number:0x_FFi64 | number:1e-3_f64 | number:2.5 | number:1 | punct:.. | number:2 | number:1 | punct:. | ident:max | punct:( | number:2 | punct:) | number:0b1010 | number:1.`},
		{"attributes are tokens", `#![allow(x)] #[derive(Debug)]`, `punct:# | punct:! | punct:[ | ident:allow | punct:( | ident:x | punct:) | punct:] | punct:# | punct:[ | ident:derive | punct:( | ident:Debug | punct:) | punct:]`},
		{"operators", `a::b -> c => d ..= e ... f >>= g`, `ident:a | punct::: | ident:b | punct:-> | ident:c | punct:=> | ident:d | punct:..= | ident:e | punct:... | ident:f | punct:>>= | ident:g`},
		{"keywords", `fn Self self union`, `keyword:fn | keyword:Self | keyword:self | ident:union`},
		{"shebang", "#!/usr/bin/env run\nfn", "comment:#!/usr/bin/env run | keyword:fn"},
		{"unterminated", `"abc`, `string:"abc`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lexDump(tt.src); got != tt.want {
				t.Errorf("lex(%q)\n got: %s\nwant: %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestCommentKinds(t *testing.T) {
	l := lex([]byte("/// d\n//! i\n//// p\n// l\n/** d */\n/*! i */\n/* b */\n/**/"))
	var got []code.CommentKind
	for _, c := range l.comments {
		got = append(got, c.Kind)
	}
	want := []code.CommentKind{code.DocComment, code.DocComment, code.LineComment, code.LineComment, code.DocComment, code.DocComment, code.BlockComment, code.BlockComment}
	if !slices.Equal(got, want) {
		t.Errorf("kinds = %v; want %v", got, want)
	}
}

func TestFileLayout(t *testing.T) {
	tests := []struct {
		rel, module string
		root, modRS bool
	}{
		{"src/lib.rs", "proj", true, true},
		{"src/main.rs", "proj", true, true},
		{"src/geo/mod.rs", "proj.geo", false, true},
		{"src/geo/point.rs", "proj.geo.point", false, false},
		{"src/bin/tool.rs", "tool", true, true},
		{"src/bin/tool/main.rs", "tool", true, true},
		{"src/bin/tool/cli.rs", "tool.cli", false, false},
		{"tests/it.rs", "it", true, true},
		{"tests/common/mod.rs", "common", false, true},
		{"build.rs", "build", true, true},
		{"my-crate/src/lib.rs", "my_crate", true, true},
		{"single.rs", "proj.single", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			ly := fileLayout(tt.rel, "/nonexistent/proj/"+tt.rel)
			if ly.module != tt.module || ly.root != tt.root || ly.modRS != tt.modRS {
				t.Errorf("fileLayout(%q) = %q root=%v modRS=%v; want %q root=%v modRS=%v", tt.rel, ly.module, ly.root, ly.modRS, tt.module, tt.root, tt.modRS)
			}
		})
	}
}

func TestSpans(t *testing.T) {
	src := "/// Doc.\n#[derive(Debug)]\npub struct S {\n    a: u8,\n}\n\nimpl S {\n    pub fn get(&self) -> u8 { self.a }\n}\n"
	f := code.NewFile("lib.rs", "/nonexistent/proj/lib.rs", code.Rust, []byte(src))
	parseFile(f)
	tests := []struct{ fqn, span, body string }{
		{"proj.S", "#[derive(Debug)]\npub struct S {\n    a: u8,\n}", "{\n    a: u8,\n}"},
		{"proj.S.a", "a: u8", ""},
		{"proj.S.get", "pub fn get(&self) -> u8 { self.a }", "{ self.a }"},
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
