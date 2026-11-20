package javascript

import "github.com/myoo-ai/cli/internal/code"

// typeInfo describes a parsed TypeScript type.
type typeInfo struct {
	ref     *code.Ref // last identifier of a plain type reference (Foo, ns.Foo<T>)
	nullish bool      // null or undefined, ignored in unions like `Foo | null`
}

var builtinTypes = code.StringSet(
	"string", "number", "boolean", "any", "unknown",
	"never", "object", "symbol", "bigint", "undefined", "intrinsic",
)

func (p *parser) typ() typeInfo {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.i++
		return typeInfo{}
	}
	t := p.tok()
	switch {
	case p.isOp(t, "<"), p.isOp(t, "(") && p.fnTypeAhead():
		p.fnType()
		return typeInfo{}
	case p.isOp(t, "new"):
		p.i++
		p.fnType()
		return typeInfo{}
	case p.isWord(t, "abstract") && p.isOp(p.peek(1), "new"):
		p.kw()
		p.i++
		p.fnType()
		return typeInfo{}
	}
	ti := p.unionType()
	if t := p.tok(); p.isOp(t, "extends") && !t.nl {
		p.i++
		p.unionType()
		if p.eat("?") {
			p.typ()
			p.eat(":")
			p.typ()
		}
		return typeInfo{}
	}
	return ti
}

func (p *parser) unionType() typeInfo {
	p.eat("|")
	var only typeInfo
	n := 0
	for {
		ti := p.intersectionType()
		if !ti.nullish {
			only = ti
			n++
		}
		if !p.eat("|") {
			break
		}
	}
	if n != 1 {
		return typeInfo{}
	}
	return only
}

func (p *parser) intersectionType() typeInfo {
	p.eat("&")
	ti := p.operatorType()
	for p.eat("&") {
		p.operatorType()
		ti = typeInfo{}
	}
	return ti
}

func (p *parser) operatorType() typeInfo {
	t := p.tok()
	n := p.peek(1)
	switch {
	case (p.isWord(t, "keyof") || p.isWord(t, "unique") || p.isWord(t, "readonly")) && p.startsType(n):
		p.kw()
		p.operatorType()
		return typeInfo{}
	case p.isWord(t, "infer") && isIdent(n):
		p.kw()
		p.sc.shadow(p.text(n))
		p.i++
		return typeInfo{}
	}
	ti := p.postfixType()
	return ti
}

func (p *parser) startsType(t token) bool {
	switch t.kind {
	case code.TokIdent:
		return t.sub == subNone
	case code.TokKeyword, code.TokNumber:
		return true
	case code.TokString:
		return t.sub == subNone || t.sub == subTmplFull || t.sub == subTmplHead
	case code.TokPunct:
		switch p.text(t) {
		case "(", "[", "{", "<", "-":
			return t.sub == subNone
		}
	}
	return false
}

func (p *parser) postfixType() typeInfo {
	ti := p.primaryType()
	for t := p.tok(); p.isOp(t, "[") && !t.nl; t = p.tok() {
		open := p.i
		p.i++
		if !p.is("]") {
			p.typ()
		}
		if !p.is("]") {
			p.recover(open)
		}
		p.eat("]")
		ti = typeInfo{}
	}
	return ti
}

func (p *parser) primaryType() typeInfo {
	t := p.tok()
	switch t.kind {
	case code.TokIdent:
		if t.sub != subNone {
			return typeInfo{}
		}
		name := p.text(t)
		if builtinTypes[name] && !p.isOp(p.peek(1), ".") {
			p.kw()
			return typeInfo{nullish: name == "undefined"}
		}
		info := p.namePath(true)
		if p.is("<") {
			p.typeArgs()
		}
		return typeInfo{ref: info.ref}
	case code.TokKeyword:
		switch p.text(t) {
		case "typeof":
			p.typeQuery()
			return typeInfo{}
		case "import":
			return p.importType()
		case "null":
			p.i++
			return typeInfo{nullish: true}
		case "void", "this", "true", "false":
			p.i++
		}
		return typeInfo{}
	case code.TokNumber:
		p.i++
	case code.TokString:
		switch t.sub {
		case subTmplHead, subTmplFull:
			p.template(true)
		case subNone:
			p.i++
		}
	case code.TokPunct:
		if t.sub != subNone {
			return typeInfo{}
		}
		switch p.text(t) {
		case "(":
			open := p.i
			p.i++
			ti := p.typ()
			if !p.is(")") {
				p.recover(open)
			}
			p.eat(")")
			return ti
		case "{":
			owner := p.typeOwn
			p.typeOwn = nil
			p.typeMembers(owner)
		case "[":
			p.tupleType()
		case "-":
			if p.peek(1).kind == code.TokNumber {
				p.i += 2
			}
		case "*", "?":
			p.i++
		}
	}
	return typeInfo{}
}

func (p *parser) typeQuery() {
	p.i++
	if p.is("import") {
		p.importType()
		return
	}
	n := p.tok()
	if !isIdent(n) && !p.isOp(n, "this") {
		return
	}
	p.i++
	info := exprInfo{this: p.isOp(n, "this"), path: true}
	if !info.this {
		info.ref = p.useRef(n)
	}
	p.namePathRest(info, false)
	if p.is("<") {
		p.typeArgs()
	}
}

// importType parses `import('spec').A.B<T>`.
func (p *parser) importType() typeInfo {
	p.i++
	if !p.is("(") {
		return typeInfo{}
	}
	open := p.i
	p.i++
	spec := ""
	if t := p.tok(); t.kind == code.TokString {
		spec = unquote(p.text(t))
	}
	p.recover(open)
	p.eat(")")
	info := p.namePathRest(exprInfo{require: spec}, true)
	if p.is("<") {
		p.typeArgs()
	}
	return typeInfo{ref: info.ref}
}

func (p *parser) tupleType() {
	open := p.i
	p.i++
	for !p.eof() && !p.is("]") {
		st := p.i
		p.eat("...")
		if n := p.peek(1); isName(p.tok()) && (p.isOp(n, ":") || p.isOp(n, "?") && p.isOp(p.peek(2), ":")) {
			p.i++
			p.eat("?")
			p.i++
		}
		p.typ()
		p.eat("?")
		if !p.listSeparator(open, st, "]") {
			break
		}
	}
	p.eat("]")
}

func (p *parser) typeArgs() {
	p.i++
	for !p.eof() && !p.isGT() {
		st := p.i
		p.typ()
		if !p.eat(",") {
			if p.i == st {
				p.i++
			}
			break
		}
	}
	p.eatGT()
}

// fnTypeAhead reports whether the '(' at the current token starts a
// function type instead of a parenthesized type.
func (p *parser) fnTypeAhead() bool {
	n := p.peek(1)
	if p.isOp(n, ")") || p.isOp(n, "...") {
		return true
	}
	if isIdent(n) || p.isOp(n, "this") {
		if m := p.peek(2); p.isOp(m, ":") || p.isOp(m, ",") || p.isOp(m, "?") || p.isOp(m, "=") {
			return true
		}
	}
	j := int(p.match[p.i])
	return j > 0 && p.isOp(p.at(j+1), "=>")
}

// fnType parses `<T>(params) => R` without declaring anything.
func (p *parser) fnType() {
	saved := p.sc
	p.push(scopeBlock)
	if p.is("<") {
		p.typeParameterList(false)
	}
	if p.is("(") {
		p.params(nil, true)
	}
	if p.eat("=>") {
		p.returnType(false)
	}
	p.sc = saved
}

func (p *parser) typeParameterList(declare bool) {
	open := p.i
	p.i++
	for !p.eof() && !p.isGT() {
		st := p.i
		for (p.word("in") || p.word("out") || p.is("const") || p.is("in")) && isIdent(p.peek(1)) {
			p.kw()
		}
		if t := p.tok(); isIdent(t) {
			p.i++
			var d *code.Decl
			if declare {
				d = p.declTok(code.KindTypeParameter, t, t.start)
				p.sc.bind(d.Name, d)
			} else {
				p.sc.shadow(p.text(t))
			}
			if p.eat("extends") {
				p.typ()
			}
			if p.eat("=") {
				p.typ()
			}
			if d != nil {
				d.Span.End = p.prevEnd()
			}
		}
		if !p.eat(",") {
			if p.i == st && declare {
				p.recover(open)
			} else if p.i == st {
				p.i++
			}
			break
		}
	}
	p.eatGT()
}

// returnType parses a return type, including type predicates `x is T` and
// `asserts x`. Set value for functions whose parameters we declare, so the
// predicate subject can be a reference to the parameter.
func (p *parser) returnType(value bool) typeInfo {
	t := p.tok()
	n := p.peek(1)
	if p.isWord(t, "asserts") && (isIdent(n) || p.isOp(n, "this")) && !n.nl {
		p.kw()
		if value && isIdent(n) {
			p.useRef(n)
		}
		p.i++
		if p.word("is") {
			p.kw()
			p.typ()
		}
		return typeInfo{}
	}
	if (isIdent(t) || p.isOp(t, "this")) && p.isWord(n, "is") && !n.nl {
		if value && isIdent(t) {
			p.useRef(t)
		}
		p.i++
		p.kw()
		p.typ()
		return typeInfo{}
	}
	return p.typ()
}

// typeMembers parses the members of an interface or object type literal.
// If owner is set, they become its members.
func (p *parser) typeMembers(owner *code.Decl) code.Span {
	start := p.tok().start
	p.i++
	saved := p.sc
	p.push(scopeBlock)
	for !p.eof() && !p.is("}") {
		st := p.i
		p.typeMember(owner)
		if !p.eat(";") && !p.eat(",") && p.i == st {
			p.i++
		}
	}
	p.eat("}")
	p.sc = saved
	return code.Span{Start: start, End: p.prevEnd()}
}

func (p *parser) typeMember(owner *code.Decl) {
	start := p.tok().start
	for {
		t := p.tok()
		if (p.isOp(t, "+") || p.isOp(t, "-")) && p.isWord(p.peek(1), "readonly") {
			p.i++
			continue
		}
		if n := p.peek(1); p.isWord(t, "readonly") && (isPropKey(n) || p.isOp(n, "[")) {
			p.kw()
			continue
		}
		break
	}
	t := p.tok()
	n := p.peek(1)
	switch {
	case p.isOp(t, "(") || p.isOp(t, "<"):
		p.fnSig(nil)
		return
	case p.isOp(t, "new") && (p.isOp(n, "(") || p.isOp(n, "<")):
		p.i++
		p.fnSig(nil)
		return
	case p.isOp(t, "[") && isIdent(n) && p.isOp(p.peek(2), "in"):
		p.mappedTypeMember(n)
		return
	case p.isOp(t, "[") && isIdent(n) && p.isOp(p.peek(2), ":"):
		p.indexSignature()
		return
	}
	accessor := false
	if (p.isWord(t, "get") || p.isWord(t, "set")) && (isPropKey(n) || p.isOp(n, "[")) {
		p.kw()
		accessor = true
	}
	var name string
	var nameSpan code.Span
	switch t := p.tok(); {
	case p.isOp(t, "["):
		p.index()
	case isPropKey(t):
		name, nameSpan = p.propName()
	default:
		return
	}
	p.eat("?")
	var d *code.Decl
	if p.is("(") || p.is("<") {
		if owner != nil && name != "" {
			kind := code.KindMethod
			if accessor {
				kind = code.KindProperty
			}
			d = p.memberDecl(owner, kind, name, nameSpan, start)
		}
		p.fnSig(d)
		if d != nil {
			d.Span.End = p.prevEnd()
		}
		return
	}
	if owner != nil && name != "" {
		d = p.memberDecl(owner, code.KindProperty, name, nameSpan, start)
	}
	if p.eat(":") {
		s := p.tok().start
		ti := p.typ()
		if d != nil {
			d.Type = p.textFrom(s)
			if ti.ref != nil {
				p.pr.typeRef[d] = ti.ref
			}
		}
	}
	if d != nil {
		d.Span.End = p.prevEnd()
	}
}

func (p *parser) mappedTypeMember(name token) {
	open := p.i
	p.i += 2
	p.sc.shadow(p.text(name))
	p.i++
	p.typ()
	if p.word("as") {
		p.kw()
		p.typ()
	}
	if !p.is("]") {
		p.recover(open)
	}
	p.eat("]")
	if p.is("+") || p.is("-") {
		p.i++
	}
	p.eat("?")
	if p.eat(":") {
		p.typ()
	}
}

func (p *parser) memberDecl(owner *code.Decl, kind code.DeclKind, name string, nameSpan code.Span, start int) *code.Decl {
	saved := p.parent
	p.parent = owner
	d := p.newDecl(kind, name, nameSpan, start)
	p.parent = saved
	return d
}

// fnSig parses a call, construct or method signature, from the type
// parameters to the return type. It declares only the type parameters of d.
func (p *parser) fnSig(d *code.Decl) {
	saved := p.save()
	if d != nil {
		p.parent = d
	}
	p.push(scopeFunction)
	if p.is("<") {
		p.typeParameterList(d != nil)
	}
	if p.is("(") {
		p.params(nil, true)
	}
	if p.eat(":") {
		s := p.tok().start
		p.returnType(false)
		if d != nil {
			d.Type = p.textFrom(s)
		}
	}
	p.restore(saved)
}
