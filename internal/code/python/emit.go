package python

import (
	"strings"
	"unicode"

	"github.com/myoo-ai/cli/internal/code"
)

func (p *parser) newRef(sc *scope, name string, span code.Span, n note) *code.Ref {
	r := p.f.NewRef(sc.refDecl, name, span)
	n.sc = sc
	p.info.notes[r] = &n
	return r
}

// use records the references in an expression evaluated in sc.
func (p *parser) use(n *node, sc *scope) {
	if n == nil {
		return
	}
	n.sc = sc
	switch n.kind {
	case nName:
		p.nameRef(n, sc)
	case nAttr:
		p.use(n.x, sc)
		p.attrRef(n, sc)
	case nCall:
		p.callee(n.x, sc)
		p.callArguments(n, sc, noteKwarg, code.Span{})
	case nLambda:
		p.emitLambda(n, sc, nil)
	case nComp:
		p.emitComp(n, sc)
	case nWalrus:
		p.use(n.x, sc)
		ts := sc
		for ts.kind == scComp && ts.parent != nil {
			ts = ts.parent
		}
		p.declareName(ts, n.elts[0], bindCtx{span: n.span, init: n.x})
	default:
		p.use(n.x, sc)
		for _, e := range n.elts {
			p.use(e, sc)
		}
	}
}

func (p *parser) callArguments(n *node, sc *scope, kind noteKind, span code.Span) {
	for _, a := range n.args {
		if a.kw != "" {
			p.newRef(sc, a.kw, a.kwSpan, note{kind: kind, qual: n.x})
		}
		if kind == notePatternKw {
			p.bindPattern(a.value, sc, span)
		} else {
			p.use(a.value, sc)
		}
	}
}

// nameRef records a name reference. If the first parameter of a method has
// its conventional name (self, cls), we don't count it as a reference.
func (p *parser) nameRef(n *node, sc *scope) *code.Ref {
	if n.name == "self" || n.name == "cls" {
		if b := lookupLocal(sc, n.name); b != nil && b.decl != nil && p.declNote(b.decl).selfOf != nil {
			return nil
		}
	}
	n.ref = p.newRef(sc, n.name, n.span, note{kind: noteName})
	return n.ref
}

func (p *parser) attrRef(n *node, sc *scope) *code.Ref {
	n.ref = p.newRef(sc, n.name, n.nameSpan, note{kind: noteAttr, qual: n.x})
	return n.ref
}

func (p *parser) callee(x *node, sc *scope) {
	if x == nil {
		return
	}
	x.sc = sc
	var r *code.Ref
	switch x.kind {
	case nName:
		r = p.nameRef(x, sc)
	case nAttr:
		p.use(x.x, sc)
		r = p.attrRef(x, sc)
	default:
		p.use(x, sc)
	}
	if r != nil {
		r.Call = true
	}
}

// emitLambda declares the parameters of a lambda. An anonymous lambda has
// no declaration, so its parameters become locals of the enclosing
// declaration, scoped to the lambda body for resolution.
func (p *parser) emitLambda(n *node, sc *scope, named *code.Decl) {
	owner, refDecl := sc.owner, sc.refDecl
	if named != nil {
		owner, refDecl = named, named
	}
	ls := newScope(scLambda, owner, refDecl, sc)
	for _, prm := range n.lambda.params {
		p.declareParam(owner, prm, ls, sc, sc)
	}
	p.use(n.lambda.body, ls)
}

// namedLambda turns `f = lambda ...` into a function named f, as long as f
// isn't bound yet.
func (p *parser) namedLambda(target, value *node, sc *scope, span code.Span) bool {
	if target == nil || value == nil || target.kind != nName || value.kind != nLambda {
		return false
	}
	ts := p.targetScope(sc, target.name)
	if ts.names[target.name] != nil {
		return false
	}
	kind := code.KindFunction
	if ts.kind == scClass {
		kind = code.KindMethod
	}
	d := p.f.NewDecl(ts.owner, kind, target.name, target.span, span)
	if body := value.lambda.body; body != nil {
		d.Body = body.span
	}
	ts.names[target.name] = &binding{decl: d}
	value.sc = sc
	p.emitLambda(value, sc, d)
	return true
}

// emitComp declares comprehension variables in their own scope. The first
// iterable has to be evaluated in the enclosing scope, like Python does.
func (p *parser) emitComp(n *node, sc *scope) {
	cs := newScope(scComp, sc.owner, sc.refDecl, sc)
	for i, cl := range n.comp.clauses {
		if i == 0 {
			p.use(cl.iter, sc)
		} else {
			p.use(cl.iter, cs)
		}
		p.bindTarget(cl.target, cs, bindCtx{span: cl.span, comp: true})
		for _, f := range cl.ifs {
			p.use(f, cs)
		}
	}
	for _, e := range n.comp.elts {
		p.use(e, cs)
	}
}

type bindCtx struct {
	span      code.Span // the declaring statement
	ann, init *node
	comp      bool // a comprehension target binds in the comprehension scope
}

func (p *parser) bindTarget(n *node, sc *scope, ctx bindCtx) {
	if n == nil {
		return
	}
	n.sc = sc
	switch n.kind {
	case nName:
		p.declareName(sc, n, ctx)
	case nTuple, nList:
		inner := bindCtx{span: ctx.span, comp: ctx.comp}
		for _, e := range n.elts {
			p.bindTarget(e, sc, inner)
		}
	case nStarred:
		p.bindTarget(n.x, sc, bindCtx{span: ctx.span, comp: ctx.comp})
	case nAttr:
		if cls := p.selfClass(sc, n.x); cls != nil && !ctx.comp && memberNamed(cls, n.name) == nil {
			d := p.f.NewDecl(cls, code.KindField, n.name, n.nameSpan, ctx.span)
			p.describe(d, sc, ctx)
			return
		}
		p.use(n.x, sc)
		p.attrRef(n, sc)
	default:
		p.use(n, sc)
	}
}

// selfClass returns the class whose instance (or class object) x names
// inside a method, which is the case if x is the method's first parameter.
func (p *parser) selfClass(sc *scope, x *node) *code.Decl {
	if x == nil || x.kind != nName {
		return nil
	}
	x.sc = sc
	if b := lookupLocal(sc, x.name); b != nil && b.decl != nil {
		return p.declNote(b.decl).selfOf
	}
	return nil
}

// memberNamed returns the earliest declared member of cls named name.
func memberNamed(cls *code.Decl, name string) *code.Decl {
	var best *code.Decl
	for _, c := range cls.Children {
		if c.Name == name && isMember(c) && (best == nil || c.NameSpan.Start < best.NameSpan.Start) {
			best = c
		}
	}
	return best
}

func isMember(d *code.Decl) bool {
	switch d.Kind {
	case code.KindVariable, code.KindParameter, code.KindTypeParameter:
		return false
	}
	return true
}

// declareName binds a name. The first binding in a scope declares it, and
// later ones become references.
func (p *parser) declareName(sc *scope, n *node, ctx bindCtx) {
	ts := sc
	if !ctx.comp {
		ts = p.targetScope(sc, n.name)
	}
	if ts.names[n.name] == nil && ts.kind == scClass {
		if field := memberNamed(ts.owner, n.name); field != nil {
			ts.names[n.name] = &binding{decl: field}
		}
	}
	if ts.names[n.name] != nil {
		p.nameRef(n, sc)
		return
	}
	d := p.f.NewDecl(ts.owner, p.varKind(ts, n.name), n.name, n.span, ctx.span)
	if ts.kind == scComp || ts.kind == scLambda {
		d.Local = true
	}
	ts.names[n.name] = &binding{decl: d}
	p.describe(d, sc, ctx)
}

// describe records the annotation and initializer of a variable or field,
// and derives Decl.Type from them.
func (p *parser) describe(d *code.Decl, sc *scope, ctx bindCtx) {
	dn := p.declNote(d)
	dn.ann, dn.init = ctx.ann, ctx.init
	switch {
	case ctx.ann != nil:
		d.Type = p.text(ctx.ann.span)
	case ctx.init != nil && ctx.init.kind == nCall:
		if name := dottedName(ctx.init.x); name != "" && unicode.IsUpper(rune(name[strings.LastIndexByte(name, '.')+1])) {
			d.Type = name
		}
	case ctx.init != nil && ctx.init.kind == nName:
		if b := lookupLocal(sc, ctx.init.name); b != nil && b.decl != nil && b.decl.Kind == code.KindParameter {
			d.Type = b.decl.Type
		}
	}
}

func (p *parser) varKind(ts *scope, name string) code.DeclKind {
	switch ts.kind {
	case scClass:
		if ts.owner.Kind == code.KindEnum && !strings.HasPrefix(name, "_") {
			return code.KindEnumMember
		}
		return code.KindField
	case scModule, scFunc:
		if isConstName(name) {
			return code.KindConstant
		}
	}
	return code.KindVariable
}

// isConstName reports whether name follows the UPPER_CASE convention for
// constants.
func isConstName(name string) bool {
	upper := false
	for _, c := range name {
		switch {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c == '_' || (c >= '0' && c <= '9'):
		default:
			return false
		}
	}
	return upper
}

// targetScope returns the scope that a name assigned in sc binds in. We
// have to honor global and nonlocal declarations here.
func (p *parser) targetScope(sc *scope, name string) *scope {
	switch {
	case sc.global[name]:
		return sc.root()
	case sc.nonlocal[name]:
		var nearest *scope
		for s := sc.parent; s != nil; s = s.parent {
			if s.kind != scFunc && s.kind != scLambda {
				continue
			}
			if s.names[name] != nil {
				return s
			}
			if nearest == nil {
				nearest = s
			}
		}
		if nearest != nil {
			return nearest
		}
		return sc.root()
	}
	return sc
}

// lookupLocal finds the binding of name that's visible from sc in this
// file. A class body can't be seen from the functions, lambdas and
// comprehensions nested in it.
func lookupLocal(sc *scope, name string) *binding {
	b, _ := lookup(sc, name)
	return b
}

// lookup returns the binding of name and the scope where the search ended.
func lookup(sc *scope, name string) (*binding, *scope) {
	skipClass := false
	var last *scope
	for s := sc; s != nil; s = s.parent {
		last = s
		if s.kind == scClass && skipClass {
			continue
		}
		if s.global[name] {
			root := s.root()
			return root.names[name], root
		}
		if !s.nonlocal[name] {
			if b := s.names[name]; b != nil {
				return b, s
			}
		}
		if s.kind == scFunc || s.kind == scLambda || s.kind == scComp {
			skipClass = true
		}
	}
	return nil, last
}

// bindPattern binds the capture names of a match pattern and records the
// references in its value and class patterns.
func (p *parser) bindPattern(n *node, sc *scope, span code.Span) {
	if n == nil {
		return
	}
	n.sc = sc
	switch n.kind {
	case nName:
		if n.name != "_" {
			p.declareName(sc, n, bindCtx{span: span})
		}
	case nCall:
		p.use(n.x, sc)
		p.callArguments(n, sc, notePatternKw, span)
	case nTuple, nList:
		for _, e := range n.elts {
			p.bindPattern(e, sc, span)
		}
	case nDict:
		for i := 0; i+1 < len(n.elts); i += 2 {
			p.use(n.elts[i], sc)
			p.bindPattern(n.elts[i+1], sc, span)
		}
	case nStarred:
		p.bindPattern(n.x, sc, span)
	case nOp:
		if n.op == "as" || n.op == "|" {
			for _, e := range n.elts {
				p.bindPattern(e, sc, span)
			}
			return
		}
		p.use(n, sc)
	default:
		p.use(n, sc)
	}
}
