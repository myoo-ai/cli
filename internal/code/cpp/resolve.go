package cpp

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// maxDepth bounds how deep type resolution follows aliases, bases and receivers.
const maxDepth = 16

type resolver struct {
	infos  []*fileInfo
	byFile map[*code.File]*fileInfo
	decls  map[*code.Decl]*declInfo
	order  map[*code.File]int
	byPath map[string]*code.File
	// Declarations by namespace path and name, from all files but without the
	// file-local ones.
	global    map[string]map[string][]*code.Decl
	macros    map[string][]*code.Decl
	outOfLine map[*code.Decl][]*code.Decl
	members   map[string][]*code.Decl
	recv      map[*code.Decl]*code.Decl
	includes  map[*code.File][]*code.File
	closure   map[*code.File]map[*code.File]bool
}

func resolve(infos []*fileInfo) {
	r := &resolver{
		infos:     infos,
		byFile:    map[*code.File]*fileInfo{},
		decls:     map[*code.Decl]*declInfo{},
		order:     map[*code.File]int{},
		byPath:    map[string]*code.File{},
		global:    map[string]map[string][]*code.Decl{},
		macros:    map[string][]*code.Decl{},
		outOfLine: map[*code.Decl][]*code.Decl{},
		members:   map[string][]*code.Decl{},
		recv:      map[*code.Decl]*code.Decl{},
		includes:  map[*code.File][]*code.File{},
		closure:   map[*code.File]map[*code.File]bool{},
	}
	for i, fi := range infos {
		r.byFile[fi.f] = fi
		r.order[fi.f] = i
		r.byPath[fi.f.Path] = fi.f
		maps.Copy(r.decls, fi.decls)
		for _, m := range fi.macros {
			r.macros[m.Name] = append(r.macros[m.Name], m)
		}
	}
	for _, fi := range infos {
		for _, inc := range fi.includes {
			if inc.system {
				continue
			}
			if g := r.findInclude(fi.f, inc.path); g != nil {
				inc.ref.Target = g.Module
				r.includes[fi.f] = append(r.includes[fi.f], g)
			}
		}
		for _, sc := range fi.nsScopes {
			r.namespaceGlobals(sc)
		}
	}
	for _, fi := range infos {
		for _, d := range fi.f.Decls {
			if d.Receiver != "" {
				if cls := r.receiverClass(d); cls != nil {
					r.outOfLine[cls] = append(r.outOfLine[cls], d)
				}
			}
			if d.Kind == code.KindMethod || d.Kind == code.KindField {
				r.members[d.Name] = append(r.members[d.Name], d)
			}
		}
	}
	for _, fi := range infos {
		for _, ref := range fi.f.Refs {
			if ri := fi.refs[ref]; ri != nil && ri.kind != refInclude {
				ref.Target = r.resolveRef(ref, ri)
			}
		}
	}
}

func (r *resolver) namespaceGlobals(sc *scope) {
	names := slices.Sorted(maps.Keys(sc.names))
	for _, name := range names {
		for _, d := range sc.names[name] {
			if r.decls[d].fileLocal {
				continue
			}
			r.addGlobal(sc.nsKey, name, d)
			for s := sc; s.inline && s.parent != nil; s = s.parent {
				r.addGlobal(s.parent.nsKey, name, d)
			}
		}
	}
}

func (r *resolver) addGlobal(key, name string, d *code.Decl) {
	m := r.global[key]
	if m == nil {
		m = map[string][]*code.Decl{}
		r.global[key] = m
	}
	m[name] = append(m[name], d)
}

// findInclude resolves a quoted include. We try the including file's
// directory, then the root, include/ and src/, and last a unique path suffix.
func (r *resolver) findInclude(from *code.File, p string) *code.File {
	for _, c := range []string{path.Join(path.Dir(from.Path), p), path.Clean(p), path.Join("include", p), path.Join("src", p)} {
		if g := r.byPath[c]; g != nil && g != from {
			return g
		}
	}
	var found *code.File
	for _, fi := range r.infos {
		if strings.HasSuffix(fi.f.Path, "/"+path.Clean(p)) {
			if found != nil {
				return nil
			}
			found = fi.f
		}
	}
	return found
}

// included returns the files f includes, transitively.
func (r *resolver) included(f *code.File) map[*code.File]bool {
	if c, ok := r.closure[f]; ok {
		return c
	}
	c := map[*code.File]bool{}
	stack := []*code.File{f}
	for len(stack) > 0 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, h := range r.includes[g] {
			if !c[h] && h != f {
				c[h] = true
				stack = append(stack, h)
			}
		}
	}
	r.closure[f] = c
	return c
}

func (r *resolver) resolveRef(ref *code.Ref, ri *refInfo) *code.Decl {
	switch ri.kind {
	case refPlain:
		if ri.sc.kind == scopeMacro {
			if ds := ri.sc.names[ref.Name]; len(ds) > 0 {
				return ds[0]
			}
		}
		if m := r.visibleMacro(ref); m != nil {
			return m
		}
		if d := r.pick(ref, ri, r.lookup(ri.sc, ref.Name, ref.Span.Start)); d != nil {
			return d
		}
		if ms := r.macros[ref.Name]; len(ms) == 1 {
			return ms[0]
		}
		return nil
	case refGlobal:
		return r.pick(ref, ri, r.global[""][ref.Name])
	case refScoped:
		if ri.prev == nil || ri.prev.Target == nil {
			return nil
		}
		return r.pick(ref, ri, r.qualified(ri.prev.Target, ref.Name))
	case refThis:
		if cls := r.enclosingClass(ri.sc); cls != nil {
			return r.pick(ref, ri, r.memberLookup(cls, ref.Name, 0))
		}
		return nil
	case refMember, refResult, refElem:
		if ri.prev == nil || ri.prev.Target == nil {
			return r.uniqueMember(ref, ri)
		}
		obj := ri.prev.Target
		var cls *code.Decl
		known := true
		switch ri.kind {
		case refMember:
			if !ri.arrow && isSmartPointer(obj.Type) {
				return nil
			}
			cls, known = r.typeOfDecl(obj, 0)
		case refResult:
			switch {
			case obj.IsType():
				cls = r.canon(obj, 0)
			case obj.Callable():
				cls, known = r.typeFromString(obj.Type, r.funcScope(obj), obj.NameSpan.Start, 0)
			}
		case refElem:
			if di := r.decls[obj]; di != nil {
				cls, known = r.typeFromString(elemType(obj.Type), di.sc, obj.NameSpan.Start, 0)
			}
		}
		if cls != nil {
			return r.pick(ref, ri, r.memberLookup(cls, ref.Name, 0))
		}
		if !known {
			return r.uniqueMember(ref, ri)
		}
		return nil
	case refUnknown:
		return r.uniqueMember(ref, ri)
	}
	return nil
}

// visibleMacro returns the macro named like ref if it's defined earlier in
// the same file or in a file that file includes.
func (r *resolver) visibleMacro(ref *code.Ref) *code.Decl {
	ms := r.macros[ref.Name]
	if len(ms) == 0 {
		return nil
	}
	inc := r.included(ref.File)
	for _, m := range ms {
		if m.File == ref.File && m.NameSpan.Start < ref.Span.Start {
			return m
		}
	}
	for _, m := range ms {
		if inc[m.File] {
			return m
		}
	}
	return nil
}

// uniqueMember resolves a member of an object we can't type, if all methods
// or fields with that name belong to the same class.
func (r *resolver) uniqueMember(ref *code.Ref, ri *refInfo) *code.Decl {
	cands := r.members[ref.Name]
	if len(cands) == 0 {
		return nil
	}
	owner := r.memberOwner(cands[0])
	if owner == nil {
		return nil
	}
	for _, d := range cands[1:] {
		if r.memberOwner(d) != owner {
			return nil
		}
	}
	return r.pick(ref, ri, cands)
}

func (r *resolver) memberOwner(d *code.Decl) *code.Decl {
	if d.Receiver != "" {
		return r.receiverClass(d)
	}
	if d.Parent != nil && d.Parent.IsType() {
		return r.canon(d.Parent, 0)
	}
	return nil
}

// lookup finds the declarations visible as name from scope sc at offset
// pos.
func (r *resolver) lookup(sc *scope, name string, pos int) []*code.Decl {
	for s := sc; s != nil; s = s.parent {
		if s.hidden[name] {
			return nil
		}
		var out []*code.Decl
		for _, d := range s.names[name] {
			switch s.kind {
			case scopeFunc, scopeBlock, scopeMacro:
				if d.NameSpan.Start >= pos {
					continue
				}
			case scopeClass:
				if d.Kind != code.KindTypeParameter {
					continue
				}
			}
			out = append(out, d)
		}
		switch s.kind {
		case scopeFunc:
			if len(out) > 0 {
				return out
			}
			if ds := r.receiverLookup(s.decl, name); len(ds) > 0 {
				return ds
			}
		case scopeClass:
			if len(out) > 0 {
				return out
			}
			if s.decl != nil {
				if ms := r.memberLookup(s.decl, name, 0); len(ms) > 0 {
					return ms
				}
			}
		case scopeFile, scopeNamespace:
			out = append(out, r.global[s.nsKey][name]...)
			if len(out) > 0 {
				return out
			}
			if u := s.usingDecls[name]; u != nil && u.Target != nil {
				return []*code.Decl{u.Target}
			}
			for _, ns := range s.usingNS {
				if ds := r.usingNamespace(s.nsKey, ns, name); len(ds) > 0 {
					return ds
				}
			}
		default:
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func (r *resolver) receiverLookup(fn *code.Decl, name string) []*code.Decl {
	if fn == nil || fn.Receiver == "" {
		return nil
	}
	cls := r.receiverClass(fn)
	if cls == nil {
		return nil
	}
	if ms := r.memberLookup(cls, name, 0); len(ms) > 0 {
		return ms
	}
	return r.enclosingOf(cls, name)
}

// usingNamespace looks name up in the namespace written as ns. We resolve ns
// relative to the namespace from and the namespaces above it.
func (r *resolver) usingNamespace(from, ns, name string) []*code.Decl {
	ns = strings.TrimPrefix(ns, "::")
	for key := from; ; {
		full := ns
		if key != "" {
			full = key + "::" + ns
		}
		if ds := r.global[full][name]; len(ds) > 0 {
			return ds
		}
		if key == "" {
			return nil
		}
		if i := strings.LastIndex(key, "::"); i >= 0 {
			key = key[:i]
		} else {
			key = ""
		}
	}
}

// enclosingOf looks name up in the classes and namespaces around cls. We need
// this for out-of-line member definitions.
func (r *resolver) enclosingOf(cls *code.Decl, name string) []*code.Decl {
	di := r.decls[cls]
	if di == nil {
		return nil
	}
	for s := di.sc; s != nil && s.kind != scopeFile; s = s.parent {
		switch s.kind {
		case scopeClass:
			if s.decl != nil {
				if ms := r.memberLookup(s.decl, name, 0); len(ms) > 0 {
					return ms
				}
			}
		case scopeNamespace:
			if ds := append(slices.Clone(s.names[name]), r.global[s.nsKey][name]...); len(ds) > 0 {
				return ds
			}
		}
	}
	return nil
}

func (r *resolver) enclosingClass(sc *scope) *code.Decl {
	for s := sc; s != nil; s = s.parent {
		switch s.kind {
		case scopeClass:
			return s.decl
		case scopeFunc:
			fn := s.decl
			if fn == nil || fn.Kind == code.KindVariable {
				continue
			}
			if fn.Receiver != "" {
				return r.receiverClass(fn)
			}
			if fn.Parent != nil && fn.Parent.IsType() {
				return fn.Parent
			}
		}
	}
	return nil
}

// receiverClass resolves the Receiver of an out-of-line definition.
func (r *resolver) receiverClass(d *code.Decl) *code.Decl {
	if c, ok := r.recv[d]; ok {
		return c
	}
	r.recv[d] = nil
	di := r.decls[d]
	if di == nil {
		return nil
	}
	parts := strings.Split(d.Receiver, ".")
	cur := r.pickType(r.lookup(di.sc, parts[0], d.NameSpan.Start))
	for _, part := range parts[1:] {
		if cur == nil {
			break
		}
		cur = r.pickType(r.qualified(cur, part))
	}
	if cur != nil && cur.IsType() {
		cur = r.canon(cur, 0)
	} else {
		cur = nil
	}
	r.recv[d] = cur
	return cur
}

// qualified returns the members named name of a namespace or type.
func (r *resolver) qualified(q *code.Decl, name string) []*code.Decl {
	switch {
	case q.Kind == code.KindNamespace:
		if di := r.decls[q]; di != nil {
			return r.global[di.nsKey][name]
		}
	case q.IsType():
		if cls := r.canon(q, 0); cls != nil {
			return r.memberLookup(cls, name, 0)
		}
	}
	return nil
}

// memberLookup returns the members of class cls named name, and searches its
// bases if cls declares none.
func (r *resolver) memberLookup(cls *code.Decl, name string, depth int) []*code.Decl {
	cls = r.canon(cls, depth)
	if cls == nil || depth > maxDepth {
		return nil
	}
	var out []*code.Decl
	member := func(c *code.Decl) bool {
		return c.Name == name && c.Kind != code.KindTypeParameter && c.Kind != code.KindConstructor &&
			c.Kind != code.KindParameter && !strings.HasPrefix(c.Name, "~")
	}
	for _, c := range cls.Children {
		if member(c) {
			out = append(out, c)
		}
		if c.Kind == code.KindEnum && r.decls[c] != nil && !r.decls[c].scoped {
			for _, e := range c.Children {
				if e.Name == name {
					out = append(out, e)
				}
			}
		}
	}
	for _, c := range r.outOfLine[cls] {
		if member(c) {
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out
	}
	di := r.decls[cls]
	if di == nil {
		return nil
	}
	for _, b := range cls.Bases {
		bc, _ := r.typeFromString(strings.ReplaceAll(b, ".", "::"), di.sc, cls.NameSpan.Start, depth+1)
		if bc != nil && bc != cls {
			if ms := r.memberLookup(bc, name, depth+1); len(ms) > 0 {
				return ms
			}
		}
	}
	return nil
}

// canon follows type aliases and forward declarations to the defining
// declaration.
func (r *resolver) canon(d *code.Decl, depth int) *code.Decl {
	if d == nil || depth > maxDepth {
		return nil
	}
	di := r.decls[d]
	if di == nil {
		return d
	}
	if d.Kind == code.KindTypeAlias {
		if t, _ := r.typeFromString(d.Type, di.sc, d.NameSpan.Start, depth+1); t != nil {
			return t
		}
		return d
	}
	if d.IsType() && d.Body.Empty() {
		for _, c := range r.global[di.sc.nsKey][d.Name] {
			if c.IsType() && c.Kind != code.KindTypeAlias && !c.Body.Empty() {
				return c
			}
		}
	}
	return d
}

// typeOfDecl returns the class of the type d is declared with, or d if it's a
// type. known reports whether we understood the declared type. It's false if
// the type is missing or we couldn't deduce it.
func (r *resolver) typeOfDecl(d *code.Decl, depth int) (cls *code.Decl, known bool) {
	if d == nil || depth > maxDepth {
		return nil, false
	}
	if d.IsType() {
		return r.canon(d, depth), true
	}
	di := r.decls[d]
	if di == nil || d.Callable() || d.Kind == code.KindNamespace || d.Kind == code.KindMacro {
		return nil, false
	}
	t := d.Type
	if tt := trimType(t); tt == "" || tt == "auto" {
		if di.init != nil && di.init.Target != nil {
			x := di.init.Target
			switch {
			case x.IsType():
				return r.canon(x, depth), true
			case x.Callable():
				return r.typeFromString(x.Type, r.funcScope(x), x.NameSpan.Start, depth+1)
			}
		}
		if di.rangeOf != nil && di.rangeOf.Target != nil {
			src := di.rangeOf.Target
			if si := r.decls[src]; si != nil {
				return r.typeFromString(elemType(src.Type), si.sc, src.NameSpan.Start, depth+1)
			}
		}
		return nil, false
	}
	return r.typeFromString(t, di.sc, d.NameSpan.Start, depth+1)
}

// funcScope is the scope a function's header types are resolved in.
func (r *resolver) funcScope(fn *code.Decl) *scope {
	if di := r.decls[fn]; di != nil {
		if di.body != nil {
			return di.body
		}
		return di.sc
	}
	return nil
}

var smartPointers = map[string]bool{
	"unique_ptr": true, "shared_ptr": true, "weak_ptr": true, "optional": true,
	"reference_wrapper": true, "auto_ptr": true, "atomic": true,
}

var typeWords = map[string]bool{
	"const": true, "volatile": true, "struct": true, "class": true, "union": true,
	"enum": true, "typename": true, "static": true, "mutable": true, "constexpr": true,
}

// typeFromString resolves a written type to the class it names, looking
// through pointers, references, cv qualifiers and smart pointers. known is
// false if the text is empty or names no type, like auto or a function type.
func (r *resolver) typeFromString(t string, sc *scope, pos, depth int) (cls *code.Decl, known bool) {
	if sc == nil || depth > maxDepth {
		return nil, false
	}
	t = trimType(t)
	if t == "" || t == "auto" || strings.Contains(t, "(") {
		return nil, false
	}
	base, args, _ := strings.Cut(t, "<")
	comps := strings.Split(base, "::")
	if smartPointers[comps[len(comps)-1]] && args != "" {
		return r.typeFromString(firstArg(args), sc, pos, depth+1)
	}
	var cur *code.Decl
	for i, c := range comps {
		switch {
		case i == 0 && c == "":
			continue
		case i == 0 || i == 1 && comps[0] == "":
			if i == 1 {
				cur = r.pickType(r.global[""][c])
			} else {
				cur = r.pickType(r.lookup(sc, c, pos))
			}
		case cur != nil:
			cur = r.pickType(r.qualified(cur, c))
		}
		if cur == nil {
			return nil, true
		}
	}
	if cur == nil || !cur.IsType() {
		return nil, true
	}
	return r.canon(cur, depth+1), true
}

// isSmartPointer reports whether t is written as a smart pointer like
// std::unique_ptr<T>. Its `.` members are the library type's, and `->` reaches T.
func isSmartPointer(t string) bool {
	base, _, ok := strings.Cut(trimType(t), "<")
	if !ok {
		return false
	}
	comps := strings.Split(base, "::")
	return smartPointers[comps[len(comps)-1]]
}

// trimType strips cv qualifiers, elaborated keywords, pointers, references
// and array suffixes.
func trimType(t string) string {
	t = strings.TrimSpace(t)
	for {
		w, rest, ok := strings.Cut(t, " ")
		if !ok || !typeWords[w] {
			break
		}
		t = strings.TrimSpace(rest)
	}
	for {
		n := len(t)
		t = strings.TrimRight(t, "*& ")
		t = strings.TrimSuffix(t, "const")
		if i := strings.LastIndexByte(t, '['); i > 0 && strings.HasSuffix(t, "]") {
			t = t[:i]
		}
		if len(t) == n {
			return t
		}
	}
}

// firstArg returns the first template argument of the argument text after
// the opening `<`.
func firstArg(args string) string {
	depth := 0
	for i, c := range args {
		switch c {
		case '<', '(':
			depth++
		case ')':
			depth--
		case '>':
			if depth == 0 {
				return args[:i]
			}
			depth--
		case ',':
			if depth == 0 {
				return args[:i]
			}
		}
	}
	return args
}

// elemType returns the element type of an array or pointer type t, or the
// first template argument if t is a container. Else it returns "".
func elemType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimRight(t, "& ")
	switch {
	case strings.HasSuffix(t, "]"):
		if i := strings.LastIndexByte(t, '['); i > 0 {
			return t[:i]
		}
	case strings.HasSuffix(t, "*"):
		return t[:len(t)-1]
	}
	if _, args, ok := strings.Cut(t, "<"); ok {
		return firstArg(args)
	}
	return ""
}

// pickType keeps the types and namespaces of cands and picks one.
func (r *resolver) pickType(cands []*code.Decl) *code.Decl {
	var types []*code.Decl
	for _, d := range cands {
		if d.IsType() || d.Kind == code.KindNamespace || d.Kind == code.KindTypeParameter {
			types = append(types, d)
		}
	}
	return r.pick(nil, nil, types)
}

// pick chooses among the declarations a name can refer to. If there's exactly
// one definition we take it, else the first that matches the call arity, else
// the first. Before that we sort candidates from the referring file and the
// files it includes to the front.
func (r *resolver) pick(ref *code.Ref, ri *refInfo, cands []*code.Decl) *code.Decl {
	var uniq []*code.Decl
	seen := map[*code.Decl]bool{}
	for _, d := range cands {
		if d != nil && !seen[d] {
			seen[d] = true
			uniq = append(uniq, d)
		}
	}
	switch len(uniq) {
	case 0:
		return nil
	case 1:
		return uniq[0]
	}
	if ref != nil {
		inc := r.included(ref.File)
		rank := func(d *code.Decl) int {
			switch {
			case d.File == ref.File:
				return 0
			case inc[d.File]:
				return 1
			}
			return 2
		}
		slices.SortStableFunc(uniq, func(a, b *code.Decl) int {
			if c := cmp.Compare(rank(a), rank(b)); c != 0 {
				return c
			}
			if c := cmp.Compare(r.order[a.File], r.order[b.File]); c != 0 {
				return c
			}
			return cmp.Compare(a.NameSpan.Start, b.NameSpan.Start)
		})
	}
	var defs []*code.Decl
	for _, d := range uniq {
		if di := r.decls[d]; di != nil && di.def {
			defs = append(defs, d)
		}
	}
	if len(defs) == 1 {
		return defs[0]
	}
	pool := uniq
	if len(defs) > 0 {
		pool = defs
	}
	if ri != nil && ri.arity >= 0 {
		for _, d := range pool {
			if di := r.decls[d]; di != nil && d.Callable() && di.arity >= 0 &&
				(di.arity == ri.arity || di.variadic && ri.arity >= di.arity) {
				return d
			}
		}
	}
	return pool[0]
}
