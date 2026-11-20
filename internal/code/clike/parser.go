package clike

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// maxDepth bounds parser recursion on pathological nesting. We skip deeper
// groups and don't record references in them.
const maxDepth = 400

type parser struct {
	f     *code.File
	src   string
	cs    bool
	toks  []tok
	match []int // index of the matching bracket, -1 if unmatched
	p     int
	eof   tok
	info  *fileInfo

	parent *code.Decl // container of new declarations
	scope  *code.Decl // innermost declaration with a body, for Ref.Scope
	ctx    *code.Decl // innermost declaration around the cursor
	header *code.Decl
	anon   *anonCtx
	exec   int // >0 inside executable code, where declarations are local

	binds []binding // lexically visible locals, innermost last
	marks []int     // start of each lexical frame in binds

	inCase bool // parsing a switch case label
	// Inside C# set/init/add/remove accessors `value` is an implicit
	// parameter.
	setter bool
	// type of the property whose accessor is being parsed
	valueType string
	depth     int
}

type binding struct {
	name string
	d    *code.Decl
}

type stops struct {
	comma   bool // stop at ',' (argument and initializer lists)
	colon   bool // stop at a ':' that doesn't close a conditional
	arrow   bool // stop at '->' / '=>' (switch labels and arms)
	pattern bool // stop at the pattern words and/or/when
	query   bool // stop at LINQ clause keywords
}

func parse(f *code.File) *fileInfo {
	src := string(f.Src)
	p := &parser{
		f:    f,
		src:  src,
		cs:   f.Lang == code.CSharp,
		info: &fileInfo{refs: map[*code.Ref]*refInfo{}, arity: map[*code.Decl]arity{}, hidden: map[*code.Decl]bool{}},
	}
	p.toks = lex(f, src, p.cs)
	p.match = matchBrackets(p.toks)
	p.eof = tok{s: len(src), e: len(src), ft: -1}
	if p.cs {
		p.csUnit()
	} else {
		p.javaUnit()
	}
	return p.info
}

// matchBrackets returns the index of each bracket's partner, or -1 if there's
// none. An unclosed parenthesis or bracket gets -(j+2), where j is the next
// ';', '{' or '}' outside nested groups.
func matchBrackets(toks []tok) []int {
	m := make([]int, len(toks))
	var stack []int
	for i := range toks {
		m[i] = -1
		if toks[i].kind != code.TokPunct {
			continue
		}
		var open string
		switch toks[i].text {
		case "(", "[", "{":
			stack = append(stack, i)
			continue
		case ")":
			open = "("
		case "]":
			open = "["
		case "}":
			open = "{"
		default:
			continue
		}
		for k := len(stack) - 1; k >= 0 && k >= len(stack)-32; k-- {
			if toks[stack[k]].text == open {
				m[stack[k]] = i
				m[i] = stack[k]
				stack = stack[:k]
				break
			}
		}
	}
	next := make([]int, len(toks)+1)
	next[len(toks)] = len(toks)
	for j := len(toks) - 1; j >= 0; j-- {
		t := toks[j]
		switch {
		case t.kind == code.TokPunct && (t.text == ";" || t.text == "{" || t.text == "}"):
			next[j] = j
		case m[j] > j:
			next[j] = next[m[j]+1]
		default:
			next[j] = next[j+1]
		}
		if m[j] == -1 && t.kind == code.TokPunct && (t.text == "(" || t.text == "[") {
			m[j] = -(next[j+1] + 2)
		}
	}
	return m
}

func (p *parser) at(i int) *tok {
	if i >= 0 && i < len(p.toks) {
		return &p.toks[i]
	}
	return &p.eof
}

func (p *parser) cur() *tok          { return p.at(p.p) }
func (p *parser) peek(n int) *tok    { return p.at(p.p + n) }
func (p *parser) is(s string) bool   { return p.at(p.p).text == s && p.at(p.p).kind != 0 }
func (p *parser) isIdent(i int) bool { return p.at(i).kind == code.TokIdent }

// isWord reports whether token i is the contextual keyword w.
func (p *parser) isWord(i int, w string) bool {
	t := p.at(i)
	return t.kind == code.TokIdent && t.text == w
}

func (p *parser) accept(s string) bool {
	if p.is(s) {
		p.p++
		return true
	}
	return false
}

// keyword reclassifies a contextual keyword in File.Tokens.
func (p *parser) keyword(i int) {
	if t := p.at(i); t.ft >= 0 && t.kind == code.TokIdent {
		p.f.Tokens[t.ft].Kind = code.TokKeyword
	}
}

// closeOf returns the index of the bracket closing the opener at i. An
// unclosed parenthesis or bracket ends at the next ';', '{' or '}' outside
// nested groups. An unclosed brace runs to the end of the token stream.
func (p *parser) closeOf(i int) int {
	if i >= 0 && i < len(p.match) {
		if m := p.match[i]; m > i {
			return m
		} else if m <= -2 {
			return -m - 2
		}
	}
	return len(p.toks)
}

// past returns the index after the group from open to close. That's past the
// closing bracket, or at the token that ended an unclosed group, since that
// token belongs to the enclosing construct.
func (p *parser) past(open, close int) int {
	if close < len(p.toks) && open >= 0 && open < len(p.match) && p.match[open] == close {
		return close + 1
	}
	return min(close, len(p.toks))
}

// endOff returns the end offset of token i, clamped to the source.
func (p *parser) endOff(i int) int {
	if i >= len(p.toks) {
		return len(p.src)
	}
	return p.at(i).e
}

func (p *parser) prevEnd() int {
	if p.p == 0 {
		return 0
	}
	return p.at(min(p.p, len(p.toks)) - 1).e
}

// seq parses the comma-separated items of the group opened at open, moves
// past it and returns how many items there were.
func (p *parser) seq(open int, item func()) int {
	close := p.closeOf(open)
	p.p = max(p.p, open+1)
	n := 0
	expect := true
	for p.p < close {
		if p.is(",") {
			p.p++
			expect = true
			continue
		}
		start := p.p
		item()
		if expect {
			n++
			expect = false
		}
		if p.p == start {
			// item stopped at a stray token, so skip it.
			p.p++
		}
	}
	if p.p <= close {
		p.p = p.past(open, close)
	}
	return n
}

// arityAt counts the arguments of the group opened at i.
func (p *parser) arityAt(i int) int {
	close := p.closeOf(i)
	if close >= len(p.toks) {
		return -1
	}
	if close == i+1 {
		return 0
	}
	n := 1
	for j := i + 1; j < close; j++ {
		if m := p.match[j]; m > j {
			j = m
			continue
		}
		if p.toks[j].text == "," && p.toks[j].kind == code.TokPunct {
			n++
		}
	}
	return n
}

func (p *parser) push() { p.marks = append(p.marks, len(p.binds)) }

func (p *parser) pop() {
	if n := len(p.marks); n > 0 {
		p.binds = p.binds[:p.marks[n-1]]
		p.marks = p.marks[:n-1]
	}
}

func (p *parser) bind(d *code.Decl) {
	if d != nil && d.Name != "" {
		p.binds = append(p.binds, binding{d.Name, d})
	}
}

func (p *parser) lookupLocal(name string, w want) *code.Decl {
	for _, b := range slices.Backward(p.binds) {
		if b.name != name {
			continue
		}
		switch {
		case w == wantType && !b.d.IsType():
			continue
		case w == wantCall && !p.cs && !b.d.Callable():
			// Java keeps methods and variables in separate namespaces.
			continue
		}
		return b.d
	}
	return nil
}

// nameOf returns the identifier of t and its span. We drop the '@' of C#
// verbatim identifiers and decode Java unicode escapes.
func (p *parser) nameOf(t *tok) (string, code.Span) {
	if t.at {
		return t.text[1:], code.Span{Start: t.s + 1, End: t.e}
	}
	if !p.cs {
		return decodeEscapes(t.text), code.Span{Start: t.s, End: t.e}
	}
	return t.text, code.Span{Start: t.s, End: t.e}
}

func (p *parser) newRef(i int, w want, q qual) *code.Ref {
	name, span := p.nameOf(p.at(i))
	r := p.f.NewRef(p.scope, name, span)
	ri := &refInfo{ctx: p.ctx, header: p.header, anon: p.anon, qual: q, want: w, arity: -1}
	if q.kind == qualNone {
		ri.local = p.lookupLocal(name, w)
		ri.caseLabel = p.inCase
	}
	r.Call = w == wantCall
	p.info.refs[r] = ri
	return r
}

// importRef records an identifier of an import or using directive.
func (p *parser) importRef(i int) *code.Ref {
	name, span := p.nameOf(p.at(i))
	r := p.f.NewRef(p.scope, name, span)
	r.Import = true
	p.info.refs[r] = &refInfo{ctx: p.ctx, want: wantType, arity: -1, imp: true}
	return r
}

func (p *parser) newDecl(parent *code.Decl, kind code.DeclKind, i, start int) *code.Decl {
	name, span := p.nameOf(p.at(i))
	return p.newDeclSpan(parent, kind, name, span, start)
}

func (p *parser) newDeclSpan(parent *code.Decl, kind code.DeclKind, name string, nameSpan code.Span, start int) *code.Decl {
	d := p.f.NewDecl(parent, kind, name, nameSpan, code.Span{Start: start, End: nameSpan.End})
	if p.exec > 0 || kind == code.KindParameter {
		d.Local = true
	}
	if p.exec > 0 && parent != nil && parent.IsType() &&
		(kind == code.KindVariable || kind == code.KindParameter || kind == code.KindConstant) {
		p.info.hidden[d] = true
	}
	return d
}

// local declares a pattern, out or query variable in the current frame.
func (p *parser) local(i, start int, typ string) *code.Decl {
	d := p.newDecl(p.parent, code.KindVariable, i, start)
	d.Local = true
	d.Type = typ
	p.bind(d)
	return d
}

type saved struct {
	parent, scope, ctx, header *code.Decl
	refs                       int
}

// enter makes d the container and context for what follows. With withScope
// it also becomes the Ref.Scope.
func (p *parser) enter(d *code.Decl, withScope bool) saved {
	s := saved{p.parent, p.scope, p.ctx, p.header, len(p.f.Refs)}
	p.parent, p.ctx, p.header = d, d, nil
	if withScope {
		p.scope = d
	}
	return s
}

// leave restores the state saved by enter. If d turned out to have no body,
// references scoped to d move to the outer scope.
func (p *parser) leave(d *code.Decl, s saved) {
	if d != nil && d.Body.Empty() && p.scope == d {
		for _, r := range p.f.Refs[s.refs:] {
			if r.Scope == d {
				r.Scope = s.scope
			}
		}
	}
	p.parent, p.scope, p.ctx, p.header = s.parent, s.scope, s.ctx, s.header
}

var javaPrims = code.StringSet("boolean", "byte", "char", "short", "int", "long", "float", "double", "void")
var csPrims = code.StringSet("bool", "byte", "sbyte", "char", "short", "ushort", "int", "uint", "long", "ulong",
	"float", "double", "decimal", "string", "object", "void", "nint", "nuint", "dynamic")

func (p *parser) isPrim(t *tok) bool {
	if t.kind != code.TokKeyword && t.kind != code.TokIdent {
		return false
	}
	if p.cs {
		return csPrims[t.text] && (t.kind == code.TokKeyword || t.text == "nint" || t.text == "nuint" || t.text == "dynamic")
	}
	return javaPrims[t.text] && t.kind == code.TokKeyword
}

func (p *parser) skipAnnotation(i int) int {
	i++
	for p.isIdent(i) {
		i++
		if p.at(i).text == "." && p.isIdent(i+1) {
			i++
			continue
		}
		break
	}
	if p.at(i).text == "(" {
		i = p.past(i, p.closeOf(i))
	}
	return i
}

func (p *parser) isAnnotation(i int) bool {
	return !p.cs && p.at(i).text == "@" && p.isIdent(i+1)
}

// scanType reports where a type starting at token i ends, but doesn't emit
// anything.
func (p *parser) scanType(i int) (int, bool) { return p.scanTypeD(i, 0) }

func (p *parser) scanTypeD(i, depth int) (int, bool) {
	if depth > 64 {
		return i, false
	}
	for p.isAnnotation(i) {
		i = p.skipAnnotation(i)
	}
	t := p.at(i)
	switch {
	case p.cs && t.text == "(" && t.kind == code.TokPunct:
		j, ok := p.scanTupleType(i, depth)
		if !ok {
			return i, false
		}
		i = j
	case p.isPrim(t):
		i++
	case t.kind == code.TokIdent:
		i++
		if p.cs && p.at(i).text == "::" && p.isIdent(i+1) {
			i += 2
		}
		for {
			if p.at(i).text == "<" {
				j, ok := p.scanTypeArgs(i, depth)
				if !ok {
					return i, true
				}
				i = j
			}
			if p.at(i).text == "." && (p.isIdent(i+1) || p.isAnnotation(i+1)) {
				i++
				for p.isAnnotation(i) {
					i = p.skipAnnotation(i)
				}
				if !p.isIdent(i) {
					return i, false
				}
				i++
				continue
			}
			break
		}
	default:
		return i, false
	}
	for {
		for p.isAnnotation(i) {
			i = p.skipAnnotation(i)
		}
		switch p.at(i).text {
		case "[":
			j := i + 1
			for p.cs && p.at(j).text == "," {
				j++
			}
			if p.at(j).text != "]" {
				return i, true
			}
			i = j + 1
		case "?", "*":
			if !p.cs {
				return i, true
			}
			i++
		default:
			return i, true
		}
	}
}

func (p *parser) scanTupleType(i, depth int) (int, bool) {
	close := p.closeOf(i)
	if close >= len(p.toks) {
		return i, false
	}
	j, n := i+1, 0
	for j < close {
		k, ok := p.scanTypeD(j, depth+1)
		if !ok {
			return i, false
		}
		if p.isIdent(k) {
			k++
		}
		n++
		if p.at(k).text == "," {
			j = k + 1
			continue
		}
		if k != close {
			return i, false
		}
		j = k
	}
	if n < 2 {
		return i, false
	}
	return close + 1, true
}

// scanTypeArgs scans a type argument list `<...>` opened at i.
func (p *parser) scanTypeArgs(i, depth int) (int, bool) {
	j := i + 1
	if p.at(j).text == ">" {
		return j + 1, true
	}
	for {
		if p.cs {
			for p.at(j).text == "," {
				j++
			}
			if p.at(j).text == ">" {
				return j + 1, true
			}
		}
		if !p.cs && p.at(j).text == "?" {
			j++
			if w := p.at(j).text; w == "extends" || w == "super" {
				k, ok := p.scanTypeD(j+1, depth+1)
				if !ok {
					return i, false
				}
				j = k
			}
		} else {
			k, ok := p.scanTypeD(j, depth+1)
			if !ok {
				return i, false
			}
			j = k
		}
		switch p.at(j).text {
		case ",":
			j++
		case ">":
			return j + 1, true
		default:
			return i, false
		}
	}
}

// typeText renders tokens [start, end) as type text, without annotations.
func (p *parser) typeText(start, end int) string {
	var b strings.Builder
	prev := ""
	prevWord := false
	for i := start; i < end; i++ {
		if p.isAnnotation(i) {
			i = p.skipAnnotation(i) - 1
			continue
		}
		t := p.at(i)
		word := t.kind == code.TokIdent || t.kind == code.TokKeyword
		spaced := t.text == "extends" || t.text == "super" || t.text == "&"
		if b.Len() > 0 && ((word && prevWord) || spaced || prev == "&") {
			b.WriteByte(' ')
		}
		if t.at {
			b.WriteString(t.text[1:])
		} else {
			b.WriteString(t.text)
		}
		prev, prevWord = t.text, word
	}
	return b.String()
}

// emitType records references for the type in tokens [start, end). It
// returns the type text and the reference to its main name, the last segment
// outside type arguments.
func (p *parser) emitType(start, end int) (string, *code.Ref) {
	var head, prev *code.Ref
	angle, paren := 0, 0
	for i := start; i < end; i++ {
		if p.isAnnotation(i) {
			j := p.skipAnnotation(i)
			save := p.p
			p.p = i
			p.annotation()
			p.p = save
			i = j - 1
			prev = nil
			continue
		}
		t := p.at(i)
		switch {
		case t.kind == code.TokIdent:
			if p.cs && t.text == "global" && p.at(i+1).text == "::" {
				p.keyword(i)
				prev = nil
				continue
			}
			if paren > 0 && i > start {
				// C# tuple element names follow their type.
				if pt := p.at(i - 1); pt.kind == code.TokIdent || p.isPrim(pt) || pt.text == ">" || pt.text == "]" || pt.text == "?" {
					prev = nil
					continue
				}
			}
			q := qual{}
			if prev != nil && i > start && (p.at(i-1).text == "." || p.at(i-1).text == "::") {
				q = qual{kind: qualRef, ref: prev}
			}
			prev = p.newRef(i, wantType, q)
			if angle == 0 && paren == 0 {
				head = prev
			}
			continue
		case t.text == "<":
			angle++
		case t.text == ">":
			angle--
		case t.text == "(":
			paren++
		case t.text == ")":
			paren--
		case t.text == "." || t.text == "::":
			continue
		case p.isPrim(t):
			if t.kind == code.TokIdent {
				p.keyword(i)
			}
		}
		prev = nil
	}
	return p.typeText(start, end), head
}

// annotation parses a Java annotation at the cursor.
func (p *parser) annotation() {
	p.p++
	var last *code.Ref
	for p.isIdent(p.p) {
		q := qual{}
		if last != nil {
			q = qual{kind: qualRef, ref: last}
		}
		last = p.newRef(p.p, wantType, q)
		p.p++
		if p.is(".") && p.isIdent(p.p+1) {
			p.p++
			continue
		}
		break
	}
	if p.is("(") {
		p.seq(p.p, func() {
			if p.isIdent(p.p) && p.peek(1).text == "=" && last != nil {
				p.newRef(p.p, wantValue, qual{kind: qualRef, ref: last})
				p.p += 2
			}
			p.expr(stops{comma: true})
		})
	}
}

// attributes parses a C# attribute section at the cursor.
func (p *parser) attributes() {
	open := p.p
	p.p++
	if p.isIdent(p.p) && p.peek(1).text == ":" || p.cur().kind == code.TokKeyword && p.peek(1).text == ":" {
		p.keyword(p.p)
		p.p += 2
	}
	p.seq(open, func() {
		if !p.isIdent(p.p) {
			p.expr(stops{comma: true})
			return
		}
		var last *code.Ref
		for p.isIdent(p.p) {
			q := qual{}
			if last != nil {
				q = qual{kind: qualRef, ref: last}
			}
			last = p.newRef(p.p, wantType, q)
			p.info.refs[last].attr = true
			p.p++
			if p.is("<") {
				if j, ok := p.scanTypeArgs(p.p, 0); ok {
					p.emitType(p.p, j)
					p.p = j
				}
			}
			if (p.is(".") || p.is("::")) && p.isIdent(p.p+1) {
				p.p++
				continue
			}
			break
		}
		if p.is("(") {
			p.seq(p.p, func() {
				switch {
				case p.isIdent(p.p) && p.peek(1).text == "=":
					p.newRef(p.p, wantValue, qual{kind: qualRef, ref: last})
					p.p += 2
				case p.isIdent(p.p) && p.peek(1).text == ":":
					p.p += 2
				}
				p.expr(stops{comma: true})
			})
		}
	})
}

// genericFollows reports whether t can follow the type arguments of a name in
// an expression. We need this to tell `F<T>(x)` from comparisons.
func (p *parser) genericFollows(t *tok) bool {
	switch t.text {
	case "(", ")", ".", "::", "]", "}", ";", ",", "?.", "[":
		return t.kind == code.TokPunct
	}
	return false
}

// expr scans an expression up to a closing bracket, ';' or a stop token at
// the same nesting level, and records references and local declarations.
func (p *parser) expr(st stops) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.skipExpr(st)
		return
	}
	var prev qual
	dot, pendingQ := false, 0
	for p.p < len(p.toks) {
		t := p.cur()
		switch t.kind {
		case code.TokPunct:
			if p.punctExpr(st, &prev, &dot, &pendingQ) {
				return
			}
		case code.TokString:
			if len(t.holes) > 0 {
				p.holes(t)
			}
			p.p++
			prev, dot = qual{kind: qualExternal}, false
		case code.TokNumber:
			p.p++
			prev, dot = qual{kind: qualExternal}, false
		case code.TokKeyword:
			if !p.keywordExpr(st, &prev, &dot) {
				p.p++
				prev, dot = qual{}, false
			}
		case code.TokIdent:
			if p.identExpr(st, &prev, &dot) {
				return
			}
		default:
			p.p++
		}
	}
}

func (p *parser) punctExpr(st stops, prev *qual, dot *bool, pendingQ *int) bool {
	switch p.cur().text {
	case ")", "]", "}", ";":
		return true
	case ",":
		if st.comma {
			return true
		}
	case ":":
		if *pendingQ > 0 {
			*pendingQ--
		} else if st.colon {
			return true
		}
	case "=>", "->":
		if st.arrow {
			return true
		}
	case "?":
		if p.peek(1).text != "[" {
			*pendingQ++
		}
	case ".", "?.", "::":
		*dot = true
		p.p++
		return false
	case "!":
		if n := p.peek(1).text; p.cs && (n == "." || n == "?." || n == "[") {
			p.p++
			return false
		}
	case "(":
		if !p.lambdaParens(st) {
			p.group()
		}
		*prev, *dot = qual{kind: qualOpaque}, false
		return false
	case "[":
		p.group()
		*prev, *dot = qual{kind: qualOpaque}, false
		return false
	case "{":
		p.initializer(qual{}, false)
		*prev, *dot = qual{kind: qualOpaque}, false
		return false
	case "<":
		if *dot {
			if j, ok := p.scanTypeArgs(p.p, 0); ok {
				p.emitType(p.p, j)
				p.p = j
				return false
			}
		}
	case "@":
		if p.isAnnotation(p.p) {
			p.annotation()
			return false
		}
	}
	p.p++
	*prev, *dot = qual{}, false
	return false
}

// skipExpr skips an expression and doesn't record anything.
func (p *parser) skipExpr(st stops) {
	for p.p < len(p.toks) {
		t := p.cur()
		switch t.text {
		case ")", "]", "}", ";":
			return
		case ",":
			if st.comma {
				return
			}
		case "(", "[", "{":
			p.p = max(p.p+1, p.past(p.p, p.closeOf(p.p)))
			continue
		}
		p.p++
	}
}

// group scans a parenthesized or bracketed list of expressions.
func (p *parser) group() {
	open := p.p
	p.depth++
	if p.depth > maxDepth {
		p.p = p.past(open, p.closeOf(open))
	} else {
		p.seq(open, func() { p.expr(stops{comma: true}) })
	}
	p.depth--
}

// initializer scans a brace-enclosed initializer. If there's a qualifier, as
// in C# object initializers and with expressions, names assigned at the start
// of an element are members of it. Anonymous type members are declarations,
// so they get no reference.
func (p *parser) initializer(q qual, anonType bool) {
	p.seq(p.p, func() {
		if p.isIdent(p.p) && p.peek(1).text == "=" {
			switch {
			case anonType:
				p.p += 2
			case q.kind != qualNone:
				p.newRef(p.p, wantValue, q)
				p.p += 2
			}
		}
		p.expr(stops{comma: true})
	})
}

// holes scans the expressions inside a C# interpolated string.
func (p *parser) holes(t *tok) {
	toks, match, pos, eof := p.toks, p.match, p.p, p.eof
	for _, h := range t.holes {
		p.toks = lexRange(p.src, h.Start, h.End)
		p.match = matchBrackets(p.toks)
		p.p = 0
		p.eof = tok{s: h.End, e: h.End, ft: -1}
		for p.p < len(p.toks) {
			start := p.p
			p.expr(stops{})
			if p.p == start {
				p.p++
			}
		}
	}
	p.toks, p.match, p.p, p.eof = toks, match, pos, eof
}

func (p *parser) arrow() string {
	if p.cs {
		return "=>"
	}
	return "->"
}

// identExpr handles an identifier in an expression. It returns true if the
// identifier is a stop word of st.
func (p *parser) identExpr(st stops, prev *qual, dot *bool) bool {
	i := p.p
	t := p.cur()
	next := p.peek(1)
	if !*dot {
		switch {
		case st.query && queryWords[t.text]:
			return true
		case st.pattern && (t.text == "when" || (p.cs && (t.text == "and" || t.text == "or"))):
			return true
		case next.text == p.arrow() && !st.arrow:
			p.push()
			d := p.newDecl(p.parent, code.KindParameter, i, t.s)
			p.bind(d)
			p.p += 2
			p.lambdaBody()
			p.pop()
			*prev = qual{kind: qualOpaque}
			return false
		}
		if p.cs && p.csContextual(st, prev) {
			return false
		}
	}
	q := qual{}
	if *dot {
		q = *prev
		if q.kind == qualNone {
			q.kind = qualOpaque
		}
	}
	*dot = false
	if p.cs && t.text == "_" && q.kind == qualNone && p.lookupLocal("_", wantValue) == nil {
		p.p++
		*prev = qual{kind: qualOpaque}
		return false
	}
	if p.setter && t.text == "value" && q.kind == qualNone && p.lookupLocal("value", wantValue) == nil {
		p.p++
		*prev = qual{kind: qualType, typ: p.valueType}
		return false
	}
	end := i + 1
	if next.text == "<" {
		if j, ok := p.scanTypeArgs(i+1, 0); ok && p.genericFollows(p.at(j)) {
			end = j
		}
	}
	w := wantValue
	switch {
	case p.at(end).text == "(":
		w = wantCall
	case end > i+1:
		w = wantType
	}
	r := p.newRef(i, w, q)
	if end > i+1 {
		p.emitType(i+1, end)
	}
	p.p = end
	if w == wantCall {
		p.info.refs[r].arity = p.arityAt(p.p)
		p.group()
		*prev = qual{kind: qualCall, ref: r}
		return false
	}
	*prev = qual{kind: qualRef, ref: r}
	return false
}

// csContextual handles C# contextual keywords at the start of an
// expression operand. It reports whether it consumed anything.
func (p *parser) csContextual(st stops, prev *qual) bool {
	i := p.p
	t := p.cur()
	next := p.peek(1)
	switch t.text {
	case "var":
		switch {
		case next.kind == code.TokIdent:
			p.keyword(i)
			p.local(i+1, t.s, "")
			p.p += 2
			*prev = qual{kind: qualOpaque}
			return true
		case next.text == "(":
			p.keyword(i)
			p.p++
			p.designations(t.s)
			*prev = qual{kind: qualOpaque}
			return true
		}
	case "async", "static":
		if next.text == "delegate" || (next.kind == code.TokIdent && p.peek(2).text == "=>") ||
			(next.text == "(" && p.at(p.closeOf(i+1)+1).text == "=>") {
			p.keyword(i)
			p.p++
			return true
		}
	case "await":
		if next.kind == code.TokIdent || next.kind == code.TokKeyword || next.text == "(" {
			p.keyword(i)
			p.p++
			return true
		}
	case "nameof":
		if next.text == "(" {
			p.keyword(i)
			p.p++
			p.group()
			*prev = qual{kind: qualExternal}
			return true
		}
	case "with":
		if next.text == "{" && prev.kind != qualNone {
			p.keyword(i)
			p.p++
			p.initializer(*prev, false)
			*prev = qual{kind: qualOpaque}
			return true
		}
	case "from":
		if p.queryStart(i + 1) {
			p.query(st)
			*prev = qual{kind: qualOpaque}
			return true
		}
	}
	if next.text == ":" && (p.at(i-1).text == "(" || p.at(i-1).text == ",") && st.comma {
		// named argument
		p.p += 2
		return true
	}
	return false
}

// designations declares the names of a C# deconstruction `(a, (b, c))`.
func (p *parser) designations(start int) {
	p.seq(p.p, func() {
		switch {
		case p.is("("):
			p.designations(start)
		case p.isIdent(p.p) && p.cur().text != "_":
			p.local(p.p, start, "")
			p.p++
		default:
			p.expr(stops{comma: true})
		}
	})
}

// lambdaParens handles a parenthesized lambda parameter list at the cursor.
func (p *parser) lambdaParens(st stops) bool {
	close := p.closeOf(p.p)
	if st.arrow || p.match[p.p] != close || p.at(close+1).text != p.arrow() {
		return false
	}
	p.push()
	p.seq(p.p, p.lambdaParam)
	p.p++ // arrow
	p.lambdaBody()
	p.pop()
	return true
}

func (p *parser) lambdaParam() {
	start := p.cur().s
	p.parameterModifiers(true)
	if p.isIdent(p.p) && (p.peek(1).text == "," || p.peek(1).text == ")") {
		d := p.newDecl(p.parent, code.KindParameter, p.p, start)
		p.bind(d)
		p.p++
		return
	}
	if j, ok := p.scanType(p.p); ok && p.isIdent(j) {
		typ := ""
		if j == p.p+1 && p.isWord(p.p, "var") {
			p.keyword(p.p)
		} else {
			typ, _ = p.emitType(p.p, j)
		}
		d := p.newDecl(p.parent, code.KindParameter, j, start)
		d.Type = typ
		p.bind(d)
		p.p = j + 1
		if p.is("=") {
			p.p++
			p.expr(stops{comma: true})
		}
		return
	}
	p.expr(stops{comma: true})
}

func (p *parser) parameterModifiers(lambda bool) {
	for {
		switch {
		case p.isAnnotation(p.p):
			p.annotation()
			continue
		case p.cs && p.is("["):
			p.attributes()
			continue
		}
		modifier := p.is("final")
		if p.cs {
			modifier = modifier || p.is("ref") || p.is("in") || p.is("out") || p.is("params")
			if lambda {
				modifier = modifier || p.isWord(p.p, "scoped")
			} else {
				modifier = modifier || p.is("readonly")
			}
		}
		if !modifier {
			return
		}
		if lambda {
			p.keyword(p.p)
		}
		p.p++
	}
}

func (p *parser) lambdaBody() {
	if p.is("{") {
		p.block()
		return
	}
	p.expr(stops{comma: true})
}

// keywordExpr handles a keyword in an expression and reports whether it
// consumed anything.
func (p *parser) keywordExpr(st stops, prev *qual, dot *bool) bool {
	t := p.cur()
	next := p.peek(1)
	switch t.text {
	case "new":
		p.newExpr(prev)
		*dot = false
		return true
	case "this", "super", "base":
		p.p++
		if !*dot && p.is("(") {
			p.group()
			*prev = qual{}
		} else {
			kind := qualSuper
			if t.text == "this" {
				kind = qualThis
			}
			*prev = qual{kind: kind}
		}
		*dot = false
		return true
	case "class":
		if *dot {
			p.p++
			*prev, *dot = qual{kind: qualExternal}, false
			return true
		}
	case "switch":
		if next.text == "{" && p.cs {
			p.switchExpr()
		} else {
			p.switchStmt()
		}
		*prev, *dot = qual{kind: qualOpaque}, false
		return true
	case "instanceof", "is":
		p.p++
		p.pattern(stops{comma: st.comma, colon: true, arrow: st.arrow, pattern: true})
		*prev, *dot = qual{}, false
		return true
	case "as", "stackalloc":
		p.p++
		if j, ok := p.scanType(p.p); ok {
			p.emitType(p.p, j)
			p.p = j
		}
		*prev, *dot = qual{}, false
		return true
	case "typeof", "sizeof", "default":
		p.typeOperand()
		*prev, *dot = qual{kind: qualExternal}, false
		return true
	case "out", "ref", "in":
		if !p.cs {
			return false
		}
		p.referenceArgument(t)
		*prev, *dot = qual{}, false
		return true
	case "delegate":
		p.anonymousDelegate()
		*prev, *dot = qual{kind: qualOpaque}, false
		return true
	case "true", "false", "null":
		p.p++
		*prev, *dot = qual{kind: qualExternal}, false
		return true
	}
	if p.isPrim(t) {
		p.p++
		*prev, *dot = qual{kind: qualExternal}, false
		return true
	}
	return false
}

func (p *parser) anonymousDelegate() {
	p.p++
	p.push()
	if p.is("(") {
		p.seq(p.p, p.lambdaParam)
	}
	if p.is("{") {
		p.block()
	}
	p.pop()
}

func (p *parser) typeOperand() {
	p.p++
	if !p.is("(") {
		return
	}
	close := p.closeOf(p.p)
	if j, ok := p.scanType(p.p + 1); ok && j == close {
		p.emitType(p.p+1, j)
		p.p = close + 1
	} else {
		p.group()
	}
}

func (p *parser) referenceArgument(t *tok) {
	start := t.s
	p.p++
	if p.isWord(p.p, "var") && p.isIdent(p.p+1) {
		p.keyword(p.p)
		p.local(p.p+1, start, "")
		p.p += 2
	} else if j, ok := p.scanType(p.p); ok && p.isIdent(j) && t.text == "out" {
		typ, _ := p.emitType(p.p, j)
		p.local(j, start, typ)
		p.p = j + 1
	}
}

// newExpr parses an instance or array creation at the cursor.
func (p *parser) newExpr(prev *qual) {
	p.p++
	switch {
	case p.is("("):
		p.group()
		if p.is("{") {
			p.initializer(qual{kind: qualOpaque}, false)
		}
		*prev = qual{kind: qualOpaque}
		return
	case p.is("{"):
		p.initializer(qual{}, true)
		*prev = qual{kind: qualOpaque}
		return
	case p.is("["):
		p.group()
		if p.is("{") {
			p.initializer(qual{}, false)
		}
		*prev = qual{kind: qualOpaque}
		return
	}
	j, ok := p.scanType(p.p)
	if !ok {
		*prev = qual{}
		return
	}
	text, head := p.emitType(p.p, j)
	p.p = j
	array := p.at(j-1).text == "]"
	*prev = qual{kind: qualOpaque}
	if head != nil && !array {
		*prev = qual{kind: qualRef, ref: head}
	}
	switch {
	case p.is("("):
		if head != nil {
			head.Call = true
			p.info.refs[head].arity = p.arityAt(p.p)
		}
		p.group()
		if p.is("{") {
			if p.cs {
				p.initializer(qual{kind: qualRef, ref: head}, false)
			} else {
				p.anonBody(text)
			}
		}
	case p.is("{"):
		if p.cs && !array && head != nil {
			head.Call = true
			p.initializer(qual{kind: qualRef, ref: head}, false)
		} else {
			p.initializer(qual{}, false)
		}
	case p.is("["):
		for p.is("[") {
			p.group()
		}
		if p.is("{") {
			p.initializer(qual{}, false)
		}
		*prev = qual{kind: qualOpaque}
	}
}

// anonBody parses a Java anonymous class body. The members become locals of
// the enclosing declaration.
func (p *parser) anonBody(base string) {
	a := &anonCtx{base: base, ctx: p.ctx, parent: p.anon}
	saveAnon := p.anon
	p.anon = a
	p.exec++
	p.classBody(p.parent, "", a, false)
	p.exec--
	p.anon = saveAnon
}

var queryWords = code.StringSet("from", "join", "on", "equals", "into", "let", "where", "orderby",
	"ascending", "descending", "select", "group", "by")

// queryStart reports whether a LINQ query starts after `from` at i, as in
// `from x in` or `from T x in`.
func (p *parser) queryStart(i int) bool {
	if p.isIdent(i) && p.at(i+1).text == "in" {
		return true
	}
	j, ok := p.scanType(i)
	return ok && p.isIdent(j) && p.at(j+1).text == "in"
}

// query parses a C# query expression starting at `from`. Range variables
// are locals visible in the rest of the query.
func (p *parser) query(st stops) {
	p.push()
	defer p.pop()
	sub := stops{comma: st.comma, colon: st.colon, query: true}
	last := ""
	for p.p < len(p.toks) {
		t := p.cur()
		if p.is(",") && last == "orderby" {
			p.p++
			p.expr(sub)
			continue
		}
		if t.kind != code.TokIdent || !queryWords[t.text] {
			return
		}
		p.keyword(p.p)
		p.p++
		last = t.text
		switch t.text {
		case "from", "join":
			start := t.s
			if p.isIdent(p.p) && p.peek(1).text == "in" {
				p.local(p.p, start, "")
				p.p++
			} else if j, ok := p.scanType(p.p); ok && p.isIdent(j) {
				typ, _ := p.emitType(p.p, j)
				p.local(j, start, typ)
				p.p = j + 1
			}
			p.accept("in")
			p.expr(sub)
		case "let", "into":
			if p.isIdent(p.p) {
				p.local(p.p, t.s, "")
				p.p++
			}
			if t.text == "let" && p.accept("=") {
				p.expr(sub)
			}
		case "ascending", "descending":
		default:
			p.expr(sub)
		}
	}
}

// pattern parses a pattern after is/instanceof/case or in a switch arm, and
// declares the designations in the current frame.
func (p *parser) pattern(st stops) {
	for {
		p.patternPrimary(st)
		if !p.cs || !(p.isWord(p.p, "and") || p.isWord(p.p, "or")) {
			return
		}
		p.keyword(p.p)
		p.p++
	}
}

func (p *parser) patternPrimary(st stops) {
	for p.cs && p.isWord(p.p, "not") {
		p.keyword(p.p)
		p.p++
	}
	if !p.cs {
		p.accept("final")
	}
	start := p.cur().s
	switch {
	case p.cs && p.isWord(p.p, "var"):
		p.keyword(p.p)
		p.p++
		if p.isIdent(p.p) {
			p.local(p.p, start, "")
			p.p++
		} else if p.is("(") {
			p.designations(start)
		}
		return
	case p.cs && p.is("{"):
		p.propertyPattern(qual{kind: qualOpaque})
		p.designation(start, "")
		return
	case p.is("("):
		p.seq(p.p, p.positionalPattern)
		p.designation(start, "")
		return
	case p.cs && (p.is("<") || p.is(">") || p.is("<=") || p.is(">=")):
		p.p++
		p.expr(stops{comma: true, colon: true, arrow: true, pattern: true})
		return
	}
	if j, ok := p.scanType(p.p); ok && j > p.p {
		after := p.at(j)
		named := after.kind == code.TokIdent && after.text != "when" && !(p.cs && (after.text == "and" || after.text == "or"))
		if named || after.text == "(" || (p.cs && after.text == "{") {
			typ, head := p.emitType(p.p, j)
			p.p = j
			if p.is("(") {
				p.seq(p.p, p.positionalPattern)
			}
			if p.cs && p.is("{") {
				q := qual{kind: qualOpaque}
				if head != nil {
					q = qual{kind: qualRef, ref: head}
				}
				p.propertyPattern(q)
			}
			p.designation(start, typ)
			return
		}
	}
	p.expr(stops{comma: true, colon: st.colon, arrow: true, pattern: true})
}

func (p *parser) positionalPattern() {
	if p.cs && p.isIdent(p.p) && p.peek(1).text == ":" {
		p.p += 2
	}
	p.pattern(stops{comma: true, pattern: true})
}

// designation declares the variable named after a pattern, if any.
func (p *parser) designation(start int, typ string) {
	t := p.cur()
	if t.kind != code.TokIdent || t.text == "when" || (p.cs && (t.text == "and" || t.text == "or")) {
		return
	}
	if t.text != "_" {
		p.local(p.p, start, typ)
	}
	p.p++
}

func (p *parser) propertyPattern(q qual) {
	p.seq(p.p, func() {
		if p.isIdent(p.p) && (p.peek(1).text == ":" || p.peek(1).text == ".") {
			qq := q
			for p.isIdent(p.p) {
				r := p.newRef(p.p, wantValue, qq)
				qq = qual{kind: qualRef, ref: r}
				p.p++
				if !p.is(".") {
					break
				}
				p.p++
			}
			p.accept(":")
		}
		p.pattern(stops{comma: true, pattern: true})
	})
}

// switchExpr parses a C# switch expression body `{ pattern => expr, ... }`.
func (p *parser) switchExpr() {
	p.p++
	p.seq(p.p, func() {
		p.push()
		p.pattern(stops{comma: true, arrow: true, pattern: true})
		if p.isWord(p.p, "when") {
			p.keyword(p.p)
			p.p++
			p.expr(stops{comma: true, arrow: true})
		}
		if p.accept("=>") {
			p.expr(stops{comma: true})
		}
		p.pop()
	})
}
