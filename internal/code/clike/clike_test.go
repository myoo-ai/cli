package clike

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func TestGolden(t *testing.T) {
	for _, name := range []string{"java", "csharp"} {
		t.Run(name, func(t *testing.T) {
			ix, err := code.Load(code.LoadOptions{
				Root:      filepath.Join("testdata", name),
				Frontends: []code.Frontend{New()},
				Resolve:   true,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range ix.Errors {
				t.Errorf("load: %v", err)
			}
			got := code.Dump(ix)
			golden := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("dump differs from %s (run go test -update and review the diff):\n%s", golden, firstDiff(got, string(want)))
			}
		})
	}
}

func firstDiff(got, want string) string {
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(gl), len(wl)) {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			return fmt.Sprintf("line %d\n got: %s\nwant: %s", i+1, g, w)
		}
	}
	return ""
}

func TestSpans(t *testing.T) {
	tests := []struct {
		name, src  string
		lang       code.Lang
		decl       string
		span, body string
	}{
		{
			name: "java annotated method",
			lang: code.Java,
			src:  "class A {\n  /** doc */\n  @Override\n  public void m() { x(); }\n}",
			decl: "m",
			span: "@Override\n  public void m() { x(); }",
			body: "{ x(); }",
		},
		{
			name: "csharp attributed class",
			lang: code.CSharp,
			src:  "/// doc\n[Serializable]\npublic sealed class C { }",
			decl: "C",
			span: "[Serializable]\npublic sealed class C { }",
			body: "{ }",
		},
		{
			name: "csharp expression-bodied method",
			lang: code.CSharp,
			src:  "class C { static int F() => 1 + 2; }",
			decl: "F",
			span: "static int F() => 1 + 2;",
			body: "1 + 2",
		},
		{
			name: "java second declarator",
			lang: code.Java,
			src:  "class A { private int a = 1, b; }",
			decl: "b",
			span: "private int a = 1, b;",
		},
		{
			name: "csharp file-scoped namespace",
			lang: code.CSharp,
			src:  "namespace N.M;\nclass C {}\n",
			decl: "N.M",
			span: "namespace N.M;\nclass C {}\n",
		},
		{
			name: "java catch parameter",
			lang: code.Java,
			src:  "class A { void m() { try { } catch (final IOException | Error e) { } } }",
			decl: "e",
			span: "final IOException | Error e",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := code.NewFile("x", "x", tc.lang, []byte(tc.src))
			if err := New().Parse(f); err != nil {
				t.Fatal(err)
			}
			var d *code.Decl
			for _, c := range f.Decls {
				if c.Name == tc.decl {
					d = c
				}
			}
			if d == nil {
				t.Fatalf("no declaration %q", tc.decl)
			}
			if got := f.Text(d.Span); got != tc.span {
				t.Errorf("span = %q, want %q", got, tc.span)
			}
			if got := f.Text(d.Body); got != tc.body {
				t.Errorf("body = %q, want %q", got, tc.body)
			}
		})
	}
}

// TestTruncatedInput parses and resolves prefixes of each fixture, a few
// bytes apart, to hit unterminated literals, comments and brackets in all
// positions. The frontend shouldn't panic on any of them.
func TestTruncatedInput(t *testing.T) {
	for _, dir := range []string{"java", "csharp"} {
		err := filepath.WalkDir(filepath.Join("testdata", dir), func(path string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fe := New()
			lang, ok := fe.Lang(path)
			if !ok {
				return nil
			}
			t.Run(path, func(t *testing.T) {
				var files []*code.File
				for n := 0; n <= len(src); n += 3 {
					f := code.NewFile(path, path, lang, src[:n])
					if err := fe.Parse(f); err != nil {
						t.Fatalf("prefix %d: %v", n, err)
					}
					files = append(files, f)
				}
				fe.Resolve(&code.Index{}, files)
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// FuzzParse checks that parsing and resolving arbitrary input doesn't panic
// or hang. The seed corpus runs with go test.
func FuzzParse(f *testing.F) {
	f.Add("class A { void m() { int x = (1 + ; } }", false)
	f.Add(`var s = $"{a ? $"{b}" : c}"; namespace N; record R(int X);`, true)
	f.Add("@interface A { int v() default 1; } enum E { X { void m() {} }; }", false)
	f.Fuzz(func(t *testing.T, src string, cs bool) {
		lang, path := code.Java, "f.java"
		if cs {
			lang, path = code.CSharp, "f.cs"
		}
		file := code.NewFile(path, path, lang, []byte(src))
		fe := New()
		if err := fe.Parse(file); err != nil {
			t.Fatal(err)
		}
		fe.Resolve(&code.Index{}, []*code.File{file})
	})
}

func BenchmarkParse(b *testing.B) {
	var src strings.Builder
	for src.Len() < 400_000 {
		data, err := os.ReadFile(filepath.Join("testdata", "csharp", "App", "Program.cs"))
		if err != nil {
			b.Fatal(err)
		}
		src.Write(data)
	}
	data := []byte(src.String())
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		parse(code.NewFile("p.cs", "p.cs", code.CSharp, data))
	}
}
