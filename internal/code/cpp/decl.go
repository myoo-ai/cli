package cpp

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type declCtx uint8

const (
	ctxTop   declCtx = iota // file or namespace scope
	ctxClass                // class body
	ctxLocal                // function body
	ctxFor                  // for-init, stops at the `:` of a range-for
	ctxCond                 // condition or catch parameter, not ended by `;`
	ctxParam
)

type tparam struct {
	name       string
	span, full code.Span
}

// tmplInfo holds a template parameter list until we get to the declaration it
// belongs to.
type tmplInfo struct {
	start  int
	params []tparam
}

func (p *parser) takeTmpl() *tmplInfo {
	t := p.tmpl
	p.tmpl = nil
	return t
}

type specs struct {
	start                     int
	static, extern, typedef   bool
	friend, constexpr, constq bool
	hasType, auto             bool
	tFrom, tTo                int        // token range of the type
	tdecl                     *code.Decl // type defined in the specifiers
	typeText                  string     // used instead of the token range if set
	typedefNamed              bool       // `typedef struct {..} Name;` named tdecl
	anonymous                 bool       // tdecl has no name
	hadSpec                   bool
}

func (p *parser) items(sc *scope, end int) {
	ctx := ctxTop
	if sc.kind == scopeClass {
		ctx = ctxClass
	}
	for p.i < end {
		start := p.i
		p.declaration(sc, end, ctx)
		if p.i <= start {
			p.i = start + 1
		}
	}
}

func (p *parser) declaration(sc *scope, end int, ctx declCtx) {
	i := p.i
	s := p.text(i)
	switch {
	case s == ";":
		p.i++
	case s == "namespace" || s == "inline" && p.is(i+1, "namespace"):
		p.namespace(sc, end)
	case s == "template":
		p.template(sc, end, ctx)
	case s == "using":
		p.using(sc, end)
	case (s == "public" || s == "private" || s == "protected") && sc.kind == scopeClass:
		p.i++
		for p.i < end && !p.is(p.i, ":") && p.ident(p.i) {
			p.i++
		}
		if p.is(p.i, ":") {
			p.i++
		}
	case s == "extern" && p.kind(i+1) == code.TokString && p.is(i+2, "{"):
		m := p.closeOf(i+2, end)
		p.i = i + 3
		p.items(sc, m)
		p.i = m + 1
	case s == "static_assert" || s == "_Static_assert" || s == "concept" || s == "asm" || s == "__asm__":
		e := p.exprEnd(i+1, end, false, false)
		p.scanExpr(sc, i+1, e)
		p.i = e + 1
	case s == "friend" && (p.is(i+1, "class") || p.is(i+1, "struct") || p.is(i+1, "union")):
		e := p.exprEnd(i+2, end, false, false)
		p.scanType(sc, i+2, e)
		p.i = e + 1
	case s == "{":
		m := p.closeOf(i, end)
		p.i = i + 1
		p.stmts(newScope(scopeBlock, sc, nil, sc.owner), m)
		p.i = m + 1
	case ctx != ctxLocal && p.ident(i) && p.is(i+1, "(") && !p.ctorAhead(sc, i, end):
		p.macroCall(sc, end)
	case ctx != ctxLocal && p.ident(i) && macroLike(s) && p.newline(i+1) &&
		(p.kind(i+1) == code.TokKeyword || p.is(i+1, "}")):
		p.newRef(sc, i, refPlain, nil)
		p.i++
	default:
		mark := len(p.f.Refs)
		sp := p.declSpecs(sc, end)
		if !p.simpleDecl(sc, end, sp, ctx, mark) && p.i == i {
			p.i++
		}
	}
}

// macroCall handles `NAME(args)` where we expected a declaration. That's a
// macro invocation, maybe followed by a braced body like the one of the test
// macro TEST(a, b) { ... }, which we parse as a block.
func (p *parser) macroCall(sc *scope, end int) {
	i := p.i
	m := p.closeOf(i+1, end)
	p.scanExpr(sc, i, m+1)
	p.i = m + 1
	switch {
	case p.is(p.i, ";"):
		p.i++
	case p.is(p.i, "{"):
		c := p.closeOf(p.i, end)
		p.i++
		p.stmts(newScope(scopeBlock, sc, nil, sc.owner), c)
		p.i = c + 1
	}
}

// declSpecs parses declaration specifiers and the type at p.i.
func (p *parser) declSpecs(sc *scope, end int) specs {
	sp := specs{start: p.i, tFrom: -1}
	for p.i < end {
		i := p.i
		s := p.text(i)
		switch {
		case s == "[" && p.is(i+1, "["), attrWords[s] && p.is(i+1, "("):
			p.skipAttributes(end)
			continue
		case specKeywords[s]:
			sp.hadSpec = true
			switch s {
			case "static":
				sp.static = true
			case "extern":
				sp.extern = true
				if p.kind(i+1) == code.TokString {
					p.i++
				}
			case "typedef":
				sp.typedef = true
			case "friend":
				sp.friend = true
			case "constexpr", "consteval", "constinit":
				sp.constexpr = true
			}
			p.i++
			if (s == "explicit" || s == "_Noreturn") && p.is(p.i, "(") {
				p.i = p.closeOf(p.i, end) + 1
			}
			continue
		case cvKeywords[s]:
			sp.hadSpec = true
			sp.constq = sp.constq || s == "const"
			p.i++
			continue
		case sp.hasType:
		case builtinTypes[s]:
			sp.tFrom = i
			for p.i < end && (builtinTypes[p.text(p.i)] || cvKeywords[p.text(p.i)]) {
				p.i++
			}
			sp.tTo = p.i
			sp.hasType = true
			sp.auto = s == "auto" && sp.tTo == i+1
			continue
		case (s == "decltype" || s == "typeof" || s == "__typeof__" || s == "_Atomic") && p.is(i+1, "("):
			m := p.closeOf(i+1, end)
			p.scanExpr(sc, i+2, m)
			sp.tFrom, sp.tTo, sp.hasType = i, m+1, true
			p.i = m + 1
			continue
		case s == "class" || s == "struct" || s == "union" || s == "enum":
			p.classSpec(sc, end, &sp)
			continue
		case s == "typename":
			p.i++
			continue
		case p.ident(i) || s == "::":
			if p.ctorAhead(sc, i, end) {
				return sp
			}
			if p.ident(i) && macroLike(s) && (p.kind(i+1) == code.TokKeyword && !p.is(i+1, "operator") ||
				p.ident(i+1) && (p.ident(i+2) || p.kind(i+2) == code.TokKeyword || p.is(i+2, "*") || p.is(i+2, "&") || p.is(i+2, "::"))) {
				p.newRef(sc, i, refPlain, nil)
				p.i++
				continue
			}
			j := p.typeName(sc, i, end)
			sp.tFrom, sp.tTo, sp.hasType = i, j, true
			p.i = j
			continue
		}
		break
	}
	return sp
}

func (p *parser) typeText(sp specs) string {
	t := sp.typeText
	if t == "" && sp.tFrom >= 0 {
		t = p.compact(sp.tFrom, sp.tTo)
	}
	if sp.constq && t != "" {
		t = "const " + t
	}
	return t
}

// classSpec parses a class, struct, union or enum specifier at p.i. Besides
// definitions it handles forward declarations and elaborated type names.
func (p *parser) classSpec(sc *scope, end int, sp *specs) {
	start := p.i
	mark := len(p.f.Refs)
	kw := p.text(p.i)
	p.i++
	scoped := false
	if kw == "enum" && (p.is(p.i, "class") || p.is(p.i, "struct")) {
		scoped = true
		p.i++
	}
	for {
		p.skipAttributes(end)
		if p.ident(p.i) && macroLike(p.text(p.i)) && p.ident(p.i+1) {
			p.newRef(sc, p.i, refPlain, nil)
			p.i++
			continue
		}
		break
	}
	nameTok := p.className(sc, end)
	for p.is(p.i, "final") || p.is(p.i, "sealed") {
		p.i++
	}
	bases := p.classBases(sc, end, kw == "enum")
	kind := code.KindStruct
	switch kw {
	case "class":
		kind = code.KindClass
	case "enum":
		kind = code.KindEnum
	}
	sp.hasType, sp.tFrom = true, start
	if !p.is(p.i, "{") {
		sp.tTo = p.i
		if nameTok < 0 {
			return
		}
		if p.is(p.i, ";") && !sp.typedef && !sp.friend && !sp.hadSpec && sp.start == start {
			tm := p.takeTmpl()
			from := start
			if tm != nil {
				from = tm.start
			}
			d := p.newDecl(sc, kind, nameTok, p.spanOf(from, p.i))
			sp.tdecl = d
			p.register(sc, d)
			return
		}
		kind, prev := p.qualifier(nameTok, start)
		p.newRef(sc, nameTok, kind, prev)
		return
	}
	p.classDefinition(sc, end, sp, kind, nameTok, mark, scoped, bases)
}

func (p *parser) classDefinition(sc *scope, end int, sp *specs, kind code.DeclKind, nameTok, mark int, scoped bool, bases []string) {
	open := p.i
	close := p.closeOf(open, end)
	if nameTok < 0 && sp.typedef && p.ident(close+1) && (p.is(close+2, ";") || p.is(close+2, ",")) {
		nameTok = close + 1
		sp.typedefNamed = true
	}
	sp.tTo = close + 1
	if nameTok < 0 {
		sp.anonymous = true
		p.i = open + 1
		switch {
		case kind == code.KindEnum:
			p.enumerators(sc, sc, false, open+1, close)
		case sc.kind == scopeClass:
			p.items(sc, close)
		default:
			p.items(newScope(scopeClass, sc, nil, sc.owner), close)
		}
		p.i = close + 1
		return
	}
	tm := p.takeTmpl()
	from := sp.start
	if tm != nil {
		from = tm.start
	}
	last := close
	if p.is(close+1, ";") {
		last = close + 1
	} else if sp.typedefNamed && p.is(close+2, ";") {
		last = close + 2
	}
	d := p.newDecl(sc, kind, nameTok, p.spanOf(from, last))
	for _, r := range p.f.Refs[mark:] {
		if r.Scope == sc.owner {
			r.Scope = d
		}
	}
	d.Body = p.spanOf(open, close)
	d.Bases = bases
	di := p.info(d)
	di.def, di.scoped = true, scoped
	bs := newScope(scopeClass, sc, d, d)
	di.body = bs
	if tm != nil {
		p.typeParams(d, bs, tm.params)
	}
	p.register(sc, d)
	sp.tdecl = d
	sp.typeText = d.Name
	p.i = open + 1
	if kind == code.KindEnum {
		p.enumerators(bs, sc, scoped, open+1, close)
	} else {
		p.items(bs, close)
	}
	p.i = close + 1
}

func (p *parser) className(sc *scope, end int) int {
	nameTok := -1
	if !p.ident(p.i) && !p.is(p.i, "::") {
		return nameTok
	}
	from := p.i
	if p.is(p.i, "::") {
		p.i++
	}
	for p.ident(p.i) {
		if p.is(p.i+1, "::") {
			kind, prev := p.qualifier(p.i, from)
			p.newRef(sc, p.i, kind, prev)
			p.i += 2
			continue
		}
		nameTok = p.i
		p.i++
		break
	}
	if p.is(p.i, "<") {
		if e := p.templateArgsEnd(p.i, end, false); e >= 0 {
			p.scanType(sc, p.i+1, e)
			p.i = e + 1
		}
	}
	return nameTok
}

func (p *parser) classBases(sc *scope, end int, enum bool) []string {
	if !p.is(p.i, ":") {
		return nil
	}
	p.i++
	stop := p.i
	for stop < end && !p.is(stop, "{") && !p.is(stop, ";") {
		stop++
	}
	var bases []string
	if enum {
		p.scanType(sc, p.i, stop)
	} else {
		for _, s := range p.splitTop(p.i, stop) {
			e := p.exprEnd(s, stop, true, false)
			for p.is(s, "public") || p.is(s, "private") || p.is(s, "protected") || p.is(s, "virtual") {
				s++
			}
			if s < e {
				bases = append(bases, baseName(p.compact(s, e)))
				p.scanType(sc, s, e)
			}
		}
	}
	p.i = stop
	return bases
}

// baseName renders a base class as written, but with `::` as "." and without
// template arguments.
func baseName(s string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '<':
			depth++
		case c == '>':
			depth--
		case depth > 0:
		case c == ':' && i+1 < len(s) && s[i+1] == ':':
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// enumerators parses the enumerator list [from, to) into scope bs. The owner
// of bs is the enum, or the enclosing declaration for anonymous enums.
// Unscoped enumerators are also visible in outer.
func (p *parser) enumerators(bs, outer *scope, scoped bool, from, to int) {
	for _, s := range p.splitTop(from, to) {
		e := p.exprEnd(s, to, true, false)
		p.i = s
		p.skipAttributes(e)
		if !p.ident(p.i) {
			continue
		}
		d := p.newDecl(bs, code.KindEnumMember, p.i, p.spanOf(s, e-1))
		p.info(d).def = true
		bs.add(d)
		if !scoped && outer != bs {
			p.register(outer, d)
		}
		if p.is(p.i+1, "=") {
			p.scanExpr(bs, p.i+2, e)
		}
	}
}

// register makes d visible in sc and marks file-local declarations.
func (p *parser) register(sc *scope, d *code.Decl) {
	sc.add(d)
	if sc.anon {
		p.info(d).fileLocal = true
	}
}

func (p *parser) namespace(sc *scope, end int) {
	start := p.i
	inline := map[int]bool{}
	lead := p.is(p.i, "inline")
	if lead {
		p.i++
	}
	p.i++
	p.skipAttributes(end)
	var comps []int
	for p.i < end {
		in := p.is(p.i, "inline")
		if in {
			p.i++
		}
		if !p.ident(p.i) {
			break
		}
		inline[p.i] = in
		comps = append(comps, p.i)
		p.i++
		if !p.is(p.i, "::") {
			break
		}
		p.i++
	}
	if lead && len(comps) > 0 {
		inline[comps[len(comps)-1]] = true
	}
	if p.is(p.i, "=") {
		e := p.exprEnd(p.i+1, end, false, false)
		p.scanType(sc, p.i+1, e)
		p.i = e + 1
		return
	}
	if !p.is(p.i, "{") {
		return
	}
	open := p.i
	close := p.closeOf(open, end)
	inner := sc
	if len(comps) == 0 {
		inner = &scope{kind: scopeNamespace, parent: sc, owner: sc.owner, nsKey: sc.nsKey, names: sc.names, anon: true}
		inner.usingDecls = sc.usingDecls
	}
	for _, c := range comps {
		d := p.newDecl(inner, code.KindNamespace, c, p.spanOf(start, close))
		d.Body = p.spanOf(open, close)
		di := p.info(d)
		di.def = true
		di.nsKey = p.text(c)
		if inner.nsKey != "" {
			di.nsKey = inner.nsKey + "::" + p.text(c)
		}
		p.register(inner, d)
		ns := newScope(scopeNamespace, inner, d, d)
		ns.nsKey = di.nsKey
		ns.inline = inline[c]
		di.body = ns
		p.fi.nsScopes = append(p.fi.nsScopes, ns)
		inner = ns
	}
	p.i = open + 1
	p.items(inner, close)
	p.i = close + 1
}

func (p *parser) using(sc *scope, end int) {
	start := p.i
	i := p.i + 1
	e := p.exprEnd(i, end, false, false)
	defer func() { p.i = e + 1 }()
	switch {
	case p.is(i, "namespace"):
		p.scanType(sc, i+1, e)
		sc.usingNS = append(sc.usingNS, p.compact(i+1, e))
	case p.is(i, "enum"):
		p.scanType(sc, i+1, e)
	case p.ident(i) && (p.is(i+1, "=") || p.is(i+1, "[")):
		p.i = i + 1
		p.skipAttributes(e)
		eq := p.i
		tm := p.takeTmpl()
		from := start
		if tm != nil {
			from = tm.start
		}
		d := p.newDecl(sc, code.KindTypeAlias, i, p.spanOf(from, min(e, len(p.t)-1)))
		p.info(d).def = true
		if tm != nil {
			p.typeParams(d, newScope(scopeBlock, sc, nil, sc.owner), tm.params)
		}
		if p.is(eq, "=") {
			p.scanType(sc, eq+1, e)
			d.Type = p.compact(eq+1, e)
		}
		p.register(sc, d)
	default:
		p.scanType(sc, i, e)
		for k := e - 1; k >= i; k-- {
			if r := p.refAt[k]; r != nil {
				if sc.usingDecls == nil {
					sc.usingDecls = map[string]*code.Ref{}
				}
				sc.usingDecls[r.Name] = r
				break
			}
		}
	}
}

func (p *parser) template(sc *scope, end int, ctx declCtx) {
	start := p.i
	p.i++
	if !p.is(p.i, "<") {
		// An explicit instantiation like template class Box<int>;
		e := p.exprEnd(p.i, end, false, false)
		p.scanType(sc, p.i, e)
		p.i = e + 1
		return
	}
	gt := p.templateArgsEnd(p.i, end, false)
	if gt < 0 {
		p.i++
		return
	}
	params := p.templateParams(sc, p.i+1, gt)
	if p.tmpl != nil {
		p.tmpl.params = append(p.tmpl.params, params...)
	} else {
		p.tmpl = &tmplInfo{start: start, params: params}
	}
	p.i = gt + 1
	if p.is(p.i, "requires") {
		p.requires(sc, end)
	}
	p.declaration(sc, end, ctx)
	p.tmpl = nil
}

// requires skips a requires-clause but still records the references in it.
func (p *parser) requires(sc *scope, end int) {
	p.i++
	for p.i < end {
		from := p.i
		switch {
		case p.is(p.i, "("):
			p.i = p.closeOf(p.i, end) + 1
		case p.ident(p.i) || p.is(p.i, "::"):
			p.i = p.typeName(sc, p.i, end)
		default:
			return
		}
		p.scanExpr(sc, from+1, p.i-1)
		if !p.is(p.i, "&&") && !p.is(p.i, "||") {
			return
		}
		p.i++
	}
}

func (p *parser) templateParams(sc *scope, from, to int) []tparam {
	var out []tparam
	for _, s := range p.splitTop(from, to) {
		e := p.exprEnd(s, to, true, false)
		j := s
		if p.is(j, "template") && p.is(j+1, "<") {
			if gt := p.templateArgsEnd(j+1, e, false); gt >= 0 {
				j = gt + 1
			}
		}
		eq := j
		for eq < e && !p.is(eq, "=") {
			eq++
		}
		name := -1
		if (p.is(j, "class") || p.is(j, "typename")) && !p.is(j+2, "::") {
			j++
			if p.is(j, "...") {
				j++
			}
			if p.ident(j) && j < eq {
				name = j
			}
		} else {
			for k := eq - 1; k >= j; k-- {
				if p.ident(k) {
					name = k
					break
				}
			}
			if name >= 0 {
				p.scanType(sc, j, name)
			}
		}
		if eq < e {
			p.scanType(sc, eq+1, e)
		}
		if name >= 0 {
			out = append(out, tparam{name: p.text(name), span: p.span(name), full: p.spanOf(s, e-1)})
		}
	}
	return out
}

// typeParams declares template parameters under d, visible in sc.
func (p *parser) typeParams(d *code.Decl, sc *scope, params []tparam) {
	for _, tp := range params {
		x := p.f.NewDecl(d, code.KindTypeParameter, tp.name, tp.span, tp.full)
		x.Local = true
		p.fi.decls[x] = &declInfo{sc: sc, def: true, arity: -1}
		sc.add(x)
	}
}
