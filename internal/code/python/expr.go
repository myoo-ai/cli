package python

import "github.com/myoo-ai/cli/internal/code"

type nodeKind uint8

const (
	nName nodeKind = iota + 1
	nAttr
	nCall
	nSubscript
	nLambda
	nComp
	nTuple
	nList
	nSet
	nDict
	nStarred
	nWalrus
	nStr
	nOp
	nConst
)

// node is an expression. We don't model operator precedence. The operands
// of binary and unary operators are kept flat in elts, because that's all
// reference extraction and type inference need.
type node struct {
	kind     nodeKind
	span     code.Span
	name     string    // nName, nAttr member, nWalrus target
	nameSpan code.Span // nName, nAttr member, nWalrus target
	op       string    // nOp operator: "|", "if", "not", "as", "slice", "mixed"...
	x        *node     // nAttr, nCall and nSubscript base, nStarred and nWalrus value
	elts     []*node   // operands, items, f-string fields. nDict holds key/value pairs (nil key for **x)
	args     []arg     // nCall
	lambda   *lambdaNode
	comp     *compNode
	strs     []code.Span // nStr: the string tokens

	sc     *scope    // lexical scope, set when the node is emitted
	ref    *code.Ref // nName and nAttr reference
	parsed *node     // parsed string annotation of nStr, see resolver.stringAnnotation
}

type arg struct {
	kw     string
	kwSpan code.Span
	star   int // 1 for *x, 2 for **x
	value  *node
}

type param struct {
	name     string
	nameSpan code.Span
	span     code.Span
	star     int
	ann, def *node
}

type lambdaNode struct {
	params []param
	body   *node
}

type compNode struct {
	elts    []*node // element, or key and value
	clauses []compClause
}

type compClause struct {
	span   code.Span
	target *node
	iter   *node
	ifs    []*node
}

// mode restricts what an expression can contain in the current context.
type mode struct {
	noIn    bool // `in` ends the expression (for targets)
	noCond  bool // `if` ends the expression (comprehension clauses)
	pattern bool // match patterns: `as` binds, `if` ends the pattern
}

func (p *parser) startsExpr() bool {
	t := p.tok()
	switch t.kind {
	case tName, tNumber, tString:
		return true
	case tKeyword:
		switch t.text {
		case "None", "True", "False", "lambda", "not", "await", "yield":
			return true
		}
	case tOp:
		switch t.text {
		case "(", "[", "{", "-", "+", "~", "*", "**", "...":
			return true
		}
	}
	return false
}

func (p *parser) span(start int) code.Span {
	return code.Span{Start: start, End: max(p.lastEnd, start)}
}

// exprList parses comma-separated (starred) expressions. If there's more
// than one, they make a tuple.
func (p *parser) exprList(m mode) *node {
	start := p.tok().span.Start
	first := p.starTest(m)
	if first == nil || !p.isOp(",") {
		return first
	}
	elts := []*node{first}
	for p.acceptOp(",") {
		if !p.startsExpr() {
			break
		}
		e := p.starTest(m)
		if e == nil {
			break
		}
		elts = append(elts, e)
	}
	return &node{kind: nTuple, span: p.span(start), elts: elts}
}

func (p *parser) exprListYield(m mode) *node {
	if p.isKw("yield") {
		return p.yieldExpr()
	}
	return p.exprList(m)
}

func (p *parser) starTest(m mode) *node {
	if p.isOp("*") {
		start := p.next().span.Start
		x := p.orExpr(m)
		return &node{kind: nStarred, span: p.span(start), x: x}
	}
	return p.test(m)
}

func (p *parser) test(m mode) *node {
	t := p.tok()
	switch {
	case t.kind == tKeyword && t.text == "lambda":
		return p.lambdaExpr(m)
	case t.kind == tKeyword && t.text == "yield":
		return p.yieldExpr()
	case t.kind == tName && p.peek(1).kind == tOp && p.peek(1).text == ":=":
		p.next()
		p.next()
		target := nameNode(t)
		v := p.test(m)
		return &node{kind: nWalrus, span: p.span(t.span.Start), name: t.text, nameSpan: t.span, x: v, elts: []*node{target}}
	}
	start := t.span.Start
	x := p.orExpr(m)
	if x == nil {
		return nil
	}
	if !m.noCond && !m.pattern && p.isKw("if") {
		p.next()
		cond := p.orExpr(mode{noCond: true})
		elts := []*node{x, cond}
		if p.acceptKw("else") {
			elts = append(elts, p.test(m))
		}
		x = &node{kind: nOp, op: "if", span: p.span(start), elts: elts}
	}
	if m.pattern && p.isKw("as") {
		p.next()
		if nt := p.nameTok(); nt != nil {
			name := nameNode(nt)
			x = &node{kind: nOp, op: "as", span: p.span(start), elts: []*node{x, name}}
		}
	}
	return x
}

func (p *parser) orExpr(m mode) *node {
	start := p.tok().span.Start
	first := p.unary(m)
	if first == nil {
		return nil
	}
	operands, op := []*node{first}, ""
	for {
		o, ok := p.binOp(m)
		if !ok {
			break
		}
		if op == "" {
			op = o
		} else if op != o {
			op = "mixed"
		}
		r := p.unary(m)
		if r == nil {
			break
		}
		operands = append(operands, r)
	}
	if len(operands) == 1 {
		return first
	}
	return &node{kind: nOp, op: op, span: p.span(start), elts: operands}
}

var binaryOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "//": true, "%": true, "**": true, "@": true,
	"<<": true, ">>": true, "&": true, "|": true, "^": true, "<": true, ">": true,
	"<=": true, ">=": true, "==": true, "!=": true, "<>": true,
}

func (p *parser) binOp(m mode) (string, bool) {
	t := p.tok()
	switch {
	case t.kind == tOp && binaryOps[t.text]:
		p.next()
		return t.text, true
	case t.kind != tKeyword:
		return "", false
	}
	switch t.text {
	case "and", "or":
		p.next()
		return t.text, true
	case "in":
		if m.noIn {
			return "", false
		}
		p.next()
		return "in", true
	case "is":
		p.next()
		p.acceptKw("not")
		return "is", true
	case "not":
		if n := p.peek(1); n.kind == tKeyword && n.text == "in" && !m.noIn {
			p.next()
			p.next()
			return "not in", true
		}
	}
	return "", false
}

func (p *parser) unary(m mode) *node {
	t := p.tok()
	if (t.kind == tOp && (t.text == "-" || t.text == "+" || t.text == "~")) ||
		(t.kind == tKeyword && (t.text == "not" || t.text == "await")) {
		p.next()
		x := p.unary(m)
		return &node{kind: nOp, op: t.text, span: p.span(t.span.Start), elts: []*node{x}}
	}
	return p.primary(m)
}

func (p *parser) primary(m mode) *node {
	x := p.atom(m)
	if x == nil {
		return nil
	}
	start := x.span.Start
	for {
		switch {
		case p.isOp("."):
			nt := p.peek(1)
			if nt.kind != tName {
				p.next()
				return x
			}
			p.next()
			p.next()
			x = &node{kind: nAttr, x: x, name: nt.text, nameSpan: nt.span, span: p.span(start)}
		case p.isOp("("):
			p.next()
			args := p.callArgs()
			p.acceptOp(")")
			x = &node{kind: nCall, x: x, args: args, span: p.span(start)}
		case p.isOp("["):
			p.next()
			items := p.subscriptItems()
			p.acceptOp("]")
			x = &node{kind: nSubscript, x: x, elts: items, span: p.span(start)}
		default:
			return x
		}
	}
}

func (p *parser) atom(m mode) *node {
	t := p.tok()
	switch t.kind {
	case tName:
		p.next()
		return nameNode(t)
	case tNumber:
		p.next()
		return &node{kind: nConst, span: t.span}
	case tString:
		return p.strings()
	case tKeyword:
		switch t.text {
		case "None", "True", "False":
			p.next()
			return &node{kind: nConst, span: t.span, name: t.text}
		}
	case tOp:
		switch t.text {
		case "...":
			p.next()
			return &node{kind: nConst, span: t.span}
		case "(":
			return p.parenAtom(m)
		case "[":
			return p.listAtom(m)
		case "{":
			return p.braceAtom(m)
		}
	}
	return nil
}

func (p *parser) startsComp() bool {
	return p.isKw("for") || (p.isKw("async") && p.peek(1).kind == tKeyword && p.peek(1).text == "for")
}

func (p *parser) collectionStart(m mode, kind nodeKind, close string) (int, mode, *node) {
	start := p.next().span.Start
	inner := mode{pattern: m.pattern}
	if p.acceptOp(close) {
		return start, inner, &node{kind: kind, span: p.span(start)}
	}
	return start, inner, nil
}

func (p *parser) parenAtom(m mode) *node {
	start, inner, empty := p.collectionStart(m, nTuple, ")")
	if empty != nil {
		return empty
	}
	if p.isKw("yield") {
		y := p.yieldExpr()
		p.acceptOp(")")
		return y
	}
	first := p.starTest(inner)
	if first == nil {
		p.acceptOp(")")
		return &node{kind: nConst, span: p.span(start)}
	}
	if p.startsComp() {
		return p.comprehension(start, []*node{first}, ")")
	}
	if !p.isOp(",") {
		p.acceptOp(")")
		return first
	}
	return p.collectionTail(start, first, nTuple, ")", inner)
}

func (p *parser) listAtom(m mode) *node {
	start := p.next().span.Start
	inner := mode{pattern: m.pattern}
	var elts []*node
	for !p.isOp("]") && p.tok().kind != tEOF {
		e := p.starTest(inner)
		if e == nil {
			break
		}
		if len(elts) == 0 && p.startsComp() {
			return p.comprehension(start, []*node{e}, "]")
		}
		elts = append(elts, e)
		if !p.acceptOp(",") {
			break
		}
	}
	p.acceptOp("]")
	return &node{kind: nList, span: p.span(start), elts: elts}
}

func (p *parser) braceAtom(m mode) *node {
	start, inner, empty := p.collectionStart(m, nDict, "}")
	if empty != nil {
		return empty
	}
	if !p.isOp("**") {
		first := p.starTest(inner)
		if first == nil {
			p.acceptOp("}")
			return &node{kind: nConst, span: p.span(start)}
		}
		if !p.isOp(":") {
			if p.startsComp() {
				return p.comprehension(start, []*node{first}, "}")
			}
			return p.collectionTail(start, first, nSet, "}", inner)
		}
		p.next()
		v := p.test(inner)
		if p.startsComp() {
			return p.comprehension(start, []*node{first, v}, "}")
		}
		pairs := []*node{first, v}
		if !p.acceptOp(",") {
			p.acceptOp("}")
			return &node{kind: nDict, span: p.span(start), elts: pairs}
		}
		return p.dictRest(start, pairs, inner)
	}
	return p.dictRest(start, nil, inner)
}

func (p *parser) collectionTail(start int, first *node, kind nodeKind, closer string, m mode) *node {
	elts := []*node{first}
	for p.acceptOp(",") && !p.isOp(closer) {
		e := p.starTest(m)
		if e == nil {
			break
		}
		elts = append(elts, e)
	}
	p.acceptOp(closer)
	return &node{kind: kind, span: p.span(start), elts: elts}
}

func (p *parser) comprehension(start int, elts []*node, closer string) *node {
	c := p.compTail(start, elts)
	p.acceptOp(closer)
	c.span = p.span(start)
	return c
}

func (p *parser) dictRest(start int, pairs []*node, m mode) *node {
	for !p.isOp("}") && p.tok().kind != tEOF {
		if p.isOp("**") {
			s := p.next().span.Start
			x := p.orExpr(m)
			pairs = append(pairs, nil, &node{kind: nStarred, op: "**", span: p.span(s), x: x})
		} else {
			k := p.test(m)
			if k == nil || !p.acceptOp(":") {
				break
			}
			pairs = append(pairs, k, p.test(m))
		}
		if !p.acceptOp(",") {
			break
		}
	}
	p.acceptOp("}")
	return &node{kind: nDict, span: p.span(start), elts: pairs}
}

func (p *parser) compTail(start int, elts []*node) *node {
	c := &compNode{elts: elts}
	for p.startsComp() {
		cs := p.tok().span.Start
		p.acceptKw("async")
		p.next()
		cl := compClause{target: p.exprList(mode{noIn: true})}
		p.acceptKw("in")
		cl.iter = p.test(mode{noCond: true})
		for p.isKw("if") {
			p.next()
			cl.ifs = append(cl.ifs, p.test(mode{noCond: true}))
		}
		cl.span = p.span(cs)
		c.clauses = append(c.clauses, cl)
	}
	return &node{kind: nComp, span: p.span(start), comp: c}
}

func (p *parser) callArgs() []arg {
	var args []arg
	for !p.isOp(")") && p.tok().kind != tEOF {
		var a arg
		t := p.tok()
		switch {
		case p.isOp("*"):
			p.next()
			a.star, a.value = 1, p.test(mode{})
		case p.isOp("**"):
			p.next()
			a.star, a.value = 2, p.test(mode{})
		case t.kind == tName && p.peek(1).kind == tOp && p.peek(1).text == "=":
			p.next()
			p.next()
			a.kw, a.kwSpan, a.value = t.text, t.span, p.test(mode{})
		default:
			a.value = p.test(mode{})
			if a.value != nil && p.startsComp() {
				a.value = p.compTail(a.value.span.Start, []*node{a.value})
			}
		}
		if a.value == nil {
			break
		}
		args = append(args, a)
		if !p.acceptOp(",") {
			break
		}
	}
	return args
}

func (p *parser) subscriptItems() []*node {
	var items []*node
	for !p.isOp("]") && p.tok().kind != tEOF {
		it := p.sliceItem()
		if it == nil {
			break
		}
		items = append(items, it)
		if !p.acceptOp(",") {
			break
		}
	}
	return items
}

func (p *parser) sliceItem() *node {
	start := p.tok().span.Start
	var parts []*node
	if !p.isOp(":") {
		e := p.starTest(mode{})
		if e == nil || !p.isOp(":") {
			return e
		}
		parts = append(parts, e)
	}
	for p.acceptOp(":") {
		if !p.isOp(":") && !p.isOp(",") && !p.isOp("]") {
			if e := p.test(mode{}); e != nil {
				parts = append(parts, e)
			}
		}
	}
	return &node{kind: nOp, op: "slice", span: p.span(start), elts: parts}
}

func (p *parser) lambdaExpr(m mode) *node {
	start := p.next().span.Start
	params := p.params(":", false)
	p.acceptOp(":")
	body := p.test(mode{noCond: m.noCond})
	return &node{kind: nLambda, span: p.span(start), lambda: &lambdaNode{params: params, body: body}}
}

// params parses a parameter list up to closer, which is `)` for def (with
// annotations) or `:` for lambda.
func (p *parser) params(closer string, annotations bool) []param {
	var params []param
	for !p.isOp(closer) && p.tok().kind != tEOF && p.tok().kind != tNewline {
		start := p.tok().span.Start
		if p.acceptOp("/") {
			if !p.acceptOp(",") {
				break
			}
			continue
		}
		star := 0
		if p.acceptOp("*") {
			star = 1
		} else if p.acceptOp("**") {
			star = 2
		}
		nt := p.tok()
		if nt.kind != tName {
			if star == 1 && p.acceptOp(",") {
				continue
			}
			break
		}
		p.next()
		prm := param{name: nt.text, nameSpan: nt.span, star: star}
		if annotations && p.acceptOp(":") {
			prm.ann = p.starTest(mode{})
		}
		if p.acceptOp("=") {
			prm.def = p.test(mode{})
		}
		prm.span = p.span(start)
		params = append(params, prm)
		if !p.acceptOp(",") {
			break
		}
	}
	return params
}

func (p *parser) yieldExpr() *node {
	start := p.next().span.Start
	var elts []*node
	if p.acceptKw("from") {
		elts = append(elts, p.test(mode{}))
	} else if p.startsExpr() {
		elts = append(elts, p.exprList(mode{}))
	}
	return &node{kind: nOp, op: "yield", span: p.span(start), elts: elts}
}

// strings parses implicitly concatenated string literals, plus the
// expressions in their f-string replacement fields.
func (p *parser) strings() *node {
	start := p.tok().span.Start
	n := &node{kind: nStr}
	for p.tok().kind == tString {
		t := p.next()
		n.strs = append(n.strs, t.span)
		for _, fs := range t.fields {
			if e := p.subExpr(fs); e != nil {
				n.elts = append(n.elts, e)
			}
		}
	}
	n.span = p.span(start)
	return n
}

// subExpr parses the expression in span sp of src, which is an f-string
// field or a string annotation. The expression can end early, like `x=` in
// a self-documenting field.
func (p *parser) subExpr(sp code.Span) *node {
	l := lex(p.src, sp.Start, sp.End, false, true)
	sub := &parser{f: p.f, src: p.src, toks: l.toks, info: p.info, sub: true}
	return sub.exprListYield(mode{})
}
