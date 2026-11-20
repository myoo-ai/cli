package cpp

import "github.com/myoo-ai/cli/internal/code"

func (p *parser) stmts(sc *scope, end int) {
	for p.i < end {
		start := p.i
		p.statement(sc, end)
		if p.i <= start {
			p.i = start + 1
		}
	}
}

func (p *parser) statement(sc *scope, end int) {
	i := p.i
	if i >= end {
		return
	}
	s := p.text(i)
	switch s {
	case "{":
		m := p.closeOf(i, end)
		p.i = i + 1
		p.stmts(newScope(scopeBlock, sc, nil, sc.owner), m)
		p.i = m + 1
		return
	case ";":
		p.i++
		return
	}
	if p.kind(i) == code.TokKeyword {
		switch s {
		case "if", "while", "switch", "for":
			p.controlStatement(sc, end, s)
			return
		case "do":
			p.i++
			p.statement(sc, end)
			if p.is(p.i, "while") && p.is(p.i+1, "(") {
				close := p.closeOf(p.i+1, end)
				p.scanExpr(sc, p.i+2, close)
				p.i = close + 1
			}
			if p.is(p.i, ";") {
				p.i++
			}
			return
		case "case":
			e := p.exprEnd(i+1, end, false, true)
			p.scanExpr(sc, i+1, e)
			p.i = e + 1
			return
		case "default":
			p.i++
			if p.is(p.i, ":") {
				p.i++
			}
			return
		case "try":
			p.i++
			p.statement(sc, end)
			p.handlers(sc, end)
			return
		case "else", "break", "continue":
			p.i++
			return
		case "return", "throw", "co_return", "co_yield", "goto":
			p.i++
			p.exprStatement(sc, end)
			return
		case "using", "typedef", "namespace", "template", "static_assert", "_Static_assert", "asm":
			p.declaration(sc, end, ctxLocal)
			return
		}
	}
	if p.ident(i) && p.is(i+1, ":") {
		p.i += 2
		return
	}
	if p.looksLikeDecl(i, end) {
		p.declaration(sc, end, ctxLocal)
		return
	}
	p.exprStatement(sc, end)
}

func (p *parser) controlStatement(sc *scope, end int, keyword string) {
	p.i++
	if keyword != "for" {
		for p.is(p.i, "constexpr") || p.is(p.i, "!") || p.is(p.i, "consteval") {
			p.i++
		}
	}
	ns := newScope(scopeBlock, sc, nil, sc.owner)
	if p.is(p.i, "(") {
		close := p.closeOf(p.i, end)
		if keyword == "for" {
			p.forHeader(ns, p.i+1, close)
		} else {
			p.condition(ns, p.i+1, close)
		}
		p.i = close + 1
	}
	p.statement(ns, end)
	if keyword == "if" && p.is(p.i, "else") {
		p.i++
		p.statement(ns, end)
	}
}

func (p *parser) exprStatement(sc *scope, end int) {
	e := p.exprEnd(p.i, end, false, false)
	p.scanExpr(sc, p.i, e)
	p.i = e
	if p.is(p.i, ";") {
		p.i++
	}
}

// looksLikeDecl guesses whether the statement at i is a declaration.
func (p *parser) looksLikeDecl(i, end int) bool {
	if p.kind(i) == code.TokKeyword {
		return declKeywords[p.text(i)]
	}
	if !p.ident(i) && !p.is(i, "::") {
		return false
	}
	if attrWords[p.text(i)] {
		return true
	}
	j := p.skipTypeName(i, end)
	if j < 0 {
		return false
	}
	for cvKeywords[p.text(j)] {
		j++
	}
	k := j
	for p.is(k, "*") || p.is(k, "&") || p.is(k, "&&") || cvKeywords[p.text(k)] {
		k++
	}
	if !p.ident(k) {
		return false
	}
	if k == j {
		return true
	}
	switch p.text(k + 1) {
	case "=", ";", ",", "[", "{", ")", ":", "(":
		return true
	}
	return false
}

// condition parses the parenthesized condition [from, to) of if, while and
// switch, including C++17 init-statements and declarations.
func (p *parser) condition(sc *scope, from, to int) {
	if semi := p.exprEnd(from, to, false, false); semi < to && p.is(semi, ";") {
		p.partial(sc, from, semi)
		from = semi + 1
	}
	p.partial(sc, from, to)
}

// partial parses [from, to) as a declaration if it looks like one, else as an
// expression.
func (p *parser) partial(sc *scope, from, to int) {
	saved := p.i
	defer func() { p.i = saved }()
	if p.looksLikeDecl(from, to) {
		p.i = from
		mark := len(p.f.Refs)
		sp := p.declSpecs(sc, to)
		if p.simpleDecl(sc, to, sp, ctxCond, mark) {
			if p.i < to {
				p.scanExpr(sc, p.i, to)
			}
			return
		}
		from = p.i
	}
	p.scanExpr(sc, from, to)
}

// forHeader parses the parenthesized header [from, to) of a for loop.
func (p *parser) forHeader(sc *scope, from, to int) {
	saved := p.i
	defer func() { p.i = saved }()
	if semi := p.exprEnd(from, to, false, false); semi < to && p.is(semi, ";") {
		p.partial(sc, from, semi)
		p.scanExpr(sc, semi+1, to)
		return
	}
	if !p.looksLikeDecl(from, to) {
		p.scanExpr(sc, from, to)
		return
	}
	p.i = from
	first := len(p.f.Decls)
	mark := len(p.f.Refs)
	sp := p.declSpecs(sc, to)
	p.simpleDecl(sc, to, sp, ctxFor, mark)
	made := p.f.Decls[first:]
	if p.is(p.i, ":") {
		p.scanExpr(sc, p.i+1, to)
		if p.i+2 == to {
			if r := p.refAt[p.i+1]; r != nil {
				p.rangeVariables(made, r)
			}
		}
		return
	}
	if p.i < to {
		p.scanExpr(sc, p.i, to)
	}
}

func (p *parser) rangeVariables(decls []*code.Decl, r *code.Ref) {
	for _, d := range decls {
		if di := p.info(d); di != nil && d.Kind == code.KindVariable {
			di.rangeOf = r
		}
	}
}
