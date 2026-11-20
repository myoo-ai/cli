package cpp

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type declarator struct {
	nameTok  int // first token of the name, -1 for abstract declarators
	name     string
	nameSpan code.Span
	recv     string // qualifier of an out-of-line name, "::" written as "."
	ptr      string // pointer and reference operators
	arr      string // array suffixes
	nested   bool   // (*name), a function pointer or pointer to array
	fn       bool
	ctor     bool
	dtor     bool
	open     int // open and close parens of the parameter list
	close    int
	bindings []int // structured binding names
}

func (p *parser) declarator(sc *scope, end int, ctx declCtx, auto bool) declarator {
	d := declarator{nameTok: -1, open: -1, close: -1}
	var ptr strings.Builder
	for p.i < end {
		s := p.text(p.i)
		switch {
		case s == "*" || s == "&" || s == "&&" || s == "^":
			ptr.WriteString(s)
			p.i++
			continue
		case cvKeywords[s] || s == "..." || s == "_Nonnull" || s == "_Nullable":
			p.i++
			continue
		case s == "[" && p.is(p.i+1, "["), attrWords[s] && p.is(p.i+1, "("):
			p.skipAttributes(end)
			continue
		}
		break
	}
	d.ptr = ptr.String()
	switch {
	case p.is(p.i, "(") && (p.is(p.i+1, "*") || p.is(p.i+1, "&") || p.is(p.i+1, "^") ||
		p.ident(p.i+1) && p.is(p.i+2, "::")):
		m := p.closeOf(p.i, end)
		p.i++
		inner := p.declarator(sc, m, ctxParam, false)
		inner.ptr = d.ptr + inner.ptr
		d = inner
		d.nested = true
		p.i = m + 1
		if p.is(p.i, "(") {
			c := p.closeOf(p.i, end)
			p.params(newScope(scopeBlock, sc, nil, sc.owner), p.i, c, nil)
			d.arr += "(" + p.compact(p.i+1, c) + ")"
			p.i = c + 1
		}
	case p.is(p.i, "[") && auto && ctx != ctxClass && ctx != ctxParam:
		m := p.closeOf(p.i, end)
		for k := p.i + 1; k < m; k++ {
			if p.ident(k) {
				d.bindings = append(d.bindings, k)
			}
		}
		p.i = m + 1
		return d
	default:
		p.declName(sc, end, &d)
	}
	for p.i < end {
		switch {
		case p.is(p.i, "["):
			if p.is(p.i+1, "[") {
				p.skipAttributes(end)
				continue
			}
			m := p.closeOf(p.i, end)
			p.scanExpr(sc, p.i+1, m)
			d.arr += "[" + p.compact(p.i+1, m) + "]"
			p.i = m + 1
			continue
		case p.is(p.i, "(") && d.nameTok >= 0 && !d.nested && !d.fn:
			if ctx == ctxLocal || ctx == ctxFor || ctx == ctxCond || ctx == ctxParam ||
				ctx == ctxTop && !p.looksLikeParams(p.i, end) {
				return d
			}
			d.fn = true
			d.open = p.i
			d.close = p.closeOf(p.i, end)
			p.i = d.close + 1
			return d
		case attrWords[p.text(p.i)] && p.is(p.i+1, "("):
			p.skipAttributes(end)
			continue
		}
		break
	}
	return d
}

// declName parses the declarator id at p.i. Besides plain and qualified names
// it handles destructor names and operator function ids.
func (p *parser) declName(sc *scope, end int, d *declarator) {
	from := p.i
	var recv []string
	if p.is(p.i, "::") {
		p.i++
	}
	for p.i < end {
		switch {
		case p.is(p.i, "~") && p.ident(p.i+1):
			d.nameTok = p.i
			d.name = "~" + p.text(p.i+1)
			d.nameSpan = p.spanOf(p.i, p.i+1)
			d.dtor = true
			p.i += 2
		case p.is(p.i, "operator"):
			p.operatorName(sc, end, d)
		case p.ident(p.i) && p.is(p.i+1, "::") || p.ident(p.i) && p.is(p.i+1, "<") && p.qualifiedAfterArgs(p.i+1, end):
			kind, prev := p.qualifier(p.i, from)
			p.newRef(sc, p.i, kind, prev)
			recv = append(recv, p.text(p.i))
			p.i++
			if p.is(p.i, "<") {
				e := p.templateArgsEnd(p.i, end, false)
				p.scanType(sc, p.i+1, e)
				p.i = e + 1
			}
			p.i++
			if p.is(p.i, "template") {
				p.i++
			}
			if p.is(p.i, "*") {
				// A pointer to member like int C::*pm.
				p.i++
				d.ptr += "*"
				recv = nil
				for p.i < end && cvKeywords[p.text(p.i)] {
					p.i++
				}
			}
			continue
		case p.ident(p.i):
			d.nameTok = p.i
			d.name = p.text(p.i)
			d.nameSpan = p.span(p.i)
			p.i++
		}
		break
	}
	if d.nameTok < 0 {
		return
	}
	if len(recv) > 0 {
		d.recv = strings.Join(recv, ".")
		d.ctor = !d.dtor && d.name == recv[len(recv)-1]
	} else if sc.kind == scopeClass && sc.decl != nil {
		d.ctor = d.name == sc.decl.Name
	}
}

// qualifiedAfterArgs reports whether the template argument list at lt is
// followed by `::`, as in Box<T>::get.
func (p *parser) qualifiedAfterArgs(lt, end int) bool {
	e := p.templateArgsEnd(lt, end, false)
	return e >= 0 && p.is(e+1, "::")
}

func (p *parser) operatorName(sc *scope, end int, d *declarator) {
	s := p.i
	j := s + 1
	switch {
	case p.is(j, "(") && p.is(j+1, ")"), p.is(j, "[") && p.is(j+1, "]"):
		j += 2
	case p.is(j, "new") || p.is(j, "delete") || p.is(j, "co_await"):
		j++
		if p.is(j, "[") && p.is(j+1, "]") {
			j += 2
		}
	case p.kind(j) == code.TokString:
		j++
		if p.ident(j) {
			j++
		}
	case p.kind(j) == code.TokPunct && !p.is(j, "("):
		j++
	default:
		// A conversion function like operator const char*().
		for j < end && !p.is(j, "(") {
			if p.ident(j) {
				kind, prev := p.qualifier(j, s+1)
				p.newRef(sc, j, kind, prev)
			}
			j++
		}
		d.nameTok = s
		d.name = "operator " + p.compact(s+1, j)
		d.nameSpan = p.spanOf(s, j-1)
		p.i = j
		return
	}
	var b strings.Builder
	b.WriteString("operator")
	for k := s + 1; k < j; k++ {
		if p.kind(k) == code.TokKeyword {
			b.WriteByte(' ')
		}
		b.WriteString(p.text(k))
	}
	d.nameTok = s
	d.name = b.String()
	d.nameSpan = p.spanOf(s, j-1)
	p.i = j
}

// looksLikeParams tells a parameter list apart from constructor arguments in
// a namespace-scope declaration like `Logger log("app");`.
func (p *parser) looksLikeParams(open, end int) bool {
	close := p.closeOf(open, end)
	if close == open+1 {
		return true
	}
	first := open + 1
	if !p.ident(first) && p.kind(first) != code.TokKeyword && !p.is(first, "::") &&
		!p.is(first, "...") && !(p.is(first, "[") && p.is(first+1, "[")) {
		return false
	}
	for k := first; k < close; k++ {
		switch p.kind(k) {
		case code.TokString, code.TokNumber:
			return false
		case code.TokKeyword:
			if p.is(k, "this") || p.is(k, "true") || p.is(k, "false") || p.is(k, "nullptr") || p.is(k, "new") || p.is(k, "sizeof") {
				return false
			}
		case code.TokPunct:
			switch p.text(k) {
			case "::", "*", "&", "&&", "<", ">", ">>", ",", "...", "[", "]", "(", ")":
			case "=":
				return true
			default:
				return false
			}
		}
	}
	return true
}

// params declares the parameters [open+1, close) in scope fs, and under fn if
// it isn't nil.
func (p *parser) params(fs *scope, open, close int, fn *code.Decl) (n int, variadic bool, names []string) {
	if close <= open+1 {
		return 0, false, nil
	}
	saved := p.i
	defer func() { p.i = saved }()
	items := p.splitTop(open+1, close)
	for _, s := range items {
		e := p.exprEnd(s, close, true, false)
		if p.is(s, "...") && e == s+1 {
			variadic = true
			continue
		}
		if len(items) == 1 && p.is(s, "void") && e == s+1 {
			return 0, false, nil
		}
		n++
		p.i = s
		sp := p.declSpecs(fs, e)
		if !sp.hasType {
			p.scanType(fs, p.i, e)
			continue
		}
		pack := p.is(p.i, "...")
		d := p.declarator(fs, e, ctxParam, sp.auto)
		if pack || strings.Contains(p.compact(s, p.i), "...") {
			variadic = true
		}
		if p.is(p.i, "=") {
			p.scanExpr(fs, p.i+1, e)
		} else if p.i < e {
			p.scanType(fs, p.i, e)
		}
		if d.nameTok < 0 {
			continue
		}
		names = append(names, d.name)
		if fn == nil {
			continue
		}
		prm := p.f.NewDecl(fn, code.KindParameter, d.name, d.nameSpan, p.spanOf(s, e-1))
		prm.Type = p.typeText(sp) + d.ptr + d.arr
		p.fi.decls[prm] = &declInfo{sc: fs, def: true, arity: -1}
		fs.add(prm)
	}
	return n, variadic, names
}

// simpleDecl parses the declarators of a simple declaration after we've
// parsed its specifiers sp. mark is the number of references before the
// specifiers. It reports whether it recognized a declaration.
func (p *parser) simpleDecl(sc *scope, end int, sp specs, ctx declCtx, mark int) bool {
	tm := p.takeTmpl()
	start := sp.start
	if tm != nil {
		start = tm.start
	}
	if !sp.hasType && !p.ctorAhead(sc, p.i, end) && !p.is(p.i, "operator") && !p.is(p.i, "~") {
		return false
	}
	if sp.tdecl != nil && p.is(p.i, ";") {
		p.i++
		return true
	}
	if sp.anonymous && p.is(p.i, ";") {
		p.i++
		return true
	}
	typ := p.typeText(sp)
	first := true
	for p.i < end {
		dstart := p.i
		d := p.declarator(sc, end, ctx, sp.auto)
		if d.nameTok < 0 && len(d.bindings) == 0 {
			return !first || sp.tdecl != nil
		}
		from := dstart
		if first {
			from = start
		}
		first = false
		var made []*code.Decl
		if d.fn && !d.nested {
			fn := p.function(sc, end, sp, ctx, mark, tm, d, typ, from)
			if !fn.Body.Empty() {
				return true
			}
			made = append(made, fn)
		} else {
			made = p.variables(sc, end, sp, ctx, d, typ, from)
			if ctx == ctxFor && p.is(p.i, ":") {
				return true
			}
		}
		if p.is(p.i, ",") {
			p.i++
			continue
		}
		if p.is(p.i, ";") && ctx != ctxFor && ctx != ctxCond {
			for _, x := range made {
				x.Span.End = p.t[p.i].e
			}
			p.i++
		}
		return true
	}
	return true
}

func (p *parser) function(sc *scope, end int, sp specs, ctx declCtx, mark int, tm *tmplInfo, d declarator, typ string, from int) *code.Decl {
	kind := code.KindFunction
	switch {
	case sp.friend:
	case d.ctor:
		kind = code.KindConstructor
	case ctx == ctxClass || d.recv != "":
		kind = code.KindMethod
	}
	fn := p.f.NewDecl(sc.owner, kind, d.name, d.nameSpan, p.spanOf(from, d.close))
	fn.Receiver = d.recv
	if kind != code.KindConstructor && !d.dtor {
		fn.Type = typ + d.ptr
	}
	fi := &declInfo{sc: sc, arity: -1}
	p.fi.decls[fn] = fi
	fs := newScope(scopeFunc, sc, fn, fn)
	fi.body = fs
	if tm != nil {
		p.typeParams(fn, fs, tm.params)
	}
	fi.arity, fi.variadic, _ = p.params(fs, d.open, d.close, fn)
	headerEnd := len(p.f.Refs)
	p.i = d.close + 1
	body := p.funcTail(fs, fn, end)
	for _, r := range p.f.Refs[mark:] {
		if ri := p.fi.refs[r]; ri != nil && ri.sc == sc {
			ri.sc = fs
		}
	}
	if body {
		for _, r := range p.f.Refs[mark:headerEnd] {
			if r.Scope == sc.owner {
				r.Scope = fn
			}
		}
		fi.def = true
	} else {
		// A prototype has no body, so we move the references in it to the
		// enclosing declaration.
		for _, r := range p.f.Refs[mark:] {
			if r.Scope == fn {
				r.Scope = sc.owner
			}
		}
	}
	fn.Span = p.spanOf(from, p.i-1)
	if fn.Receiver == "" && kind != code.KindConstructor && !d.dtor {
		p.register(sc, fn)
	}
	if sp.static && (sc.kind == scopeFile || sc.kind == scopeNamespace) {
		fi.fileLocal = true
	}
	return fn
}

// funcTail parses what follows a parameter list: qualifiers, a trailing
// return type, `= 0`, a constructor initializer list and the body. It
// reports whether there was a body.
func (p *parser) funcTail(fs *scope, fn *code.Decl, end int) bool {
	tryBlock := false
	for p.i < end {
		s := p.text(p.i)
		switch {
		case s == "const" || s == "volatile" || s == "&" || s == "&&" || s == "mutable":
			p.i++
		case (s == "noexcept" || s == "throw") && p.is(p.i+1, "("):
			m := p.closeOf(p.i+1, end)
			p.scanExpr(fs, p.i+2, m)
			p.i = m + 1
		case s == "noexcept":
			p.i++
		case s == "[" && p.is(p.i+1, "["), attrWords[s] && p.is(p.i+1, "("):
			p.skipAttributes(end)
		case s == "->":
			p.trailingReturnType(fs, fn, end)
		case s == "requires":
			p.requires(fs, end)
		case s == "=":
			if p.is(p.i+1, "0") || p.is(p.i+1, "default") || p.is(p.i+1, "delete") {
				p.i += 2
				continue
			}
			return false
		case s == ":":
			p.memInits(fs, end)
		case s == "try":
			tryBlock = true
			p.i++
		case s == "{":
			close := p.closeOf(p.i, end)
			fn.Body = p.spanOf(p.i, close)
			p.i++
			p.stmts(fs, close)
			p.i = close + 1
			if tryBlock {
				p.handlers(fs, end)
			}
			return true
		case s == "override" || s == "final" || p.ident(p.i) && macroLike(s) && !p.is(p.i+1, "("):
			// Macros such as Q_DECL_OVERRIDE or __THROW-like annotations.
			p.i++
		default:
			return false
		}
	}
	return false
}

func (p *parser) trailingReturnType(fs *scope, fn *code.Decl, end int) {
	j := p.i + 1
	for j < end && !p.is(j, "{") && !p.is(j, ";") && !p.is(j, "=") && !p.is(j, "requires") &&
		!p.is(j, "override") && !p.is(j, "final") && !p.is(j, ",") {
		switch {
		case p.is(j, "<"):
			if e := p.templateArgsEnd(j, end, false); e >= 0 {
				j = e
			}
		case p.is(j, "(") || p.is(j, "["):
			j = p.closeOf(j, end)
		}
		j++
	}
	p.scanType(fs, p.i+1, j)
	fn.Type = p.compact(p.i+1, j)
	p.i = j
}

// memInits parses a constructor initializer list starting at the `:`.
func (p *parser) memInits(fs *scope, end int) {
	p.i++
	for p.i < end {
		switch {
		case p.is(p.i, "{") && p.memInitBody(p.i):
			return
		case p.ident(p.i) || p.is(p.i, "::"):
			p.i = p.typeName(fs, p.i, end)
		case p.is(p.i, "(") || p.is(p.i, "{"):
			m := p.closeOf(p.i, end)
			p.scanExpr(fs, p.i+1, m)
			p.i = m + 1
		case p.is(p.i, ",") || p.is(p.i, "..."):
			p.i++
		default:
			return
		}
	}
}

// memInitBody reports whether the brace at i starts the constructor body, as
// opposed to a braced member initializer. We can tell because the body
// follows a closing bracket, not a member name.
func (p *parser) memInitBody(i int) bool {
	return !p.ident(i-1) && !p.is(i-1, ">") && !p.is(i-1, ">>")
}

// variables declares the variables, fields or type aliases of declarator d
// and parses their initializers.
func (p *parser) variables(sc *scope, end int, sp specs, ctx declCtx, d declarator, typ string, from int) []*code.Decl {
	kind := code.KindVariable
	switch {
	case sp.typedef:
		kind = code.KindTypeAlias
	case ctx == ctxClass && !sp.friend || d.recv != "":
		kind = code.KindField
	case ctx == ctxTop && (sp.constexpr || sp.constq && !strings.Contains(d.ptr, "*")):
		kind = code.KindConstant
	}
	toks := []int{d.nameTok}
	if len(d.bindings) > 0 {
		toks = d.bindings
	}
	var made []*code.Decl
	for _, k := range toks {
		name, nameSpan := d.name, d.nameSpan
		if len(d.bindings) > 0 {
			name, nameSpan = p.text(k), p.span(k)
		}
		if kind == code.KindTypeAlias && sp.tdecl != nil && name == sp.tdecl.Name {
			continue
		}
		x := p.f.NewDecl(sc.owner, kind, name, nameSpan, p.spanOf(from, p.i-1))
		x.Receiver = d.recv
		if len(d.bindings) == 0 {
			x.Type = typ + d.ptr + d.arr
		}
		def := !sp.extern && !(sp.static && ctx == ctxClass)
		p.fi.decls[x] = &declInfo{sc: sc, def: def, arity: -1}
		if x.Receiver == "" {
			p.register(sc, x)
		}
		if sp.static && (sc.kind == scopeFile || sc.kind == scopeNamespace) {
			p.info(x).fileLocal = true
		}
		made = append(made, x)
	}
	p.initializer(sc, end, ctx, sp, made)
	for _, x := range made {
		x.Span.End = p.t[min(p.i, len(p.t))-1].e
	}
	return made
}

func (p *parser) initializer(sc *scope, end int, ctx declCtx, sp specs, made []*code.Decl) {
	var x *code.Decl
	if len(made) == 1 {
		x = made[0]
	}
	switch {
	case p.is(p.i, "="):
		s := p.i + 1
		if x != nil && p.is(s, "[") {
			if le := p.lambdaEnd(s, end); le >= 0 && (p.is(le+1, ";") || p.is(le+1, ",") || le+1 >= end) {
				x.Kind = code.KindFunction
				x.Type = ""
				p.lambda(sc, s, le, x)
				p.i = le + 1
				return
			}
		}
		e := p.exprEnd(s, end, true, false)
		p.scanExpr(sc, s, e)
		p.deduce(x, sp, s, e)
		p.i = e
	case p.is(p.i, "{"), p.is(p.i, "(") && ctx != ctxClass:
		m := p.closeOf(p.i, end)
		p.scanExpr(sc, p.i+1, m)
		p.i = m + 1
	case p.is(p.i, ":") && ctx == ctxClass:
		e := p.exprEnd(p.i+1, end, true, false)
		p.scanExpr(sc, p.i+1, e)
		p.i = e
	}
}

// deduce records how to infer the type of an auto variable from the
// initializer [s, e). We handle `new T(...)`, `T(...)`, `T{...}`,
// `make_unique<T>()` and the result of a call.
func (p *parser) deduce(x *code.Decl, sp specs, s, e int) {
	if x == nil || !sp.auto || e <= s {
		return
	}
	di := p.info(x)
	if p.is(s, "new") {
		if k := p.newTypeName(s+1, e); k >= 0 {
			x.Type = p.compact(s+1, k+1)
			di.init = p.refAt[k]
		}
		return
	}
	last := e - 1
	if !p.is(last, ")") && !p.is(last, "}") {
		return
	}
	open := p.match[last]
	if open <= s {
		return
	}
	callee := open - 1
	if p.is(callee, ">") || p.is(callee, ">>") {
		lt := p.templateName(callee, s)
		if lt >= s {
			name := p.text(lt)
			if name == "make_unique" || name == "make_shared" {
				p.deduceSmartPointer(x, s, lt, callee, name)
				return
			}
			callee = lt
		}
	}
	r := p.refAt[callee]
	if r == nil {
		return
	}
	di.init = r
	start := callee
	for start-2 >= s && p.is(start-1, "::") && p.ident(start-2) {
		start -= 2
	}
	if start == s && r.Name != "" && r.Name[0] >= 'A' && r.Name[0] <= 'Z' {
		x.Type = p.compact(start, callee+1)
	}
}

func (p *parser) deduceSmartPointer(x *code.Decl, from, nameTok, end int, name string) {
	k := nameTok + 2
	j := p.skipTypeName(k, end)
	if j <= k {
		return
	}
	q := nameTok
	for q-2 >= from && p.is(q-1, "::") && p.ident(q-2) {
		q -= 2
	}
	x.Type = p.compact(q, nameTok) + strings.TrimPrefix(name, "make_") + "_ptr<" + p.compact(k, j) + ">"
}

// handlers parses catch clauses after a try block.
func (p *parser) handlers(sc *scope, end int) {
	for p.is(p.i, "catch") && p.is(p.i+1, "(") {
		open := p.i + 1
		close := p.closeOf(open, end)
		hs := newScope(scopeBlock, sc, nil, sc.owner)
		if !(p.is(open+1, "...") && close == open+2) {
			p.i = open + 1
			mark := len(p.f.Refs)
			sp := p.declSpecs(hs, close)
			p.simpleDecl(hs, close, sp, ctxCond, mark)
		}
		p.i = close + 1
		p.statement(hs, end)
	}
}
