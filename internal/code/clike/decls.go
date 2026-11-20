package clike

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

var javaModifiers = code.StringSet("public", "protected", "private", "static", "abstract", "final", "native",
	"synchronized", "transient", "volatile", "strictfp", "default")
var csModifiers = code.StringSet("public", "private", "protected", "internal", "static", "abstract", "virtual",
	"override", "sealed", "readonly", "const", "extern", "unsafe", "volatile", "new", "fixed", "ref", "event")
var csContextualModifiers = code.StringSet("partial", "async", "required", "file", "scoped")
var accessorWords = code.StringSet("get", "set", "init", "add", "remove")

type mods struct {
	cnst, event bool
}

// modifiers also consumes the annotations and attributes before a
// declaration.
func (p *parser) modifiers() mods {
	var m mods
	for p.p < len(p.toks) {
		t := p.cur()
		switch {
		case p.isAnnotation(p.p) && p.peek(1).text != "interface":
			p.annotation()
		case p.cs && t.text == "[" && t.kind == code.TokPunct:
			p.attributes()
		case t.kind == code.TokKeyword && !p.cs && javaModifiers[t.text]:
			if n := p.peek(1).text; t.text == "default" && (n == ":" || n == "->") {
				return m
			}
			p.p++
		case t.kind == code.TokKeyword && p.cs && csModifiers[t.text]:
			m.cnst = m.cnst || t.text == "const"
			m.event = m.event || t.text == "event"
			p.p++
		case t.kind == code.TokIdent && p.cs && csContextualModifiers[t.text] && p.modifierFollows(p.p+1):
			p.keyword(p.p)
			p.p++
		case t.kind == code.TokIdent && !p.cs && t.text == "sealed" && p.modifierFollows(p.p+1):
			p.keyword(p.p)
			p.p++
		case !p.cs && t.text == "non" && p.peek(1).text == "-" && p.isWord(p.p+2, "sealed"):
			p.keyword(p.p)
			p.keyword(p.p + 2)
			p.p += 3
		default:
			return m
		}
	}
	return m
}

func (p *parser) modifierFollows(i int) bool {
	t := p.at(i)
	return t.kind == code.TokIdent || t.kind == code.TokKeyword
}

func (p *parser) typeKeywordAt(i int) bool {
	t := p.at(i)
	switch t.text {
	case "class", "interface", "enum":
		return t.kind == code.TokKeyword
	case "struct":
		return p.cs && t.kind == code.TokKeyword
	case "delegate":
		n := p.at(i + 1).text
		return p.cs && t.kind == code.TokKeyword && n != "(" && n != "{"
	case "@":
		return !p.cs && p.at(i+1).text == "interface"
	case "record":
		if t.kind != code.TokIdent {
			return false
		}
		if p.cs {
			n := p.at(i + 1).text
			return p.isIdent(i+1) || n == "class" || n == "struct"
		}
		n := p.at(i + 2).text
		return p.isIdent(i+1) && (n == "(" || n == "<")
	}
	return false
}

// typeDecl parses a class, interface, enum, record, struct, annotation type
// or delegate. The cursor has to be on the keyword.
func (p *parser) typeDecl(parent *code.Decl, startOff int) *code.Decl {
	kind := code.KindClass
	isRecord := false
	switch p.cur().text {
	case "interface":
		kind = code.KindInterface
	case "struct":
		kind = code.KindStruct
	case "enum":
		kind = code.KindEnum
	case "@":
		p.p++
		kind = code.KindInterface
	case "record":
		isRecord = true
		p.keyword(p.p)
		if p.cs && (p.peek(1).text == "struct" || p.peek(1).text == "class") {
			if p.peek(1).text == "struct" {
				kind = code.KindStruct
			}
			p.p++
		}
	case "delegate":
		return p.delegateDecl(parent, startOff)
	}
	p.p++
	if !p.isIdent(p.p) {
		return nil
	}
	d := p.newDecl(parent, kind, p.p, startOff)
	p.p++
	s := p.enter(d, true)
	p.header = d
	if p.is("<") {
		p.typeParams(d)
	}
	if p.is("(") {
		p.header = nil
		p.components(d, isRecord)
		p.header = d
	}
	bases := d
	if kind == code.KindEnum && p.cs {
		bases = nil // a C# enum's base list names its underlying integral type
	}
loop:
	for {
		switch {
		case !p.cs && (p.is("extends") || p.is("implements")):
			p.p++
			p.baseList(bases)
		case !p.cs && p.isWord(p.p, "permits"):
			p.keyword(p.p)
			p.p++
			p.baseList(nil)
		case p.cs && p.is(":"):
			p.p++
			p.baseList(bases)
		case p.cs && p.isWord(p.p, "where") && p.isIdent(p.p+1) && p.peek(2).text == ":":
			p.constraints()
		default:
			break loop
		}
	}
	p.header = nil
	switch {
	case p.is("{"):
		if kind == code.KindEnum {
			d.Body = p.enumBody(d)
		} else {
			d.Body = p.classBody(d, d.Name, nil, isRecord)
		}
	default:
		p.accept(";")
	}
	d.Span.End = p.prevEnd()
	p.leave(nil, s)
	return d
}

// baseList parses comma-separated base types. If d isn't nil, they're
// recorded in d.Bases.
func (p *parser) baseList(d *code.Decl) {
	for {
		j, ok := p.scanType(p.p)
		if !ok {
			return
		}
		text, _ := p.emitType(p.p, j)
		p.p = j
		if d != nil {
			d.Bases = append(d.Bases, text)
		}
		if p.cs && p.is("(") {
			// arguments of a primary constructor base call
			h := p.header
			p.header = nil
			p.exec++
			p.group()
			p.exec--
			p.header = h
		}
		if !p.accept(",") {
			return
		}
	}
}

// components parses the fields of a Java record header, the properties of a
// C# positional record or the parameters of a C# primary constructor.
func (p *parser) components(d *code.Decl, isRecord bool) {
	open := p.p
	kind := code.KindParameter
	switch {
	case isRecord && p.cs:
		kind = code.KindProperty
	case isRecord:
		kind = code.KindField
	}
	n := p.seq(open, func() {
		start := p.cur().s
		p.parameterModifiers(false)
		j, ok := p.scanType(p.p)
		if !ok {
			p.expr(stops{comma: true})
			return
		}
		typ, _ := p.emitType(p.p, j)
		p.p = j
		if !p.cs && p.accept("...") {
			typ += "..."
		}
		if !p.isIdent(p.p) {
			return
		}
		c := p.newDecl(d, kind, p.p, start)
		c.Type = typ
		p.p++
		if p.accept("=") {
			p.expr(stops{comma: true})
		}
		c.Span.End = p.prevEnd()
	})
	p.info.arity[d] = arity{n: n}
}

// classBody parses `{ members }` and returns the span. If anon is set this
// is an anonymous body, so we create the members as locals under owner and
// leave the reference scope alone.
func (p *parser) classBody(owner *code.Decl, ownerName string, anon *anonCtx, isRecord bool) code.Span {
	open := p.p
	close := p.closeOf(open)
	p.p++
	s := p.enter(owner, anon == nil)
	p.depth++
	if p.depth > maxDepth {
		p.p = close
	}
	for p.p < close {
		start := p.p
		p.member(owner, ownerName, anon, isRecord)
		if p.p == start {
			p.p++
		}
	}
	p.depth--
	return p.leaveBody(open, close, s)
}

func (p *parser) member(owner *code.Decl, ownerName string, anon *anonCtx, isRecord bool) {
	if p.accept(";") {
		return
	}
	add := func(d *code.Decl) {
		if anon != nil && d != nil {
			anon.members = append(anon.members, d)
		}
	}
	startOff := p.cur().s
	m := p.modifiers()
	if p.typeKeywordAt(p.p) {
		add(p.typeDecl(owner, startOff))
		return
	}
	t := p.cur()
	if !p.cs && t.text == "{" {
		// initializer block
		p.exec++
		p.block()
		p.exec--
		return
	}
	tpStart := -1
	if !p.cs && t.text == "<" {
		j, ok := p.skipAngles(p.p)
		if !ok {
			p.skipMember()
			return
		}
		tpStart = p.p
		p.p = j
		t = p.cur()
	}
	switch {
	case p.cs && t.text == "~" && p.isIdent(p.p+1) && p.peek(2).text == "(":
		name, span := p.nameOf(p.peek(1))
		d := p.newDeclSpan(owner, code.KindMethod, "~"+name, code.Span{Start: t.s, End: span.End}, startOff)
		p.signature(d, -1, -1, -1, p.p+2)
	case t.kind == code.TokIdent && t.text == ownerName && p.peek(1).text == "(":
		add(p.method(owner, code.KindConstructor, startOff, -1, -1, p.p, tpStart))
	case !p.cs && isRecord && t.kind == code.TokIdent && t.text == ownerName && p.peek(1).text == "{":
		d := p.newDecl(owner, code.KindConstructor, p.p, startOff)
		p.p++
		s := p.enter(d, true)
		p.exec++
		d.Body = p.block()
		p.exec--
		d.Span.End = p.prevEnd()
		p.leave(d, s)
		p.info.arity[d] = p.info.arity[owner]
	case p.cs && (t.text == "implicit" || t.text == "explicit") && p.peek(1).text == "operator":
		p.p += 2
		j, ok := p.scanType(p.p)
		if !ok {
			p.skipMember()
			return
		}
		name := "operator " + p.typeText(p.p, j)
		d := p.newDeclSpan(owner, code.KindMethod, name, code.Span{Start: p.at(p.p - 1).s, End: p.at(j - 1).e}, startOff)
		p.signature(d, p.p, j, -1, j)
	default:
		p.typedMember(owner, startOff, tpStart, m, add)
	}
}

func (p *parser) typedMember(owner *code.Decl, startOff, tpStart int, m mods, add func(*code.Decl)) {
	j, ok := p.scanType(p.p)
	if !ok {
		p.skipMember()
		return
	}
	ts := p.p
	nt := p.at(j)
	switch {
	case p.cs && nt.text == "operator":
		k := j + 1
		var sym strings.Builder
		for k < len(p.toks) && k < j+4 && p.at(k).text != "(" {
			sym.WriteString(p.at(k).text)
			k++
		}
		d := p.newDeclSpan(owner, code.KindMethod, "operator "+sym.String(), code.Span{Start: nt.s, End: p.at(k - 1).e}, startOff)
		p.signature(d, ts, j, -1, k)
	case p.cs && nt.text == "this" && p.at(j+1).text == "[":
		d := p.newDeclSpan(owner, code.KindProperty, "this[]", code.Span{Start: nt.s, End: nt.e}, startOff)
		p.propertyRest(d, ts, j, j+1)
	case nt.kind == code.TokIdent:
		ni := p.memberName(j)
		if ni > j {
			p.emitType(j, ni-1)
		}
		switch n := p.at(ni + 1).text; {
		case n == "(" || (p.cs && n == "<"):
			add(p.method(owner, code.KindMethod, startOff, ts, j, ni, tpStart))
		case p.cs && (n == "{" || n == "=>"):
			kind := code.KindProperty
			if m.event {
				kind = code.KindField
			}
			d := p.newDecl(owner, kind, ni, startOff)
			add(d)
			p.propertyRest(d, ts, j, ni+1)
		default:
			kind := code.KindField
			if m.cnst {
				kind = code.KindConstant
			}
			p.fields(owner, startOff, ts, j, kind, add)
		}
	default:
		p.skipMember()
	}
}

func (p *parser) memberName(ni int) int {
	for p.cs {
		// explicit interface implementation like IFoo<T>.Bar
		k := ni + 1
		if p.at(k).text == "<" {
			if e, ok := p.scanTypeArgs(k, 0); ok {
				k = e
			}
		}
		if p.at(k).text != "." || !p.isIdent(k+1) {
			break
		}
		ni = k + 1
	}
	return ni
}

// skipAngles skips a Java type parameter list `<...>` at i.
func (p *parser) skipAngles(i int) (int, bool) {
	depth := 0
	for j := i; j < len(p.toks); j++ {
		switch p.toks[j].text {
		case "<":
			depth++
		case ">":
			depth--
			if depth == 0 {
				return j + 1, true
			}
		case "(", "{", ";", "}":
			return i, false
		}
	}
	return i, false
}

func (p *parser) skipMember() {
	for p.p < len(p.toks) {
		switch p.cur().text {
		case ";":
			p.p++
			return
		case "{":
			p.p = p.past(p.p, p.closeOf(p.p))
			return
		case "(", "[":
			p.p = p.past(p.p, p.closeOf(p.p))
			continue
		case "}":
			return
		}
		p.p++
	}
}

func (p *parser) method(owner *code.Decl, kind code.DeclKind, startOff, ts, te, ni, tpStart int) *code.Decl {
	d := p.newDecl(owner, kind, ni, startOff)
	p.signature(d, ts, te, tpStart, ni+1)
	return d
}

// signature parses the rest of a method-like declaration d. The return type
// is in tokens [ts, te) and Java type parameters start at tpStart. From token
// after on come the type parameters, parameters, throws/constraints and body.
func (p *parser) signature(d *code.Decl, ts, te, tpStart, after int) {
	s := p.enter(d, true)
	p.push()
	if ts >= 0 && te > ts {
		d.Type, _ = p.emitType(ts, te)
	}
	if tpStart >= 0 {
		p.p = tpStart
		p.typeParams(d)
	}
	p.p = after
	if p.is("<") {
		p.typeParams(d)
	}
	p.info.arity[d] = p.params(d)
	for !p.cs && p.is("[") && p.peek(1).text == "]" {
		p.p += 2
	}
	if !p.cs && p.accept("throws") {
		p.baseList(nil)
	}
	if p.cs {
		p.constraints()
		if p.is(":") {
			p.p++
			if p.is("base") || p.is("this") {
				p.p++
			}
			p.exec++
			if p.is("(") {
				p.group()
			}
			p.exec--
		}
	}
	if !p.cs && p.accept("default") {
		p.exec++
		p.expr(stops{})
		p.exec--
	}
	switch {
	case p.is("{"):
		p.exec++
		d.Body = p.block()
		p.exec--
	case p.accept("=>"):
		d.Body = p.exprBody()
	}
	p.accept(";")
	p.pop()
	d.Span.End = p.prevEnd()
	p.leave(d, s)
}

// exprBody parses an expression body (`=> expr`). The span it returns stands
// in for the declaration's brace body.
func (p *parser) exprBody() code.Span {
	p.exec++
	start := p.cur().s
	p.expr(stops{})
	end := p.prevEnd()
	p.exec--
	if end <= start {
		return code.Span{}
	}
	return code.Span{Start: start, End: end}
}

// params parses a parameter list in parentheses, or in brackets for C#
// indexers, and binds the parameters in the current frame.
func (p *parser) params(owner *code.Decl) arity {
	if !p.is("(") && !p.is("[") {
		return arity{}
	}
	var ar arity
	ar.n = p.seq(p.p, func() {
		if p.param(owner) {
			ar.varargs = true
		}
	})
	return ar
}

func (p *parser) param(owner *code.Decl) (varargs bool) {
	start := p.cur().s
	ext := false
	for {
		switch {
		case p.isAnnotation(p.p):
			p.annotation()
			continue
		case p.cs && p.is("["):
			p.attributes()
			continue
		case !p.cs && p.is("final"):
			p.p++
			continue
		case p.cs && (p.is("ref") || p.is("out") || p.is("in") || p.is("params") || p.is("this") || p.is("readonly") || p.isWord(p.p, "scoped")):
			varargs = varargs || p.is("params")
			ext = ext || p.is("this")
			p.keyword(p.p)
			p.p++
			continue
		}
		break
	}
	j, ok := p.scanType(p.p)
	if !ok {
		p.expr(stops{comma: true})
		return varargs
	}
	typ, _ := p.emitType(p.p, j)
	p.p = j
	if !p.cs && p.accept("...") {
		varargs = true
		typ += "..."
	}
	if !p.cs && p.accept("this") {
		return varargs // receiver parameter
	}
	if !p.isIdent(p.p) {
		return varargs
	}
	d := p.newDecl(owner, code.KindParameter, p.p, start)
	d.Type = typ
	p.p++
	for !p.cs && p.is("[") && p.peek(1).text == "]" {
		p.p += 2
		d.Type += "[]"
	}
	p.bind(d)
	if p.accept("=") {
		p.expr(stops{comma: true})
	}
	d.Span.End = p.prevEnd()
	if ext {
		p.info.ext = append(p.info.ext, owner)
	}
	return varargs
}

// typeParams parses a type parameter list `<...>` at the cursor.
func (p *parser) typeParams(owner *code.Decl) {
	p.p++
	for p.p < len(p.toks) {
		for p.isAnnotation(p.p) {
			p.annotation()
		}
		if p.cs && p.is("[") {
			p.attributes()
		}
		if p.cs && (p.is("in") || p.is("out")) {
			p.p++
		}
		if p.isIdent(p.p) {
			d := p.newDecl(owner, code.KindTypeParameter, p.p, p.cur().s)
			p.p++
			if !p.cs && p.accept("extends") {
				p.typeBounds()
			}
			d.Span.End = p.prevEnd()
		}
		if !p.accept(",") {
			p.accept(">")
			return
		}
	}
}

func (p *parser) typeBounds() {
	for {
		j, ok := p.scanType(p.p)
		if !ok {
			return
		}
		p.emitType(p.p, j)
		p.p = j
		if !p.accept("&") {
			return
		}
	}
}

// constraints parses C# `where T : ...` clauses.
func (p *parser) constraints() {
	for p.isWord(p.p, "where") && p.isIdent(p.p+1) && p.peek(2).text == ":" {
		p.keyword(p.p)
		p.newRef(p.p+1, wantType, qual{})
		p.p += 3
		for {
			switch {
			case p.is("new") && p.peek(1).text == "(":
				p.p += 3
			case p.is("class") || p.is("struct") || p.is("default") || p.isWord(p.p, "unmanaged") || p.isWord(p.p, "notnull"):
				p.keyword(p.p)
				p.p++
				p.accept("?")
			default:
				j, ok := p.scanType(p.p)
				if !ok {
					return
				}
				p.emitType(p.p, j)
				p.p = j
			}
			if !p.accept(",") {
				break
			}
		}
	}
}

// fields parses field declarators with the type in tokens [ts, te).
func (p *parser) fields(owner *code.Decl, startOff, ts, te int, kind code.DeclKind, add func(*code.Decl)) {
	typ, _ := p.emitType(ts, te)
	p.p = te
	var decls []*code.Decl
	for p.isIdent(p.p) {
		ni := p.p
		p.p++
		d := p.newDecl(owner, kind, ni, startOff)
		d.Type = typ
		for !p.cs && p.is("[") && p.peek(1).text == "]" {
			p.p += 2
			d.Type += "[]"
		}
		add(d)
		decls = append(decls, d)
		if p.cs && p.is("[") {
			p.group() // fixed-size buffer
		}
		if p.accept("=") {
			s := p.enter(d, false)
			p.exec++
			p.expr(stops{comma: true})
			p.exec--
			p.leave(nil, s)
		}
		if !p.accept(",") {
			break
		}
	}
	p.accept(";")
	end := p.prevEnd()
	for _, d := range decls {
		d.Span.End = max(end, d.Span.End)
	}
}

// propertyRest parses a C# property, indexer or event from the name on,
// accessors included. The type is in tokens [ts, te).
func (p *parser) propertyRest(d *code.Decl, ts, te, after int) {
	s := p.enter(d, true)
	d.Type, _ = p.emitType(ts, te)
	p.p = after
	p.push()
	if p.is("[") {
		p.info.arity[d] = p.params(d)
	}
	switch {
	case p.is("{"):
		d.Body = p.accessors(d.Type)
	case p.accept("=>"):
		d.Body = p.exprBody()
	}
	if p.accept("=") {
		p.exec++
		p.expr(stops{})
		p.exec--
	}
	p.accept(";")
	p.pop()
	d.Span.End = p.prevEnd()
	p.leave(d, s)
}

func (p *parser) accessors(typ string) code.Span {
	open := p.p
	close := p.closeOf(open)
	p.p++
	for p.p < close {
		start := p.p
		p.modifiers()
		if t := p.cur(); t.kind == code.TokIdent && accessorWords[t.text] {
			p.keyword(p.p)
			p.p++
			setter, valueType := p.setter, p.valueType
			p.setter, p.valueType = t.text != "get", typ
			p.exec++
			switch {
			case p.is("{"):
				p.block()
			case p.accept("=>"):
				p.expr(stops{})
				p.accept(";")
			default:
				p.accept(";")
			}
			p.exec--
			p.setter, p.valueType = setter, valueType
		}
		if p.p == start {
			p.p++
		}
	}
	if p.p <= close {
		p.p = min(close+1, len(p.toks))
	}
	return code.Span{Start: p.at(open).s, End: p.endOff(close)}
}

func (p *parser) enumBody(d *code.Decl) code.Span {
	open := p.p
	close := p.closeOf(open)
	p.p++
	s := p.enter(d, true)
	for p.p < close {
		start := p.p
		if !p.cs && p.accept(";") {
			for p.p < close {
				st := p.p
				p.member(d, d.Name, nil, false)
				if p.p == st {
					p.p++
				}
			}
			break
		}
		startOff := p.cur().s
		p.modifiers()
		if p.isIdent(p.p) {
			c := p.newDecl(d, code.KindEnumMember, p.p, startOff)
			p.p++
			if p.cs && p.accept("=") {
				p.exec++
				p.expr(stops{comma: true})
				p.exec--
			}
			if !p.cs && p.is("(") {
				p.exec++
				p.group()
				p.exec--
			}
			if !p.cs && p.is("{") {
				cs := p.enter(c, false)
				p.anonBody(d.Name)
				p.leave(nil, cs)
			}
			c.Span.End = p.prevEnd()
		}
		p.accept(",")
		if p.p == start {
			p.p++
		}
	}
	return p.leaveBody(open, close, s)
}

func (p *parser) leaveBody(open, close int, s saved) code.Span {
	if p.p <= close {
		p.p = min(close+1, len(p.toks))
	}
	p.leave(nil, s)
	return code.Span{Start: p.at(open).s, End: p.endOff(close)}
}

// delegateDecl parses a C# delegate type. We record it as a type alias.
func (p *parser) delegateDecl(parent *code.Decl, startOff int) *code.Decl {
	p.p++
	j, ok := p.scanType(p.p)
	if !ok || !p.isIdent(j) {
		p.skipMember()
		return nil
	}
	d := p.newDecl(parent, code.KindTypeAlias, j, startOff)
	p.signature(d, p.p, j, -1, j+1)
	return d
}

func (p *parser) javaUnit() {
	i := 0
	for p.isAnnotation(i) {
		i = p.skipAnnotation(i)
	}
	name, pkgEnd := "", -1
	if p.at(i).text == "package" {
		j := i + 1
		for p.isIdent(j) {
			j++
			if p.at(j).text != "." || !p.isIdent(j+1) {
				break
			}
			j++
		}
		name, pkgEnd = p.typeText(i+1, j), j
	}
	mod := p.f.SetModule(name)
	p.parent, p.scope, p.ctx = mod, mod, mod
	if pkgEnd >= 0 {
		for p.isAnnotation(p.p) {
			p.annotation()
		}
		p.p = pkgEnd
		p.accept(";")
	}
	for p.p < len(p.toks) {
		start := p.p
		switch {
		case p.is("import"):
			p.javaImport()
		case p.is(";"):
			p.p++
		case (p.isWord(p.p, "module") && p.isIdent(p.p+1)) || (p.isWord(p.p, "open") && p.isWord(p.p+1, "module")):
			for p.p < len(p.toks) && !p.is("{") {
				p.p++
			}
			p.p = min(p.closeOf(p.p)+1, len(p.toks))
		default:
			startOff := p.cur().s
			p.modifiers()
			if p.typeKeywordAt(p.p) {
				p.typeDecl(mod, startOff)
			} else {
				p.skipMember()
			}
		}
		if p.p == start {
			p.p++
		}
	}
}

func (p *parser) javaImport() {
	p.p++
	var imp importInfo
	imp.static = p.accept("static")
	for p.isIdent(p.p) {
		imp.refs = append(imp.refs, p.importRef(p.p))
		p.p++
		if !p.accept(".") {
			break
		}
		if p.accept("*") {
			imp.star = true
			break
		}
	}
	p.accept(";")
	if len(imp.refs) > 0 {
		p.info.imports = append(p.info.imports, imp)
	}
}

func (p *parser) csUnit() {
	mod := p.f.SetModule("")
	p.parent, p.scope, p.ctx = mod, mod, mod
	p.csMembers(mod, len(p.toks), true)
}

// csMembers parses using directives and namespace and type declarations up
// to token end. If top is set, top-level statements are allowed too.
func (p *parser) csMembers(container *code.Decl, end int, top bool) {
	for p.p < end {
		start := p.p
		t := p.cur()
		switch {
		case t.text == "extern" && p.isWord(p.p+1, "alias"):
			p.skipMember()
		case p.isWord(p.p, "global") && p.peek(1).text == "using":
			p.keyword(p.p)
			p.p++
			p.csUsing(t.s, true)
		case t.text == "using" && p.usingDirective():
			p.csUsing(t.s, false)
		case t.text == "namespace":
			p.csNamespace(container)
		case t.text == ";":
			p.p++
		default:
			p.modifiers()
			if p.typeKeywordAt(p.p) {
				p.typeDecl(container, t.s)
				break
			}
			if !top {
				p.skipMember()
				break
			}
			if t.text != "[" {
				p.p = start
			}
			p.exec++
			p.statement()
			p.exec--
		}
		if p.p == start {
			p.p++
		}
	}
}

func (p *parser) usingDirective() bool {
	i := p.p + 1
	switch {
	case p.at(i).text == "static":
		return true
	case p.at(i).text == "(":
		return false
	case p.isIdent(i) && p.at(i+1).text == "=":
		return true
	}
	j, ok := p.scanType(i)
	return ok && p.at(j).text == ";"
}

func (p *parser) csUsing(startOff int, global bool) {
	p.p++
	imp := importInfo{global: global}
	imp.static = p.accept("static")
	if p.isIdent(p.p) && p.peek(1).text == "=" {
		ni := p.p
		p.p += 2
		j, ok := p.scanType(p.p)
		if !ok {
			j = p.p
		}
		d := p.newDecl(p.parent, code.KindImport, ni, startOff)
		d.Type = p.typeText(p.p, j)
		imp.alias = d
		imp.refs = p.importPath(p.p, j)
		p.p = j
		p.accept(";")
		d.Span.End = p.prevEnd()
	} else {
		if j, ok := p.scanType(p.p); ok {
			imp.refs = p.importPath(p.p, j)
			p.p = j
		}
		p.accept(";")
	}
	if len(imp.refs) > 0 {
		p.info.imports = append(p.info.imports, imp)
	}
}

// importPath records the name segments of a using directive as import
// references. Type arguments get ordinary type references.
func (p *parser) importPath(i, j int) []*code.Ref {
	var refs []*code.Ref
	for k := i; k < j; k++ {
		t := p.at(k)
		switch {
		case t.text == "<":
			end, ok := p.scanTypeArgs(k, 0)
			if !ok {
				return refs
			}
			p.emitType(k, end)
			k = end - 1
		case p.isWord(k, "global") && p.at(k+1).text == "::":
			p.keyword(k)
		case t.kind == code.TokIdent:
			refs = append(refs, p.importRef(k))
		}
	}
	return refs
}

func (p *parser) csNamespace(container *code.Decl) {
	startOff := p.cur().s
	p.p++
	first := p.p
	for p.isIdent(p.p) {
		p.p++
		if !p.is(".") || !p.isIdent(p.p+1) {
			break
		}
		p.p++
	}
	if p.p == first {
		return
	}
	name := p.typeText(first, p.p)
	d := p.newDeclSpan(container, code.KindNamespace, name, code.Span{Start: p.at(first).s, End: p.at(p.p - 1).e}, startOff)
	s := p.enter(d, true)
	switch {
	case p.accept(";"):
		p.csMembers(d, len(p.toks), false)
		d.Span.End = len(p.src)
	case p.is("{"):
		open := p.p
		close := p.closeOf(open)
		p.p++
		p.csMembers(d, close, false)
		if p.p <= close {
			p.p = min(close+1, len(p.toks))
		}
		d.Body = code.Span{Start: p.at(open).s, End: p.endOff(close)}
		d.Span.End = d.Body.End
	}
	p.leave(nil, s)
}
