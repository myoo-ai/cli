package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// unit is one workspace file re-parsed for type checking.
type unit struct {
	f     *code.File
	af    *ast.File
	decls map[int]*code.Decl // by NameSpan.Start
	refs  map[int]*code.Ref  // by Span.Start
}

// pkg is a workspace package, made of the files in a directory that share a
// package clause.
type pkg struct {
	path   string
	name   string
	files  []*unit // non-test files
	tests  []*unit // _test.go files of the same package
	xtests []*unit // _test.go files of the external <name>_test package
	module *code.Decl

	state   int // 0 unchecked, 1 checking, 2 checked
	checked *types.Package
	variant *types.Package // checked with tests, for the external test package
}

type resolver struct {
	fset   *token.FileSet
	units  map[*token.File]*unit
	byPath map[string]*pkg
	fakes  map[string]*types.Package
}

// Resolve type-checks the workspace packages from source and maps the
// identifiers go/types resolves back to declarations. Imports from outside
// the workspace become empty packages, so references into them stay nil.
func (frontend) Resolve(ix *code.Index, files []*code.File) {
	r := &resolver{
		fset:   token.NewFileSet(),
		units:  map[*token.File]*unit{},
		byPath: map[string]*pkg{},
		fakes:  map[string]*types.Package{},
	}
	pkgs := r.group(ix, files)
	for _, p := range pkgs {
		r.checkBase(p)
		if len(p.tests) > 0 {
			p.variant = r.check(p.path, p.name, slices.Concat(p.files, p.tests), p.tests)
		}
		if len(p.xtests) > 0 {
			r.check(p.path+"_test", p.name+"_test", p.xtests, p.xtests)
		}
	}
	for _, u := range r.units {
		r.importRefs(u)
	}
}

// group parses the files and sorts them into packages. In a directory, the
// package name with the most non-test files is the importable one. We check
// other package clauses, like those of build-ignored tools, on their own.
func (r *resolver) group(ix *code.Index, files []*code.File) []*pkg {
	type dirKey struct{ dir, name string }
	byDir := map[dirKey][]*unit{}
	var dirs []string
	seenDir := map[string]bool{}
	for _, f := range files {
		af, _ := parser.ParseFile(r.fset, f.Abs, f.Src, parseMode)
		if af.Name.Name == "" {
			continue
		}
		u := &unit{f: f, af: af, decls: map[int]*code.Decl{}, refs: map[int]*code.Ref{}}
		for _, d := range f.Decls {
			u.decls[d.NameSpan.Start] = d
		}
		for _, ref := range f.Refs {
			u.refs[ref.Span.Start] = ref
		}
		r.units[r.fset.File(af.FileStart)] = u
		dir := filepath.Dir(f.Abs)
		if !seenDir[dir] {
			seenDir[dir] = true
			dirs = append(dirs, dir)
		}
		k := dirKey{dir, af.Name.Name}
		byDir[k] = append(byDir[k], u)
	}
	slices.Sort(dirs)
	mods := map[string][2]string{}
	var out []*pkg
	for _, dir := range dirs {
		names := map[string][]*unit{}
		var order []string
		for k, us := range byDir {
			if k.dir == dir {
				names[k.name] = us
				order = append(order, k.name)
			}
		}
		slices.Sort(order)
		primary := primaryPackage(order, names)
		path := r.importPath(ix, dir, mods)
		for _, name := range order {
			us := names[name]
			slices.SortFunc(us, func(a, b *unit) int { return strings.Compare(a.f.Path, b.f.Path) })
			if name == primary+"_test" && name != primary {
				continue
			}
			p := &pkg{path: path, name: name}
			if name != primary {
				p.path = path + "#" + name
			}
			for _, u := range us {
				if isTest(u) {
					p.tests = append(p.tests, u)
				} else {
					p.files = append(p.files, u)
				}
			}
			if name == primary {
				p.xtests = names[primary+"_test"]
				if _, dup := r.byPath[p.path]; !dup {
					r.byPath[p.path] = p
				}
			}
			if all := slices.Concat(p.files, p.tests); len(all) > 0 {
				p.module = all[0].f.Module
			}
			out = append(out, p)
		}
	}
	return out
}

func primaryPackage(order []string, names map[string][]*unit) string {
	primary, most := "", -1
	for _, name := range order {
		n := 0
		for _, u := range names[name] {
			if !isTest(u) {
				n++
			}
		}
		if n > most {
			primary, most = name, n
		}
	}
	return primary
}

func isTest(u *unit) bool { return strings.HasSuffix(u.f.Abs, "_test.go") }

// importPath derives the import path of dir from the nearest go.mod. If
// there is none, we use the path relative to the workspace root.
func (r *resolver) importPath(ix *code.Index, dir string, mods map[string][2]string) string {
	mod, ok := mods[dir]
	if !ok {
		modDir := findModDir(dir)
		mod = [2]string{modDir, ""}
		if modDir != "" {
			mod[1] = modulePath(modDir)
		}
		mods[dir] = mod
	}
	base, prefix := ix.Root, ""
	if mod[0] != "" {
		base, prefix = mod[0], mod[1]
	}
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	rel = filepath.ToSlash(rel)
	switch {
	case rel == ".":
		return prefix
	case prefix == "":
		return rel
	}
	return prefix + "/" + rel
}

func (r *resolver) checkBase(p *pkg) *types.Package {
	switch p.state {
	case 1:
		return r.fake(p.path)
	case 2:
		return p.checked
	}
	p.state = 1
	files := p.files
	if len(files) == 0 {
		files = p.tests
	}
	p.checked = r.check(p.path, p.name, files, files)
	p.state = 2
	return p.checked
}

// check type-checks files as package path and resolves the references in
// the owned files. We expect type errors, since external packages are
// missing, and ignore them.
func (r *resolver) check(path, name string, files, owned []*unit) (checked *types.Package) {
	asts := make([]*ast.File, len(files))
	for i, u := range files {
		asts[i] = u.af
	}
	info := &types.Info{
		Uses:      map[*ast.Ident]types.Object{},
		Implicits: map[ast.Node]types.Object{},
	}
	conf := types.Config{
		Importer:    importer{r},
		Error:       func(error) {},
		FakeImportC: true,
	}
	defer func() {
		if recover() != nil {
			checked = types.NewPackage(path, name)
		}
		r.record(info, owned)
	}()
	checked, _ = conf.Check(path, r.fset, asts, info)
	return checked
}

type importer struct{ r *resolver }

func (im importer) Import(path string) (*types.Package, error) {
	if p, ok := im.r.byPath[path]; ok {
		if p.variant != nil {
			return p.variant, nil
		}
		return im.r.checkBase(p), nil
	}
	return im.r.fake(path), nil
}

// fake returns an empty, complete package to stand in for a package that
// isn't in the workspace.
func (r *resolver) fake(path string) *types.Package {
	if p, ok := r.fakes[path]; ok {
		return p
	}
	p := types.NewPackage(path, guessName(path))
	p.MarkComplete()
	r.fakes[path] = p
	return p
}

// guessName guesses the package name of an import path from its last
// element, skipping major version suffixes like "/v2" or ".v3".
func guessName(path string) string {
	elems := strings.Split(path, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && isMajorVersion(name) {
		name = elems[len(elems)-2]
	}
	if before, after, ok := strings.CutLast(name, "."); ok && before != "" && isMajorVersion(after) {
		name = before
	}
	name = strings.TrimPrefix(name, "go-")
	return strings.Map(func(c rune) rune {
		if c == '-' || c == '.' {
			return '_'
		}
		return c
	}, name)
}

func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

func (r *resolver) unitAt(pos token.Pos) (*unit, int) {
	if !pos.IsValid() {
		return nil, 0
	}
	tf := r.fset.File(pos)
	if tf == nil {
		return nil, 0
	}
	return r.units[tf], tf.Offset(pos)
}

func (r *resolver) record(info *types.Info, owned []*unit) {
	mine := map[*unit]bool{}
	for _, u := range owned {
		mine[u] = true
	}
	for id, obj := range info.Uses {
		u, off := r.unitAt(id.Pos())
		if u == nil || !mine[u] {
			continue
		}
		if ref := u.refs[off]; ref != nil {
			ref.Target = r.declOf(obj)
		}
	}
}

// declOf maps a types.Object to the declaration at its position. The
// package name of an unaliased import maps to the imported package's module.
func (r *resolver) declOf(obj types.Object) *code.Decl {
	switch o := obj.(type) {
	case *types.PkgName:
		if u, off := r.unitAt(o.Pos()); u != nil {
			if d := u.decls[off]; d != nil && d.Kind == code.KindImport {
				return d
			}
		}
		if o.Imported() == nil {
			return nil
		}
		return r.module(o.Imported().Path())
	case *types.Var:
		obj = o.Origin()
	case *types.Func:
		obj = o.Origin()
	}
	if obj.Pkg() == nil {
		return nil
	}
	u, off := r.unitAt(obj.Pos())
	if u == nil {
		return nil
	}
	return u.decls[off]
}

func (r *resolver) module(path string) *code.Decl {
	if p, ok := r.byPath[path]; ok {
		return p.module
	}
	return nil
}

// importRefs resolves the package-name references inside import paths.
func (r *resolver) importRefs(u *unit) {
	for _, s := range u.af.Imports {
		p, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		name := p[strings.LastIndexByte(p, '/')+1:]
		_, end := r.unitAt(s.Path.End())
		if ref := u.refs[end-1-len(name)]; ref != nil && ref.Import {
			ref.Target = r.module(p)
		}
	}
}
