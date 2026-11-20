package php

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// stops lists the tokens that end an expression at the top level, on top
// of `;`, close tags and unmatched closing brackets, which always do.
type stops struct {
	comma, colon, arrow, as bool
}

// recv is the operand that a following `->` or `::` applies to.
type recv struct {
	kind recvKind
	cls  *code.Decl
	key  string
	at   int
}

// name is a possibly qualified name: `Foo`, `A\Foo`, `\A\Foo` or
// `namespace\Foo`.
type name struct {
	parts       []string
	first, last int // token indices
	fq, rel     bool
	text        string // as written
}

var builtinTypes = code.StringSet("int", "integer", "float", "double", "string", "bool", "boolean", "array", "callable",
	"iterable", "object", "mixed", "void", "null", "never", "false", "true", "static", "self", "parent", "resource")

// expr scans an expression. It records references, and declares the
// variables that assignments introduce.
func (p *parser) expr(st stops) {
	var r recv
	for p.p < len(p.t) {
		i := p.p
		x := p.text(i)
		t := p.t[i]
		switch {
		case x == ";" || x == ")" || x == "]" || x == "}" || p.isCloseTag(i):
			return
		case st.comma && x == ",", st.colon && x == ":", st.arrow && x == "=>", st.as && p.kw(i, "as"):
			return
		case x == "[" && p.destructuringAt(i):
			p.destructure()
			r = recv{}
		case x == "(" || x == "[" || x == "{":
			p.group()
			r = recv{}
		case x == "#" && p.is(i+1, "["):
			p.attributes()
		case p.isVar(i):
			r = p.variableOperand(i)
		case x == "->" || x == "?->":
			r = p.memberAccess(r)
		case x == "::":
			r = p.staticAccess(r)
		case t.kind == code.TokString:
			p.stringFields(t)
			p.p++
			r = recv{}
		case p.kw(i, "new"):
			r = p.newExpr()
		case p.kw(i, "function") || p.kw(i, "fn"):
			p.closure()
			r = recv{}
		case p.kw(i, "static") && (p.kw(i+1, "function") || p.kw(i+1, "fn")):
			p.p++
			p.closure()
			r = recv{}
		case p.kw(i, "instanceof"):
			p.p++
			if p.nameStart(p.p) {
				nm, _ := p.readName()
				p.classRef(nm, p.classKey(nm))
			}
			r = recv{}
		case p.kw(i, "list") && p.is(i+1, "(") && p.match[i+1] > 0 && p.is(p.match[i+1]+1, "="):
			p.p++
			p.destructure()
			r = recv{}
		case (p.kw(i, "self") || p.kw(i, "static") || p.kw(i, "parent")) && p.is(i+1, "::"):
			r = p.selfRecv(strings.ToLower(x))
			p.p++
		case p.nameStart(i):
			r = p.nameOperand()
		default:
			p.p++
			r = recv{}
		}
	}
}

// group scans the bracket group that opens at the cursor. Inside are
// expressions separated by commas or `=>`.
func (p *parser) group() {
	if p.depth > maxDepth {
		p.skipGroup()
		return
	}
	closer := map[string]string{"(": ")", "[": "]", "{": "}"}[p.text(p.p)]
	p.depth++
	defer func() { p.depth-- }()
	p.p++
	for p.p < len(p.t) {
		start := p.p
		p.expr(stops{})
		switch x := p.text(p.p); {
		case x == closer:
			p.p++
			return
		case x == ";" || x == ")" || x == "]" || x == "}" || p.isCloseTag(p.p):
			return
		}
		if p.p == start {
			p.p++
		}
	}
}

// destructuringAt reports whether the `[` at i starts an array
// destructuring. That's the case if it starts an operand and its `]` is
// followed by `=`.
func (p *parser) destructuringAt(i int) bool {
	if i > 0 {
		prev := p.t[i-1]
		if prev.kind == code.TokIdent || prev.kind == code.TokString || prev.kind == code.TokNumber {
			return false
		}
		if x := p.text(i - 1); x == ")" || x == "]" || x == "}" {
			return false
		}
	}
	end := p.match[i]
	return end > i && p.is(end+1, "=")
}

// variableOperand records the variable at token i. If an assignment
// introduces the variable, we declare it, else we record a reference to it.
func (p *parser) variableOperand(i int) recv {
	name := p.varName(i)
	p.p++
	if name == "this" {
		if p.cls != nil && p.cls.decl != nil {
			return recv{kind: recvClass, cls: p.cls.decl}
		}
		return recv{}
	}
	d := p.sc.lookup(name)
	if d == nil && !superGlobals[name] && p.assigns(i) {
		d = p.declare(code.KindVariable, name, i, code.Span{Start: p.t[i].s, End: p.stmtEnd(i)})
		if p.is(i+1, "=") && p.kw(i+2, "new") {
			if key := p.newKey(i + 3); key != "" {
				p.info.types[d] = key
			}
		}
		return recv{}
	}
	at := p.varRef(name, i, d)
	if p.is(p.p, "(") {
		p.info.refs[at].ref.Call = true
		p.group()
		return recv{}
	}
	return recv{kind: recvRef, at: at}
}

// assigns reports whether the variable at i is assigned to: `$x = `,
// `$x ??= ` or `$x[...] = `.
func (p *parser) assigns(i int) bool {
	j := i + 1
	if p.is(j, "??=") {
		return true
	}
	for p.is(j, "[") && p.match[j] > j {
		j = p.match[j] + 1
	}
	return p.is(j, "=")
}

// stmtEnd returns the end offset of the statement that token i belongs to,
// which is the last token before a `;`, close tag or unmatched closing
// bracket.
func (p *parser) stmtEnd(i int) int {
	end := p.t[i].e
	for k := i; k < len(p.t); k++ {
		x := p.text(k)
		if x == ";" || x == ")" || x == "]" || x == "}" || x == "," || p.isCloseTag(k) {
			break
		}
		if m := p.match[k]; m > k {
			k = m
		}
		end = p.t[min(k, len(p.t)-1)].e
	}
	return end
}

// newKey returns the key of the class instantiated by the `new` before
// token j. It doesn't record references.
func (p *parser) newKey(j int) string {
	switch {
	case p.kw(j, "self") || p.kw(j, "static"):
		if p.cls != nil {
			return p.cls.key
		}
	case p.kw(j, "parent"):
		if p.cls != nil {
			return p.cls.parentKey
		}
	case p.nameStart(j):
		save := p.p
		p.p = j
		nm, _ := p.readName()
		p.p = save
		return p.classKey(nm)
	}
	return ""
}

func (p *parser) selfRecv(word string) recv {
	if p.cls == nil {
		return recv{}
	}
	if word == "parent" {
		return recv{kind: recvKey, key: p.cls.parentKey}
	}
	if p.cls.decl == nil {
		return recv{}
	}
	return recv{kind: recvClass, cls: p.cls.decl}
}

func (p *parser) memberRef(nameSpan code.Span, kind memberKind, r recv) int {
	ref := p.f.NewRef(p.sc.refScope, p.f.Text(nameSpan), nameSpan)
	ref.Call = kind == memMethod
	ri := refInfo{ref: ref, kind: refMember, member: kind, recv: r.kind, cls: r.cls, key: r.key, recvAt: r.at}
	p.info.refs = append(p.info.refs, ri)
	return len(p.info.refs) - 1
}

// memberAccess parses `->name`, `->name(...)` and the dynamic forms.
func (p *parser) memberAccess(r recv) recv {
	p.p++
	at := p.p
	switch {
	case p.isName(at):
		return p.namedMember(at, memProp, r)
	case p.isVar(at):
		p.variableOperand(at)
	case p.is(at, "{"):
		p.group()
	}
	return recv{}
}

// staticAccess parses `::name()`, `::CONST`, `::$prop` and `::class`.
func (p *parser) staticAccess(r recv) recv {
	p.p++
	at := p.p
	switch {
	case p.kw(at, "class"):
		p.p++
	case p.isVar(at):
		p.p++
		idx := p.memberRef(p.nameSpan(at), memProp, r)
		return recv{kind: recvRef, at: idx}
	case p.isName(at):
		return p.namedMember(at, memConst, r)
	}
	return recv{}
}

// namedMember records the member named by token at and parses the call
// arguments. If a call follows, the member is a method, else it keeps kind.
func (p *parser) namedMember(at int, kind memberKind, r recv) recv {
	p.p = at + 1
	if p.is(p.p, "(") {
		kind = memMethod
	}
	idx := p.memberRef(p.nameSpan(at), kind, r)
	if kind == memMethod {
		p.group()
	}
	return recv{kind: recvRef, at: idx}
}

// newExpr handles `new Name(...)`, `new static`, `new class (...) {...}` and
// `new $class`.
func (p *parser) newExpr() recv {
	p.p++
	var r recv
	switch {
	case p.kw(p.p, "class"):
		p.anonClass()
		return recv{}
	case p.kw(p.p, "self") || p.kw(p.p, "static") || p.kw(p.p, "parent"):
		if key := p.newKey(p.p); key != "" {
			r = recv{kind: recvKey, key: key}
		}
		p.p++
	case p.nameStart(p.p):
		nm, _ := p.readName()
		key := p.classKey(nm)
		p.info.refs[p.classRef(nm, key)].ref.Call = true
		r = recv{kind: recvKey, key: key}
	default:
		return recv{}
	}
	if p.is(p.p, "(") {
		p.group()
	}
	return r
}

// anonClass parses `class (args) extends A implements B { members }` after
// `new`. Its members don't get declarations of their own.
func (p *parser) anonClass() {
	p.p++
	if p.is(p.p, "(") {
		p.group()
	}
	cc := &classCtx{}
	p.classHeader(cc)
	if p.is(p.p, "{") {
		outer := p.cls
		p.cls = cc
		p.classBody(cc)
		p.cls = outer
	}
}

// closure handles `function (...) use (...) { }` and `fn (...) => expr`.
func (p *parser) closure() {
	arrow := p.kw(p.p, "fn")
	p.p++
	p.consume("&")
	fs := fnScope{owner: p.sc.owner, refScope: p.sc.refScope, local: true, closure: !arrow, arrow: arrow}
	if arrow {
		fs.parent = p.sc
	}
	p.fnRest(fs)
}

func (p *parser) stringFields(t tok) {
	if p.depth > maxDepth {
		return
	}
	for _, fd := range t.fields {
		if fd.varName {
			name := string(p.f.Src[fd.s:fd.e])
			ref := p.f.NewRef(p.sc.refScope, name, code.Span{Start: fd.s, End: fd.e})
			p.info.refs = append(p.info.refs, refInfo{ref: ref, kind: refVar, decl: p.sc.lookup(name)})
			continue
		}
		sub := lex(p.f.Src, fd.s, fd.e, true)
		var toks []tok
		for _, st := range sub.toks {
			if st.kind != code.TokComment {
				toks = append(toks, st)
			}
		}
		saveT, saveM, saveP := p.t, p.match, p.p
		p.t, p.match, p.p = toks, matchBrackets(p.f.Src, toks), 0
		p.depth++
		for p.p < len(p.t) {
			start := p.p
			p.expr(stops{})
			if p.p == start {
				p.p++
			}
		}
		p.depth--
		p.t, p.match, p.p = saveT, saveM, saveP
	}
}

func (p *parser) nameStart(i int) bool {
	switch {
	case p.is(i, "\\"):
		return p.nameTok(i+1) && p.t[i].e == p.t[i+1].s
	case p.kw(i, "namespace"):
		return p.is(i+1, "\\")
	}
	return p.isName(i)
}

// nameTok reports whether token i can be a segment of a qualified name.
// Segments are allowed to be reserved words.
func (p *parser) nameTok(i int) bool {
	return p.isName(i) || i < len(p.t) && i >= 0 && p.t[i].kind == code.TokKeyword && isIdentStart(p.f.Src[p.t[i].s])
}

func (p *parser) typeStart(i int) bool {
	if p.is(i, "?") || p.is(i, "(") && p.nameStart(i+1) || p.nameStart(i) {
		return true
	}
	return i < len(p.t) && p.t[i].kind == code.TokKeyword && builtinTypes[strings.ToLower(p.text(i))]
}

func (p *parser) readName() (name, bool) {
	i := p.p
	n := name{first: i}
	switch {
	case p.is(i, "\\"):
		n.fq = true
		i++
	case p.kw(i, "namespace") && p.is(i+1, "\\"):
		n.rel = true
		i += 2
	}
	if !p.nameTok(i) {
		return name{}, false
	}
	n.parts = append(n.parts, p.text(i))
	i++
	for p.is(i, "\\") && p.nameTok(i+1) && p.t[i-1].e == p.t[i].s && p.t[i].e == p.t[i+1].s {
		n.parts = append(n.parts, p.text(i+1))
		i += 2
	}
	n.last = i - 1
	n.text = p.compact(n.first, i)
	p.p = i
	return n, true
}

// classKey resolves a class name against the namespace and the imports.
func (p *parser) classKey(n name) string {
	if len(n.parts) == 0 {
		return ""
	}
	joined := strings.Join(n.parts, "\\")
	switch {
	case n.fq:
		return strings.ToLower(joined)
	case n.rel:
		return strings.ToLower(qualify(p.ns.name, joined))
	}
	first := strings.ToLower(n.parts[0])
	if len(n.parts) == 1 && builtinTypes[first] {
		switch first {
		case "self", "static":
			if p.cls != nil {
				return p.cls.key
			}
		case "parent":
			if p.cls != nil {
				return p.cls.parentKey
			}
		}
		return ""
	}
	if e, ok := p.ns.classes[first]; ok {
		return strings.ToLower(strings.Join(append([]string{e.full}, n.parts[1:]...), "\\"))
	}
	return strings.ToLower(qualify(p.ns.name, joined))
}

// classRef records a reference to the class name n and returns its index.
// If an unqualified name is an imported alias, the reference targets the
// import.
func (p *parser) classRef(n name, key string) int {
	ref := p.f.NewRef(p.sc.refScope, n.parts[len(n.parts)-1], p.span(n.last, n.last))
	ri := refInfo{ref: ref, kind: refClass}
	if key != "" {
		ri.keys = []string{key}
	}
	if len(n.parts) == 1 && !n.fq && !n.rel {
		ri.decl = p.ns.classes[strings.ToLower(n.parts[0])].alias
	}
	p.info.refs = append(p.info.refs, ri)
	return len(p.info.refs) - 1
}

// funcKeys returns the candidate keys for a called function name. If the
// name is unqualified and not imported, we fall back to the global function.
func (p *parser) funcKeys(n name) ([]string, *code.Decl) {
	if n.fq || n.rel || len(n.parts) > 1 {
		return []string{p.classKey(n)}, nil
	}
	lower := strings.ToLower(n.parts[0])
	if e, ok := p.ns.funcs[lower]; ok {
		return []string{strings.ToLower(e.full)}, e.alias
	}
	if p.ns.name == "" {
		return []string{lower}, nil
	}
	return []string{strings.ToLower(p.ns.name) + "\\" + lower, lower}, nil
}

// constKeys returns the candidate keys for a constant name. The fallback
// to the global constant works the same as for functions.
func (p *parser) constKeys(n name) ([]string, *code.Decl) {
	joined := strings.Join(n.parts, "\\")
	switch {
	case n.fq:
		return []string{constKey(joined)}, nil
	case n.rel:
		return []string{constKey(qualify(p.ns.name, joined))}, nil
	case len(n.parts) > 1:
		if e, ok := p.ns.classes[strings.ToLower(n.parts[0])]; ok {
			return []string{constKey(strings.Join(append([]string{e.full}, n.parts[1:]...), "\\"))}, nil
		}
		return []string{constKey(qualify(p.ns.name, joined))}, nil
	}
	if e, ok := p.ns.consts[joined]; ok {
		return []string{constKey(e.full)}, e.alias
	}
	if p.ns.name == "" {
		return []string{joined}, nil
	}
	return []string{constKey(qualify(p.ns.name, joined)), joined}, nil
}

// nameOperand parses a class name before `::`, a function call or a constant
// in an expression.
func (p *parser) nameOperand() recv {
	before := p.text(p.p - 1)
	nm, ok := p.readName()
	if !ok {
		p.p++
		return recv{}
	}
	lower := strings.ToLower(nm.text)
	single := len(nm.parts) == 1 && !nm.fq && !nm.rel
	switch next := p.text(p.p); {
	case next == "::":
		key := p.classKey(nm)
		p.classRef(nm, key)
		return recv{kind: recvKey, key: key}
	case next == "(":
		if single && lower == "define" {
			p.define()
		}
		keys, alias := p.funcKeys(nm)
		ref := p.f.NewRef(p.sc.refScope, nm.parts[len(nm.parts)-1], p.span(nm.last, nm.last))
		ref.Call = true
		p.info.refs = append(p.info.refs, refInfo{ref: ref, kind: refFunc, keys: keys, decl: alias})
		at := len(p.info.refs) - 1
		if single && lower == "compact" {
			p.compactRefs()
		}
		p.group()
		return recv{kind: recvRef, at: at}
	case single && p.t[nm.last].kind == code.TokKeyword:
		return recv{}
	case single && next == ":" && (before == "(" || before == ","):
		return recv{} // named argument
	case single && lower == "from" && strings.EqualFold(before, "yield"):
		return recv{}
	}
	keys, alias := p.constKeys(nm)
	ref := p.f.NewRef(p.sc.refScope, nm.parts[len(nm.parts)-1], p.span(nm.last, nm.last))
	p.info.refs = append(p.info.refs, refInfo{ref: ref, kind: refConst, keys: keys, decl: alias})
	return recv{}
}

// define declares the global constant of `define('NAME', value)`. The
// cursor has to be on the parenthesis.
func (p *parser) define() {
	at := p.p + 1
	if at >= len(p.t) || p.t[at].kind != code.TokString || !p.is(at+1, ",") {
		return
	}
	lit := p.text(at)
	if len(lit) < 2 || lit[0] != '\'' && lit[0] != '"' {
		return
	}
	full := lit[1 : len(lit)-1]
	if !validName(full) {
		return
	}
	full = strings.TrimPrefix(full, "\\")
	short := full[strings.LastIndexByte(full, '\\')+1:]
	end := p.t[at].e - 1
	d := p.f.NewDecl(p.f.Module, code.KindConstant, short, code.Span{Start: end - len(short), End: end}, code.Span{Start: p.t[p.p-1].s, End: p.stmtEnd(p.p - 1)})
	d.Local = false
	p.info.keys[d] = constKey(full)
}

// validName reports whether s is a PHP name, qualified or not.
func validName(s string) bool {
	s = strings.TrimPrefix(s, "\\")
	for part := range strings.SplitSeq(s, "\\") {
		if part == "" || !isIdentStart(part[0]) {
			return false
		}
		for i := range len(part) {
			if !isIdentChar(part[i]) {
				return false
			}
		}
	}
	return s != ""
}

// compactRefs records the variables named by `compact('a', 'b')`. The
// cursor has to be on the parenthesis.
func (p *parser) compactRefs() {
	end := p.match[p.p]
	for k := p.p + 1; k < end; k++ {
		lit := p.text(k)
		if p.t[k].kind != code.TokString || len(lit) < 3 || lit[0] != '\'' && lit[0] != '"' {
			continue
		}
		name := lit[1 : len(lit)-1]
		if !validName(name) || strings.Contains(name, "\\") {
			continue
		}
		span := code.Span{Start: p.t[k].s + 1, End: p.t[k].e - 1}
		ref := p.f.NewRef(p.sc.refScope, name, span)
		p.info.refs = append(p.info.refs, refInfo{ref: ref, kind: refVar, decl: p.sc.lookup(name)})
	}
}

// typ parses a type at the cursor and returns it as written, plus the class
// key if the type names exactly a single class. Class names in the type
// become references.
func (p *parser) typ() (string, string) {
	start := p.p
	var keys []string
	open := 0
	for p.p < len(p.t) {
		i := p.p
		switch {
		case p.is(i, "?") || p.is(i, "|"):
			p.p++
		case p.is(i, "&") && !p.isVar(i+1) && !p.is(i+1, "...") && !p.is(i+1, "&"):
			p.p++
		case p.is(i, "(") && p.nameStart(i+1):
			open++
			p.p++
		case p.is(i, ")") && open > 0:
			open--
			p.p++
		case p.t[i].kind == code.TokKeyword && builtinTypes[strings.ToLower(p.text(i))] && !p.is(i+1, "\\"):
			if key := p.classKey(name{parts: []string{p.text(i)}}); key != "" {
				keys = append(keys, key)
			}
			p.p++
		case p.nameStart(i):
			nm, _ := p.readName()
			if len(nm.parts) == 1 && !nm.fq && builtinTypes[strings.ToLower(nm.parts[0])] {
				if lower := strings.ToLower(nm.parts[0]); lower != "null" && lower != "false" && lower != "true" {
					keys = append(keys, "")
				}
				continue
			}
			key := p.classKey(nm)
			p.classRef(nm, key)
			keys = append(keys, key)
		default:
			return p.typeResult(start, keys)
		}
	}
	return p.typeResult(start, keys)
}

func (p *parser) typeResult(start int, keys []string) (string, string) {
	if p.p == start {
		return "", ""
	}
	key := ""
	if len(keys) == 1 {
		key = keys[0]
	}
	return p.compact(start, p.p), key
}
