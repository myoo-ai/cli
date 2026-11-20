package codegraph

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/langs"
	"github.com/myoo-ai/uml/model"
)

var project = map[string]string{
	"pyproject.toml": "",
	"calculator.py": `def calculate(a, b):
    return add(a, b)


def add(a, b):
    return a + b


class Calc:
    def __init__(self):
        self.total = 0

    def run(self, x):
        self.total = calculate(self.total, x)
        return self.helper()

    def helper(self):
        return self.total
`,
	"main.py": `from calculator import calculate, Calc

print(calculate(1, 1))


def main():
    c = Calc()
    c.run(2)
`,
	"pkg/other.py": `def lonely():
    return 1
`,
}

func writeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range project {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// dump renders the diagram as "group > group > vertex" lines and edges.
func dump(d *model.Diagram) string {
	var lines []string
	var walk func(es []*model.Element, prefix string)
	walk = func(es []*model.Element, prefix string) {
		for _, e := range es {
			if e.Kind == model.GroupElem {
				walk(e.Children, prefix+e.Name+" > ")
				continue
			}
			lines = append(lines, "vertex "+prefix+e.Name)
		}
	}
	walk(d.Elements, "")
	for _, r := range d.Relations {
		lines = append(lines, fmt.Sprintf("edge %s -> %s", r.From.ID, r.To.ID))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

func TestGraph(t *testing.T) {
	root := writeProject(t)
	ix, err := code.Load(code.LoadOptions{Root: root, Frontends: langs.All(), Resolve: true})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		build func(g *graph)
		want  string
	}{
		{
			name:  "directory",
			build: func(g *graph) { g.directory(func(*code.File) bool { return true }) },
			want: `edge calculator.Calc.run -> calculator.Calc.helper
edge calculator.Calc.run -> calculator.calculate
edge calculator.calculate -> calculator.add
edge main.main -> calculator.Calc.__init__
edge main.main -> calculator.Calc.run
edge module:main.py -> calculator.calculate
vertex calculator.py > Calc.__init__()
vertex calculator.py > Calc.helper()
vertex calculator.py > Calc.run()
vertex calculator.py > add()
vertex calculator.py > calculate()
vertex main.py > (module code)
vertex main.py > main()
vertex pkg/other.py > lonely()`,
		},
		{
			// Calls between two neighbors, like Calc.run -> calculate, are left out.
			name:  "file with neighbors",
			build: func(g *graph) { g.file(ix.File("main.py")) },
			want: `edge main.main -> calculator.Calc.__init__
edge main.main -> calculator.Calc.run
edge module:main.py -> calculator.calculate
vertex calculator.py > Calc.__init__()
vertex calculator.py > Calc.run()
vertex calculator.py > calculate()
vertex main.py > (module code)
vertex main.py > main()`,
		},
		{
			name:  "symbol depth 1",
			build: func(g *graph) { g.around(ix.Lookup("calculator.calculate"), 1) },
			want: `edge calculator.Calc.run -> calculator.calculate
edge calculator.calculate -> calculator.add
edge module:main.py -> calculator.calculate
vertex calculator.py > Calc.run()
vertex calculator.py > add()
vertex calculator.py > calculate()
vertex main.py > (module code)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGraph(ix)
			tt.build(g)
			if got := dump(g.diagram("t")); got != tt.want {
				t.Errorf("graph =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestAggregation checks that large directories become a graph of files and
// that files with many neighbors draw one node per neighboring file.
func TestAggregation(t *testing.T) {
	root := t.TempDir()
	var lib, app strings.Builder
	for i := range maxFunctions {
		fmt.Fprintf(&lib, "def f%d():\n    return %d\n\n\n", i, i)
	}
	app.WriteString("from lib.funcs import *\n\n\ndef main():\n")
	for i := range maxFunctions {
		fmt.Fprintf(&app, "    f%d()\n", i)
	}
	app.WriteString("\n\ndef helper():\n    main()\n")
	for name, src := range map[string]string{"pyproject.toml": "", "lib/funcs.py": lib.String(), "app.py": app.String()} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := code.Load(code.LoadOptions{Root: root, Frontends: langs.All(), Resolve: true})
	if err != nil {
		t.Fatal(err)
	}

	g := newGraph(ix)
	g.directory(func(*code.File) bool { return true })
	want := "edge file:app.py -> file:lib/funcs.py\nvertex ./ > app.py\nvertex lib/ > funcs.py"
	if got := dump(g.diagram("t")); !g.fileLevel || got != want {
		t.Errorf("directory graph (fileLevel=%v) =\n%s\nwant\n%s", g.fileLevel, got, want)
	}

	g = newGraph(ix)
	g.file(ix.File("app.py"))
	want = "edge app.helper -> app.main\nedge app.main -> file:lib/funcs.py\nvertex app.py > helper()\nvertex app.py > main()\nvertex lib/funcs.py"
	if got := dump(g.diagram("t")); got != want {
		t.Errorf("file graph =\n%s\nwant\n%s", got, want)
	}
}

func TestSanitize(t *testing.T) {
	tests := map[string]string{
		"/Users/example/projects/python/calculator": "Users-example-projects-python-calculator",
		`C:\work\my app\`:                 "C-work-my-app",
		"/root/x/calculator.calculate->a": "root-x-calculator.calculate-a",
	}
	for in, want := range tests {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q; want %q", in, got, want)
		}
	}
}
