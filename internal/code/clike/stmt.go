package clike

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// block parses a brace-enclosed block in a new lexical frame and returns
// its span.
func (p *parser) block() code.Span {
	open := p.p
	close := p.closeOf(open)
	p.p++
	p.push()
	p.depth++
	if p.depth > maxDepth {
		p.p = close
	}
	for p.p < close {
		start := p.p
		p.statement()
		if p.p == start {
			p.p++
		}
	}
	p.depth--
	p.pop()
	if p.p <= close {
		p.p = min(close+1, len(p.toks))
	}
	return code.Span{Start: p.at(open).s, End: p.endOff(close)}
}

// substatement parses the body of an if/while/for in a new frame.
func (p *parser) substatement() {
	p.push()
	p.statement()
	p.pop()
}

func (p *parser) parenExpr() {
	if !p.is("(") {
		return
	}
	p.seq(p.p, func() { p.expr(stops{comma: true}) })
}

// endGroup moves past the group from open to close, unless the cursor has
// already overrun it.
func (p *parser) endGroup(open, close int) {
	if p.p <= close {
		p.p = p.past(open, close)
	}
}

func (p *parser) endStatement() {
	p.accept(";")
}

func (p *parser) statement() {
	t := p.cur()
	if t.kind == code.TokIdent && p.peek(1).text == ":" && p.peek(1).kind == code.TokPunct {
		p.p += 2 // label
		return
	}
	switch t.text {
	case "{":
		p.block()
		return
	case ";":
		p.p++
		return
	case "if":
		p.p++
		p.parenExpr()
		p.substatement()
		if p.accept("else") {
			p.substatement()
		}
		return
	case "while":
		p.p++
		p.parenExpr()
		p.substatement()
		return
	case "do":
		p.p++
		p.substatement()
		if p.accept("while") {
			p.parenExpr()
		}
		p.endStatement()
		return
	case "for":
		p.forStmt()
		return
	case "try":
		p.tryStmt()
		return
	case "switch":
		p.switchStmt()
		p.endStatement()
		return
	case "return", "throw":
		if t.kind == code.TokKeyword {
			p.p++
			p.expr(stops{})
			p.endStatement()
			return
		}
	case "break", "continue":
		p.p++
		if p.isIdent(p.p) {
			p.p++
		}
		p.endStatement()
		return
	case "else", "catch", "finally", "case", "default":
		if t.kind == code.TokKeyword {
			p.p++
			return
		}
	}
	if p.cs && p.csStatement(t) || !p.cs && p.javaStatement(t) {
		return
	}
	if p.localDecl(true) {
		return
	}
	p.expr(stops{})
	p.endStatement()
}

func (p *parser) csStatement(t *tok) bool {
	switch t.text {
	case "foreach":
		p.forStmt()
	case "goto":
		p.p++
		if p.accept("case") {
			p.expr(stops{})
		} else if !p.accept("default") && p.isIdent(p.p) {
			p.p++
		}
		p.endStatement()
	case "lock", "fixed":
		p.resourceStatement()
	case "using":
		if p.peek(1).text == "(" {
			p.resourceStatement()
			return true
		}
		p.p++
		if !p.localDecl(true) {
			p.expr(stops{})
			p.endStatement()
		}
	case "checked", "unchecked", "unsafe":
		if p.peek(1).text != "{" {
			return false
		}
		p.p++
		p.block()
	case "yield":
		if n := p.peek(1).text; n != "return" && n != "break" {
			return false
		}
		p.keyword(p.p)
		p.p += 2
		p.expr(stops{})
		p.endStatement()
	case "await":
		if n := p.peek(1).text; n != "foreach" && n != "using" {
			return false
		}
		p.keyword(p.p)
		p.p++
		p.statement()
	default:
		return false
	}
	return true
}

func (p *parser) resourceStatement() {
	p.p++
	p.push()
	if p.is("(") {
		open, close := p.p, p.closeOf(p.p)
		p.p++
		if !p.localDecl(false) {
			p.expr(stops{})
		}
		p.endGroup(open, close)
	}
	p.statement()
	p.pop()
}

func (p *parser) javaStatement(t *tok) bool {
	switch t.text {
	case "synchronized":
		p.p++
		p.parenExpr()
		p.substatement()
	case "assert":
		p.p++
		p.expr(stops{colon: true})
		if p.accept(":") {
			p.expr(stops{})
		}
		p.endStatement()
	case "yield":
		if t.kind != code.TokIdent || !p.javaYield() {
			return false
		}
		p.keyword(p.p)
		p.p++
		p.expr(stops{})
		p.endStatement()
	default:
		return false
	}
	return true
}

// javaYield reports whether `yield` at the cursor starts a yield statement,
// as opposed to an expression that uses a variable called yield.
func (p *parser) javaYield() bool {
	switch n := p.peek(1); n.text {
	case "=", ".", "[", "++", "--", "+=", "-=", "*=", "/=", ";":
		return n.kind != code.TokPunct
	}
	return true
}

func (p *parser) localModifier(i int) bool {
	t := p.at(i)
	if p.cs {
		switch t.text {
		case "const", "static", "unsafe", "extern", "ref", "readonly":
			return t.kind == code.TokKeyword
		case "async", "scoped":
			n := p.at(i + 1)
			return t.kind == code.TokIdent && (n.kind == code.TokIdent || p.isPrim(n) || n.text == "ref" || n.text == "static")
		}
		return false
	}
	switch t.text {
	case "final", "abstract", "static", "strictfp":
		return t.kind == code.TokKeyword
	case "sealed":
		return t.kind == code.TokIdent && (p.at(i+1).kind == code.TokKeyword || p.isIdent(i+1))
	case "non":
		return p.at(i+1).text == "-" && p.isWord(i+2, "sealed")
	}
	return false
}

// localDecl parses a local variable declaration, local function or Java
// local type at the cursor, and reports whether there was one. If there
// wasn't, the cursor doesn't move unless we consumed annotations or
// attributes. If term is set we also consume the terminating ';'.
func (p *parser) localDecl(term bool) bool {
	start := p.p
	startOff := p.cur().s
	emitted := false
	isConst := false
	for {
		switch {
		case p.isAnnotation(p.p) && !p.isWord(p.p+1, "interface"):
			p.annotation()
			emitted = true
			continue
		case p.cs && term && p.is("[") && p.p == start:
			// attributes on a local function
			p.attributes()
			emitted = true
			continue
		case p.localModifier(p.p):
			if p.is("const") {
				isConst = true
			}
			if p.is("non") {
				p.keyword(p.p)
				p.keyword(p.p + 2)
				p.p += 3
				continue
			}
			p.keyword(p.p)
			p.p++
			continue
		}
		break
	}
	if !p.cs && p.typeKeywordAt(p.p) {
		p.typeDecl(p.parent, startOff)
		return true
	}
	if j, ok := p.scanType(p.p); ok && p.isIdent(j) && p.declType(p.p, j) {
		n := p.at(j + 1)
		switch {
		case p.cs && term && (n.text == "(" || n.text == "<"):
			d := p.method(p.parent, code.KindFunction, startOff, p.p, j, j, -1)
			p.bind(d)
			return true
		case n.kind == code.TokPunct && (n.text == "=" || n.text == ";" || n.text == "," || (!p.cs && (n.text == ":" || n.text == "["))),
			p.cs && n.text == "in":
			kind := code.KindVariable
			if isConst {
				kind = code.KindConstant
			}
			p.localVars(startOff, p.p, j, kind, term)
			return true
		}
	}
	if !emitted {
		p.p = start
	}
	return false
}

// declType rejects "types" that are statement keywords.
func (p *parser) declType(ts, te int) bool {
	if te != ts+1 {
		return true
	}
	switch p.at(ts).text {
	case "await", "yield", "nameof":
		return p.at(ts).kind != code.TokIdent
	}
	return true
}

// localVars parses `Type a = x, b` with the type in tokens [ts, te).
func (p *parser) localVars(startOff, ts, te int, kind code.DeclKind, term bool) {
	typ := ""
	if te == ts+1 && p.isWord(ts, "var") {
		p.keyword(ts)
	} else {
		typ, _ = p.emitType(ts, te)
	}
	p.p = te
	var decls []*code.Decl
	for p.isIdent(p.p) {
		ni := p.p
		p.p++
		dtyp := typ
		for !p.cs && p.is("[") && p.peek(1).text == "]" {
			p.p += 2
			dtyp += "[]"
		}
		d := p.newDecl(p.parent, kind, ni, startOff)
		d.Type = dtyp
		p.bind(d)
		decls = append(decls, d)
		if p.accept("=") {
			if d.Type == "" {
				d.Type = p.inferredType()
			}
			p.expr(stops{comma: true})
		}
		if !p.accept(",") {
			break
		}
	}
	if term && p.is(";") {
		p.p++
	}
	end := p.prevEnd()
	for _, d := range decls {
		d.Span.End = max(end, d.Span.End)
	}
}

// inferredType returns the class of a `new T(...)` initializer at the
// cursor, or "".
func (p *parser) inferredType() string {
	if !p.is("new") {
		return ""
	}
	j, ok := p.scanType(p.p + 1)
	if !ok || p.at(j-1).text == "]" || (p.at(j).text != "(" && p.at(j).text != "{") {
		return ""
	}
	return p.typeText(p.p+1, j)
}

func (p *parser) forStmt() {
	p.p++
	if !p.is("(") {
		return
	}
	open, close := p.p, p.closeOf(p.p)
	p.p++
	p.push()
	defer p.pop()
	if !p.localDecl(false) {
		for p.p < close && !p.is(";") && !p.is(":") && !p.is("in") {
			start := p.p
			p.expr(stops{comma: true})
			p.accept(",")
			if p.p == start {
				p.p++
			}
		}
	}
	switch {
	case p.is(":") || p.is("in"):
		p.p++
		p.expr(stops{})
	case p.is(";"):
		p.p++
		p.expr(stops{})
		if p.accept(";") {
			for p.p < close {
				start := p.p
				p.expr(stops{comma: true})
				p.accept(",")
				if p.p == start {
					p.p++
				}
			}
		}
	}
	for p.p < close {
		start := p.p
		p.expr(stops{})
		if p.p == start {
			p.p++
		}
	}
	p.endGroup(open, close)
	p.statement()
}

func (p *parser) tryStmt() {
	p.p++
	pushed := false
	if p.is("(") {
		p.push()
		pushed = true
		open, close := p.p, p.closeOf(p.p)
		p.p++
		for p.p < close {
			start := p.p
			if !p.localDecl(false) {
				p.expr(stops{})
			}
			p.accept(";")
			if p.p == start {
				p.p++
			}
		}
		p.endGroup(open, close)
	}
	if p.is("{") {
		p.block()
	}
	if pushed {
		p.pop()
	}
	for p.is("catch") {
		p.p++
		p.push()
		if p.is("(") {
			p.catchParameter()
		}
		if p.cs && p.isWord(p.p, "when") {
			p.keyword(p.p)
			p.p++
			p.parenExpr()
		}
		if p.is("{") {
			p.block()
		}
		p.pop()
	}
	if p.accept("finally") && p.is("{") {
		p.block()
	}
}

func (p *parser) catchParameter() {
	open, close := p.p, p.closeOf(p.p)
	p.p++
	start := p.cur().s
	for p.isAnnotation(p.p) || p.is("final") {
		if p.is("final") {
			p.p++
		} else {
			p.annotation()
		}
	}
	var typ strings.Builder
	for {
		j, ok := p.scanType(p.p)
		if !ok {
			break
		}
		text, _ := p.emitType(p.p, j)
		typ.WriteString(text)
		p.p = j
		if !p.is("|") {
			break
		}
		typ.WriteString("|")
		p.p++
	}
	if p.isIdent(p.p) {
		d := p.newDecl(p.parent, code.KindParameter, p.p, start)
		d.Type = typ.String()
		p.bind(d)
		p.p++
	}
	for p.p < close {
		before := p.p
		p.expr(stops{})
		if p.p == before {
			p.p++
		}
	}
	p.endGroup(open, close)
}

// switchStmt parses a switch statement, or a Java switch expression. The
// cursor has to be on `switch`.
func (p *parser) switchStmt() {
	p.p++
	p.parenExpr()
	if !p.is("{") {
		return
	}
	close := p.closeOf(p.p)
	p.p++
	for p.p < close {
		start := p.p
		if p.is("case") || p.is("default") {
			p.push()
			arrow := false
			for !arrow && (p.is("case") || (p.is("default") && (p.peek(1).text == ":" || p.peek(1).text == "->"))) {
				if p.cur().text == "case" {
					p.p++
					p.caseLabel()
				} else {
					p.p++
				}
				switch {
				case p.accept(":"):
				case p.accept("->"):
					arrow = true
				}
			}
			if arrow {
				switch {
				case p.is("{"):
					p.block()
				case p.is("throw"):
					p.statement()
				default:
					p.expr(stops{})
					p.endStatement()
				}
			} else {
				p.caseStatements(close)
			}
			p.pop()
		} else {
			p.statement()
		}
		if p.p == start {
			p.p++
		}
	}
	if p.p <= close {
		p.p = min(close+1, len(p.toks))
	}
}

func (p *parser) caseStatements(close int) {
	for p.p < close && !p.is("case") && !(p.is("default") && (p.peek(1).text == ":" || p.peek(1).text == "->")) {
		start := p.p
		p.statement()
		if p.p == start {
			p.p++
		}
	}
}

func (p *parser) caseLabel() {
	p.inCase = true
	for {
		p.pattern(stops{comma: true, colon: true, arrow: true, pattern: true})
		if p.cs || !p.accept(",") {
			break
		}
	}
	p.inCase = false
	if p.isWord(p.p, "when") {
		p.keyword(p.p)
		p.p++
		p.expr(stops{colon: true, arrow: true})
	}
}
