package php

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
				t.Errorf("Dump() differs from %s, run go test -update and review the diff\n%s", golden, firstDiff(got, string(want)))
			}
		})
	}
}

// dumpComments lists the comments, since code.Dump leaves them out.
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

// lexDump renders the tokens of src as kind:text, with string fields in
// braces.
func lexDump(src string) string {
	l := lex([]byte(src), 0, len(src), startsInCode([]byte(src)))
	var parts []string
	for _, t := range l.toks {
		var s strings.Builder
		s.WriteString(t.kind.String() + ":" + src[t.s:t.e])
		for _, f := range t.fields {
			if f.varName {
				s.WriteString(" ${" + src[f.s:f.e] + "}")
			} else {
				s.WriteString(" {" + src[f.s:f.e] + "}")
			}
		}
		parts = append(parts, s.String())
	}
	return strings.Join(parts, " | ")
}

func TestLexer(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"inline html", "<p>a</p><?php $x ?>\n<b>", "punct:<?php | ident:$x | punct:?>"},
		{"echo tag and xml", "<?xml version=\"1.0\"?><?= $t ?>", "punct:<?= | ident:$t | punct:?>"},
		{"comment ends at close tag", "<?php // c ?> html # x", "punct:<?php | comment:// c | punct:?>"},
		{"hash comment and attribute", "<?php # c\n#[A(1)] fn() => 1;", "punct:<?php | comment:# c | punct:# | punct:[ | ident:A | punct:( | number:1 | punct:) | punct:] | keyword:fn | punct:( | punct:) | punct:=> | number:1 | punct:;"},
		{"doc and block comments", "<?php /** d */ /**/ /* b */", "punct:<?php | comment:/** d */ | comment:/**/ | comment:/* b */"},
		{"variables", "<?php $a $$b ${'c'}", "punct:<?php | ident:$a | punct:$ | ident:$b | punct:$ | punct:{ | string:'c' | punct:}"},
		{"interpolation", `<?php "a $b[0] {$c->d['e']} $f->g ${h} \$i"`, `punct:<?php | string:"a $b[0] {$c->d['e']} $f->g ${h} \$i" {$b} {$c->d['e']} {$f->g} ${h}`},
		{"heredoc flexible closing", "<?php $s = <<<EOT\n  a {$b}\n  EOT;\n$c;", "punct:<?php | ident:$s | punct:= | string:<<<EOT\n  a {$b}\n  EOT {$b} | punct:; | ident:$c | punct:;"},
		{"nowdoc", "<?php <<<'X'\n$a\nX . 1", "punct:<?php | string:<<<'X'\n$a\nX | punct:. | number:1"},
		{"casts", "<?php (int) $a; ( string )$b; (Foo) $c;", "punct:<?php | keyword:(int) | ident:$a | punct:; | keyword:( string ) | ident:$b | punct:; | punct:( | ident:Foo | punct:) | ident:$c | punct:;"},
		{"keywords as member names", "<?php $a->list(); A::new(); function print() {} A::class", "punct:<?php | ident:$a | punct:-> | ident:list | punct:( | punct:) | punct:; | ident:A | punct::: | ident:new | punct:( | punct:) | punct:; | keyword:function | ident:print | punct:( | punct:) | punct:{ | punct:} | ident:A | punct::: | keyword:class"},
		{"contextual enum", "<?php enum Suit {} enum(1);", "punct:<?php | keyword:enum | ident:Suit | punct:{ | punct:} | ident:enum | punct:( | number:1 | punct:) | punct:;"},
		{"operators", "<?php $a ??= $b?->c <=> 1.5e3 ** 0x1F;", "punct:<?php | ident:$a | punct:??= | ident:$b | punct:?-> | ident:c | punct:<=> | number:1.5e3 | punct:** | number:0x1F | punct:;"},
		{"halt compiler", "<?php __halt_compiler(); <?php $x", "punct:<?php | keyword:__halt_compiler | punct:( | punct:) | punct:;"},
		{"snippet without tags", "rename", "ident:rename"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lexDump(tt.src); got != tt.want {
				t.Errorf("lex(%q)\n got: %s\nwant: %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestSpans(t *testing.T) {
	src := "<?php\nnamespace A;\n/** Doc. */\n#[Attr]\nfinal class C {\n    public function __construct(private int $x = 1) {}\n    abstract protected function m(): void;\n}\n"
	f := code.NewFile("m.php", "/m.php", code.PHP, []byte(src))
	parse(f)
	tests := []struct {
		fqn, span, body string
	}{
		{"A", "namespace A;\n/** Doc. */\n#[Attr]\nfinal class C {\n    public function __construct(private int $x = 1) {}\n    abstract protected function m(): void;\n}", ""},
		{"A.C", "#[Attr]\nfinal class C {\n    public function __construct(private int $x = 1) {}\n    abstract protected function m(): void;\n}", "{\n    public function __construct(private int $x = 1) {}\n    abstract protected function m(): void;\n}"},
		{"A.C.__construct", "public function __construct(private int $x = 1) {}", "{}"},
		{"A.C.__construct->x", "private int $x = 1", ""},
		{"A.C.x", "private int $x = 1", ""},
		{"A.C.m", "abstract protected function m(): void;", ""},
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

// TestTruncated parses all prefixes of the testdata files, to cut strings,
// comments, heredocs and brackets at each possible offset.
func TestTruncated(t *testing.T) {
	err := filepath.WalkDir("testdata", func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || filepath.Ext(path) == ".golden" {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for n := range len(src) + 1 {
			f := code.NewFile("m.php", "/m.php", code.PHP, src[:n])
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s cut at %d: %v", path, n, r)
					}
				}()
				resolve([]*code.File{f}, map[*code.File]*fileInfo{f: parse(f)})
			}()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
