package javascript

import (
	"github.com/myoo-ai/cli/internal/code"
)

type fileInfo struct {
	f  *code.File
	pr *parseResult
}

// container is what a member access looks into. That's either a declaration
// with members (class, interface, enum, namespace, object literal) or a
// module.
type container struct {
	decl *code.Decl
	mod  *fileInfo
}

func (c container) empty() bool { return c.decl == nil && c.mod == nil }

type exportKey struct {
	fi   *fileInfo
	name string
}

type resolver struct {
	modules
	infos    map[*code.File]*fileInfo
	order    []*fileInfo
	visiting map[exportKey]bool
	members  map[string][]*code.Decl // member declarations by name
	globals  map[string][]*code.Decl // top-level declarations of script files and `declare global`
}

func newResolver(root string, infos []*fileInfo) *resolver {
	r := &resolver{
		modules:  newModules(root),
		infos:    map[*code.File]*fileInfo{},
		visiting: map[exportKey]bool{},
		members:  map[string][]*code.Decl{},
		globals:  map[string][]*code.Decl{},
	}
	for _, fi := range infos {
		if fi == nil {
			continue
		}
		r.order = append(r.order, fi)
		r.infos[fi.f] = fi
		r.byPath[fi.f.Path] = fi
		if !fi.pr.esm {
			for _, d := range fi.f.Module.Children {
				if d.Kind != code.KindImport {
					r.globals[d.Name] = append(r.globals[d.Name], d)
				}
			}
		}
		for _, d := range fi.pr.globals {
			r.globals[d.Name] = append(r.globals[d.Name], d)
		}
		for _, d := range fi.f.Decls {
			if isMember(d) {
				r.members[d.Name] = append(r.members[d.Name], d)
			}
		}
	}
	return r
}

func isMember(d *code.Decl) bool {
	switch d.Kind {
	case code.KindMethod, code.KindField, code.KindProperty:
	default:
		return false
	}
	switch d.Parent.Kind {
	case code.KindClass, code.KindInterface, code.KindVariable, code.KindConstant, code.KindProperty, code.KindTypeAlias:
		return true
	}
	return false
}

func (r *resolver) run() {
	for _, fi := range r.order {
		for _, ref := range fi.f.Refs {
			r.resolve(fi, ref)
		}
	}
}

func (r *resolver) info(f *code.File) *fileInfo { return r.infos[f] }

func (r *resolver) resolve(fi *fileInfo, ref *code.Ref) *code.Decl {
	m := fi.pr.refs[ref]
	if m == nil {
		return ref.Target
	}
	switch m.state {
	case 1:
		return nil
	case 2:
		return ref.Target
	}
	m.state = 1
	var t *code.Decl
	switch {
	case m.obj != objNone:
		t = r.member(fi, ref, m)
	case m.spec != "":
		if mod := r.module(fi, m.spec); mod != nil {
			t = r.export(mod, ref.Name)
		}
	default:
		t = r.lexical(fi, m.sc, ref.Name, m.typePos)
	}
	ref.Target = t
	m.state = 2
	return t
}

func (r *resolver) lexical(fi *fileInfo, sc *scope, name string, typePos bool) *code.Decl {
	for s := sc; s != nil; s = s.parent {
		if ds, ok := s.names[name]; ok {
			return pick(filterKind(ds, typePos))
		}
		if b, ok := s.imports[name]; ok {
			if mod := r.module(fi, b.spec); mod != nil {
				return r.export(mod, b.name)
			}
			return nil
		}
	}
	return r.global(name)
}

// filterKind prefers type declarations in type positions and values
// elsewhere. If that leaves nothing, we keep all candidates.
func filterKind(ds []*code.Decl, typePos bool) []*code.Decl {
	if len(ds) < 2 {
		return ds
	}
	var out []*code.Decl
	for _, d := range ds {
		isType := d.Kind == code.KindInterface || d.Kind == code.KindTypeAlias || d.Kind == code.KindTypeParameter
		if isType == typePos || d.Kind == code.KindClass || d.Kind == code.KindEnum || d.Kind == code.KindNamespace || d.Kind == code.KindImport {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return ds
	}
	return out
}

// pick chooses between declarations of the same name. We take the only one,
// else the only one with a body (to skip overloads and ambient
// declarations), else the first.
func pick(ds []*code.Decl) *code.Decl {
	if len(ds) == 0 {
		return nil
	}
	if len(ds) == 1 {
		return ds[0]
	}
	var body *code.Decl
	n := 0
	for _, d := range ds {
		if !d.Body.Empty() {
			body = d
			n++
		}
	}
	if n == 1 {
		return body
	}
	return ds[0]
}

// global resolves a free identifier to a top-level declaration of a script
// file (no imports or exports) or of a `declare global` block. If the name
// is declared in more than one file, we can't tell which is meant and
// return nil.
func (r *resolver) global(name string) *code.Decl {
	ds := r.globals[name]
	if len(ds) == 0 {
		return nil
	}
	for _, d := range ds[1:] {
		if d.File != ds[0].File {
			return nil
		}
	}
	return pick(ds)
}

func (r *resolver) export(fi *fileInfo, name string) *code.Decl {
	key := exportKey{fi, name}
	if r.visiting[key] {
		return nil
	}
	r.visiting[key] = true
	defer delete(r.visiting, key)
	if e, ok := fi.pr.exports[name]; ok {
		switch {
		case e.decl != nil:
			return e.decl
		case e.spec != "":
			if mod := r.module(fi, e.spec); mod != nil {
				return r.export(mod, e.name)
			}
			return nil
		default:
			return r.lexical(fi, e.sc, e.name, false)
		}
	}
	if name == "default" {
		return nil
	}
	for _, spec := range fi.pr.stars {
		if mod := r.module(fi, spec); mod != nil {
			if d := r.export(mod, name); d != nil {
				return d
			}
		}
	}
	return nil
}

func (r *resolver) member(fi *fileInfo, ref *code.Ref, m *refMeta) *code.Decl {
	var c container
	resolvedObj := false
	switch m.obj {
	case objThis:
		c = container{decl: m.thisDecl}
	case objSuper:
		if m.thisDecl != nil {
			c = r.baseContainer(m.thisDecl)
		}
	case objRequire:
		c = container{mod: r.module(fi, m.spec)}
	case objRef:
		if obj := r.resolve(fi, m.objRef); obj != nil {
			resolvedObj = true
			c = r.containerOf(obj, 0)
		} else if r.isExportsObject(fi, m.objRef) {
			c = container{mod: fi}
		}
	}
	if !c.empty() {
		return r.memberOf(c, ref.Name, 0)
	}
	if resolvedObj {
		return r.uniqueMember(ref.Name)
	}
	return nil
}

// isExportsObject reports whether ref is the free CommonJS `exports` or
// `module.exports` of its file.
func (r *resolver) isExportsObject(fi *fileInfo, ref *code.Ref) bool {
	m := fi.pr.refs[ref]
	if m == nil || ref.Name != "exports" || ref.Target != nil {
		return false
	}
	if m.obj == objNone {
		return true
	}
	if m.obj != objRef || m.objRef.Name != "module" || m.objRef.Target != nil {
		return false
	}
	mm := fi.pr.refs[m.objRef]
	return mm != nil && mm.obj == objNone
}

func (r *resolver) baseContainer(cls *code.Decl) container {
	fi := r.info(cls.File)
	if fi == nil {
		return container{}
	}
	for _, b := range fi.pr.bases[cls] {
		if bd := r.resolve(fi, b); bd != nil && bd != cls {
			if c := r.containerOf(bd, 0); !c.empty() {
				return c
			}
		}
	}
	return container{}
}

func (r *resolver) containerOf(d *code.Decl, depth int) container {
	if d == nil || depth > 16 {
		return container{}
	}
	fi := r.info(d.File)
	if fi == nil {
		return container{}
	}
	switch d.Kind {
	case code.KindImport:
		a, ok := fi.pr.aliases[d]
		if !ok {
			return container{}
		}
		switch {
		case a.ref != nil:
			return r.containerOf(r.resolve(fi, a.ref), depth+1)
		case a.sc != nil:
			return r.containerOf(r.lexical(fi, a.sc, a.name, false), depth+1)
		}
		mod := r.module(fi, a.spec)
		if mod == nil {
			return container{}
		}
		if a.name == "*" {
			return container{mod: mod}
		}
		return r.containerOf(r.export(mod, a.name), depth+1)
	case code.KindClass, code.KindInterface, code.KindEnum, code.KindNamespace:
		return container{decl: d}
	case code.KindTypeAlias, code.KindVariable, code.KindConstant, code.KindParameter, code.KindField, code.KindProperty:
		if hasMembers(d) || len(fi.pr.shorthand[d]) > 0 {
			return container{decl: d}
		}
		if tr := fi.pr.typeRef[d]; tr != nil {
			return r.containerOf(r.resolve(fi, tr), depth+1)
		}
	}
	return container{}
}

func hasMembers(d *code.Decl) bool {
	for _, c := range d.Children {
		if c.Kind != code.KindTypeParameter && c.Kind != code.KindParameter {
			return true
		}
	}
	return false
}

// memberOf looks up member name in c, walking base classes and merged
// declarations.
func (r *resolver) memberOf(c container, name string, depth int) *code.Decl {
	if depth > 16 {
		return nil
	}
	if c.mod != nil {
		if d := r.export(c.mod, name); d != nil {
			return d
		}
		if c.mod.pr.cjsWhole {
			if def := r.export(c.mod, "default"); def != nil {
				return r.memberOf(r.containerOf(def, 0), name, depth+1)
			}
		}
		return nil
	}
	d := c.decl
	if d == nil {
		return nil
	}
	var cands []*code.Decl
	for _, g := range merged(d) {
		for _, ch := range g.Children {
			if ch.Name == name && ch.Kind != code.KindTypeParameter && ch.Kind != code.KindParameter {
				cands = append(cands, ch)
			}
		}
	}
	if len(cands) > 0 {
		return pick(cands)
	}
	fi := r.info(d.File)
	if fi == nil {
		return nil
	}
	if sh := fi.pr.shorthand[d][name]; sh != nil {
		return r.resolve(fi, sh)
	}
	for _, g := range merged(d) {
		for _, b := range fi.pr.bases[g] {
			bd := r.resolve(fi, b)
			if bd == nil || bd == d {
				continue
			}
			if m := r.memberOf(r.containerOf(bd, 0), name, depth+1); m != nil {
				return m
			}
		}
	}
	return nil
}

// merged returns d plus the declarations TypeScript merges with it:
// same-named namespaces, interfaces, enums and classes in the same scope.
func merged(d *code.Decl) []*code.Decl {
	switch d.Kind {
	case code.KindNamespace, code.KindInterface, code.KindEnum, code.KindClass:
	default:
		return []*code.Decl{d}
	}
	if d.Parent == nil {
		return []*code.Decl{d}
	}
	var out []*code.Decl
	for _, s := range d.Parent.Children {
		if s == d || s.Name == d.Name && (s.Kind == code.KindNamespace || s.Kind == code.KindInterface || s.Kind == code.KindEnum || s.Kind == code.KindClass) {
			out = append(out, s)
		}
	}
	return out
}

// uniqueMember resolves obj.name for an object of unknown type, but only if
// all member declarations with that name in the workspace share a parent.
// We never do this for members of built-in objects: `items.map` on an
// untyped array must not resolve to a workspace method named map.
func (r *resolver) uniqueMember(name string) *code.Decl {
	if builtinMembers[name] {
		return nil
	}
	ds := r.members[name]
	if len(ds) == 0 {
		return nil
	}
	for _, d := range ds[1:] {
		if d.Parent != ds[0].Parent {
			return nil
		}
	}
	return pick(ds)
}

// builtinMembers are the members of the standard built-in objects (Object,
// Function, Array, String, Number, Promise, Map, Set, Date, RegExp, Error,
// iterators, events, console and JSON).
var builtinMembers = code.StringSet(
	"constructor", "prototype", "length", "name", "toString", "toLocaleString", "valueOf",
	"hasOwnProperty", "isPrototypeOf", "propertyIsEnumerable", "call", "apply", "bind",
	"at", "concat", "copyWithin", "entries", "every", "fill", "filter", "find", "findIndex",
	"findLast", "findLastIndex", "flat", "flatMap", "forEach", "includes", "indexOf", "join",
	"keys", "lastIndexOf", "map", "pop", "push", "reduce", "reduceRight", "reverse", "shift",
	"slice", "some", "sort", "splice", "toReversed", "toSorted", "toSpliced", "unshift",
	"values", "with", "charAt", "charCodeAt", "codePointAt", "endsWith", "localeCompare",
	"match", "matchAll", "normalize", "padEnd", "padStart", "repeat", "replace", "replaceAll",
	"search", "split", "startsWith", "substring", "substr", "toLowerCase", "toUpperCase",
	"trim", "trimEnd", "trimStart", "toFixed", "toPrecision", "toExponential", "then",
	"catch", "finally", "get", "set", "has", "delete", "clear", "add", "size", "next",
	"return", "throw", "done", "value", "getTime", "toISOString", "toJSON", "test", "exec",
	"source", "flags", "message", "stack", "cause", "log", "error", "warn", "info", "debug",
	"addEventListener", "removeEventListener", "dispatchEvent", "on", "off", "once", "emit",
	"stringify", "parse",
)
