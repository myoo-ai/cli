package cpp

import "github.com/myoo-ai/cli/internal/code"

// literalNames are identifiers that behave like literals, so they get no
// reference.
var literalNames = map[string]bool{
	"NULL": true, "__func__": true, "__FUNCTION__": true, "__PRETTY_FUNCTION__": true,
	"__FILE__": true, "__LINE__": true, "__DATE__": true, "__TIME__": true,
}

// templateArgsEnd returns the index of the token that closes the template
// argument list opened by the `<` at i, or -1. In strict mode, which is for
// expressions, we reject the list if it has tokens that rarely show up in
// template arguments, or if the token after it can't follow a template-id. A
// `>>` closes two levels and we consume it whole.
func (p *parser) templateArgsEnd(i, end int, strict bool) int {
	depth := 0
	for j := i; j < end; j++ {
		t := p.t[j]
		switch p.text(j) {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		case "(", "[":
			j = p.closeOf(j, end)
			continue
		case ";", "{", "}", ")", "]":
			return -1
		case "&&", "||", "?", "=", "==", "!=", "<=", ">=", "!", "+=", "-=", "<<", "|", "^", "%", "/":
			if strict {
				return -1
			}
		}
		if t.kind == code.TokString && strict {
			return -1
		}
		if depth > 0 {
			continue
		}
		if strict && !p.afterTemplateArgs(j+1) {
			return -1
		}
		return j
	}
	return -1
}

func (p *parser) afterTemplateArgs(i int) bool {
	switch p.text(i) {
	case "(", "::", "{", ")", ";", ",", "]", "}", ".", "->", ">", ">>", "...", "":
		return true
	}
	return p.ident(i)
}

// exprEnd returns the index of the first token at nesting depth 0 in [i, end)
// that ends an expression. That's `;`, plus `,` or `:` if comma or colon is
// set.
func (p *parser) exprEnd(i, end int, comma, colon bool) int {
	for j := i; j < end; j++ {
		switch p.text(j) {
		case ";":
			return j
		case ",":
			if comma {
				return j
			}
		case ":":
			if colon {
				return j
			}
		case "(", "[", "{":
			j = p.closeOf(j, end)
		case ")", "]", "}":
			return j
		case "<":
			if j > i && p.ident(j-1) {
				if e := p.templateArgsEnd(j, end, true); e >= 0 {
					j = e
				}
			}
		}
	}
	return end
}

// splitTop returns the start indexes of the comma-separated items of
// [from, to), template-argument aware.
func (p *parser) splitTop(from, to int) []int {
	var starts []int
	for i := from; i < to; {
		starts = append(starts, i)
		e := p.exprEnd(i, to, true, false)
		if e >= to || !p.is(e, ",") {
			break
		}
		i = e + 1
	}
	return starts
}

// operandStart reports whether token i starts an operand. We need this to
// decide whether `[` opens a lambda.
func (p *parser) operandStart(i, from int) bool {
	if i <= from {
		return true
	}
	prev := i - 1
	switch p.kind(prev) {
	case code.TokIdent, code.TokNumber, code.TokString:
		return false
	case code.TokKeyword:
		return p.is(prev, "return") || p.is(prev, "co_return") || p.is(prev, "co_yield") || p.is(prev, "throw")
	}
	switch p.text(prev) {
	case ")", "]", "}":
		return false
	}
	return true
}

// lambdaEnd returns the index of the closing brace of a lambda starting at
// the `[` at i, or -1 if i doesn't start a lambda.
func (p *parser) lambdaEnd(i, end int) int {
	j := p.closeOf(i, end)
	if j >= end || p.is(i+1, "[") {
		return -1
	}
	j++
	if p.is(j, "<") {
		if e := p.templateArgsEnd(j, end, false); e >= 0 {
			j = e + 1
		}
	}
	if !p.is(j, "(") && !p.is(j, "{") && !p.is(j, "mutable") && !p.is(j, "->") {
		return -1
	}
	for j < end && !p.is(j, "{") {
		switch p.text(j) {
		case "(", "[":
			j = p.closeOf(j, end)
		case ";", "}", ")", "]", ",":
			return -1
		}
		j++
	}
	if j >= end {
		return -1
	}
	return p.closeOf(j, end)
}

// scanExpr records references for the expression tokens [from, to).
func (p *parser) scanExpr(sc *scope, from, to int) {
	to = min(to, len(p.t))
	newCall := -1
	for i := from; i < to; i++ {
		t := p.t[i]
		switch t.kind {
		case code.TokIdent:
			i = p.exprIdent(sc, i, from, to, i == newCall)
			continue
		case code.TokKeyword:
			if p.is(i, "new") {
				newCall = p.newTypeName(i+1, to)
			}
			continue
		case code.TokPunct:
		default:
			continue
		}
		switch p.text(i) {
		case "[":
			if p.operandStart(i, from) {
				if e := p.lambdaEnd(i, to); e >= 0 {
					p.lambda(sc, i, e, nil)
					i = e
					continue
				}
			}
			m := p.closeOf(i, to)
			p.scanExpr(sc, i+1, m)
			i = m
		case "(", "{":
			m := p.closeOf(i, to)
			p.scanExpr(sc, i+1, m)
			i = m
		}
	}
}

// newTypeName returns the index of the last identifier of the type named
// after `new`, or -1.
func (p *parser) newTypeName(i, end int) int {
	if p.is(i, "(") {
		i = p.closeOf(i, end) + 1
	}
	if p.is(i, "::") {
		i++
	}
	last := -1
	for i < end && p.ident(i) {
		last = i
		if !p.is(i+1, "::") {
			break
		}
		i += 2
	}
	return last
}

// exprIdent records the reference for the identifier at i and returns the
// index of the last token it consumed, template arguments included.
func (p *parser) exprIdent(sc *scope, i, from, to int, isNew bool) int {
	name := p.text(i)
	if literalNames[name] {
		return i
	}
	kind, prev := p.qualifier(i, from)
	r := p.newRef(sc, i, kind, prev)
	p.fi.refs[r].arrow = p.is(i-1, "->")
	last := i
	if p.is(i+1, "<") {
		if e := p.templateArgsEnd(i+1, to, true); e >= 0 {
			p.scanExpr(sc, i+2, e)
			last = e
		}
	}
	switch {
	case p.is(last+1, "("):
		r.Call = true
		m := p.closeOf(last+1, to)
		p.fi.refs[r].arity = len(p.splitTop(last+2, m))
	case isNew:
		r.Call = true
	case p.cpp && p.is(last+1, "{") && !p.is(last+1, "::"):
		r.Call = true
	}
	return last
}

// qualifier classifies the identifier at i by the tokens before it.
func (p *parser) qualifier(i, from int) (refKind, *code.Ref) {
	if i-1 < from {
		return refPlain, nil
	}
	switch p.text(i - 1) {
	case ".", "->":
		obj := i - 2
		if obj < from {
			return refUnknown, nil
		}
		switch {
		case p.is(obj, "this"):
			return refThis, nil
		case p.ident(obj):
			if r := p.refAt[obj]; r != nil {
				return refMember, r
			}
		case p.is(obj, ")"), p.is(obj, "]"):
			return p.resultQualifier(obj, from)
		}
		return refUnknown, nil
	case "::":
		q := i - 2
		if q >= from && (p.is(q, ">") || p.is(q, ">>")) {
			q = p.templateName(q, from)
		}
		if r := p.refAt[q]; r != nil && q >= from {
			return refScoped, r
		}
		if q < from || !p.ident(q) {
			return refGlobal, nil
		}
		return refUnknown, nil
	}
	return refPlain, nil
}

func (p *parser) resultQualifier(obj, from int) (refKind, *code.Ref) {
	open := p.match[obj]
	if open <= from {
		return refUnknown, nil
	}
	callee := open - 1
	if p.is(callee, ">") || p.is(callee, ">>") {
		callee = p.templateName(callee, from)
	}
	r := p.refAt[callee]
	if r == nil {
		return refUnknown, nil
	}
	if p.is(obj, ")") {
		return refResult, r
	}
	return refElem, r
}

// templateName returns the index of the identifier whose template argument
// list closes at gt, or -1.
func (p *parser) templateName(gt, from int) int {
	depth := 0
	for j := gt; j >= from; j-- {
		switch p.text(j) {
		case ">":
			depth++
		case ">>":
			depth += 2
		case "<":
			depth--
			if depth <= 0 {
				return j - 1
			}
		case ")", "]", "}":
			if m := p.match[j]; m >= 0 {
				j = m
			}
		case ";", "{":
			return -1
		}
	}
	return -1
}

// lambda parses the lambda [open, close], where close is the brace that ends
// its body. fn is the function declaration the lambda is assigned to. If
// there's none, the parameters and locals become locals of the enclosing
// named declaration. At namespace scope they'd read as globals, so there we
// don't declare them and they only shadow outer names.
func (p *parser) lambda(sc *scope, open, close int, fn *code.Decl) {
	owner := sc.owner
	kind := scopeBlock
	if fn != nil {
		owner, kind = fn, scopeFunc
	}
	ls := newScope(kind, sc, fn, owner)
	declare := owner.Callable() || owner.Local
	capEnd := p.closeOf(open, close)
	p.lambdaCaptures(sc, ls, open+1, capEnd, declare)
	j := capEnd + 1
	if p.is(j, "<") {
		if e := p.templateArgsEnd(j, close, false); e >= 0 {
			for _, tp := range p.templateParams(ls, j+1, e) {
				if declare {
					d := p.f.NewDecl(owner, code.KindTypeParameter, tp.name, tp.span, tp.full)
					d.Local = true
					p.fi.decls[d] = &declInfo{sc: ls, arity: -1}
					ls.add(d)
				} else {
					ls.hide(tp.name)
				}
			}
			j = e + 1
		}
	}
	body := close
	for k := j; k < close; k++ {
		if p.is(k, "{") && p.closeOf(k, close+1) >= close {
			body = k
			break
		}
	}
	if p.is(j, "(") {
		pe := p.closeOf(j, body)
		var target *code.Decl
		if declare {
			target = owner
		}
		n, variadic, names := p.params(ls, j, pe, target)
		if target == nil {
			for _, nm := range names {
				ls.hide(nm)
			}
		}
		if fn != nil {
			p.info(fn).arity, p.info(fn).variadic = n, variadic
		}
		j = pe + 1
	}
	for k := j; k < body; k++ {
		switch {
		case p.is(k, "->"):
			p.scanType(ls, k+1, body)
			k = body
		case p.is(k, "(") || p.is(k, "["):
			m := p.closeOf(k, body)
			p.scanExpr(ls, k+1, m)
			k = m
		}
	}
	if fn != nil {
		fn.Body = p.spanOf(body, close)
		p.info(fn).def = true
		p.info(fn).body = ls
	}
	p.i = body + 1
	p.stmts(ls, close)
}

func (p *parser) lambdaCaptures(sc, ls *scope, from, end int, declare bool) {
	for _, s := range p.splitTop(from, end) {
		e := p.exprEnd(s, end, true, false)
		j := s
		if p.is(j, "&") || p.is(j, "*") {
			j++
		}
		if p.ident(j) && p.is(j+1, "=") || p.ident(j) && p.is(j+1, "{") {
			p.scanExpr(sc, j+1, e)
			if declare {
				v := p.newDecl(ls, code.KindVariable, j, p.spanOf(s, e-1))
				ls.add(v)
			} else {
				ls.hide(p.text(j))
			}
			continue
		}
		p.scanExpr(sc, s, e)
	}
}
