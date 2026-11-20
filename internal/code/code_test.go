package code

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFQN(t *testing.T) {
	f := NewFile("calculator.py", "/x/calculator.py", Python, []byte("def calculate(a, b):\n    return a + b\n"))
	mod := f.SetModule("calculator")
	fn := f.NewDecl(mod, KindFunction, "calculate", Span{4, 13}, Span{0, 37})
	param := f.NewDecl(fn, KindParameter, "a", Span{14, 15}, Span{14, 15})
	inner := f.NewDecl(fn, KindFunction, "inner", Span{0, 0}, Span{0, 0})
	innerVar := f.NewDecl(inner, KindVariable, "x", Span{0, 0}, Span{0, 0})
	method := f.NewDecl(mod, KindMethod, "Add", Span{0, 0}, Span{0, 0})
	method.Receiver = "Calc"

	tests := []struct {
		d     *Decl
		want  string
		local bool
	}{
		{mod, "calculator", false},
		{fn, "calculator.calculate", false},
		{param, "calculator.calculate->a", true},
		{inner, "calculator.calculate->inner", true},
		{innerVar, "calculator.calculate->inner->x", true},
		{method, "calculator.Calc.Add", false},
	}
	for _, tt := range tests {
		if got := tt.d.FQN(); got != tt.want {
			t.Errorf("FQN() = %q; want %q", got, tt.want)
		}
		if tt.d.Local != tt.local {
			t.Errorf("%s Local = %v; want %v", tt.want, tt.d.Local, tt.local)
		}
	}
	if got := param.Owner(); got != fn {
		t.Errorf("Owner() = %v; want calculate", got.FQN())
	}
}

func TestPositions(t *testing.T) {
	src := "ab\r\nc\u00e9\U0001F600x\nlast"
	f := NewFile("a.txt", "/a.txt", Python, []byte(src))
	tests := []struct {
		off              int
		pos              Pos
		utf16Line, utf16 int
	}{
		{0, Pos{1, 1}, 0, 0},
		{4, Pos{2, 1}, 1, 0},
		{5, Pos{2, 2}, 1, 1},
		{11, Pos{2, 4}, 1, 4}, // the emoji takes two UTF-16 units
		{13, Pos{3, 1}, 2, 0},
	}
	for _, tt := range tests {
		if got := f.Pos(tt.off); got != tt.pos {
			t.Errorf("Pos(%d) = %v; want %v", tt.off, got, tt.pos)
		}
		if got := f.Offset(tt.pos); got != tt.off {
			t.Errorf("Offset(%v) = %d; want %d", tt.pos, got, tt.off)
		}
		line, char := f.UTF16Pos(tt.off)
		if line != tt.utf16Line || char != tt.utf16 {
			t.Errorf("UTF16Pos(%d) = %d:%d; want %d:%d", tt.off, line, char, tt.utf16Line, tt.utf16)
		}
		if got := f.OffsetUTF16(line, char); got != tt.off {
			t.Errorf("OffsetUTF16(%d, %d) = %d; want %d", line, char, got, tt.off)
		}
	}
	if got := f.Line(1); got != "ab" {
		t.Errorf("Line(1) = %q; want %q (CR stripped)", got, "ab")
	}
}

func TestGitignore(t *testing.T) {
	rules := parseGitignore("", []byte("# comment\n*.log\n/build/\ndocs/**/*.tmp\n!keep.log\nnode_modules\ngen/**\n!gen/keep.py\ntest_[!a].py\n"))
	rules = append(rules, parseGitignore("sub", []byte("local.txt\n"))...)
	tests := []struct {
		path  string
		dir   bool
		ignor bool
	}{
		{"a.log", false, true},
		{"x/y/a.log", false, true},
		{"keep.log", false, false},
		{"build", true, true},
		{"src/build", true, false},
		{"build", false, false},
		{"docs/a/b/c.tmp", false, true},
		{"docs/c.tmp", false, true},
		{"x/node_modules", true, true},
		{"sub/local.txt", false, true},
		{"local.txt", false, false},
		{"gen", true, false},
		{"gen/x.py", false, true},
		{"gen/keep.py", false, false},
		{"test_b.py", false, true},
		{"test_a.py", false, false},
	}
	for _, tt := range tests {
		if got, _ := rules.match(tt.path, tt.dir); got != tt.ignor {
			t.Errorf("match(%q, dir=%v) = %v; want %v", tt.path, tt.dir, got, tt.ignor)
		}
	}
}

type fakeFrontend struct{}

func (fakeFrontend) Lang(path string) (Lang, bool) {
	return Python, filepath.Ext(path) == ".py"
}

func (fakeFrontend) Parse(f *File) error {
	f.SetModule(f.Path)
	return nil
}

func (fakeFrontend) Resolve(*Index, []*File) {}

func TestLoadWalk(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".gitignore":              "ignored/\n*.gen.py\n!vendor/\n",
		"a.py":                    "",
		"b.gen.py":                "",
		"notes.txt":               "",
		"pkg/c.py":                "",
		"pkg/.gitignore":          "skip.py\n",
		"pkg/skip.py":             "",
		"ignored/d.py":            "",
		"node_modules/e.py":       "",
		".hidden/f.py":            "",
		"pkg/deep/g.py":           "",
		"pkg/deep/.gitignore":     "!skip.py\n",
		"pkg/deep/skip.py":        "",
		"pkg/deep/bin/ignored.py": "",
		"vendor/v.py":             "",
	}
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := Load(LoadOptions{Root: root, Frontends: []Frontend{fakeFrontend{}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range ix.Files {
		got = append(got, f.Path)
	}
	want := []string{"a.py", "pkg/c.py", "pkg/deep/g.py", "pkg/deep/skip.py"}
	if !slices.Equal(got, want) {
		t.Errorf("loaded %q; want %q", got, want)
	}
	if ix.File("pkg/c.py") == nil || ix.File(filepath.Join(root, "a.py")) == nil {
		t.Error("File lookup by relative or absolute path failed")
	}
	if len(ix.Lookup("pkg/c.py")) != 1 {
		t.Error("Lookup by module FQN failed")
	}

	// Patterns added after the defaults skip more, and a negation lints a
	// folder the defaults would skip. The patterns also win over .gitignore
	// files, so the "!vendor/" in one doesn't bring vendor back.
	ix, err = Load(LoadOptions{Root: root, Frontends: []Frontend{fakeFrontend{}}, Ignore: append(DefaultIgnore(), "pkg/deep/", "!node_modules/")})
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, f := range ix.Files {
		got = append(got, f.Path)
	}
	if want := []string{"a.py", "node_modules/e.py", "pkg/c.py"}; !slices.Equal(got, want) {
		t.Errorf("with custom patterns loaded %q; want %q", got, want)
	}
}

func TestLineOf(t *testing.T) {
	f := NewFile("a.go", "/a.go", Go, []byte("one\ntwo é\n\nfour"))
	for _, tc := range []struct{ off, want int }{
		{-2, 1}, {0, 1}, {3, 1}, {4, 2}, {8, 2}, {10, 2},
		{11, 3}, {12, 4}, {16, 4}, {18, 4},
	} {
		if got := f.LineOf(tc.off); got != tc.want {
			t.Errorf("LineOf(%d) = %d, want %d", tc.off, got, tc.want)
		}
	}
}
