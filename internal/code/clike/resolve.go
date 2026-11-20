package clike

import (
	"maps"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type typeKey struct {
	ctx, header *code.Decl
	name        string
}

type resolver struct {
	files []*code.File
	infos map[*code.File]*fileInfo
	fqn   map[*code.Decl]string
	byFQN map[code.Lang]map[string][]*code.Decl
	// Java package and C# namespace paths with all their prefixes
	spaces map[code.Lang]map[string]bool
	// type members by simple name, for the unique-member fallback and enum
	// case labels
	byName map[code.Lang]map[string][]*code.Decl
	ext    map[string][]*code.Decl
	arity  map[*code.Decl]arity
	hidden map[*code.Decl]bool
	// C# global using directives, which apply to all files
	globals []importInfo
	bases   map[*code.Decl][]*code.Decl
	types   map[typeKey]*code.Decl
	// references that name a package or namespace path
	paths map[*code.Ref]string
	depth int
}

func newResolver(files []*code.File, infos map[*code.File]*fileInfo) *resolver {
	return &resolver{
		files:  files,
		infos:  infos,
		fqn:    map[*code.Decl]string{},
		byFQN:  map[code.Lang]map[string][]*code.Decl{},
		spaces: map[code.Lang]map[string]bool{},
		byName: map[code.Lang]map[string][]*code.Decl{},
		ext:    map[string][]*code.Decl{},
		arity:  map[*code.Decl]arity{},
		hidden: map[*code.Decl]bool{},
		bases:  map[*code.Decl][]*code.Decl{},
		types:  map[typeKey]*code.Decl{},
		paths:  map[*code.Ref]string{},
	}
}

func (r *resolver) run() {
	for _, f := range r.files {
		fi := r.infos[f]
		if fi == nil {
			continue
		}
		lang := f.Lang
		if r.byFQN[lang] == nil {
			r.byFQN[lang] = map[string][]*code.Decl{}
			r.spaces[lang] = map[string]bool{}
			r.byName[lang] = map[string][]*code.Decl{}
		}
		r.fqn[f.Module] = f.Module.Name
		r.addSpace(lang, f.Module.Name)
		for _, d := range f.Decls {
			fqn := d.FQN()
			r.fqn[d] = fqn
			r.byFQN[lang][fqn] = append(r.byFQN[lang][fqn], d)
			switch d.Kind {
			case code.KindNamespace:
				r.addSpace(lang, fqn)
			case code.KindMethod, code.KindField, code.KindProperty, code.KindConstant, code.KindEnumMember:
				if !d.Local && d.Parent != nil && d.Parent.IsType() {
					r.byName[lang][d.Name] = append(r.byName[lang][d.Name], d)
				}
			}
		}
		maps.Copy(r.arity, fi.arity)
		maps.Copy(r.hidden, fi.hidden)
		for _, d := range fi.ext {
			r.ext[d.Name] = append(r.ext[d.Name], d)
		}
		for _, imp := range fi.imports {
			if imp.global {
				r.globals = append(r.globals, imp)
			}
		}
	}
	for _, f := range r.files {
		fi := r.infos[f]
		if fi == nil {
			continue
		}
		r.resolveImports(f, fi)
		for _, ref := range f.Refs {
			ri := fi.refs[ref]
			if ri == nil || ri.imp {
				continue
			}
			ref.Target = r.resolveRef(ref, ri)
		}
	}
}

func (r *resolver) addSpace(lang code.Lang, path string) {
	for path != "" {
		r.spaces[lang][path] = true
		path = parentPath(path)
	}
}

func parentPath(path string) string {
	if before, _, ok := strings.CutLast(path, "."); ok {
		return before
	}
	return ""
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// typeAt returns the first type declared with the given FQN.
func (r *resolver) typeAt(lang code.Lang, fqn string) *code.Decl {
	return firstType(r.byFQN[lang][fqn])
}

// namespaceDecl returns a C# namespace declaration with the given FQN whose
// written name is exactly name. That way renaming it through a reference
// can't rewrite part of a dotted name.
func (r *resolver) namespaceDecl(lang code.Lang, fqn, name string) *code.Decl {
	for _, d := range r.byFQN[lang][fqn] {
		if d.Kind == code.KindNamespace && d.Name == name {
			return d
		}
	}
	return nil
}

func importPath(imp importInfo, n int) string {
	var b strings.Builder
	for i, ref := range imp.refs[:n] {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(ref.Name)
	}
	return b.String()
}

func (r *resolver) resolveImports(f *code.File, fi *fileInfo) {
	for _, imp := range fi.imports {
		path := ""
		var cur *code.Decl
		for i, ref := range imp.refs {
			if cur != nil {
				ms := r.members(cur, ref.Name)
				if imp.static && i == len(imp.refs)-1 && !imp.star {
					ref.Target = r.overload(ms, -1)
					break
				}
				cur = firstType(ms)
				ref.Target = cur
				if cur == nil {
					break
				}
				continue
			}
			path = join(path, ref.Name)
			if t := r.typeAt(f.Lang, path); t != nil {
				ref.Target, cur = t, t
				continue
			}
			ref.Target = r.namespaceDecl(f.Lang, path, ref.Name)
			if !r.spaces[f.Lang][path] {
				break
			}
			r.paths[ref] = path
		}
	}
}

func firstType(ds []*code.Decl) *code.Decl {
	for _, d := range ds {
		if d.IsType() {
			return d
		}
	}
	return nil
}

func (r *resolver) resolveRef(ref *code.Ref, ri *refInfo) *code.Decl {
	if ri.local != nil {
		return ri.local
	}
	if ri.ctx == nil {
		ri.ctx = ref.Scope
	}
	switch ri.qual.kind {
	case qualNone:
		return r.unqualified(ref, ri)
	case qualExternal:
		return nil
	}
	return r.qualified(ref, ri)
}

// cleanType reduces a written type to a dotted name by dropping type
// arguments and the suffixes [], ? and *. Tuple and union types give "".
func cleanType(s string) string {
	s = strings.TrimPrefix(s, "global::")
	if strings.ContainsAny(s, "(|") {
		return ""
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '<':
			depth++
		case '>':
			depth--
		case '[', ']', '?', '*', ' ':
		case ':':
			if depth == 0 && i+1 < len(s) && s[i+1] == ':' {
				b.WriteByte('.')
				i++
			}
		default:
			if depth == 0 {
				b.WriteByte(c)
			}
		}
	}
	return strings.Trim(b.String(), ".")
}

func (r *resolver) resolveType(ctx *code.Decl, name string, header *code.Decl) *code.Decl {
	name = cleanType(name)
	if name == "" || ctx == nil || name == "var" {
		return nil
	}
	key := typeKey{ctx, header, name}
	if d, ok := r.types[key]; ok {
		return d
	}
	r.types[key] = nil
	if r.depth > 48 {
		return nil
	}
	r.depth++
	d := r.resolveTypeUncached(ctx, name, header)
	r.depth--
	r.types[key] = d
	return d
}

func (r *resolver) resolveTypeUncached(ctx *code.Decl, name string, header *code.Decl) *code.Decl {
	segs := strings.Split(name, ".")
	lang := ctx.File.Lang
	d := r.simpleType(ctx, segs[0], header)
	rest := segs[1:]
	if d == nil {
		prefixes := []string{""}
		if lang == code.CSharp {
			prefixes = append(prefixes, r.namespacesOf(ctx)...)
		}
	search:
		for k := len(segs); k >= 1; k-- {
			for _, pre := range prefixes {
				if t := r.typeAt(lang, join(pre, strings.Join(segs[:k], "."))); t != nil {
					d, rest = t, segs[k:]
					break search
				}
			}
		}
		if d == nil {
			return nil
		}
	}
	for _, s := range rest {
		d = r.deref(d)
		if d == nil {
			return nil
		}
		d = firstType(r.members(d, s))
		if d == nil {
			return nil
		}
	}
	return d
}

// namespacesOf lists the namespace paths enclosing d, innermost first,
// including their parent paths.
func (r *resolver) namespacesOf(d *code.Decl) []string {
	var out []string
	for p := d; p != nil; p = p.Parent {
		if p.Kind != code.KindNamespace {
			continue
		}
		for ns := r.fqn[p]; ns != ""; ns = parentPath(ns) {
			if !slices.Contains(out, ns) {
				out = append(out, ns)
			}
		}
	}
	return out
}

// deref returns the type a using alias names. Other declarations are
// returned unchanged.
func (r *resolver) deref(d *code.Decl) *code.Decl {
	if d == nil || d.Kind != code.KindImport {
		return d
	}
	return r.resolveType(d.Parent, d.Type, nil)
}

// simpleType resolves a one-segment type name seen from ctx.
func (r *resolver) simpleType(ctx *code.Decl, name string, header *code.Decl) *code.Decl {
	lang := ctx.File.Lang
	for d := ctx; d != nil; d = d.Parent {
		switch {
		case d.IsType():
			if c := childNamed(d, name, isTypeParam); c != nil {
				return c
			}
			if d != header {
				if t := firstType(r.members(d, name)); t != nil {
					return t
				}
			}
		case d.Kind == code.KindNamespace:
			for ns := r.fqn[d]; ns != ""; ns = parentPath(ns) {
				if t := r.typeAt(lang, ns+"."+name); t != nil {
					return t
				}
			}
		case d.Kind != code.KindModule:
			if c := childNamed(d, name, func(c *code.Decl) bool { return c.IsType() || c.Kind == code.KindTypeParameter }); c != nil {
				return c
			}
		}
	}
	fi := r.infos[ctx.File]
	if lang == code.Java {
		for _, imp := range fi.imports {
			if !imp.star && imp.refs[len(imp.refs)-1].Name == name {
				if t := r.typeAt(lang, importPath(imp, len(imp.refs))); t != nil {
					return t
				}
			}
		}
		if t := r.typeAt(lang, join(ctx.File.Module.Name, name)); t != nil {
			return t
		}
		for _, imp := range fi.imports {
			if imp.star {
				if t := r.importedType(lang, imp, name); t != nil {
					return t
				}
			}
		}
		return nil
	}
	if t := r.typeAt(lang, name); t != nil {
		return t
	}
	imports := append(slices.Clip(fi.imports), r.globals...)
	for _, imp := range imports {
		if imp.alias != nil && imp.alias.Name == name && (imp.global || imp.alias.File == ctx.File) {
			return imp.alias
		}
	}
	for _, imp := range imports {
		if imp.alias != nil {
			continue
		}
		if t := r.importedType(lang, imp, name); t != nil {
			return t
		}
	}
	return nil
}

func (r *resolver) importedType(lang code.Lang, imp importInfo, name string) *code.Decl {
	path := importPath(imp, len(imp.refs))
	if !imp.static {
		return r.typeAt(lang, path+"."+name)
	}
	if t := r.typeAt(lang, path); t != nil {
		return firstType(r.members(t, name))
	}
	return nil
}

func isTypeParam(d *code.Decl) bool { return d.Kind == code.KindTypeParameter }

func childNamed(d *code.Decl, name string, ok func(*code.Decl) bool) *code.Decl {
	for _, c := range d.Children {
		if c.Name == name && ok(c) {
			return c
		}
	}
	return nil
}

// parts returns all declarations of type t. There can be several for C#
// partial types.
func (r *resolver) parts(t *code.Decl) []*code.Decl {
	var out []*code.Decl
	for _, d := range r.byFQN[t.File.Lang][r.fqn[t]] {
		if d.IsType() {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		out = append(out, t)
	}
	return out
}

// members returns the members of type t named name. They come from the
// nearest type in the base hierarchy that declares any.
func (r *resolver) members(t *code.Decl, name string) []*code.Decl {
	var seen []*code.Decl
	return r.membersRec(t, name, &seen)
}

func (r *resolver) membersRec(t *code.Decl, name string, seen *[]*code.Decl) []*code.Decl {
	if t == nil || slices.Contains(*seen, t) || len(*seen) > 64 {
		return nil
	}
	parts := r.parts(t)
	*seen = append(*seen, parts...)
	var out []*code.Decl
	for _, pt := range parts {
		for _, c := range pt.Children {
			if c.Name == name && !r.hidden[c] {
				out = append(out, c)
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, b := range r.basesOf(t) {
		if res := r.membersRec(b, name, seen); len(res) > 0 {
			return res
		}
	}
	return nil
}

func (r *resolver) basesOf(t *code.Decl) []*code.Decl {
	if b, ok := r.bases[t]; ok {
		return b
	}
	parts := r.parts(t)
	for _, pt := range parts {
		r.bases[pt] = nil
	}
	var out []*code.Decl
	for _, pt := range parts {
		for _, name := range pt.Bases {
			b := r.deref(r.resolveType(pt, name, pt))
			if b != nil && b.IsType() && !slices.Contains(parts, b) && !slices.Contains(out, b) {
				out = append(out, b)
			}
		}
	}
	for _, pt := range parts {
		r.bases[pt] = out
	}
	return out
}

func isValue(d *code.Decl) bool {
	switch d.Kind {
	case code.KindVariable, code.KindParameter, code.KindField, code.KindProperty, code.KindConstant, code.KindEnumMember:
		return true
	}
	return false
}

func isTypeLike(d *code.Decl) bool {
	return d.IsType() || d.Kind == code.KindTypeParameter || d.Kind == code.KindImport || d.Kind == code.KindNamespace
}

func filter(ds []*code.Decl, ok func(*code.Decl) bool) []*code.Decl {
	var out []*code.Decl
	for _, d := range ds {
		if ok(d) {
			out = append(out, d)
		}
	}
	return out
}

// pick chooses among same-named candidates by the kind the reference
// position can name, then among overloads.
func (r *resolver) pick(cands []*code.Decl, ri *refInfo) *code.Decl {
	if len(cands) == 0 {
		return nil
	}
	var pool []*code.Decl
	switch ri.want {
	case wantType:
		pool = filter(cands, isTypeLike)
	case wantCall:
		if pool = filter(cands, (*code.Decl).Callable); len(pool) == 0 {
			pool = filter(cands, isValue)
		}
	default:
		if pool = filter(cands, isValue); len(pool) == 0 {
			if pool = filter(cands, isTypeLike); len(pool) == 0 {
				pool = filter(cands, (*code.Decl).Callable)
			}
		}
	}
	return r.overload(pool, ri.arity)
}

// overload narrows the pool to the overloads whose arity matches n, if any
// match. If exactly one of the rest has a body, like the implementation part
// of a partial method, we pick that, else the first.
func (r *resolver) overload(pool []*code.Decl, n int) *code.Decl {
	if len(pool) == 0 {
		return nil
	}
	if n >= 0 && len(pool) > 1 {
		matching := filter(pool, func(d *code.Decl) bool {
			a, ok := r.arity[d]
			return ok && (a.n == n || (a.varargs && n >= a.n-1))
		})
		if len(matching) > 0 {
			pool = matching
		}
	}
	if len(pool) > 1 {
		if bodied := filter(pool, func(d *code.Decl) bool { return !d.Body.Empty() }); len(bodied) == 1 {
			return bodied[0]
		}
	}
	return pool[0]
}

func (r *resolver) unqualified(ref *code.Ref, ri *refInfo) *code.Decl {
	name := ref.Name
	ctx := ri.ctx
	if ri.want == wantType {
		if ri.attr {
			if t := r.simpleType(ctx, name+"Attribute", ri.header); t != nil {
				return t
			}
		}
		if t := r.simpleType(ctx, name, ri.header); t != nil {
			return t
		}
		return r.markPath(ref, ctx)
	}
	for a := ri.anon; a != nil; a = a.parent {
		if d := r.pick(filter(a.members, func(d *code.Decl) bool { return d.Name == name }), ri); d != nil {
			return d
		}
		if base := r.deref(r.resolveType(a.ctx, a.base, nil)); base != nil {
			if d := r.pick(r.members(base, name), ri); d != nil {
				return d
			}
		}
	}
	for d := ctx; d != nil; d = d.Parent {
		switch {
		case d.IsType():
			if d != ri.header {
				if c := r.pick(r.members(d, name), ri); c != nil {
					return c
				}
			}
			if c := childNamed(d, name, isTypeParam); c != nil {
				return c
			}
		case d.Kind == code.KindNamespace:
		case d.Kind == code.KindModule:
			// C# local functions of top-level statements
			local := filter(d.Children, func(c *code.Decl) bool { return c.Name == name && c.Kind == code.KindFunction })
			if c := r.pick(local, ri); c != nil {
				return c
			}
		default:
			// local functions and types anywhere in an enclosing body
			local := filter(d.Children, func(c *code.Decl) bool {
				return c.Name == name && (c.Kind == code.KindFunction || c.IsType() || c.Kind == code.KindTypeParameter)
			})
			if c := r.pick(local, ri); c != nil {
				return c
			}
		}
	}
	if t := r.simpleType(ctx, name, ri.header); t != nil {
		return t
	}
	if d := r.staticImport(ref, ri); d != nil {
		return d
	}
	if ri.caseLabel {
		enums := filter(r.byName[ref.File.Lang][name], func(d *code.Decl) bool { return d.Kind == code.KindEnumMember })
		if len(enums) == 1 {
			return enums[0]
		}
	}
	return r.markPath(ref, ctx)
}

// staticImport resolves a name through Java static imports and C# using
// static directives.
func (r *resolver) staticImport(ref *code.Ref, ri *refInfo) *code.Decl {
	lang := ref.File.Lang
	imports := r.infos[ref.File].imports
	if lang == code.CSharp {
		imports = append(slices.Clip(imports), r.globals...)
	}
	for _, imp := range imports {
		if !imp.static {
			continue
		}
		n := len(imp.refs)
		if lang == code.Java && !imp.star {
			if imp.refs[n-1].Name != ref.Name {
				continue
			}
			n--
		}
		if t := r.typeAt(lang, importPath(imp, n)); t != nil {
			if d := r.pick(r.members(t, ref.Name), ri); d != nil {
				return d
			}
		}
	}
	return nil
}

// markPath records an unresolved name that names a package or namespace
// path, and returns the namespace declaration it names if there is one.
func (r *resolver) markPath(ref *code.Ref, ctx *code.Decl) *code.Decl {
	lang := ref.File.Lang
	var cands []string
	if lang == code.CSharp {
		for _, ns := range r.namespacesOf(ctx) {
			cands = append(cands, ns+"."+ref.Name)
		}
	}
	cands = append(cands, ref.Name)
	for _, c := range cands {
		if r.spaces[lang][c] {
			r.paths[ref] = c
			return r.namespaceDecl(lang, c, ref.Name)
		}
	}
	return nil
}

func (r *resolver) enclosingType(d *code.Decl) *code.Decl {
	for ; d != nil; d = d.Parent {
		if d.IsType() {
			return d
		}
	}
	return nil
}

func (r *resolver) qualified(ref *code.Ref, ri *refInfo) *code.Decl {
	name := ref.Name
	q := ri.qual
	switch q.kind {
	case qualThis:
		if a := ri.anon; a != nil {
			if d := r.pick(filter(a.members, func(d *code.Decl) bool { return d.Name == name }), ri); d != nil {
				return d
			}
			if base := r.deref(r.resolveType(a.ctx, a.base, nil)); base != nil {
				return r.memberOf(base, ref, ri)
			}
			return nil
		}
		for t := r.enclosingType(ri.ctx); t != nil; t = r.enclosingType(t.Parent) {
			if d := r.pick(r.members(t, name), ri); d != nil {
				return d
			}
		}
		return nil
	case qualSuper:
		if a := ri.anon; a != nil {
			if base := r.deref(r.resolveType(a.ctx, a.base, nil)); base != nil {
				return r.memberOf(base, ref, ri)
			}
			return nil
		}
		for _, b := range r.basesOf(r.enclosingType(ri.ctx)) {
			if d := r.pick(r.members(b, name), ri); d != nil {
				return d
			}
		}
		return nil
	case qualType:
		if t := r.deref(r.resolveType(ri.ctx, q.typ, nil)); t != nil {
			return r.memberOf(t, ref, ri)
		}
		return nil
	case qualOpaque:
		return r.unique(ref, ri)
	case qualCall:
		c := q.ref.Target
		if c == nil || !c.Callable() {
			return nil
		}
		if t := r.deref(r.resolveType(c, c.Type, nil)); t != nil {
			return r.memberOf(t, ref, ri)
		}
		return nil
	case qualRef:
		e := q.ref.Target
		if e == nil {
			if path := r.paths[q.ref]; path != "" {
				return r.inPath(path, ref)
			}
			return nil
		}
		return r.memberOfEntity(e, ref, ri)
	}
	return nil
}

// memberOfEntity resolves `e.name`, where e is the entity the qualifier
// refers to.
func (r *resolver) memberOfEntity(e *code.Decl, ref *code.Ref, ri *refInfo) *code.Decl {
	switch {
	case e.Kind == code.KindNamespace:
		return r.inPath(r.fqn[e], ref)
	case e.Kind == code.KindImport:
		if t := r.deref(e); t != nil {
			return r.memberOf(t, ref, ri)
		}
		if path := cleanType(e.Type); r.spaces[e.File.Lang][path] {
			return r.inPath(path, ref)
		}
		return nil
	case e.IsType():
		return r.memberOf(e, ref, ri)
	case e.Kind == code.KindEnumMember:
		return r.memberOf(e.Parent, ref, ri)
	case e.Kind == code.KindTypeParameter || e.Callable():
		return nil
	}
	if typ := cleanType(e.Type); typ == "" {
		return r.unique(ref, ri)
	}
	if t := r.deref(r.resolveType(e, e.Type, nil)); t != nil {
		return r.memberOf(t, ref, ri)
	}
	return nil
}

func (r *resolver) memberOf(t *code.Decl, ref *code.Ref, ri *refInfo) *code.Decl {
	if !t.IsType() {
		return nil
	}
	if d := r.pick(r.members(t, ref.Name), ri); d != nil {
		return d
	}
	if ri.want == wantCall {
		return r.extension(ref, ri)
	}
	return nil
}

// extension resolves a call to a C# extension method if all extension
// methods with that name belong to the same static class.
func (r *resolver) extension(ref *code.Ref, ri *refInfo) *code.Decl {
	cands := filter(r.ext[ref.Name], func(d *code.Decl) bool { return d.File.Lang == ref.File.Lang })
	if len(cands) == 0 || !sameOwner(r, cands) {
		return nil
	}
	n := ri.arity
	if n >= 0 {
		n++
	}
	return r.overload(cands, n)
}

func sameOwner(r *resolver, ds []*code.Decl) bool {
	for _, d := range ds[1:] {
		if r.fqn[d.Parent] != r.fqn[ds[0].Parent] {
			return false
		}
	}
	return true
}

// inPath resolves name inside a package or namespace path.
func (r *resolver) inPath(path string, ref *code.Ref) *code.Decl {
	lang := ref.File.Lang
	full := join(path, ref.Name)
	if t := r.typeAt(lang, full); t != nil {
		return t
	}
	if r.spaces[lang][full] {
		r.paths[ref] = full
		return r.namespaceDecl(lang, full, ref.Name)
	}
	return nil
}

// unique resolves a member of an expression we can't type, if only one type
// in the workspace declares a member with that name and kind.
func (r *resolver) unique(ref *code.Ref, ri *refInfo) *code.Decl {
	var ok func(*code.Decl) bool
	switch ri.want {
	case wantType:
		return nil
	case wantCall:
		ok = (*code.Decl).Callable
	default:
		ok = isValue
	}
	cands := filter(r.byName[ref.File.Lang][ref.Name], ok)
	if len(cands) == 0 || !sameOwner(r, cands) {
		if ri.want == wantCall {
			return r.extension(ref, ri)
		}
		return nil
	}
	return r.overload(cands, ri.arity)
}
