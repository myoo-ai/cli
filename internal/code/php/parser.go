package php

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// maxDepth bounds how deep the parser recurses into nested brackets. We
// skip deeper brackets, so hostile input can't exhaust the stack.
const maxDepth = 400

type scope struct {
	owner    *code.Decl // receives the variables declared in the scope
	refScope *code.Decl // Ref.Scope of the references in the scope
	vars     map[string]*code.Decl
	// Set for arrow functions, which can see the variables of the enclosing
	// scope.
	parent *scope
	// Set when the owner isn't a function, like a closure at file level.
	// New declarations there still have to be local.
	local bool
}

func (s *scope) lookup(name string) *code.Decl {
	for x := s; x != nil; x = x.parent {
		if d := x.vars[name]; d != nil {
			return d
		}
	}
	return nil
}

// nsCtx is the namespace we're parsing, with its imports.
type nsCtx struct {
	decl    *code.Decl // namespace declaration, or the module
	name    string     // "App\Models", "" for the global namespace
	classes map[string]useEntry
	funcs   map[string]useEntry
	consts  map[string]useEntry
}

type useEntry struct {
	full  string
	alias *code.Decl // the import declaration of an `as` alias
}

func newNS(decl *code.Decl, name string) *nsCtx {
	return &nsCtx{decl: decl, name: name, classes: map[string]useEntry{}, funcs: map[string]useEntry{}, consts: map[string]useEntry{}}
}

// classCtx is the class whose body we're parsing.
type classCtx struct {
	decl      *code.Decl // nil for anonymous classes
	key       string
	parentKey string
}

type parser struct {
	f      *code.File
	t      []tok
	match  []int
	p      int
	info   *fileInfo
	ns     *nsCtx
	openNS *code.Decl // statement-form namespace that runs until the next one
	sc     *scope
	global *scope
	cls    *classCtx
	depth  int
}

func parse(f *code.File) *fileInfo {
	l := lex(f.Src, 0, len(f.Src), startsInCode(f.Src))
	codeToks := code.SetTokens(f, l.toks, tok.token)
	f.Comments = l.comments
	f.SetModule("")
	p := &parser{
		f:    f,
		t:    codeToks,
		info: &fileInfo{keys: map[*code.Decl]string{}, types: map[*code.Decl]string{}, bases: map[*code.Decl][]string{}},
	}
	p.match = matchBrackets(f.Src, codeToks)
	p.ns = newNS(f.Module, "")
	p.global = &scope{owner: f.Module, refScope: f.Module, vars: map[string]*code.Decl{}}
	p.sc = p.global
	p.statements(false)
	p.closeNamespace(len(p.t))
	return p.info
}

// matchBrackets returns, for each token, the index of the bracket that
// matches it, or -1.
func matchBrackets(src []byte, toks []tok) []int {
	m := make([]int, len(toks))
	var stack []int
	for i, t := range toks {
		m[i] = -1
		if t.kind != code.TokPunct || t.e-t.s != 1 {
			continue
		}
		switch c := src[t.s]; c {
		case '(', '[', '{':
			stack = append(stack, i)
		case ')', ']', '}':
			open := map[byte]byte{')': '(', ']': '[', '}': '{'}[c]
			// A `}` also closes any parentheses and brackets left open in
			// its block. Those stay unmatched.
			for n, s := range slices.Backward(stack) {
				if src[toks[s].s] == open {
					m[i], m[s] = s, i
					stack = stack[:n]
					break
				}
				if c != '}' {
					break
				}
			}
		}
	}
	return m
}

func (p *parser) text(i int) string {
	if i < 0 || i >= len(p.t) {
		return ""
	}
	return string(p.f.Src[p.t[i].s:p.t[i].e])
}

func (p *parser) is(i int, s string) bool { return p.text(i) == s }

// kw reports whether token i is the keyword w. PHP keywords are
// case-insensitive.
func (p *parser) kw(i int, w string) bool {
	return i >= 0 && i < len(p.t) && p.t[i].kind == code.TokKeyword && strings.EqualFold(p.text(i), w)
}

func (p *parser) isVar(i int) bool {
	return i >= 0 && i < len(p.t) && p.t[i].kind == code.TokIdent && p.f.Src[p.t[i].s] == '$'
}

func (p *parser) isName(i int) bool {
	return i >= 0 && i < len(p.t) && p.t[i].kind == code.TokIdent && p.f.Src[p.t[i].s] != '$'
}

func (p *parser) isOpenTag(i int) bool { return strings.HasPrefix(p.text(i), "<?") }

func (p *parser) isCloseTag(i int) bool { return p.is(i, "?>") }

func (p *parser) span(i, j int) code.Span {
	j = min(j, len(p.t)-1)
	if j < i {
		return code.Span{Start: p.t[i].s, End: p.t[i].e}
	}
	return code.Span{Start: p.t[i].s, End: p.t[j].e}
}

// nameSpan returns the span of variable token i without the `$`.
func (p *parser) nameSpan(i int) code.Span {
	t := p.t[i]
	if p.f.Src[t.s] == '$' {
		return code.Span{Start: t.s + 1, End: t.e}
	}
	return code.Span{Start: t.s, End: t.e}
}

func (p *parser) varName(i int) string { return p.text(i)[1:] }

func (p *parser) prevEnd() int {
	if p.p == 0 {
		return 0
	}
	return p.t[min(p.p, len(p.t))-1].e
}

func (p *parser) skipGroup() {
	if m := p.match[p.p]; m > p.p {
		p.p = m + 1
		return
	}
	p.p++
}

// groupEnd returns the index of the bracket that closes the one at open. If
// the bracket is unbalanced, we return the next `;`, close tag or `{`
// instead, so a missing bracket can't swallow the rest of the file.
func (p *parser) groupEnd(open int) int {
	if m := p.match[open]; m > open {
		return m
	}
	k := open + 1
	for k < len(p.t) && !p.is(k, ";") && !p.isCloseTag(k) && !p.is(k, "{") {
		k++
	}
	return k
}

// closeGroup moves the cursor to end, and past it if it's a matched closing
// bracket.
func (p *parser) closeGroup(end int) {
	p.p = min(end, len(p.t))
	if p.p < len(p.t) && p.match[p.p] >= 0 && p.match[p.p] < p.p {
		p.p++
	}
}

func (p *parser) consume(s string) {
	if p.is(p.p, s) {
		p.p++
	}
}

// statements parses statements up to the end. If inBlock is set, we stop at
// the `}` that closes the block and leave it for the caller.
func (p *parser) statements(inBlock bool) {
	for p.p < len(p.t) {
		if p.is(p.p, "}") {
			if inBlock {
				return
			}
			p.p++
			continue
		}
		start := p.p
		p.statement()
		if p.p == start {
			p.p++
		}
	}
}

func (p *parser) block() {
	if p.depth > maxDepth {
		p.skipGroup()
		return
	}
	p.depth++
	p.p++
	p.statements(true)
	p.consume("}")
	p.depth--
}

var (
	controlParens  = code.StringSet("if", "elseif", "while", "switch")
	controlBare    = code.StringSet("else", "do", "try", "finally", "endif", "endwhile", "endfor", "endforeach", "endswitch", "enddeclare")
	classModifier  = code.StringSet("abstract", "final", "readonly")
	memberModifier = code.StringSet("public", "protected", "private", "static", "abstract", "final", "readonly", "var")
)

func (p *parser) statement() {
	i := p.p
	x := strings.ToLower(p.text(i))
	switch {
	case p.isOpenTag(i) || p.isCloseTag(i) || x == ";" || x == ":":
		p.p++
	case x == "{":
		p.block()
	case x == "#" && p.is(i+1, "["), p.t[i].kind == code.TokKeyword && classModifier[x]:
		p.declaration()
	case p.kw(i, "class") || p.kw(i, "interface") || p.kw(i, "trait") || p.kw(i, "enum"):
		p.declaration()
	case p.kw(i, "function") && (p.isName(i+1) || p.is(i+1, "&") && p.isName(i+2)):
		p.declaration()
	case p.kw(i, "namespace") && !p.is(i+1, "\\"):
		p.namespace()
	case p.kw(i, "use") && p.sc == p.global && p.cls == nil:
		p.useImports()
	case p.kw(i, "const"):
		p.constants(nil, i)
	case p.kw(i, "static") && p.isVar(i+1):
		p.staticVars()
	case p.kw(i, "global"):
		p.globals()
	case p.kw(i, "foreach"):
		p.foreach()
	case p.kw(i, "for"):
		p.forStmt()
	case p.kw(i, "catch"):
		p.catch()
	case p.kw(i, "declare"):
		p.p++
		if p.is(p.p, "(") {
			p.skipGroup()
		}
	case p.t[i].kind == code.TokKeyword && controlParens[x]:
		p.p++
		if p.is(p.p, "(") {
			p.group()
		}
	case p.t[i].kind == code.TokKeyword && controlBare[x]:
		p.p++
	case p.kw(i, "case"):
		p.p++
		p.expr(stops{colon: true})
		if p.is(p.p, ":") || p.is(p.p, ";") {
			p.p++
		}
	case p.kw(i, "default") && (p.is(i+1, ":") || p.is(i+1, ";")):
		p.p += 2
	case p.isName(i) && p.is(i+1, ":"):
		p.p += 2 // goto label
	default:
		p.expr(stops{})
		p.consume(";")
	}
}

// declaration parses a class-like or function declaration with its
// leading attributes and modifiers. If there's no declaration, it parses
// the attributes and leaves the rest for the next statement.
func (p *parser) declaration() {
	start := p.p
	p.attributes()
	mods := p.p
	for p.p < len(p.t) && p.t[p.p].kind == code.TokKeyword && classModifier[strings.ToLower(p.text(p.p))] {
		p.p++
	}
	switch {
	case p.kw(p.p, "class") || p.kw(p.p, "interface") || p.kw(p.p, "trait") || p.kw(p.p, "enum"):
		if p.isName(p.p + 1) {
			p.classDecl(start)
			return
		}
	case p.kw(p.p, "function") && (p.isName(p.p+1) || p.is(p.p+1, "&") && p.isName(p.p+2)):
		p.function(start)
		return
	}
	// Without a declaration, the attributes belong to the expression that
	// follows, and we skip a stray modifier.
	p.p = mods
	if p.p == start {
		p.p++
	}
}

// attributes parses `#[Attr(args), Other]` groups and records their class
// names as references.
func (p *parser) attributes() {
	for p.is(p.p, "#") && p.is(p.p+1, "[") {
		open := p.p + 1
		end := p.groupEnd(open)
		p.p += 2
		for p.p < end {
			if p.nameStart(p.p) {
				nm, _ := p.readName()
				p.classRef(nm, p.classKey(nm))
				if p.is(p.p, "(") {
					p.group()
				}
				continue
			}
			p.p++
		}
		p.closeGroup(end)
	}
}

// namespace parses `namespace A\B;` and the braced `namespace A\B { }`.
func (p *parser) namespace() {
	kw := p.p
	p.closeNamespace(kw)
	p.p++
	var decl *code.Decl
	name := ""
	if p.nameStart(p.p) {
		nm, _ := p.readName()
		name = strings.Join(nm.parts, "\\")
		decl = p.f.NewDecl(p.f.Module, code.KindNamespace, strings.Join(nm.parts, "."), p.span(nm.first, nm.last), code.Span{Start: p.t[kw].s, End: len(p.f.Src)})
	}
	scopeDecl := decl
	if scopeDecl == nil {
		scopeDecl = p.f.Module
	}
	p.ns = newNS(scopeDecl, name)
	p.global.refScope = scopeDecl
	if !p.is(p.p, "{") {
		p.openNS = decl
		p.consume(";")
		return
	}
	body := p.p
	p.block()
	if decl != nil {
		decl.Body = p.span(body, p.p-1)
		decl.Span.End = p.prevEnd()
	}
	p.ns = newNS(p.f.Module, "")
	p.global.refScope = p.f.Module
}

// closeNamespace ends the open statement-form namespace before token at.
func (p *parser) closeNamespace(at int) {
	if p.openNS == nil {
		return
	}
	if at > 0 {
		p.openNS.Span.End = p.t[min(at, len(p.t))-1].e
	}
	p.openNS = nil
}

// useImports parses `use A\B [as C], ...;`, `use function ...`, `use const
// ...` and group imports `use A\{B, C as D}`.
func (p *parser) useImports() {
	start := p.p
	p.p++
	kind := importKind(p.text(p.p))
	if kind != refClass {
		p.p++
	}
	for p.p < len(p.t) {
		nm, ok := p.readName()
		if !ok {
			break
		}
		if p.is(p.p, "\\") && p.is(p.p+1, "{") {
			prefix := strings.Join(nm.parts, "\\")
			end := p.groupEnd(p.p + 1)
			p.p += 2
			for p.p < end && !p.is(p.p, ";") && !p.isCloseTag(p.p) {
				k := kind
				if ik := importKind(p.text(p.p)); ik != refClass {
					k = ik
					p.p++
				}
				item, ok := p.readName()
				if !ok {
					p.p++
					continue
				}
				p.importItem(start, k, prefix+"\\"+strings.Join(item.parts, "\\"), item)
				p.consume(",")
			}
			if p.p == end {
				p.consume("}")
			}
		} else {
			p.importItem(start, kind, strings.Join(nm.parts, "\\"), nm)
		}
		if !p.is(p.p, ",") {
			break
		}
		p.p++
	}
	for p.p < len(p.t) && !p.is(p.p, ";") && !p.isCloseTag(p.p) {
		p.p++
	}
	p.consume(";")
}

func importKind(word string) refKind {
	switch strings.ToLower(word) {
	case "function":
		return refFunc
	case "const":
		return refConst
	}
	return refClass
}

func (p *parser) importItem(start int, kind refKind, full string, nm name) {
	alias := full[strings.LastIndexByte(full, '\\')+1:]
	var decl *code.Decl
	if p.kw(p.p, "as") && p.isName(p.p+1) {
		at := p.p + 1
		alias = p.text(at)
		decl = p.f.NewDecl(p.ns.decl, code.KindImport, alias, p.span(at, at), p.span(start, at))
		decl.Type = full
		p.p += 2
	}
	ref := p.f.NewRef(p.sc.refScope, nm.parts[len(nm.parts)-1], p.span(nm.last, nm.last))
	ref.Import = true
	ri := refInfo{ref: ref, kind: kind}
	entry := useEntry{full: full, alias: decl}
	switch kind {
	case refFunc:
		ri.keys = []string{strings.ToLower(full)}
		p.ns.funcs[strings.ToLower(alias)] = entry
	case refConst:
		ri.keys = []string{constKey(full)}
		p.ns.consts[alias] = entry
	default:
		ri.keys = []string{strings.ToLower(full)}
		p.ns.classes[strings.ToLower(alias)] = entry
	}
	p.info.refs = append(p.info.refs, ri)
}

// constants parses `const [type] A = 1, B = 2;` in a namespace (cls nil)
// or a class body. start is the first token of the declaration.
func (p *parser) constants(cls *classCtx, start int) {
	p.p++
	for p.p < len(p.t) {
		eq := p.p
		for eq < len(p.t) && !p.is(eq, "=") && !p.is(eq, ";") && !p.is(eq, ",") && !p.is(eq, "}") && eq-p.p < 16 {
			eq++
		}
		at := eq - 1
		if !p.is(eq, "=") || at < p.p || p.t[at].kind != code.TokIdent && p.t[at].kind != code.TokKeyword {
			break
		}
		typ := ""
		if at > p.p {
			typ = p.compact(p.p, at)
		}
		var d *code.Decl
		switch {
		case cls == nil:
			d = p.f.NewDecl(p.ns.decl, code.KindConstant, p.text(at), p.span(at, at), p.span(start, at))
			p.info.keys[d] = constKey(qualify(p.ns.name, p.text(at)))
		case cls.decl != nil:
			d = p.f.NewDecl(cls.decl, code.KindConstant, p.text(at), p.span(at, at), p.span(start, at))
		}
		p.p = eq + 1
		p.expr(stops{comma: true})
		if d != nil {
			d.Type = typ
			d.Span.End = p.prevEnd()
		}
		if !p.is(p.p, ",") {
			break
		}
		p.p++
		start = p.p
	}
	p.consume(";")
}

// compact returns the source of tokens [i, j) without white space. We use
// it for things like types.
func (p *parser) compact(i, j int) string {
	var b strings.Builder
	for k := i; k < j && k < len(p.t); k++ {
		b.WriteString(p.text(k))
	}
	return b.String()
}

func qualify(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "\\" + name
}

// function parses a named function. A function declared inside a function
// body is still global, so it belongs to the namespace.
func (p *parser) function(start int) {
	p.p++
	p.consume("&")
	at := p.p
	p.p++
	d := p.f.NewDecl(p.ns.decl, code.KindFunction, p.text(at), p.span(at, at), code.Span{Start: p.t[start].s})
	p.info.keys[d] = strings.ToLower(qualify(p.ns.name, p.text(at)))
	outer := p.cls
	p.cls = nil
	d.Body = p.fnRest(fnScope{owner: d, refScope: d, result: d})
	p.cls = outer
	d.Span.End = p.prevEnd()
}

// fnScope describes the function whose signature and body fnRest parses.
type fnScope struct {
	owner, refScope *code.Decl
	local           bool
	parent          *scope     // set for arrow functions
	promote         *code.Decl // class that receives promoted constructor parameters
	result          *code.Decl // declaration whose Type is the result type
	closure         bool       // a `use (...)` clause may follow the parameters
	arrow           bool       // the body is `=> expr` ending at a comma
}

// fnRest parses parameters, a closure's use clause, the result type and
// the body. If the body is a block, it returns the block's span.
func (p *parser) fnRest(fs fnScope) code.Span {
	outer := p.sc
	p.sc = &scope{owner: fs.owner, refScope: fs.refScope, vars: map[string]*code.Decl{}, parent: fs.parent, local: fs.local}
	defer func() { p.sc = outer }()
	if p.is(p.p, "(") {
		p.params(fs.promote)
	}
	if fs.closure && p.kw(p.p, "use") {
		p.closureUses(outer)
	}
	if p.is(p.p, ":") {
		p.p++
		typ, key := p.typ()
		if fs.result != nil {
			fs.result.Type = typ
			if key != "" {
				p.info.types[fs.result] = key
			}
		}
	}
	switch {
	case p.is(p.p, "=>"):
		p.p++
		p.expr(stops{comma: fs.arrow})
		if !fs.arrow {
			p.consume(";")
		}
	case p.is(p.p, "{"):
		start := p.p
		p.block()
		return p.span(start, p.p-1)
	case p.is(p.p, ";"):
		p.p++
	}
	return code.Span{}
}

var (
	paramModifier = code.StringSet("public", "protected", "private", "readonly", "final")
	superGlobals  = code.StringSet("GLOBALS", "_SERVER", "_GET", "_POST", "_FILES", "_COOKIE", "_SESSION", "_REQUEST", "_ENV", "this")
)

// params parses a parameter list. A parameter with a visibility or readonly
// modifier also declares a property of promote.
func (p *parser) params(promote *code.Decl) {
	end := p.groupEnd(p.p)
	p.p++
	for p.p < end {
		start := p.p
		p.attributes()
		promoted := false
		for p.p < end && p.t[p.p].kind == code.TokKeyword && paramModifier[strings.ToLower(p.text(p.p))] {
			promoted = true
			p.p++
			if p.is(p.p, "(") {
				p.skipGroup()
			}
		}
		typ, key := "", ""
		if !p.isVar(p.p) && !p.is(p.p, "&") && !p.is(p.p, "...") {
			typ, key = p.typ()
		}
		p.consume("&")
		p.consume("...")
		if p.isVar(p.p) {
			at := p.p
			name := p.varName(at)
			p.p++
			d := p.declare(code.KindParameter, name, at, p.span(start, at))
			d.Type = typ
			if key != "" {
				p.info.types[d] = key
			}
			if p.is(p.p, "=") {
				p.p++
				p.expr(stops{comma: true})
				d.Span.End = p.prevEnd()
			}
			if promoted && promote != nil {
				prop := p.f.NewDecl(promote, code.KindProperty, name, p.nameSpan(at), d.Span)
				prop.Type = typ
				if key != "" {
					p.info.types[prop] = key
				}
			}
		}
		for p.p < end && !p.is(p.p, ",") {
			p.p++
		}
		p.consume(",")
		if p.p == start {
			p.p++
		}
	}
	p.closeGroup(end)
}

func (p *parser) declare(kind code.DeclKind, name string, at int, span code.Span) *code.Decl {
	d := p.f.NewDecl(p.sc.owner, kind, name, p.nameSpan(at), span)
	if p.sc.local {
		d.Local = true
	}
	p.sc.vars[name] = d
	return d
}

// closureUses parses `use ($a, &$b)`. A captured variable is the variable
// of the enclosing scope. If that doesn't exist yet, a by-reference capture
// creates it.
func (p *parser) closureUses(outer *scope) {
	p.p++
	if !p.is(p.p, "(") {
		return
	}
	end := p.groupEnd(p.p)
	for p.p++; p.p < end; p.p++ {
		byRef := p.is(p.p, "&")
		if byRef {
			p.p++
		}
		if !p.isVar(p.p) {
			continue
		}
		name := p.varName(p.p)
		target := outer.lookup(name)
		switch {
		case target != nil:
			p.varRef(name, p.p, target)
		case byRef:
			cur := p.sc
			p.sc = outer
			target = p.declare(code.KindVariable, name, p.p, p.span(p.p, p.p))
			p.sc = cur
		default:
			target = p.declare(code.KindVariable, name, p.p, p.span(p.p, p.p))
		}
		p.sc.vars[name] = target
	}
	p.closeGroup(end)
}

// classDecl parses a class, interface, trait or enum. start is the first
// attribute or modifier, and the cursor has to be on the keyword.
func (p *parser) classDecl(start int) {
	kind := code.KindClass
	switch strings.ToLower(p.text(p.p)) {
	case "interface":
		kind = code.KindInterface
	case "enum":
		kind = code.KindEnum
	}
	at := p.p + 1
	p.p += 2
	d := p.f.NewDecl(p.ns.decl, kind, p.text(at), p.span(at, at), code.Span{Start: p.t[start].s})
	key := strings.ToLower(qualify(p.ns.name, p.text(at)))
	p.info.keys[d] = key
	cc := &classCtx{decl: d, key: key}
	outerSc, outerCls := p.sc, p.cls
	p.sc = &scope{owner: d, refScope: d, vars: map[string]*code.Decl{}}
	p.cls = cc
	if kind == code.KindEnum && p.is(p.p, ":") {
		p.p++
		d.Type, _ = p.typ()
	}
	p.classHeader(cc)
	if p.is(p.p, "{") {
		body := p.p
		p.classBody(cc)
		d.Body = p.span(body, p.p-1)
	}
	d.Span.End = p.prevEnd()
	p.sc, p.cls = outerSc, outerCls
}

// classHeader parses the extends and implements clauses.
func (p *parser) classHeader(cc *classCtx) {
	for p.kw(p.p, "extends") || p.kw(p.p, "implements") {
		extends := p.kw(p.p, "extends")
		p.p++
		for p.nameStart(p.p) {
			nm, _ := p.readName()
			key := p.classKey(nm)
			p.classRef(nm, key)
			if extends && cc.parentKey == "" {
				cc.parentKey = key
			}
			if cc.decl != nil {
				cc.decl.Bases = append(cc.decl.Bases, nm.text)
				p.info.bases[cc.decl] = append(p.info.bases[cc.decl], key)
			}
			if !p.is(p.p, ",") {
				break
			}
			p.p++
		}
	}
}

// classBody parses `{ members }`.
func (p *parser) classBody(cc *classCtx) {
	if p.depth > maxDepth {
		p.skipGroup()
		return
	}
	p.depth++
	defer func() { p.depth-- }()
	p.p++
	for p.p < len(p.t) && !p.is(p.p, "}") {
		start := p.p
		p.member(cc)
		if p.p == start {
			p.p++
		}
	}
	p.consume("}")
}

func (p *parser) member(cc *classCtx) {
	start := p.p
	p.attributes()
	for p.p < len(p.t) && p.t[p.p].kind == code.TokKeyword && memberModifier[strings.ToLower(p.text(p.p))] {
		p.p++
		if p.is(p.p, "(") {
			p.skipGroup() // private(set)
		}
	}
	switch {
	case p.kw(p.p, "function"):
		p.method(cc, start)
	case p.kw(p.p, "const"):
		p.constants(cc, start)
	case p.kw(p.p, "use"):
		p.traitUse(cc)
	case p.kw(p.p, "case"):
		p.enumCase(cc, start)
	case p.is(p.p, "}"):
	case p.isVar(p.p) || p.typeStart(p.p):
		p.property(cc, start)
	default:
		p.p++
	}
}

// method parses a method. Methods of anonymous classes don't get a
// declaration, so their parameters and variables become locals of the
// enclosing scope.
func (p *parser) method(cc *classCtx, start int) {
	p.p++
	p.consume("&")
	if p.p >= len(p.t) || p.t[p.p].kind != code.TokIdent {
		return
	}
	at := p.p
	p.p++
	if cc.decl == nil {
		p.fnRest(fnScope{owner: p.sc.owner, refScope: p.sc.refScope, local: true})
		return
	}
	kind := code.KindMethod
	if strings.EqualFold(p.text(at), "__construct") {
		kind = code.KindConstructor
	}
	d := p.f.NewDecl(cc.decl, kind, p.text(at), p.span(at, at), code.Span{Start: p.t[start].s})
	d.Body = p.fnRest(fnScope{owner: d, refScope: d, result: d, promote: cc.decl})
	d.Span.End = p.prevEnd()
}

// property parses `[type] $a [= x], $b;` with optional PHP 8.4 hooks.
func (p *parser) property(cc *classCtx, start int) {
	typ, key := "", ""
	if !p.isVar(p.p) {
		typ, key = p.typ()
	}
	for p.isVar(p.p) {
		at := p.p
		p.p++
		var d *code.Decl
		if cc.decl != nil {
			d = p.f.NewDecl(cc.decl, code.KindProperty, p.varName(at), p.nameSpan(at), p.span(start, at))
			d.Type = typ
			if key != "" {
				p.info.types[d] = key
			}
		}
		if p.is(p.p, "=") {
			p.p++
			p.expr(stops{comma: true})
		}
		if p.is(p.p, "{") {
			p.hooks(d)
		}
		if d != nil {
			d.Span.End = p.prevEnd()
		}
		if !p.is(p.p, ",") {
			break
		}
		p.p++
		start = p.p
	}
	p.consume(";")
}

// hooks parses the get and set hooks of a property. Their parameters and
// variables are locals of the property.
func (p *parser) hooks(prop *code.Decl) {
	end := p.groupEnd(p.p)
	owner := prop
	if owner == nil {
		owner = p.sc.owner
	}
	for p.p++; p.p < end; {
		start := p.p
		p.attributes()
		for p.kw(p.p, "final") || p.is(p.p, "&") {
			p.p++
		}
		if name := strings.ToLower(p.text(p.p)); p.isName(p.p) && (name == "get" || name == "set") {
			p.p++
			p.fnRest(fnScope{owner: owner, refScope: p.sc.refScope, local: true})
		}
		if p.p == start {
			p.p++
		}
	}
	p.closeGroup(end)
}

// traitUse parses `use A, B;` and `use A { ... }` in a class body.
func (p *parser) traitUse(cc *classCtx) {
	p.p++
	for p.nameStart(p.p) {
		nm, _ := p.readName()
		key := p.classKey(nm)
		p.classRef(nm, key)
		if cc.decl != nil {
			p.info.bases[cc.decl] = append(p.info.bases[cc.decl], key)
		}
		if !p.is(p.p, ",") {
			break
		}
		p.p++
	}
	if p.is(p.p, "{") {
		p.skipGroup()
	}
	p.consume(";")
}

// enumCase parses `case Name [= value];`.
func (p *parser) enumCase(cc *classCtx, start int) {
	p.p++
	if p.p >= len(p.t) || p.t[p.p].kind != code.TokIdent && p.t[p.p].kind != code.TokKeyword {
		return
	}
	at := p.p
	p.p++
	var d *code.Decl
	if cc.decl != nil {
		d = p.f.NewDecl(cc.decl, code.KindEnumMember, p.text(at), p.span(at, at), p.span(start, at))
	}
	if p.is(p.p, "=") {
		p.p++
		p.expr(stops{})
	}
	if d != nil {
		d.Span.End = p.prevEnd()
	}
	p.consume(";")
}

// staticVars parses `static $a = 1, $b;` in a function.
func (p *parser) staticVars() {
	p.p++
	for p.isVar(p.p) {
		at := p.p
		name := p.varName(at)
		p.p++
		var d *code.Decl
		if p.sc.vars[name] == nil && !superGlobals[name] {
			d = p.declare(code.KindVariable, name, at, p.span(at, at))
		} else {
			p.varRef(name, at, p.sc.lookup(name))
		}
		if p.is(p.p, "=") {
			p.p++
			p.expr(stops{comma: true})
			if d != nil {
				d.Span.End = p.prevEnd()
			}
		}
		if !p.is(p.p, ",") {
			break
		}
		p.p++
	}
	p.consume(";")
}

// globals parses `global $a, $b;`, which binds the names to the variables
// of the file scope.
func (p *parser) globals() {
	p.p++
	for p.isVar(p.p) {
		at := p.p
		name := p.varName(at)
		g := p.global.vars[name]
		if g == nil {
			cur := p.sc
			p.sc = p.global
			g = p.declare(code.KindVariable, name, at, p.span(at, at))
			p.sc = cur
		} else {
			p.varRef(name, at, g)
		}
		p.sc.vars[name] = g
		p.p++
		if !p.is(p.p, ",") {
			break
		}
		p.p++
	}
	p.consume(";")
}

// foreach parses the header of `foreach ($xs as $k => $v)`. The targets
// declare variables.
func (p *parser) foreach() {
	p.p++
	if !p.is(p.p, "(") {
		return
	}
	end := p.match[p.p]
	if end < 0 {
		p.group()
		return
	}
	p.p++
	p.expr(stops{as: true})
	if p.kw(p.p, "as") {
		p.p++
		p.target()
		if p.is(p.p, "=>") {
			p.p++
			p.target()
		}
	}
	for p.p < end {
		start := p.p
		p.expr(stops{})
		if p.p == start {
			p.p++
		}
	}
	p.p = end + 1
}

// forStmt parses `for (init; cond; step)`.
func (p *parser) forStmt() {
	p.p++
	if !p.is(p.p, "(") {
		return
	}
	end := p.match[p.p]
	if end < 0 {
		p.group()
		return
	}
	for p.p++; p.p < end; {
		start := p.p
		p.expr(stops{})
		if p.p == start {
			p.p++
		}
	}
	p.p = end + 1
}

// catch parses `catch (A | B $e)`.
func (p *parser) catch() {
	p.p++
	if !p.is(p.p, "(") {
		return
	}
	end := p.match[p.p]
	if end < 0 {
		p.group()
		return
	}
	var keys []string
	for p.p++; p.p < end; {
		switch {
		case p.isVar(p.p):
			d := p.assignVar(p.p)
			if d != nil && len(keys) == 1 {
				p.info.types[d] = keys[0]
			}
			p.p++
		case p.nameStart(p.p):
			nm, _ := p.readName()
			key := p.classKey(nm)
			p.classRef(nm, key)
			keys = append(keys, key)
		default:
			p.p++
		}
	}
	p.p = end + 1
}

// target parses a single assignment target of foreach or a destructuring.
func (p *parser) target() {
	p.consume("&")
	switch {
	case p.isVar(p.p) && !p.is(p.p+1, "[") && !p.is(p.p+1, "->") && !p.is(p.p+1, "?->") && !p.is(p.p+1, "::"):
		p.assignVar(p.p)
		p.p++
	case p.is(p.p, "["):
		p.destructure()
	case p.kw(p.p, "list") && p.is(p.p+1, "("):
		p.p++
		p.destructure()
	default:
		p.expr(stops{comma: true, arrow: true})
	}
}

// destructure parses `[$a, 'k' => $b, [$c]]` or the parenthesis of
// `list(...)` as assignment targets.
func (p *parser) destructure() {
	end := p.match[p.p]
	if end < 0 || p.depth > maxDepth {
		p.group()
		return
	}
	p.depth++
	defer func() { p.depth-- }()
	for p.p++; p.p < end; {
		if p.is(p.p, ",") {
			p.p++
			continue
		}
		start := p.p
		if p.elementHasKey(end) {
			p.expr(stops{comma: true, arrow: true})
			p.consume("=>")
		}
		p.target()
		for p.p < end && !p.is(p.p, ",") {
			p.p++
		}
		if p.p == start {
			p.p++
		}
	}
	p.p = end + 1
}

// elementHasKey reports whether the array element at the cursor has a
// `key =>` part before the next comma or end.
func (p *parser) elementHasKey(end int) bool {
	for k := p.p; k < end; k++ {
		switch {
		case p.is(k, "=>"):
			return true
		case p.is(k, ","):
			return false
		case p.match[k] > k:
			k = p.match[k]
		}
	}
	return false
}

// assignVar records the variable that token at assigns to. If the scope has
// no variable of that name yet (and it isn't a superglobal), we declare it
// and return it. Otherwise we record a reference and return nil.
func (p *parser) assignVar(at int) *code.Decl {
	name := p.varName(at)
	if d := p.sc.lookup(name); d != nil || superGlobals[name] {
		p.varRef(name, at, d)
		return nil
	}
	return p.declare(code.KindVariable, name, at, p.span(at, at))
}

func (p *parser) varRef(name string, at int, target *code.Decl) int {
	ref := p.f.NewRef(p.sc.refScope, name, p.nameSpan(at))
	p.info.refs = append(p.info.refs, refInfo{ref: ref, kind: refVar, decl: target})
	return len(p.info.refs) - 1
}
