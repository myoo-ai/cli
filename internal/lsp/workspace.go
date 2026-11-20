package lsp

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/langs"
	"github.com/myoo-ai/cli/internal/command"
)

// workspace is the parsed and resolved project the commands work on.
type workspace struct {
	env  *command.Env
	root string
	ix   *code.Index
	// files by symlink-resolved absolute path, because language servers often
	// report /private/var/... for /var/... and the like
	real map[string]*code.File
	// C and C++ declarations by their FQN without the module, for same
	linked map[string][]*code.Decl
}

// rootFor picks the workspace root. That's --root if it's set, else the
// project root of the working directory, or of the target file if that's
// somewhere else.
func rootFor(env *command.Env, opts options, file string) string {
	if opts.root != "" {
		return opts.root
	}
	root := code.FindRoot(env.Dir)
	if file != "" && !within(root, file) {
		root = code.FindRoot(file)
	}
	return root
}

func loadWorkspace(env *command.Env, root, file string) (*workspace, error) {
	paths := []string{root}
	if file != "" && !within(root, file) {
		paths = append(paths, file)
	}
	ix, err := code.Load(code.LoadOptions{Root: root, Paths: paths, Frontends: langs.All(), Resolve: true})
	if err != nil {
		return nil, err
	}
	ws := &workspace{env: env, root: ix.Root, ix: ix, real: map[string]*code.File{}}
	for _, f := range ix.Files {
		ws.real[realPath(f.Abs)] = f
	}
	return ws, nil
}

// file returns the indexed file at abs, following symlinks, or nil.
func (ws *workspace) file(abs string) *code.File {
	if f := ws.ix.File(abs); f != nil {
		return f
	}
	return ws.real[realPath(abs)]
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// inside reports whether abs is in the workspace root, either as given or
// with symlinks resolved.
func (ws *workspace) inside(abs string) bool {
	return within(ws.root, abs) || within(realPath(ws.root), realPath(abs))
}

// display returns abs relative to the working directory if it's inside it,
// with slashes as separators.
func (ws *workspace) display(abs string) string {
	if within(ws.env.Dir, abs) {
		if rel, err := filepath.Rel(ws.env.Dir, abs); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(abs)
}

// position renders "<file>:<line>:<col>" for an offset in f, which
// terminals turn into a link.
func (ws *workspace) position(f *code.File, off int) string {
	p := f.Pos(off)
	return command.Point(ws.display(f.Abs), p.Line, p.Col)
}

func (ws *workspace) dominantLang() code.Lang {
	counts := map[code.Lang]int{}
	for _, f := range ws.ix.Files {
		counts[f.Lang]++
	}
	var best code.Lang
	for _, l := range langNames {
		if counts[l] > counts[best] {
			best = l
		}
	}
	return best
}

// frontendFor returns a fresh frontend for path, plus the language it detected.
func frontendFor(path string) (code.Frontend, code.Lang, bool) {
	for _, fe := range langs.All() {
		if l, ok := fe.Lang(path); ok {
			return fe, l, true
		}
	}
	return nil, "", false
}

// reparse parses src as the new content of f, but doesn't resolve references.
func reparse(f *code.File, src []byte) (nf *code.File, err error) {
	fe, _, ok := frontendFor(f.Abs)
	if !ok {
		return nil, fmt.Errorf("no parser for %s", f.Path)
	}
	nf = code.NewFile(f.Path, f.Abs, f.Lang, src)
	defer func() {
		if r := recover(); r != nil {
			nf, err = nil, fmt.Errorf("%s: parser crashed: %v", f.Path, r)
		}
	}()
	if err := fe.Parse(nf); err != nil {
		return nil, err
	}
	if nf.Module == nil {
		nf.SetModule("")
	}
	return nf, nil
}

// same returns the declarations with the same FQN as d, plus in C and C++
// those with the same qualified name in other files. We need the second
// part because C and C++ FQNs start with the file's module, so a prototype
// in a header and its definition in a source file differ there.
func (ws *workspace) same(d *code.Decl) []*code.Decl {
	out := slices.Clone(ws.ix.Lookup(d.FQN()))
	key, ok := linkKey(d)
	if !ok {
		return out
	}
	if ws.linked == nil {
		ws.linked = map[string][]*code.Decl{}
		for o := range ws.ix.Decls() {
			if k, ok := linkKey(o); ok {
				ws.linked[k] = append(ws.linked[k], o)
			}
		}
	}
	for _, o := range ws.linked[key] {
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	return out
}

// linkKey returns the FQN of a C or C++ declaration without the module, if
// the declaration can have external linkage.
func linkKey(d *code.Decl) (string, bool) {
	if d.Local || d.Kind == code.KindModule || d.File.Lang != code.C && d.File.Lang != code.CPP {
		return "", false
	}
	fqn := d.FQN()
	if d.File.Module.Name == "" {
		return fqn, true
	}
	return strings.CutPrefix(fqn, d.File.Module.Name+".")
}

// canon maps a declaration to the one that stands for its symbol. A
// prototype maps to its definition if there's exactly one. Partial types,
// namespaces and the module declarations of a multi-file package map to the
// first declaration that has a body.
func (ws *workspace) canon(d *code.Decl) *code.Decl {
	switch {
	case d.Kind == code.KindModule || d.Kind == code.KindNamespace || d.IsType():
		same := ws.same(d)
		for _, o := range same {
			if o.Kind == d.Kind && !o.Body.Empty() {
				return o
			}
		}
		for _, o := range same {
			if o.Kind == d.Kind {
				return o
			}
		}
	case d.Callable() && d.Body.Empty():
		var def *code.Decl
		for _, o := range ws.same(d) {
			if o.Callable() && !o.Body.Empty() {
				if def != nil {
					return d
				}
				def = o
			}
		}
		if def != nil {
			return def
		}
	}
	return d
}

// members returns the declarations that canon maps to rep, with rep first.
func (ws *workspace) members(rep *code.Decl) []*code.Decl {
	out := []*code.Decl{rep}
	for _, o := range ws.same(rep) {
		if o != rep && ws.canon(o) == rep {
			out = append(out, o)
		}
	}
	return out
}

// sourceSpan returns the span of d, or the whole file for a module.
// writeSource prints the full lines it covers.
func sourceSpan(d *code.Decl) code.Span {
	if d.Kind == code.KindModule {
		return code.Span{Start: 0, End: len(d.File.Src)}
	}
	return d.Span
}

const maxSourceLines = 50

// writeSource prints the lines span covers, each with a tab in front, up to
// maxSourceLines of them.
func writeSource(w io.Writer, f *code.File, span code.Span) {
	first := f.LineOf(span.Start)
	last := f.LineOf(max(span.End-1, span.Start))
	if span.Start == 0 && len(f.Src) == 0 {
		return
	}
	var b strings.Builder
	for n := first; n <= last; n++ {
		if n-first == maxSourceLines {
			b.WriteString("\t...\n")
			break
		}
		b.WriteByte('\t')
		b.WriteString(f.Line(n))
		b.WriteByte('\n')
	}
	_, _ = w.Write([]byte(b.String()))
}

// scopeAt returns the innermost declaration with a body around off, or the
// module if there's none. This has to stay in sync with Ref.Scope.
func scopeAt(f *code.File, off int) *code.Decl {
	best := f.Module
	for _, d := range f.Decls {
		if d.Body.Empty() || !d.Span.Contains(off) || d.NameSpan.Start == off {
			continue
		}
		if best == f.Module || best.Span.Encloses(d.Span) {
			best = d
		}
	}
	return best
}

// site is a reference location.
type site struct {
	file  *code.File
	span  code.Span
	name  string
	scope *code.Decl
}

// label renders the reference as the FQN of its scope joined to the name it
// refers to, with "->" inside callables and local scopes and "." elsewhere.
func (s site) label() string {
	if s.scope == nil {
		return s.name
	}
	fqn := s.scope.FQN()
	switch {
	case fqn == "":
		return s.name
	case s.scope.Callable() || s.scope.Local:
		return fqn + "->" + s.name
	}
	return fqn + "." + s.name
}

func sortSites(sites []site) []site {
	slices.SortFunc(sites, func(a, b site) int {
		return cmp.Or(cmp.Compare(a.file.Path, b.file.Path), cmp.Compare(a.span.Start, b.span.Start))
	})
	return slices.CompactFunc(sites, func(a, b site) bool { return a.file == b.file && a.span.Start == b.span.Start })
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
