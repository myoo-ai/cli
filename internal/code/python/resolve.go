package python

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// resolver resolves the references of re-parsed copies of the files. We map
// the results back to the original declarations and references by offset,
// so Parse doesn't need to share any state with Resolve.
type resolver struct {
	byFile  map[*code.File]*fileInfo
	modules map[string]*fileInfo
	suffix  map[string]*fileInfo

	targets map[*code.Ref]*code.Decl
	busy    map[*code.Ref]bool
	imports map[importRef]*code.Decl
	impBusy map[importRef]bool
	stars   map[*scope]bool // module scopes whose star imports are being searched
	depth   int
}

// value is what an expression evaluates to, as far as member lookup needs
// it. It can be a module, a class, an instance, a function or super() inside
// a class.
type value struct {
	decl  *code.Decl
	inst  bool
	super *code.Decl
}

func resolve(files []*code.File) {
	r := &resolver{
		byFile:  map[*code.File]*fileInfo{},
		modules: map[string]*fileInfo{},
		suffix:  map[string]*fileInfo{},
		targets: map[*code.Ref]*code.Decl{},
		busy:    map[*code.Ref]bool{},
		imports: map[importRef]*code.Decl{},
		impBusy: map[importRef]bool{},
		stars:   map[*scope]bool{},
	}
	type pair struct {
		orig *code.File
		info *fileInfo
	}
	pairs := make([]pair, 0, len(files))
	for _, f := range files {
		cp := code.NewFile(f.Path, f.Abs, f.Lang, f.Src)
		info := parseFile(cp)
		r.byFile[cp] = info
		pairs = append(pairs, pair{f, info})
		name := cp.Module.Name
		if prev, ok := r.modules[name]; !ok || (strings.HasSuffix(prev.f.Path, ".pyi") && !strings.HasSuffix(cp.Path, ".pyi")) {
			r.modules[name] = info
		}
	}
	type declKey struct {
		start int
		kind  code.DeclKind
	}
	origOf := map[*code.Decl]*code.Decl{}
	for _, pr := range pairs {
		byKey := map[declKey]*code.Decl{}
		for _, d := range pr.orig.Decls {
			byKey[declKey{d.NameSpan.Start, d.Kind}] = d
		}
		for _, d := range pr.info.f.Decls {
			origOf[d] = byKey[declKey{d.NameSpan.Start, d.Kind}]
		}
		origOf[pr.info.f.Module] = pr.orig.Module
	}
	for _, pr := range pairs {
		refs := map[int]*code.Ref{}
		for _, ref := range pr.orig.Refs {
			refs[ref.Span.Start] = ref
		}
		for _, ref := range pr.info.f.Refs {
			if t := r.target(ref); t != nil {
				if o := refs[ref.Span.Start]; o != nil && o.Name == ref.Name {
					o.Target = origOf[t]
				}
			}
		}
	}
}

func (r *resolver) note(ref *code.Ref) *note {
	if fi := r.byFile[ref.File]; fi != nil {
		return fi.notes[ref]
	}
	return nil
}

func (r *resolver) declNote(d *code.Decl) *declNote {
	if fi := r.byFile[d.File]; fi != nil {
		if dn := fi.declNotes[d]; dn != nil {
			return dn
		}
	}
	return &declNote{}
}

func (r *resolver) target(ref *code.Ref) *code.Decl {
	if t, ok := r.targets[ref]; ok {
		return t
	}
	if r.busy[ref] {
		return nil
	}
	r.busy[ref] = true
	defer delete(r.busy, ref)
	var t *code.Decl
	if n := r.note(ref); n != nil {
		switch n.kind {
		case noteName:
			t = r.bindingDecl(r.lookup(n.sc, ref.Name))
		case noteAttr:
			t = r.member(r.valueOf(n.qual), ref.Name)
		case noteKwarg:
			t = r.keywordParam(r.valueOf(n.qual), ref.Name)
		case notePatternKw:
			if v := r.valueOf(n.qual); isClass(v.decl) {
				t = r.classMember(v.decl, ref.Name, nil)
			}
		case noteModule:
			t = r.moduleDecl(n.imp.module)
		case noteImportName:
			t = r.resolveImport(n.imp)
		}
	}
	r.targets[ref] = t
	return t
}

// lookup extends lookupLocal to also search the `from m import *` imports
// of the module scope.
func (r *resolver) lookup(sc *scope, name string) *binding {
	b, last := lookup(sc, name)
	if b != nil || last == nil || strings.HasPrefix(name, "_") || r.stars[last] {
		return b
	}
	r.stars[last] = true
	defer delete(r.stars, last)
	for _, m := range last.stars {
		if fi := r.moduleInfo(m); fi != nil {
			if b := r.lookup(fi.module, name); b != nil {
				return b
			}
		}
	}
	return nil
}

func (r *resolver) bindingDecl(b *binding) *code.Decl {
	switch {
	case b == nil:
		return nil
	case b.decl != nil:
		return b.decl
	case b.imp != nil:
		return r.resolveImport(*b.imp)
	}
	return nil
}

func (r *resolver) resolveImport(ir importRef) *code.Decl {
	if d, ok := r.imports[ir]; ok {
		return d
	}
	if r.impBusy[ir] {
		return nil
	}
	r.impBusy[ir] = true
	defer delete(r.impBusy, ir)
	var d *code.Decl
	switch mod := r.moduleDecl(ir.module); {
	case ir.name == "":
		d = mod
	case mod != nil:
		d = r.moduleMember(mod, ir.name)
	case ir.module != "":
		d = r.moduleDecl(ir.module + "." + ir.name)
	}
	r.imports[ir] = d
	return d
}

func (r *resolver) moduleDecl(name string) *code.Decl {
	if fi := r.moduleInfo(name); fi != nil {
		return fi.f.Module
	}
	return nil
}

// moduleInfo finds a workspace module by its dotted name. If no module
// matches exactly, we accept a single module whose name ends in the dotted
// name. This helps projects whose import root isn't the workspace root.
func (r *resolver) moduleInfo(name string) *fileInfo {
	if name == "" {
		return nil
	}
	if fi := r.modules[name]; fi != nil {
		return fi
	}
	if !strings.Contains(name, ".") {
		return nil
	}
	if fi, ok := r.suffix[name]; ok {
		return fi
	}
	var found *fileInfo
	n := 0
	for k, fi := range r.modules {
		if strings.HasSuffix(k, "."+name) {
			found, n = fi, n+1
		}
	}
	if n != 1 {
		found = nil
	}
	r.suffix[name] = found
	return found
}

// moduleMember returns the binding of name in module mod (re-exports
// included) or the submodule of that name. The submodule also wins if the
// binding is the import we're resolving, like `from . import sub` inside
// pkg/__init__.py.
func (r *resolver) moduleMember(mod *code.Decl, name string) *code.Decl {
	if fi := r.byFile[mod.File]; fi != nil {
		if d := r.bindingDecl(r.lookup(fi.module, name)); d != nil {
			return d
		}
	}
	return r.moduleDecl(mod.Name + "." + name)
}

func isClass(d *code.Decl) bool {
	return d != nil && (d.Kind == code.KindClass || d.Kind == code.KindEnum)
}

func (r *resolver) member(v value, name string) *code.Decl {
	switch {
	case v.super != nil:
		seen := map[*code.Decl]bool{v.super: true}
		for _, base := range r.bases(v.super) {
			if d := r.classMember(base, name, seen); d != nil {
				return d
			}
		}
	case v.decl == nil:
	case v.decl.Kind == code.KindModule:
		return r.moduleMember(v.decl, name)
	case isClass(v.decl):
		return r.classMember(v.decl, name, nil)
	}
	return nil
}

func (r *resolver) bases(cls *code.Decl) []*code.Decl {
	var out []*code.Decl
	for _, b := range r.declNote(cls).bases {
		if v := r.valueOf(b); isClass(v.decl) && !v.inst {
			out = append(out, v.decl)
		}
	}
	return out
}

// classMember looks up name in cls, then in its bases, depth first in
// declaration order. An @overload stub loses to the implementation.
func (r *resolver) classMember(cls *code.Decl, name string, seen map[*code.Decl]bool) *code.Decl {
	if seen == nil {
		seen = map[*code.Decl]bool{}
	}
	seen[cls] = true
	var best *code.Decl
	for _, c := range cls.Children {
		if c.Name != name || !isMember(c) {
			continue
		}
		switch {
		case best == nil:
			best = c
		case r.declNote(best).overload && !r.declNote(c).overload:
			best = c
		case r.declNote(best).overload == r.declNote(c).overload && c.NameSpan.Start < best.NameSpan.Start:
			best = c
		}
	}
	if best != nil {
		return best
	}
	for _, base := range r.bases(cls) {
		if seen[base] {
			continue
		}
		if d := r.classMember(base, name, seen); d != nil {
			return d
		}
	}
	return nil
}

// keywordParam resolves a keyword argument to the callee's parameter, or to
// the field of a class without __init__ (dataclasses).
func (r *resolver) keywordParam(v value, name string) *code.Decl {
	d := v.decl
	switch {
	case d == nil:
		return nil
	case d.Callable():
		return r.paramNamed(d, name)
	case isClass(d) && !v.inst:
		if init := r.classMember(d, "__init__", nil); init != nil && init.Callable() {
			return r.paramNamed(init, name)
		}
		if f := r.classMember(d, name, nil); f != nil && f.Kind == code.KindField {
			return f
		}
	}
	return nil
}

func (r *resolver) paramNamed(fn *code.Decl, name string) *code.Decl {
	for _, c := range fn.Children {
		if c.Kind == code.KindParameter && c.Name == name && r.declNote(c).star == 0 {
			return c
		}
	}
	return nil
}

var wrapperTypes = map[string]bool{
	"Optional": true, "Annotated": true, "ClassVar": true, "Final": true,
	"Required": true, "NotRequired": true, "ReadOnly": true,
}

// valueOf evaluates an expression as far as the declarations let us.
func (r *resolver) valueOf(n *node) value {
	if n == nil || r.depth > 32 {
		return value{}
	}
	r.depth++
	defer func() { r.depth-- }()
	switch n.kind {
	case nName:
		if n.ref != nil {
			return r.valueOfDecl(r.target(n.ref))
		}
		if n.sc == nil {
			return value{}
		}
		return r.valueOfDecl(r.bindingDecl(r.lookup(n.sc, n.name)))
	case nAttr:
		if n.ref != nil {
			return r.valueOfDecl(r.target(n.ref))
		}
		return r.valueOfDecl(r.member(r.valueOf(n.x), n.name))
	case nCall:
		if n.x != nil && n.x.kind == nName && n.x.name == "super" && n.x.sc != nil && r.lookup(n.x.sc, "super") == nil {
			for s := n.x.sc; s != nil; s = s.parent {
				if s.kind == scClass {
					return value{super: s.owner}
				}
			}
			return value{}
		}
		v := r.valueOf(n.x)
		switch {
		case isClass(v.decl) && !v.inst:
			return value{decl: v.decl, inst: true}
		case v.decl != nil && v.decl.Callable():
			return instance(r.valueOf(r.declNote(v.decl).ann))
		}
	case nSubscript:
		name := dottedName(n.x)
		name = name[strings.LastIndexByte(name, '.')+1:]
		if len(n.elts) > 0 {
			switch {
			case wrapperTypes[name]:
				return r.valueOf(n.elts[0])
			case name == "type" || name == "Type":
				if v := r.valueOf(n.elts[0]); isClass(v.decl) {
					return value{decl: v.decl}
				}
			}
		}
	case nOp:
		if n.op == "|" {
			for _, e := range n.elts {
				if v := r.valueOf(e); v.decl != nil {
					return v
				}
			}
		}
	case nStr:
		return r.valueOf(r.stringAnnotation(n))
	}
	return value{}
}

func instance(v value) value {
	if isClass(v.decl) && !v.inst {
		return value{decl: v.decl, inst: true}
	}
	return value{}
}

// stringAnnotation parses a forward reference ("Order") into an expression
// that we evaluate in the string's scope.
func (r *resolver) stringAnnotation(n *node) *node {
	if n.parsed != nil || len(n.strs) != 1 || n.sc == nil {
		return n.parsed
	}
	fi := r.byFile[n.sc.root().owner.File]
	if fi == nil {
		return nil
	}
	sp := n.strs[0]
	src := fi.f.Src
	q := sp.Start
	for q < sp.End && src[q] != '"' && src[q] != '\'' {
		q++
	}
	if q != sp.Start || sp.End-q < 2 || src[sp.End-1] != src[q] {
		return nil
	}
	p := &parser{f: fi.f, src: src, info: fi, sub: true}
	parsed := p.subExpr(code.Span{Start: q + 1, End: sp.End - 1})
	setScope(parsed, n.sc)
	n.parsed = parsed
	return parsed
}

func setScope(n *node, sc *scope) {
	if n == nil {
		return
	}
	n.sc = sc
	setScope(n.x, sc)
	for _, e := range n.elts {
		setScope(e, sc)
	}
	for _, a := range n.args {
		setScope(a.value, sc)
	}
}

func (r *resolver) valueOfDecl(d *code.Decl) value {
	if d == nil {
		return value{}
	}
	switch d.Kind {
	case code.KindModule, code.KindClass, code.KindEnum, code.KindFunction, code.KindMethod, code.KindConstructor:
		return value{decl: d}
	case code.KindImport:
		if imp := r.declNote(d).imp; imp != nil {
			return r.valueOfDecl(r.resolveImport(*imp))
		}
	case code.KindEnumMember:
		return value{decl: d.Parent, inst: true}
	case code.KindParameter, code.KindVariable, code.KindConstant, code.KindField, code.KindProperty:
		dn := r.declNote(d)
		switch {
		case dn.selfOf != nil:
			return value{decl: dn.selfOf, inst: !dn.clsParam}
		case dn.ann != nil:
			return instance(r.valueOf(dn.ann))
		case dn.init != nil:
			return r.valueOf(dn.init)
		}
	}
	return value{}
}
