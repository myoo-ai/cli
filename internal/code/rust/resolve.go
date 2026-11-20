package rust

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// maxDepth limits how deep we follow aliases and recurse through globs and types.
const maxDepth = 24

type resolver struct {
	refs    map[*code.Ref]*refInfo
	decls   map[*code.Decl]*declInfo
	modScop map[*code.Decl]*scope
	modUp   map[*code.Decl]*code.Decl // module to the module that declares it
	// Module to the file modules that its `mod x;` lines declare.
	modKids map[*code.Decl]map[string]*code.Decl
	crates  map[string]*code.Decl // crate name to root module
	// Type to the associated items of its impl blocks.
	assoc  map[*code.Decl]map[string][]*code.Decl
	traits map[*code.Decl][]*code.Decl
	// The crate's declarations by name, one map per kind.
	methods map[string][]*code.Decl
	fields  map[string][]*code.Decl
	macros  map[string][]*code.Decl
	done    map[*code.Ref]bool
	busy    map[*code.Ref]bool
	depth   int
}

func resolve(infos []*fileInfo) {
	r := &resolver{
		refs: map[*code.Ref]*refInfo{}, decls: map[*code.Decl]*declInfo{},
		modScop: map[*code.Decl]*scope{}, modUp: map[*code.Decl]*code.Decl{},
		modKids: map[*code.Decl]map[string]*code.Decl{}, crates: map[string]*code.Decl{},
		assoc: map[*code.Decl]map[string][]*code.Decl{}, traits: map[*code.Decl][]*code.Decl{},
		methods: map[string][]*code.Decl{}, fields: map[string][]*code.Decl{}, macros: map[string][]*code.Decl{},
		done: map[*code.Ref]bool{}, busy: map[*code.Ref]bool{},
	}
	for _, fi := range infos {
		maps.Copy(r.refs, fi.refs)
		maps.Copy(r.decls, fi.decls)
		maps.Copy(r.modScop, fi.modScop)
		maps.Copy(r.modUp, fi.modUp)
		for _, d := range fi.f.Decls {
			switch d.Kind {
			case code.KindMethod:
				r.methods[d.Name] = append(r.methods[d.Name], d)
			case code.KindField:
				r.fields[d.Name] = append(r.fields[d.Name], d)
			case code.KindMacro:
				r.macros[d.Name] = append(r.macros[d.Name], d)
			}
		}
	}
	r.moduleTree(infos)
	for _, fi := range infos {
		for _, im := range fi.impls {
			r.addImpl(im)
		}
	}
	for _, fi := range infos {
		for _, ref := range fi.f.Refs {
			r.resolveRef(ref)
		}
	}
}

// moduleTree links `mod x;` declarations to their files, starting from the
// crate roots. It also renames the file modules after their place in the
// tree, since a #[path] attribute can move a module anywhere.
func (r *resolver) moduleTree(infos []*fileInfo) {
	byPath := map[string]*fileInfo{}
	for _, fi := range infos {
		byPath[fi.f.Path] = fi
	}
	var queue []*fileInfo
	for _, fi := range infos {
		if !fi.layout.root {
			continue
		}
		queue = append(queue, fi)
		name := fi.f.Module.Name
		if old, ok := r.crates[name]; !ok || path.Base(fi.f.Path) == "lib.rs" && path.Base(old.File.Path) != "lib.rs" {
			r.crates[name] = fi.f.Module
		}
	}
	visited := map[*fileInfo]bool{}
	modRS := map[*fileInfo]bool{}
	for _, fi := range queue {
		visited[fi] = true
		modRS[fi] = fi.layout.modRS
	}
	for len(queue) > 0 {
		fi := queue[0]
		queue = queue[1:]
		for _, l := range fi.links {
			var child *fileInfo
			for _, p := range modFiles(fi, l, modRS[fi]) {
				if child = byPath[p]; child != nil {
					break
				}
			}
			if child == nil || child == fi {
				continue
			}
			l.ref.Target = child.f.Module
			r.done[l.ref] = true
			kids := r.modKids[l.parent]
			if kids == nil {
				kids = map[string]*code.Decl{}
				r.modKids[l.parent] = kids
			}
			kids[l.ref.Name] = child.f.Module
			if visited[child] {
				continue
			}
			visited[child] = true
			modRS[child] = child.layout.modRS || l.path != ""
			child.f.Module.Name = l.parent.FQN() + "." + l.ref.Name
			r.modUp[child.f.Module] = l.parent
			queue = append(queue, child)
		}
	}
	// If no crate root reaches a file, we hang it below the module its path
	// names.
	byName := map[string]*code.Decl{}
	for _, fi := range infos {
		byName[fi.f.Module.Name] = fi.f.Module
	}
	for _, fi := range infos {
		if visited[fi] || fi.layout.root {
			continue
		}
		name := fi.f.Module.Name
		for i := strings.LastIndexByte(name, '.'); i > 0; i = strings.LastIndexByte(name[:i], '.') {
			if up := byName[name[:i]]; up != nil {
				r.modUp[fi.f.Module] = up
				break
			}
		}
	}
}

// modFiles returns the paths a `mod x;` declaration in fi can load. Next to
// a mod.rs-like file that's x.rs or x/mod.rs, otherwise the same paths in a
// directory named after the file. A #[path] is relative to the file.
func modFiles(fi *fileInfo, l modLink, modRS bool) []string {
	if l.path != "" && len(l.inline) == 0 {
		return []string{path.Join(path.Dir(fi.f.Path), l.path)}
	}
	dir := path.Dir(fi.f.Path)
	if !modRS {
		dir = path.Join(dir, strings.TrimSuffix(path.Base(fi.f.Path), ".rs"))
	}
	for _, s := range l.inline {
		dir = path.Join(dir, s)
	}
	if l.path != "" {
		return []string{path.Join(dir, l.path)}
	}
	return []string{path.Join(dir, l.ref.Name+".rs"), path.Join(dir, l.ref.Name, "mod.rs")}
}

// addImpl records the associated items and the trait of an impl block on
// the type it implements.
func (r *resolver) addImpl(im *implInfo) {
	if im.self == nil {
		return
	}
	t := r.deref(r.resolveRef(im.self))
	if t == nil {
		return
	}
	m := r.assoc[t]
	if m == nil {
		m = map[string][]*code.Decl{}
		r.assoc[t] = m
	}
	for _, d := range im.members {
		m[d.Name] = append(m[d.Name], d)
	}
	if im.trait == nil {
		return
	}
	if tr := r.deref(r.resolveRef(im.trait)); tr != nil && tr.Kind == code.KindInterface && !slices.Contains(r.traits[t], tr) {
		r.traits[t] = append(r.traits[t], tr)
	}
	if im.traitAs != "" && !slices.Contains(t.Bases, im.traitAs) {
		t.Bases = append(t.Bases, im.traitAs)
	}
}

func (r *resolver) resolveRef(ref *code.Ref) *code.Decl {
	if ref == nil {
		return nil
	}
	if r.done[ref] || r.busy[ref] {
		return ref.Target
	}
	ri := r.refs[ref]
	if ri == nil {
		return ref.Target
	}
	r.busy[ref] = true
	var t *code.Decl
	off := ref.Span.Start
	switch ri.kind {
	case refPlain:
		t = r.lookup(ri.sc, ref.Name, off, false)
	case refMacro:
		t = r.lookup(ri.sc, ref.Name, off, true)
		if t == nil && len(r.macros[ref.Name]) == 1 {
			t = r.macros[ref.Name][0]
		}
	case refUse:
		t = r.lookup(ri.sc, ref.Name, -1, false)
	case refCrate:
		t = r.crates[ref.Name]
	case refPrefix:
		t = r.member(r.prefixBase(ri), ref.Name)
	case refSeg:
		if ri.prev != nil {
			t = r.member(r.resolveRef(ri.prev), ref.Name)
		}
	case refMember:
		t = r.memberOf(ri, ref)
	case refField:
		t = r.field(ri, ref.Name)
	}
	delete(r.busy, ref)
	r.done[ref] = true
	ref.Target = t
	return t
}

// lookup finds name in the lexical scopes around sc. We check bindings
// visible at off, items, imports and generic parameters up to the enclosing
// module, then the crates. Macros are scoped textually across modules.
func (r *resolver) lookup(sc *scope, name string, off int, macro bool) *code.Decl {
	for s := sc; s != nil; s = s.parent {
		if !macro {
			if s.hidden[name] {
				return nil
			}
			for _, d := range slices.Backward(s.locals) {
				if d.Name == name && off >= 0 && r.decls[d].visible <= off {
					return d
				}
			}
		}
		for _, d := range s.items[name] {
			if (d.Kind == code.KindMacro) == macro {
				return d
			}
		}
		if macro {
			continue
		}
		if s.kind == scopeModule {
			if t := r.lookupModule(s.mod, name); t != nil {
				return t
			}
			break
		}
		if t := r.scopeImports(s, name); t != nil {
			return t
		}
	}
	if macro {
		return nil
	}
	return r.crates[name]
}

// lookupModule finds name among the items, child modules and imports of
// module m.
func (r *resolver) lookupModule(m *code.Decl, name string) *code.Decl {
	if m == nil || r.depth > maxDepth {
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()
	sc := r.modScop[m]
	if sc != nil {
		for _, d := range sc.items[name] {
			if d.Kind != code.KindMacro {
				return d
			}
		}
	}
	if k := r.modKids[m][name]; k != nil {
		return k
	}
	if sc == nil {
		return nil
	}
	return r.scopeImports(sc, name)
}

// scopeImports resolves name through the use declarations of sc.
func (r *resolver) scopeImports(sc *scope, name string) *code.Decl {
	if ref := sc.imports[name]; ref != nil {
		if t := r.resolveRef(ref); t != nil {
			return t
		}
	}
	for _, g := range sc.globs {
		var c *code.Decl
		if g.ref != nil {
			c = r.deref(r.resolveRef(g.ref))
		} else {
			c = r.prefixBase(&refInfo{sc: sc, prefix: g.prefix, supers: g.supers})
		}
		if c != nil && c != sc.mod {
			if t := r.member(c, name); t != nil {
				return t
			}
		}
	}
	return nil
}

// deref follows import aliases and type aliases to the declaration behind
// them.
func (r *resolver) deref(d *code.Decl) *code.Decl {
	for range maxDepth {
		if d == nil {
			return nil
		}
		di := r.decls[d]
		switch {
		case d.Kind == code.KindImport && di != nil:
			d = r.resolveRef(di.alias)
		case d.Kind == code.KindTypeAlias && di != nil && di.typeRef != nil:
			d = r.resolveRef(di.typeRef)
		default:
			return d
		}
	}
	return nil
}

// member finds the item name inside module or type c. In a type that can be
// an enum variant or an associated item.
func (r *resolver) member(c *code.Decl, name string) *code.Decl {
	c = r.deref(c)
	if c == nil || r.depth > maxDepth {
		return nil
	}
	switch c.Kind {
	case code.KindModule, code.KindNamespace:
		return r.lookupModule(c, name)
	case code.KindStruct, code.KindEnum, code.KindInterface, code.KindTypeAlias:
		r.depth++
		defer func() { r.depth-- }()
		return r.typeMember(c, name, true)
	}
	return nil
}

// typeMember finds name among the variants, fields, associated items and
// trait items of type t. assocOnly leaves fields out.
func (r *resolver) typeMember(t *code.Decl, name string, assocOnly bool) *code.Decl {
	for _, ch := range t.Children {
		if ch.Name != name || ch.Kind == code.KindTypeParameter || assocOnly && ch.Kind == code.KindField {
			continue
		}
		return ch
	}
	if ms := r.assoc[t][name]; len(ms) > 0 {
		return ms[0]
	}
	for _, tr := range r.traits[t] {
		for _, ch := range tr.Children {
			if ch.Name == name && ch.Kind != code.KindTypeParameter {
				return ch
			}
		}
	}
	return nil
}

// prefixBase returns the module or type a crate::, self::, super:: or
// Self:: path starts from.
func (r *resolver) prefixBase(ri *refInfo) *code.Decl {
	var base *code.Decl
	switch ri.prefix {
	case "crate":
		base = ri.sc.mod
		for base != nil && r.modUp[base] != nil {
			base = r.modUp[base]
		}
		return base
	case "Self":
		return r.selfType(ri.sc)
	case "self", "super":
		base = ri.sc.mod
	}
	for range ri.supers {
		base = r.modUp[base]
	}
	return base
}

// selfType returns the type an impl block implements. Inside the body of a
// type or trait it returns that type or trait.
func (r *resolver) selfType(sc *scope) *code.Decl {
	for s := sc; s != nil; s = s.parent {
		if s.selfImpl {
			return r.deref(r.resolveRef(s.selfType))
		}
		if s.selfDecl != nil {
			return s.selfDecl
		}
	}
	return nil
}

// memberOf resolves a field access or method call. If we know the type of
// the object, we look the member up there. If not, we can still resolve it
// if only one method or field in the crate has that name.
func (r *resolver) memberOf(ri *refInfo, ref *code.Ref) *code.Decl {
	if t := r.objType(ri.obj, ri.sc); t != nil {
		return r.typeMember(t, ref.Name, false)
	}
	cands := r.fields[ref.Name]
	if ref.Call {
		cands = r.methods[ref.Name]
	}
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}

// field resolves a field name of a struct literal or pattern.
func (r *resolver) field(ri *refInfo, name string) *code.Decl {
	var c *code.Decl
	switch {
	case ri.obj.self:
		c = r.selfType(ri.sc)
	case ri.obj.ref != nil:
		c = r.deref(r.resolveRef(ri.obj.ref))
	}
	if c == nil {
		return nil
	}
	for _, ch := range c.Children {
		if ch.Name == name && ch.Kind == code.KindField {
			return ch
		}
	}
	return nil
}

// objType returns the type declaration of what obj evaluates to.
func (r *resolver) objType(obj object, sc *scope) *code.Decl {
	if r.depth > maxDepth {
		return nil
	}
	if obj.self {
		return r.selfType(sc)
	}
	d := r.deref(r.resolveRef(obj.ref))
	if d == nil {
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()
	if obj.call {
		switch {
		case d.IsType():
			return d
		case d.Kind == code.KindEnumMember:
			return d.Parent
		case d.Callable():
			return r.declType(d)
		}
		return nil
	}
	switch d.Kind {
	case code.KindVariable, code.KindParameter, code.KindField, code.KindConstant:
		return r.declType(d)
	}
	return nil
}

// declType returns the type declaration of a binding, field or the result
// of a function.
func (r *resolver) declType(d *code.Decl) *code.Decl {
	di := r.decls[d]
	if di == nil {
		return nil
	}
	if di.selfRet || selfTypeText(d.Type) {
		return r.selfType(di.sc)
	}
	if di.typeRef != nil {
		if t := r.deref(r.resolveRef(di.typeRef)); t != nil && t.IsType() {
			return t
		}
		return nil
	}
	if di.init.ref != nil || di.init.self {
		return r.objType(di.init, di.sc)
	}
	return nil
}

// selfTypeText reports whether a type as written is Self, possibly behind
// references.
func selfTypeText(t string) bool {
	var rest []string
	for w := range strings.FieldsSeq(strings.ReplaceAll(t, "&", " ")) {
		if w != "mut" && !strings.HasPrefix(w, "'") {
			rest = append(rest, w)
		}
	}
	return len(rest) == 1 && rest[0] == "Self"
}
