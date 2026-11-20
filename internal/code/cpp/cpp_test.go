package cpp

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
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

// TestMalformed loads files with syntax errors. We write them at test time so
// tooling doesn't trip over them in the repository.
func TestMalformed(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"broken.c": "int good(void) { return 1; }\n\n" +
			"int bad(int x {\n  char *s = \"unterminated;\n  return good(\n}\n\n" +
			"struct S { int a; \n\x01\x02 int after(void) { return good(); }\n" +
			"/* unterminated comment\n",
		"broken.cpp": "namespace n {\nclass C : public {\n  void m() { x->; }\n  C(;\n};\n" +
			"template <class T> T f(T t) { return t; }\n}}}\n)))\n" +
			"int ok() { return n::f(1); }\nconst char *r = R\"x(never closed\n",
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
		t.Errorf("dump mismatch (run with -update to accept):\n%s", diff(string(want), got))
	}
}

func diff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			b.WriteString("- " + wl + "\n+ " + gl + "\n")
		}
	}
	return b.String()
}

func TestLang(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"c/a.c", "c/a.h", "mixed/b.cpp", "mixed/b.h", "inc/plain.h", "inc/klass.h"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		src := "int f(void);\n"
		if p == "inc/klass.h" {
			src = "namespace x { class Y; }\n"
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fe := New()
	tests := []struct {
		path string
		want code.Lang
	}{
		{"c/a.c", code.C},
		{"c/a.h", code.C},
		{"mixed/b.cpp", code.CPP},
		{"mixed/b.h", code.CPP},
		{"inc/plain.h", code.C},
		{"inc/klass.h", code.CPP},
		{"x/y.hpp", code.CPP},
		{"x/y.cc", code.CPP},
		{"x/y.py", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := fe.Lang(filepath.Join(root, tt.path))
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("Lang(%s) = %q, %v; want %q", tt.path, got, ok, tt.want)
			}
		})
	}
}

func TestSpans(t *testing.T) {
	src := "/** Doc. */\ntemplate <class T>\nT id(T v) { return v; }\nint a = 1, *b;\n" +
		"typedef struct { int x; } P;\nclass K { int m() const; };\nint K::m() const { return 0; }\n"
	f := code.NewFile("s.cpp", "s.cpp", code.CPP, []byte(src))
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range f.Decls {
		key := fmt.Sprintf("%s %s L%d", d.Kind, d.FQN(), f.Pos(d.NameSpan.Start).Line)
		got[key] = f.Text(d.Span) + "|" + f.Text(d.Body)
	}
	tests := []struct{ decl, want string }{
		{"function s.id L3", "template <class T>\nT id(T v) { return v; }|{ return v; }"},
		{"type_parameter s.id->T L2", "class T|"},
		{"parameter s.id->v L3", "T v|"},
		{"variable s.a L4", "int a = 1|"},
		{"variable s.b L4", "*b;|"},
		{"struct s.P L5", "typedef struct { int x; } P;|{ int x; }"},
		{"method s.K.m L6", "int m() const;|"},
		{"method s.K.m L7", "int K::m() const { return 0; }|{ return 0; }"},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			if got[tt.decl] != tt.want {
				t.Errorf("span|body = %q; want %q (all: %q)", got[tt.decl], tt.want, got)
			}
		})
	}
}

// TestRobust parses all truncations of the fixtures plus random token soup in
// both languages. The frontend shouldn't panic on any of it.
func TestRobust(t *testing.T) {
	var inputs []string
	for _, p := range []string{"testdata/cproj/main.c", "testdata/cppproj/src/main.cpp", "testdata/cppproj/include/geo/shape.h"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(b); i += 11 {
			inputs = append(inputs, string(b[:i]), string(b[i:]))
		}
	}
	vocab := strings.Fields(`{ } ( ) [ ] < > >> :: ; , = * & && -> . : ~ ... a b T x class struct
		enum namespace template typename using typedef operator new auto int const static return
		if for case this [[ ]] " ' R"( )" 1'0 /* // #define #if #else #endif public: virtual
		friend extern "C" [&] [=] decltype`)
	r := rand.New(rand.NewPCG(1, 2))
	for range 500 {
		var b strings.Builder
		for range r.IntN(60) {
			b.WriteString(vocab[r.IntN(len(vocab))])
			b.WriteString([]string{" ", "\n"}[r.IntN(2)])
		}
		inputs = append(inputs, b.String())
	}
	fe := New()
	for _, src := range inputs {
		for _, lang := range []code.Lang{code.C, code.CPP} {
			f := code.NewFile("t.cpp", "t.cpp", lang, []byte(src))
			func() {
				defer func() {
					if e := recover(); e != nil {
						t.Fatalf("%s: panic %v on %q", lang, e, src)
					}
				}()
				if err := fe.Parse(f); err != nil {
					t.Fatal(err)
				}
				fe.Resolve(nil, []*code.File{f})
			}()
		}
	}
}
