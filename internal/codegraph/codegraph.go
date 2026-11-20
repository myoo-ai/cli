// Package codegraph implements `myoo out code_graph`, which renders the call
// graph of a directory, file or symbol with the myoo-uml graph renderer.
package codegraph

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/langs"
	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/uml/model"
	"github.com/myoo-ai/uml/render"
)

const (
	// Directories with more functions get a graph of calls between files.
	maxFunctions = 60
	// A file graph shows up to this many functions from other files one by
	// one. With more, it draws a node per file.
	maxNeighbors = 16
	// Bigger graphs would still render, but nobody can read them.
	maxNodes = 300
)

// Command returns the code_graph subcommand of `myoo out`.
func Command() *command.Command {
	return &command.Command{
		Name:    "code_graph",
		Summary: "render a call graph as an image",
		Usage:   "out code_graph <directory or file or fqn of symbol> [-f/--format=<svg or png>] [-o/--out=<path to output file>] [--depth=<n>]",
		Help: `A directory shows its functions and methods with the calls between them,
grouped by file. Directories with more than 60 functions show the calls
between their files instead. A file shows its functions, the functions they
call or are called from in other files (one node per file when there are more
than 16) and those calls. A fully qualified symbol name (as printed by myoo
lsp) shows its callers and callees up to --depth levels.`,
		MinArgs: 1,
		MaxArgs: 1,
		Flags: []command.Flag{
			{Long: "format", Short: "f", Kind: command.String, Value: "svg or png", Default: "png", Usage: "image format"},
			{Long: "out", Short: "o", Kind: command.String, Value: "path", Usage: "output file (a temporary file by default)"},
			{Long: "depth", Kind: command.String, Value: "n", Default: "3", Usage: "levels of callers and callees shown for a symbol"},
		},
		Run: run,
	}
}

func run(env *command.Env, inv *command.Invocation) error {
	format := inv.String("format")
	if format != "svg" && format != "png" {
		return command.Usagef("unknown format `%s`, expected svg or png", format)
	}
	depth, err := strconv.Atoi(inv.String("depth"))
	if err != nil || depth < 1 {
		return command.Usagef("invalid --depth `%s`, expected a positive number", inv.String("depth"))
	}
	target := inv.Args[0]
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(env.Dir, target)
	}
	var (
		root  string
		isDir bool
		isFQN bool
	)
	if fi, err := os.Stat(abs); err == nil {
		root, isDir = code.FindRoot(abs), fi.IsDir()
	} else {
		root, isFQN = code.FindRoot(env.Dir), true
	}
	opts := code.LoadOptions{Root: root, Frontends: langs.All(), Resolve: true}
	if !isFQN {
		// If the user names a path, we load it even if the workspace walk
		// would skip it because it's gitignored, hidden or a build directory.
		opts.Paths = []string{root, abs}
	}
	ix, err := code.Load(opts)
	if err != nil {
		return err
	}
	g := newGraph(ix)
	var title string
	switch {
	case isFQN:
		decls := ix.Lookup(target)
		if len(decls) == 0 {
			return fmt.Errorf("unknown symbol `%s`%s", target, suggest(ix, target))
		}
		g.around(decls, depth)
		title = "Calls of " + target
	case isDir:
		prefix := filepath.Clean(abs) + string(filepath.Separator)
		g.directory(func(f *code.File) bool { return strings.HasPrefix(f.Abs, prefix) })
		title = "Call graph of " + displayPath(env.Dir, abs)
		if g.fileLevel {
			title = "Calls between the files of " + displayPath(env.Dir, abs)
		}
	default:
		f := ix.File(abs)
		if f == nil {
			return fmt.Errorf("`%s` is not a supported source file", target)
		}
		g.file(f)
		title = "Call graph of " + displayPath(env.Dir, abs)
	}
	d := g.diagram(title)
	nodes := len(d.All())
	if nodes == 0 || len(g.vertices) == 0 {
		return fmt.Errorf("no functions found in `%s`", target)
	}
	if nodes > maxNodes {
		return fmt.Errorf("the call graph has %d nodes, narrow it down to a subdirectory, a file, a symbol or a smaller --depth (at most %d)", nodes, maxNodes)
	}

	out := outputPath(env.Dir, inv.String("out"), abs, root, target, format, isFQN)
	if err := writeImage(out, d, format); err != nil {
		return err
	}
	env.Format("`<full path to output file>`")
	fmt.Fprintln(env.Stdout, out)
	return nil
}

func outputPath(dir, out, abs, root, target, format string, isFQN bool) string {
	if out == "" {
		name := abs
		if isFQN {
			name = root + "/" + target
		}
		return filepath.Join(os.TempDir(), "myoo_code_graph_"+sanitize(name)+"."+format)
	}
	if !filepath.IsAbs(out) {
		return filepath.Join(dir, out)
	}
	return out
}

// graph collects the call edges between the non-local callables and the
// module-level code of an index, plus the selection to draw.
type graph struct {
	ix       *code.Index
	calls    map[*code.Decl][]*code.Decl // caller -> callees
	callers  map[*code.Decl][]*code.Decl
	vertices map[*code.Decl]bool
	// If set, only edges that touch focus are drawn.
	focus map[*code.Decl]bool
	// vertices drawn as one node per file
	byFile map[*code.Decl]bool
	// all vertices are drawn per file
	fileLevel bool
}

func newGraph(ix *code.Index) *graph {
	g := &graph{
		ix:       ix,
		calls:    map[*code.Decl][]*code.Decl{},
		callers:  map[*code.Decl][]*code.Decl{},
		vertices: map[*code.Decl]bool{},
		byFile:   map[*code.Decl]bool{},
	}
	seen := map[[2]*code.Decl]bool{}
	for _, f := range ix.Files {
		for _, r := range f.Refs {
			if !r.Call || r.Target == nil {
				continue
			}
			callee := callableOf(r.Target)
			caller := callerOf(r.Scope, f)
			if callee == nil || caller == nil || seen[[2]*code.Decl{caller, callee}] {
				continue
			}
			seen[[2]*code.Decl{caller, callee}] = true
			g.calls[caller] = append(g.calls[caller], callee)
			g.callers[callee] = append(g.callers[callee], caller)
		}
	}
	return g
}

// callableOf maps a call target to its vertex. Functions and methods are
// their own vertex. A type maps to its constructor if it declares one, else
// to the type itself.
func callableOf(d *code.Decl) *code.Decl {
	d = nonLocal(d)
	switch {
	case d.Callable():
		return d
	case d.IsType():
		for _, c := range d.Children {
			if c.Kind == code.KindConstructor {
				return c
			}
		}
		return d
	}
	return nil
}

// callerOf returns the vertex that a call inside scope belongs to. That's
// the nearest non-local function, or the file's module for top-level code.
func callerOf(scope *code.Decl, f *code.File) *code.Decl {
	for d := scope; d != nil; d = d.Parent {
		if d.Callable() && !d.Local {
			return d
		}
	}
	return f.Module
}

func nonLocal(d *code.Decl) *code.Decl {
	for d.Local && d.Parent != nil {
		d = d.Parent
	}
	return d
}

// declared returns the non-local callables of the selected files. Files with
// top-level code that makes calls also add their module.
func (g *graph) declared(selected func(*code.File) bool) map[*code.Decl]bool {
	in := map[*code.Decl]bool{}
	for _, f := range g.ix.Files {
		if !selected(f) {
			continue
		}
		for _, d := range f.Decls {
			if d.Callable() && !d.Local {
				in[d] = true
			}
		}
		if len(g.calls[f.Module]) > 0 {
			in[f.Module] = true
		}
	}
	return in
}

// directory selects all functions of the selected files. If there are too
// many, we switch to a graph of calls between files.
func (g *graph) directory(selected func(*code.File) bool) {
	for d := range g.declared(selected) {
		g.vertices[d] = true
	}
	if len(g.vertices) > maxFunctions {
		g.fileLevel = true
		for d := range g.vertices {
			g.byFile[d] = true
		}
	}
}

// file selects the functions of f plus their callers and callees in other
// files. We only keep the calls that touch f.
func (g *graph) file(f *code.File) {
	g.focus = g.declared(func(o *code.File) bool { return o == f })
	neighbors := map[*code.Decl]bool{}
	for d := range g.focus {
		g.vertices[d] = true
		for _, edges := range [][]*code.Decl{g.calls[d], g.callers[d]} {
			for _, n := range edges {
				if !g.focus[n] {
					neighbors[n] = true
				}
			}
		}
	}
	for n := range neighbors {
		g.vertices[n] = true
		if len(neighbors) > maxNeighbors {
			g.byFile[n] = true
		}
	}
}

// around adds the symbols and their callers and callees, up to depth levels
// out.
func (g *graph) around(decls []*code.Decl, depth int) {
	for _, d := range decls {
		g.vertices[callableOrSelf(d)] = true
	}
	for _, edges := range []map[*code.Decl][]*code.Decl{g.calls, g.callers} {
		g.expand(decls, edges, depth)
	}
}

func (g *graph) expand(level []*code.Decl, edges map[*code.Decl][]*code.Decl, depth int) {
	seen := map[*code.Decl]bool{}
	for range depth {
		var next []*code.Decl
		for _, d := range level {
			for _, n := range edges[callableOrSelf(d)] {
				if seen[n] {
					continue
				}
				seen[n] = true
				g.vertices[n] = true
				next = append(next, n)
			}
		}
		if len(next) == 0 {
			break
		}
		level = next
	}
}

func callableOrSelf(d *code.Decl) *code.Decl {
	if c := callableOf(d); c != nil {
		return c
	}
	return d
}

// diagram turns the selection into a graph diagram. Functions are grouped by
// file, and in a graph of files we group the files by directory. We don't
// group the per-file neighbors of a file graph, so callers can sit left of
// the file and callees right of it.
func (g *graph) diagram(title string) *model.Diagram {
	d := &model.Diagram{Kind: model.GraphDiagram, Title: title, Direction: model.LeftRight}
	verts := slices.SortedFunc(maps.Keys(g.vertices), func(a, b *code.Decl) int {
		return cmp.Or(cmp.Compare(a.File.Path, b.File.Path), cmp.Compare(a.NameSpan.Start, b.NameSpan.Start))
	})
	groups := map[string]*model.Element{}
	nodes := map[string]*model.Element{}
	add := func(e *model.Element, gid, gname string) {
		if gid == "" {
			d.Elements = append(d.Elements, e)
			return
		}
		gr := groups[gid]
		if gr == nil {
			gr = &model.Element{ID: gid, Name: gname, Kind: model.GroupElem}
			groups[gid] = gr
			d.Elements = append(d.Elements, gr)
		}
		e.Parent = gr
		gr.Children = append(gr.Children, e)
	}
	node := func(v *code.Decl) *model.Element {
		id, name, gid, gname := v.FQN(), label(v), "file:"+v.File.Path, v.File.Path
		switch {
		case g.byFile[v] && g.fileLevel:
			dir := path.Dir(v.File.Path)
			id, name, gid, gname = "file:"+v.File.Path, path.Base(v.File.Path), "dir:"+dir, dir+"/"
		case g.byFile[v]:
			id, name, gid = "file:"+v.File.Path, v.File.Path, ""
		case v.Kind == code.KindModule:
			id = "module:" + v.File.Path
		}
		if e := nodes[id]; e != nil {
			return e
		}
		e := &model.Element{ID: id, Name: name, Kind: model.VertexElem}
		add(e, gid, gname)
		nodes[id] = e
		return e
	}
	for _, v := range verts {
		node(v)
	}
	seen := map[[2]*model.Element]bool{}
	for _, from := range verts {
		for _, to := range g.calls[from] {
			if !g.vertices[to] || g.focus != nil && !g.focus[from] && !g.focus[to] {
				continue
			}
			a, b := node(from), node(to)
			if a == b && g.byFile[from] || seen[[2]*model.Element{a, b}] {
				continue
			}
			seen[[2]*model.Element{a, b}] = true
			d.Relations = append(d.Relations, &model.Relation{
				Kind: model.Edge, From: a, To: b, Line: model.Solid, ToHead: model.HeadOpen,
			})
		}
	}
	return d
}

// label returns the text of a function node, like "calculate()" or
// "Calc.run()", or "(module code)" for the top-level statements of a file.
func label(d *code.Decl) string {
	if d.Kind == code.KindModule {
		return "(module code)"
	}
	var types []string
	if d.Receiver != "" {
		types = []string{d.Receiver}
	} else {
		for p := d.Parent; p != nil; p = p.Parent {
			if p.IsType() {
				types = append([]string{p.Name}, types...)
			}
		}
	}
	return strings.Join(append(types, d.Name), ".") + "()"
}

func suggest(ix *code.Index, fqn string) string {
	var fqns []string
	for d := range ix.Decls() {
		if d.Callable() && !d.Local {
			fqns = append(fqns, d.FQN())
		}
	}
	if best, ok := command.Closest(fqn, fqns, len(fqn)/2); ok {
		return fmt.Sprintf(", did you mean `%s`?", best)
	}
	return ", pass a directory, a file or the fully qualified name printed by `myoo lsp definition`"
}

// writeImage renders d into a temporary file next to out and then renames it
// into place. That way a failed render can't leave a truncated image, and if
// out is a symlink we replace it instead of following it.
func writeImage(out string, d *model.Diagram, format string) error {
	tmp, err := os.CreateTemp(filepath.Dir(out), ".myoo_code_graph_*")
	if err != nil {
		return fmt.Errorf("unable to create `%s`: %w", out, err)
	}
	defer os.Remove(tmp.Name())
	if err := render.Render(d, tmp, render.Options{Format: format}); err != nil {
		tmp.Close()
		return fmt.Errorf("unable to render the code graph: %w", err)
	}
	// CreateTemp makes the file private (0600), but we want images to be
	// ordinary files.
	if err := errors.Join(tmp.Chmod(0o644), tmp.Close()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return fmt.Errorf("unable to write `%s`: %w", out, err)
	}
	return nil
}

func displayPath(dir, abs string) string {
	if rel, err := filepath.Rel(dir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return abs
}

// sanitize turns a path or name like "/Users/x/py calc" into a file name
// fragment like "Users-x-py-calc".
func sanitize(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-.")
}
