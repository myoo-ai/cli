package javascript

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// exprInfo is the part of a parsed expression we need to know about for
// declarations and member resolution.
type exprInfo struct {
	ref     *code.Ref // last identifier of a path expression (a, a.b, this.a)
	path    bool      // the expression is an identifier or member path
	this    bool
	super   bool
	require string    // the expression is require('spec')
	newRef  *code.Ref // `new T(...)`: last identifier of T
	newText string    // `new T(...)`: T as written
}

func (p *parser) expression() exprInfo {
	info := p.assign()
	for p.is(",") {
		p.i++
		p.assign()
		info = exprInfo{}
	}
	return info
}

var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true,
	"<<=": true, ">>=": true, ">>>=": true, "&=": true, "|=": true, "^=": true,
	"&&=": true, "||=": true, "??=": true,
}

func (p *parser) assign() exprInfo {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.i++
		return exprInfo{}
	}
	if p.arrowAhead() {
		p.arrowFn(nil)
		return exprInfo{}
	}
	if t := p.tok(); p.isWord(t, "yield") {
		p.kw()
		if n := p.tok(); !n.nl && (p.startsExpr(n) || p.isOp(n, "*")) {
			p.eat("*")
			p.assign()
		}
		return exprInfo{}
	}
	info := p.conditional()
	if t := p.tok(); t.kind == code.TokPunct && t.sub == subNone && assignOps[p.text(t)] {
		p.i++
		p.assign()
		return exprInfo{}
	}
	return info
}

func (p *parser) conditional() exprInfo {
	return p.conditionalRest(p.binary(0))
}

func (p *parser) conditionalRest(info exprInfo) exprInfo {
	if !p.is("?") {
		return info
	}
	p.i++
	noIn := p.noIn
	p.noIn = false
	p.assign()
	p.noIn = noIn
	p.eat(":")
	p.assign()
	return exprInfo{}
}

func (p *parser) binPrec(t token) int {
	if t.sub != subNone {
		return 0
	}
	switch t.kind {
	case code.TokPunct:
		switch p.text(t) {
		case "??":
			return 1
		case "||":
			return 2
		case "&&":
			return 3
		case "|":
			return 4
		case "^":
			return 5
		case "&":
			return 6
		case "==", "!=", "===", "!==":
			return 7
		case "<", ">", "<=", ">=":
			return 8
		case "<<", ">>", ">>>":
			return 9
		case "+", "-":
			return 10
		case "*", "/", "%":
			return 11
		case "**":
			return 12
		}
	case code.TokKeyword:
		switch p.text(t) {
		case "instanceof":
			return 8
		case "in":
			if !p.noIn {
				return 8
			}
		}
	}
	return 0
}

func (p *parser) binary(minPrec int) exprInfo {
	return p.binaryRest(p.unary(), minPrec)
}

// continueExpr parses the rest of an expression if we've already parsed a
// leading object literal: `{...}.x`, `{...} as T`, `{...} || d`.
func (p *parser) continueExpr() {
	info := p.binaryRest(p.chainRest(exprInfo{}, true), 0)
	p.conditionalRest(info)
}

func (p *parser) binaryRest(left exprInfo, minPrec int) exprInfo {
	for !p.eof() {
		t := p.tok()
		if p.ts && !t.nl && (p.isWord(t, "as") || p.isWord(t, "satisfies")) && minPrec < 8 {
			p.kw()
			if !p.eat("const") {
				p.typ()
			}
			left = exprInfo{}
			continue
		}
		prec := p.binPrec(t)
		if prec == 0 || prec <= minPrec {
			return left
		}
		p.i++
		if prec == 12 {
			p.binary(prec - 1)
		} else {
			p.binary(prec)
		}
		left = exprInfo{}
	}
	return left
}

func (p *parser) unary() exprInfo {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.i++
		return exprInfo{}
	}
	t := p.tok()
	switch {
	case t.kind == code.TokPunct && t.sub == subNone:
		switch p.text(t) {
		case "!", "~", "+", "-", "++", "--":
			p.i++
			p.unary()
			return exprInfo{}
		case "<":
			if p.ts {
				p.typeArgs()
				p.unary()
				return exprInfo{}
			}
		}
	case p.isOp(t, "typeof") || p.isOp(t, "void") || p.isOp(t, "delete"):
		p.i++
		p.unary()
		return exprInfo{}
	case p.isWord(t, "await"):
		if n := p.peek(1); !n.nl && p.startsExpr(n) {
			p.kw()
			p.unary()
			return exprInfo{}
		}
	}
	info := p.callChain()
	if t := p.tok(); (p.isOp(t, "++") || p.isOp(t, "--")) && !t.nl {
		p.i++
		return exprInfo{}
	}
	return info
}

func (p *parser) startsExpr(t token) bool {
	switch t.kind {
	case code.TokIdent, code.TokString, code.TokNumber:
		return t.sub != subJSXText && t.sub != subTmplMiddle && t.sub != subTmplTail
	case code.TokKeyword:
		switch p.text(t) {
		case "in", "instanceof", "of", "case", "default", "else", "catch", "finally", "extends":
			return false
		}
		return true
	case code.TokPunct:
		if t.sub == subJSXOpen {
			return true
		}
		switch p.text(t) {
		case "(", "[", "{", "!", "~", "+", "-", "++", "--", "/", "/=", "<", "@", "#":
			return true
		}
	}
	return false
}

func (p *parser) callChain() exprInfo {
	if !p.is("new") {
		return p.chainRest(p.primary(), true)
	}
	p.i++
	if p.eat(".") {
		p.i++
		return p.chainRest(exprInfo{}, true)
	}
	s := p.tok().start
	callee := p.newCallee()
	text := p.textFrom(s)
	if p.ts && p.is("<") && p.typeArgsAhead(p.i) {
		p.typeArgs()
	}
	if callee.path && callee.ref != nil {
		callee.ref.Call = true
	}
	if p.is("(") {
		p.args()
	}
	info := exprInfo{}
	if callee.path && callee.ref != nil {
		info.newRef, info.newText = callee.ref, text
	}
	return p.chainRest(info, true)
}

// newCallee parses the constructor expression after `new`. This is a
// primary expression followed by member accesses, but no calls, since the
// first call belongs to `new`.
func (p *parser) newCallee() exprInfo {
	if p.is("new") {
		return p.callChain()
	}
	return p.chainRest(p.primary(), false)
}

// chainRest parses the member accesses and calls after a primary
// expression, tagged templates included.
func (p *parser) chainRest(info exprInfo, calls bool) exprInfo {
	for !p.eof() {
		t := p.tok()
		switch {
		case p.isOp(t, ".") || p.isOp(t, "?."):
			p.i++
			n := p.tok()
			switch {
			case p.isOp(t, "?.") && p.is("(") && calls:
				p.call(info)
				info = exprInfo{}
			case p.isOp(t, "?.") && p.is("["):
				p.index()
				info = exprInfo{}
			case isName(n):
				p.i++
				if n.kind == code.TokKeyword {
					p.setKind(p.i-1, code.TokIdent)
				}
				info = exprInfo{ref: p.memberRef(n, info, false), path: true}
			default:
				return exprInfo{}
			}
		case p.isOp(t, "["):
			p.index()
			info = exprInfo{}
		case p.isOp(t, "(") && calls:
			info = p.call(info)
		case t.kind == code.TokString && (t.sub == subTmplFull || t.sub == subTmplHead) && calls:
			if info.path && info.ref != nil {
				info.ref.Call = true
			}
			p.template(false)
			info = exprInfo{}
		case p.ts && p.isOp(t, "!") && !t.nl:
			p.i++
		case p.ts && p.isOp(t, "<") && p.typeArgsAhead(p.i):
			p.typeArgs()
		default:
			return info
		}
	}
	return info
}

func (p *parser) index() {
	open := p.i
	p.i++
	p.expression()
	if !p.is("]") {
		p.recover(open)
	}
	p.eat("]")
}

func (p *parser) call(callee exprInfo) exprInfo {
	if callee.path && callee.ref != nil {
		callee.ref.Call = true
	}
	if s := p.peek(1); p.isWord(p.at(p.i-1), "require") && !p.isOp(p.at(p.i-2), ".") &&
		s.kind == code.TokString && (s.sub == subNone || s.sub == subTmplFull) && p.isOp(p.peek(2), ")") {
		p.pr.esm = true
		p.i += 3
		return exprInfo{require: unquote(p.text(s))}
	}
	p.args()
	return exprInfo{}
}

func (p *parser) args() {
	open := p.i
	p.i++
	noIn := p.noIn
	p.noIn = false
	for !p.eof() && !p.is(")") {
		st := p.i
		p.eat("...")
		p.assign()
		if !p.listSeparator(open, st, ")") {
			break
		}
	}
	p.noIn = noIn
	p.eat(")")
}

func (p *parser) primary() exprInfo {
	t := p.tok()
	switch t.kind {
	case code.TokIdent:
		switch {
		case t.sub == subPrivate:
			p.i++
			return exprInfo{ref: p.memberRef(t, exprInfo{this: true}, false), path: true}
		case t.sub != subNone:
			p.i++
			return exprInfo{}
		case p.isText(t, "async") && p.isOp(p.peek(1), "function") && !p.peek(1).nl:
			p.kw()
			p.functionExpr()
			return exprInfo{}
		}
		p.i++
		r := p.useRef(t)
		return exprInfo{ref: r, path: r != nil}
	case code.TokKeyword:
		switch p.text(t) {
		case "this":
			p.i++
			return exprInfo{this: true, path: true}
		case "super":
			p.i++
			return exprInfo{super: true, path: true}
		case "function":
			p.functionExpr()
		case "class":
			p.class(declCtx{start: t.start}, nil, false)
		case "new":
			return p.callChain()
		case "import":
			p.i++
			if p.eat(".") {
				p.i++
			} else if p.is("(") {
				p.args()
			}
		case "null", "true", "false":
			p.i++
		}
		return exprInfo{}
	case code.TokNumber:
		p.i++
	case code.TokString:
		if t.sub == subTmplHead || t.sub == subTmplFull {
			p.template(false)
		} else {
			p.i++
		}
	case code.TokPunct:
		return p.primaryPunct(t)
	}
	return exprInfo{}
}

func (p *parser) primaryPunct(t token) exprInfo {
	switch {
	case t.sub == subJSXOpen:
		p.jsxElement()
	case p.isOp(t, "("):
		open := p.i
		p.i++
		noIn := p.noIn
		p.noIn = false
		info := p.expression()
		p.noIn = noIn
		if !p.is(")") {
			p.recover(open)
		}
		p.eat(")")
		info.path = info.path && !info.this && !info.super
		return info
	case p.isOp(t, "["):
		p.arrayLiteral()
	case p.isOp(t, "{"):
		p.objectLiteral(nil, false)
	case p.isOp(t, "@"):
		p.decorators()
		if p.is("class") {
			p.class(declCtx{start: t.start}, nil, false)
		}
	}
	return exprInfo{}
}

func (p *parser) arrayLiteral() {
	open := p.i
	p.i++
	noIn := p.noIn
	p.noIn = false
	for !p.eof() && !p.is("]") {
		st := p.i
		if p.eat(",") {
			continue
		}
		p.eat("...")
		p.assign()
		if !p.listSeparator(open, st, "]") {
			break
		}
	}
	p.noIn = noIn
	p.eat("]")
}

// objectLiteral parses `{ ... }`. Its methods and properties become members
// of owner if set, or module exports if cjs is set (`module.exports = { ... }`).
func (p *parser) objectLiteral(owner *code.Decl, cjs bool) code.Span {
	start := p.tok().start
	open := p.i
	p.i++
	noIn := p.noIn
	p.noIn = false
	for !p.eof() && !p.is("}") {
		st := p.i
		p.property(owner, cjs)
		if !p.listSeparator(open, st, "}") {
			break
		}
	}
	p.noIn = noIn
	p.eat("}")
	return code.Span{Start: start, End: p.prevEnd()}
}

func (p *parser) property(owner *code.Decl, cjs bool) {
	start := p.tok().start
	if p.eat("...") {
		p.assign()
		return
	}
	accessor := false
	for t := p.tok(); p.isWord(t, "get") || p.isWord(t, "set") || p.isWord(t, "async"); t = p.tok() {
		n := p.peek(1)
		if !isPropKey(n) && !p.isOp(n, "[") && !p.isOp(n, "*") {
			break
		}
		accessor = accessor || !p.isText(t, "async")
		p.kw()
	}
	p.eat("*")
	t := p.tok()
	var name string
	var nameSpan code.Span
	switch {
	case p.isOp(t, "["):
		p.index()
	case isPropKey(t):
		name, nameSpan = p.propName()
	default:
		return
	}
	named := name != "" && (owner != nil || cjs)
	parent := owner
	if cjs {
		parent = p.parent
	}
	methodKind, propKind := code.KindMethod, code.KindProperty
	if cjs {
		methodKind, propKind = code.KindFunction, code.KindVariable
	}
	switch {
	case p.is("(") || p.is("<"):
		var d *code.Decl
		if named {
			kind := methodKind
			if accessor {
				kind = propKind
			}
			d = p.objectMember(parent, cjs, kind, name, nameSpan, start)
		}
		saved := p.thisDecl
		p.thisDecl = owner
		p.functionRest(d, fnMethod, nil, "")
		p.thisDecl = saved
		if d != nil {
			d.Span.End = p.prevEnd()
		}
	case p.is(":"):
		p.i++
		p.propertyValue(parent, cjs, named, name, nameSpan, start)
	default:
		p.shorthandProperty(t, owner, cjs, name)
	}
}

func (p *parser) objectMember(parent *code.Decl, cjs bool, kind code.DeclKind, name string, span code.Span, start int) *code.Decl {
	d := p.memberDecl(parent, kind, name, span, start)
	if cjs {
		p.pr.exports[name] = export{decl: d}
	}
	return d
}

func (p *parser) propertyValue(parent *code.Decl, cjs, named bool, name string, span code.Span, start int) {
	v := p.tok()
	if !named {
		p.assign()
		return
	}
	if cjs && isIdent(v) && (p.isOp(p.peek(1), ",") || p.isOp(p.peek(1), "}")) {
		p.i++
		p.useRef(v)
		p.pr.exports[name] = export{sc: p.sc, name: p.text(v)}
		return
	}
	kind := code.KindProperty
	if cjs {
		kind = code.KindVariable
	}
	isFn := p.functionAhead()
	if isFn {
		kind = code.KindMethod
		if cjs {
			kind = code.KindFunction
		}
	}
	d := p.objectMember(parent, cjs, kind, name, span, start)
	switch {
	case isFn:
		p.functionValue(d)
	case p.is("{") && !cjs:
		p.objectLiteral(d, false)
		p.continueExpr()
	default:
		if info := p.assign(); info.newText != "" {
			d.Type = info.newText
			p.pr.typeRef[d] = info.newRef
		}
	}
	d.Span.End = p.prevEnd()
}

func (p *parser) shorthandProperty(t token, owner *code.Decl, cjs bool, name string) {
	if !isIdent(t) {
		return
	}
	if r := p.useRef(t); r != nil && name != "" {
		if cjs {
			p.pr.exports[name] = export{sc: p.sc, name: name}
		} else if owner != nil {
			m := p.pr.shorthand[owner]
			if m == nil {
				m = map[string]*code.Ref{}
				p.pr.shorthand[owner] = m
			}
			m[name] = r
		}
	}
	if p.eat("=") {
		p.assign()
	}
}

// template parses a template literal at the current token, including the
// expressions (or types) embedded in it.
func (p *parser) template(typeMode bool) {
	t := p.tok()
	p.i++
	if t.sub != subTmplHead {
		return
	}
	for !p.eof() {
		if typeMode {
			p.typ()
		} else {
			p.expression()
		}
		t := p.tok()
		switch t.sub {
		case subTmplMiddle:
			p.i++
			continue
		case subTmplTail:
			p.i++
		}
		return
	}
}

func (p *parser) jsxElement() {
	p.i++
	if p.tok().sub == subJSXEnd {
		p.i++
		p.jsxChildren()
		return
	}
	p.jsxTagName(true)
	if p.ts && p.is("<") {
		p.typeArgs()
	}
	for !p.eof() {
		t := p.tok()
		switch {
		case t.sub == subJSXSelf:
			p.i++
			return
		case t.sub == subJSXEnd:
			p.i++
			p.jsxChildren()
			return
		case p.isOp(t, "{"):
			open := p.i
			p.i++
			p.eat("...")
			p.assign()
			if !p.is("}") {
				p.recover(open)
			}
			p.eat("}")
		case t.sub == subJSXName:
			p.i++
			if p.is(":") {
				p.i += 2
			}
			if p.eat("=") {
				p.jsxAttributeValue()
			}
		case t.sub == subJSXOpen, t.sub == subJSXClose:
			return
		default:
			p.i++
		}
	}
}

func (p *parser) jsxAttributeValue() {
	switch v := p.tok(); {
	case v.sub == subJSXString:
		p.i++
	case p.isOp(v, "{"):
		open := p.i
		p.i++
		p.assign()
		if !p.is("}") {
			p.recover(open)
		}
		p.eat("}")
	case v.sub == subJSXOpen:
		p.jsxElement()
	}
}

func (p *parser) jsxChildren() {
	for !p.eof() {
		t := p.tok()
		switch {
		case t.sub == subJSXOpen:
			p.jsxElement()
		case t.sub == subJSXClose:
			p.i++
			if p.tok().sub != subJSXEnd {
				p.jsxTagName(false)
			}
			for !p.eof() && p.tok().sub != subJSXEnd {
				p.i++
			}
			p.i++
			return
		case p.isOp(t, "{"):
			open := p.i
			p.i++
			if !p.is("}") {
				p.expression()
			}
			if !p.is("}") {
				p.recover(open)
			}
			p.eat("}")
		default:
			p.i++
		}
	}
}

// jsxTagName records references for component tags, which are capitalized
// names and member expressions. Lowercase intrinsic elements aren't
// references.
func (p *parser) jsxTagName(open bool) {
	first := p.tok()
	if first.sub != subJSXName {
		return
	}
	end := p.i + 1
	for p.isOp(p.at(end), ".") && p.at(end+1).sub == subJSXName {
		end += 2
	}
	if p.isOp(p.at(end), ":") {
		p.i = end + 2
		return
	}
	name := p.text(first)
	c := name[0]
	intrinsic := end == p.i+1 && (c >= 'a' && c <= 'z' || strings.Contains(name, "-"))
	if intrinsic {
		p.i = end
		return
	}
	var info exprInfo
	if name == "this" {
		info = exprInfo{this: true, path: true}
	} else {
		info = exprInfo{ref: p.useRef(first), path: true}
	}
	for i := p.i + 2; i < end; i += 2 {
		info = exprInfo{ref: p.memberRef(p.at(i), info, false), path: true}
	}
	if open && info.ref != nil {
		info.ref.Call = true
	}
	p.i = end
}

func (p *parser) arrowAhead() bool {
	i := p.i
	t := p.at(i)
	if p.isWord(t, "async") {
		n := p.at(i + 1)
		if !n.nl && isIdent(n) && p.isOp(p.at(i+2), "=>") {
			return true
		}
		if !n.nl && (p.isOp(n, "(") || p.isOp(n, "<")) {
			i++
			t = n
		}
	}
	switch {
	case isIdent(t):
		n := p.at(i + 1)
		return p.isOp(n, "=>") && !n.nl
	case p.isOp(t, "("):
		return p.arrowAfterParen(i)
	case p.ts && p.isOp(t, "<"):
		j := p.angleEnd(i)
		return j > 0 && p.isOp(p.at(j+1), "(") && p.arrowAfterParen(j+1)
	}
	return false
}

func (p *parser) arrowAfterParen(i int) bool {
	j := int(p.match[i])
	if j < 0 {
		return false
	}
	n := p.at(j + 1)
	if p.isOp(n, "=>") {
		return true
	}
	if !p.ts || !p.isOp(n, ":") {
		return false
	}
	for k, steps := j+2, 0; k < len(p.toks) && steps < 256; k, steps = k+1, steps+1 {
		t := p.at(k)
		if m := p.match[k]; m > 0 {
			k = int(m)
			continue
		}
		if t.kind != code.TokPunct || t.sub != subNone {
			continue
		}
		switch p.text(t) {
		case "=>":
			return true
		case ";", ",", ")", "]", "}", "=", "?", "{":
			return false
		}
	}
	return false
}

// typeArgsAhead reports whether the '<' at i starts the type arguments of a
// call or instantiation: f<T>(x), new C<T>(), tag<T>`...`.
func (p *parser) typeArgsAhead(i int) bool {
	j := p.angleEnd(i)
	if j < 0 {
		return false
	}
	n := p.at(j + 1)
	return p.isOp(n, "(") || (n.kind == code.TokString && (n.sub == subTmplFull || n.sub == subTmplHead))
}

// angleEnd returns the index of the token that closes the type argument or
// parameter list opened by '<' at i, or -1 if the tokens can't be one.
func (p *parser) angleEnd(i int) int {
	depth := 0
	for k := i; k < len(p.toks) && k < i+512; k++ {
		t := p.at(k)
		if m := p.match[k]; m > 0 {
			k = int(m)
			continue
		}
		switch t.kind {
		case code.TokIdent, code.TokKeyword, code.TokString, code.TokNumber:
			if t.sub != subNone && t.sub != subTmplFull {
				return -1
			}
			continue
		case code.TokPunct:
		default:
			return -1
		}
		if t.sub != subNone {
			return -1
		}
		switch s := p.text(t); s {
		case "<":
			depth++
		case ">", ">>", ">>>":
			depth -= len(s)
			if depth <= 0 {
				if depth < 0 {
					return -1
				}
				return k
			}
		case ".", ",", "|", "&", ":", "?", "=>", "-", "...", "*", "=":
		default:
			return -1
		}
	}
	return -1
}
