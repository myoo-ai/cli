package python

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type scopeKind uint8

const (
	scModule scopeKind = iota + 1
	scClass
	scFunc
	scLambda
	scComp
	// scAnnot holds type parameters. We evaluate the annotations, decorators
	// and bases of the declaration it belongs to in this scope.
	scAnnot
)

// scope is a Python name scope. Its declarations go to owner and its
// references get refDecl as Ref.Scope. The two differ for lambdas and
// comprehensions, because those have no declaration of their own.
type scope struct {
	kind     scopeKind
	owner    *code.Decl
	refDecl  *code.Decl
	parent   *scope
	names    map[string]*binding
	global   map[string]bool
	nonlocal map[string]bool
	stars    []string // modules of `from m import *`
}

func newScope(kind scopeKind, owner, refDecl *code.Decl, parent *scope) *scope {
	return &scope{kind: kind, owner: owner, refDecl: refDecl, parent: parent, names: map[string]*binding{}}
}

func (s *scope) root() *scope {
	for s.parent != nil {
		s = s.parent
	}
	return s
}

// binding is what a name stands for in a scope: a declaration, or the
// imported module or member if the name comes from an unaliased import.
type binding struct {
	decl *code.Decl
	imp  *importRef
}

// importRef names an imported module or a member of it.
type importRef struct {
	module string // absolute dotted module name, "" if we can't resolve it
	name   string
}

type noteKind uint8

const (
	noteName       noteKind = iota + 1
	noteAttr                // qual.member
	noteKwarg               // keyword argument, qual is the callee
	notePatternKw           // keyword of a class pattern, qual is the class
	noteModule              // module path segment of an import
	noteImportName          // name imported by `from m import name`
)

// note is the resolution context of a reference.
type note struct {
	kind noteKind
	sc   *scope
	qual *node
	imp  importRef
}

// declNote is the extra information the resolver needs about a declaration
// on top of code.Decl.
type declNote struct {
	ann, init *node      // annotation (return annotation for functions), initializer
	imp       *importRef // KindImport alias target
	bases     []*node
	selfOf    *code.Decl // class that a method's first parameter is bound to
	clsParam  bool       // selfOf binds the class itself (classmethod)
	star      int        // *args (1) or **kwargs (2) parameter
	overload  bool
}

// fileInfo is a parsed file plus the scopes and notes resolution needs.
type fileInfo struct {
	f         *code.File
	pkg       string // package that relative imports start from
	module    *scope
	notes     map[*code.Ref]*note
	declNotes map[*code.Decl]*declNote
}

type parser struct {
	f       *code.File
	src     []byte
	toks    []tok
	i       int
	lastEnd int // end of the last consumed real token
	info    *fileInfo
	sub     bool // parsing an f-string field or string annotation
}

func parseFile(f *code.File) *fileInfo {
	l := lex(f.Src, 0, len(f.Src), true, false)
	f.Tokens, f.Comments = l.code, l.comments
	name, isPkg := moduleName(f.Path, f.Abs)
	f.SetModule(name)
	info := &fileInfo{f: f, notes: map[*code.Ref]*note{}, declNotes: map[*code.Decl]*declNote{}}
	info.pkg = name
	if !isPkg {
		info.pkg = ""
		if parent, _, ok := strings.CutLast(name, "."); ok {
			info.pkg = parent
		}
	}
	p := &parser{f: f, src: f.Src, toks: l.toks, info: info}
	info.module = newScope(scModule, f.Module, f.Module, nil)
	p.stmts(info.module, true, false)
	slices.SortFunc(f.Comments, func(a, b code.Comment) int { return a.Span.Start - b.Span.Start })
	return info
}

func (p *parser) tok() *tok { return &p.toks[p.i] }

func (p *parser) peek(k int) *tok { return &p.toks[min(p.i+k, len(p.toks)-1)] }

func (p *parser) next() *tok {
	t := &p.toks[p.i]
	switch t.kind {
	case tNewline, tIndent, tDedent, tEOF:
	default:
		p.lastEnd = t.span.End
	}
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) isOp(s string) bool { t := p.tok(); return t.kind == tOp && t.text == s }
func (p *parser) isKw(s string) bool { t := p.tok(); return t.kind == tKeyword && t.text == s }

func (p *parser) acceptOp(s string) bool {
	if p.isOp(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) acceptKw(s string) bool {
	if p.isKw(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) nameTok() *tok {
	if p.tok().kind == tName {
		return p.next()
	}
	return nil
}

// markKeyword turns a soft keyword token (match, case, type) into a
// keyword in File.Tokens.
func (p *parser) markKeyword(t *tok) {
	if !p.sub && t.code >= 0 {
		p.f.Tokens[t.code].Kind = code.TokKeyword
	}
}

func (p *parser) text(sp code.Span) string {
	return strings.Join(strings.Fields(string(p.src[sp.Start:sp.End])), " ")
}

func (p *parser) declNote(d *code.Decl) *declNote {
	dn := p.info.declNotes[d]
	if dn == nil {
		dn = &declNote{}
		p.info.declNotes[d] = dn
	}
	return dn
}

func (p *parser) atLineEnd() bool {
	switch p.tok().kind {
	case tNewline, tEOF, tDedent:
		return true
	}
	return p.isOp(";")
}

// stmts parses statements until EOF, or until the DEDENT that closes the
// block. It returns the span from the first statement to the last.
func (p *parser) stmts(sc *scope, doc, block bool) code.Span {
	var body code.Span
	first := true
	for {
		t := p.tok()
		if t.kind == tEOF || (t.kind == tDedent && block) {
			break
		}
		if t.kind == tNewline || t.kind == tIndent || t.kind == tDedent {
			p.next()
			continue
		}
		if first {
			body.Start = t.span.Start
			if doc {
				p.docstring()
			}
			first = false
		}
		start := p.i
		p.stmt(sc)
		if p.i == start {
			p.next()
		}
		body.End = p.lastEnd
	}
	return body
}

// docstring records a statement made up only of string literals as a
// DocComment. We skip f-strings and t-strings, since they aren't docstrings.
func (p *parser) docstring() {
	j := p.i
	for p.toks[j].kind == tString {
		j++
	}
	if j == p.i {
		return
	}
	if prefix := strings.ToLower(string(p.src[p.toks[p.i].span.Start:p.toks[p.i].span.End])); strings.ContainsAny(prefix[:strings.IndexAny(prefix, `"'`)], "ft") {
		return
	}
	switch end := p.toks[j]; {
	case end.kind == tNewline, end.kind == tEOF, end.kind == tDedent, end.kind == tOp && end.text == ";":
		p.f.Comments = append(p.f.Comments, code.Comment{
			Kind: code.DocComment,
			Span: code.Span{Start: p.toks[p.i].span.Start, End: p.toks[j-1].span.End},
		})
	}
}

// suite parses the block after a compound statement header, starting at
// the colon, and returns the body span.
func (p *parser) suite(sc *scope, doc bool) code.Span {
	for !p.isOp(":") && p.tok().kind != tNewline && p.tok().kind != tEOF {
		p.next()
	}
	if !p.acceptOp(":") {
		return code.Span{}
	}
	if p.tok().kind != tNewline {
		start := p.tok().span.Start
		if doc {
			p.docstring()
		}
		p.simpleStmts(sc)
		return p.span(start)
	}
	for p.tok().kind == tNewline {
		p.next()
	}
	if p.tok().kind != tIndent {
		return code.Span{}
	}
	p.next()
	body := p.stmts(sc, doc, true)
	if p.tok().kind == tDedent {
		p.next()
	}
	return body
}

func (p *parser) stmt(sc *scope) {
	t := p.tok()
	start := t.span.Start
	switch {
	case t.kind == tOp && t.text == "@":
		p.decorated(sc)
	case t.kind == tKeyword:
		switch t.text {
		case "def":
			p.funcDef(sc, start, nil)
		case "class":
			p.classDef(sc, start, nil)
		case "async":
			switch n := p.peek(1); {
			case n.kind == tKeyword && n.text == "def":
				p.funcDef(sc, start, nil)
			case n.kind == tKeyword && n.text == "for":
				p.next()
				p.forStmt(sc, start)
			case n.kind == tKeyword && n.text == "with":
				p.next()
				p.withStmt(sc, start)
			default:
				p.simpleStmts(sc)
			}
		case "if", "while":
			p.ifStmt(sc)
		case "for":
			p.forStmt(sc, start)
		case "try":
			p.tryStmt(sc)
		case "with":
			p.withStmt(sc, start)
		default:
			p.simpleStmts(sc)
		}
	case t.kind == tName && t.text == "match" && p.isMatchStmt():
		p.matchStmt(sc)
	case t.kind == tName && t.text == "type" && p.peek(1).kind == tName && p.peek(2).kind == tOp && (p.peek(2).text == "=" || p.peek(2).text == "["):
		p.typeAlias(sc, start)
	default:
		p.simpleStmts(sc)
	}
}

func (p *parser) simpleStmts(sc *scope) {
	for {
		p.simpleStmt(sc)
		if !p.acceptOp(";") || p.atLineEnd() {
			break
		}
	}
	for p.tok().kind != tNewline && p.tok().kind != tEOF && p.tok().kind != tDedent {
		p.next()
	}
	if p.tok().kind == tNewline {
		p.next()
	}
}

func (p *parser) simpleStmt(sc *scope) {
	t := p.tok()
	start := t.span.Start
	if t.kind != tKeyword {
		p.exprStmt(sc, start)
		return
	}
	switch t.text {
	case "import":
		p.importStmt(sc, start)
	case "from":
		p.fromStmt(sc, start)
	case "global", "nonlocal":
		p.next()
		for {
			nt := p.nameTok()
			if nt == nil {
				break
			}
			if t.text == "global" {
				sc.global = setName(sc.global, nt.text)
			} else {
				sc.nonlocal = setName(sc.nonlocal, nt.text)
			}
			p.use(nameNode(nt), sc)
			if !p.acceptOp(",") {
				break
			}
		}
	case "pass", "break", "continue":
		p.next()
	case "return", "del":
		p.next()
		if !p.atLineEnd() {
			p.use(p.exprList(mode{}), sc)
		}
	case "raise", "assert":
		p.next()
		if !p.atLineEnd() {
			p.use(p.test(mode{}), sc)
		}
		if p.acceptKw("from") || p.acceptOp(",") {
			p.use(p.test(mode{}), sc)
		}
	default:
		p.exprStmt(sc, start)
	}
}

func setName(m map[string]bool, name string) map[string]bool {
	if m == nil {
		m = map[string]bool{}
	}
	m[name] = true
	return m
}

var augOps = map[string]bool{
	"+=": true, "-=": true, "*=": true, "/=": true, "//=": true, "%=": true, "**=": true,
	"@=": true, "&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true,
}

func (p *parser) exprStmt(sc *scope, start int) {
	first := p.exprListYield(mode{})
	if first == nil {
		return
	}
	switch t := p.tok(); {
	case p.isOp(":"):
		p.next()
		ann := p.test(mode{})
		var value *node
		if p.acceptOp("=") {
			value = p.exprListYield(mode{})
		}
		p.use(ann, sc)
		p.use(value, sc)
		p.bindTarget(first, sc, bindCtx{span: p.span(start), ann: ann, init: value})
	case p.isOp("="):
		targets := []*node{first}
		for p.acceptOp("=") {
			targets = append(targets, p.exprListYield(mode{}))
		}
		value := targets[len(targets)-1]
		targets = targets[:len(targets)-1]
		ctx := bindCtx{span: p.span(start), init: value}
		if len(targets) == 1 && p.namedLambda(targets[0], value, sc, ctx.span) {
			return
		}
		p.use(value, sc)
		for _, target := range targets {
			p.bindTarget(target, sc, ctx)
		}
	case t.kind == tOp && augOps[t.text]:
		p.next()
		p.use(p.exprListYield(mode{}), sc)
		p.bindTarget(first, sc, bindCtx{span: p.span(start)})
	default:
		p.use(first, sc)
	}
}

func (p *parser) decorated(sc *scope) {
	start := p.tok().span.Start
	var decos []*node
	for p.acceptOp("@") {
		e := p.test(mode{})
		p.use(e, sc)
		decos = append(decos, e)
		for p.tok().kind != tNewline && p.tok().kind != tEOF {
			p.next()
		}
		for p.tok().kind == tNewline {
			p.next()
		}
	}
	switch {
	case p.isKw("def"), p.isKw("async") && p.peek(1).kind == tKeyword && p.peek(1).text == "def":
		p.funcDef(sc, start, decos)
	case p.isKw("class"):
		p.classDef(sc, start, decos)
	}
}

// decoName returns the dotted name of a decorator, without call arguments.
func decoName(n *node) string {
	if n != nil && n.kind == nCall {
		n = n.x
	}
	return dottedName(n)
}

func dottedName(n *node) string {
	switch {
	case n == nil:
		return ""
	case n.kind == nName:
		return n.name
	case n.kind == nAttr:
		if base := dottedName(n.x); base != "" {
			return base + "." + n.name
		}
	}
	return ""
}

func hasDeco(decos []*node, names ...string) bool {
	for _, d := range decos {
		name := decoName(d)
		for _, want := range names {
			if name == want || strings.HasSuffix(name, "."+want) {
				return true
			}
		}
	}
	return false
}

func (p *parser) funcDef(sc *scope, start int, decos []*node) {
	p.acceptKw("async")
	p.next()
	nt := p.nameTok()
	if nt == nil {
		return
	}
	kind := code.KindFunction
	if sc.kind == scClass {
		switch {
		case nt.text == "__init__":
			kind = code.KindConstructor
		case hasDeco(decos, "property", "cached_property", "setter", "getter", "deleter"):
			kind = code.KindProperty
		default:
			kind = code.KindMethod
		}
	}
	d := p.f.NewDecl(sc.owner, kind, nt.text, nt.span, code.Span{Start: start, End: nt.span.End})
	dn := p.declNote(d)
	dn.overload = hasDeco(decos, "overload")
	p.bindDef(sc, nt.text, d)
	as := newScope(scAnnot, d, d, sc)
	if p.isOp("[") {
		p.typeParams(as, d)
	}
	fs := newScope(scFunc, d, d, as)
	if p.acceptOp("(") {
		params := p.params(")", true)
		p.acceptOp(")")
		for i, prm := range params {
			pd := p.declareParam(d, prm, fs, as, sc)
			if i == 0 && sc.kind == scClass && prm.star == 0 && !hasDeco(decos, "staticmethod") {
				pdn := p.declNote(pd)
				pdn.selfOf = sc.owner
				pdn.clsParam = hasDeco(decos, "classmethod") || nt.text == "__new__" || nt.text == "__init_subclass__" || nt.text == "__class_getitem__"
			}
		}
	}
	if p.acceptOp("->") {
		if ret := p.test(mode{}); ret != nil {
			p.use(ret, as)
			d.Type = p.text(ret.span)
			dn.ann = ret
		}
	}
	d.Body = p.suite(fs, true)
	d.Span.End = max(d.Span.End, p.lastEnd)
}

func (p *parser) declareParam(owner *code.Decl, prm param, fs, as, outer *scope) *code.Decl {
	pd := p.f.NewDecl(owner, code.KindParameter, prm.name, prm.nameSpan, prm.span)
	pd.Local = true
	dn := p.declNote(pd)
	dn.star = prm.star
	if prm.ann != nil {
		pd.Type = p.text(prm.ann.span)
		dn.ann = prm.ann
		p.use(prm.ann, as)
	}
	p.use(prm.def, outer)
	if fs.names[prm.name] == nil {
		fs.names[prm.name] = &binding{decl: pd}
	}
	return pd
}

// bindDef binds the name of a def or class. A later definition can only
// replace an earlier one if the earlier one is an @overload stub.
func (p *parser) bindDef(sc *scope, name string, d *code.Decl) {
	ts := p.targetScope(sc, name)
	b := ts.names[name]
	if b == nil || (b.decl != nil && p.declNote(b.decl).overload && !p.declNote(d).overload) {
		ts.names[name] = &binding{decl: d}
	}
}

func (p *parser) typeParams(as *scope, owner *code.Decl) {
	p.next()
	for !p.isOp("]") && p.tok().kind != tEOF && p.tok().kind != tNewline {
		if !p.acceptOp("*") {
			p.acceptOp("**")
		}
		nt := p.nameTok()
		if nt == nil {
			break
		}
		tp := p.f.NewDecl(owner, code.KindTypeParameter, nt.text, nt.span, nt.span)
		tp.Local = true
		as.names[nt.text] = &binding{decl: tp}
		if p.acceptOp(":") {
			if bound := p.test(mode{}); bound != nil {
				p.use(bound, as)
				tp.Type = p.text(bound.span)
			}
		}
		if p.acceptOp("=") {
			p.use(p.test(mode{}), as)
		}
		tp.Span.End = p.lastEnd
		if !p.acceptOp(",") {
			break
		}
	}
	p.acceptOp("]")
}

var enumBases = map[string]bool{"Enum": true, "IntEnum": true, "StrEnum": true, "Flag": true, "IntFlag": true}

func (p *parser) classDef(sc *scope, start int, decos []*node) {
	p.next()
	nt := p.nameTok()
	if nt == nil {
		return
	}
	d := p.f.NewDecl(sc.owner, code.KindClass, nt.text, nt.span, code.Span{Start: start, End: nt.span.End})
	p.bindDef(sc, nt.text, d)
	as := newScope(scAnnot, d, d, sc)
	if p.isOp("[") {
		p.typeParams(as, d)
	}
	dn := p.declNote(d)
	if p.acceptOp("(") {
		for _, a := range p.callArgs() {
			p.use(a.value, as)
			if a.kw != "" || a.star != 0 {
				continue
			}
			d.Bases = append(d.Bases, p.text(a.value.span))
			dn.bases = append(dn.bases, a.value)
			name := dottedName(a.value)
			if enumBases[name[strings.LastIndexByte(name, '.')+1:]] {
				d.Kind = code.KindEnum
			}
		}
		p.acceptOp(")")
	}
	cs := newScope(scClass, d, d, as)
	d.Body = p.suite(cs, true)
	d.Span.End = max(d.Span.End, p.lastEnd)
}

func (p *parser) ifStmt(sc *scope) {
	for {
		p.next()
		p.use(p.test(mode{}), sc)
		p.suite(sc, false)
		if !p.isKw("elif") {
			break
		}
	}
	if p.acceptKw("else") {
		p.suite(sc, false)
	}
}

func (p *parser) forStmt(sc *scope, start int) {
	p.next()
	target := p.exprList(mode{noIn: true})
	p.acceptKw("in")
	p.use(p.exprList(mode{}), sc)
	p.bindTarget(target, sc, bindCtx{span: p.span(start)})
	p.suite(sc, false)
	if p.acceptKw("else") {
		p.suite(sc, false)
	}
}

func (p *parser) tryStmt(sc *scope) {
	p.next()
	p.suite(sc, false)
	for p.isKw("except") {
		start := p.next().span.Start
		p.acceptOp("*")
		if !p.isOp(":") {
			p.use(p.exprList(mode{}), sc)
			if p.acceptKw("as") {
				if nt := p.nameTok(); nt != nil {
					p.bindTarget(nameNode(nt), sc, bindCtx{span: p.span(start)})
				}
			}
		}
		p.suite(sc, false)
	}
	for p.isKw("else") || p.isKw("finally") {
		p.next()
		p.suite(sc, false)
	}
}

func nameNode(t *tok) *node {
	return &node{kind: nName, name: t.text, span: t.span, nameSpan: t.span}
}

func (p *parser) withStmt(sc *scope, start int) {
	p.next()
	paren := p.isOp("(") && p.parenthesizedWith()
	if paren {
		p.next()
	}
	for !(paren && p.isOp(")")) {
		e := p.test(mode{})
		if e == nil {
			break
		}
		p.use(e, sc)
		if p.acceptKw("as") {
			target := p.test(mode{})
			p.bindTarget(target, sc, bindCtx{span: p.span(start)})
		}
		if !p.acceptOp(",") {
			break
		}
	}
	if paren {
		p.acceptOp(")")
	}
	p.suite(sc, false)
}

// parenthesizedWith reports whether the `(` at the cursor opens
// parenthesized with-items (`with (a as b, c as d):`).
func (p *parser) parenthesizedWith() bool {
	depth, items := 0, false
	for j := p.i; j < len(p.toks); j++ {
		t := p.toks[j]
		switch {
		case t.kind == tNewline || t.kind == tEOF:
			return false
		case t.kind == tOp && (t.text == "(" || t.text == "[" || t.text == "{"):
			depth++
		case t.kind == tOp && (t.text == ")" || t.text == "]" || t.text == "}"):
			depth--
			if depth == 0 {
				n := p.toks[min(j+1, len(p.toks)-1)]
				return items && n.kind == tOp && n.text == ":"
			}
		case depth == 1 && (t.kind == tKeyword && t.text == "as" || t.kind == tOp && t.text == ","):
			items = true
		}
	}
	return false
}

// isMatchStmt tells a match statement apart from a use of the name `match`.
// It's a statement if the logical line ends with a colon and the block
// starts with `case`.
func (p *parser) isMatchStmt() bool {
	if n := p.peek(1); n.kind == tOp && n.text != "(" && n.text != "[" && n.text != "{" && n.text != "-" && n.text != "*" {
		return false
	}
	for j := p.i + 1; j+2 < len(p.toks); j++ {
		if p.toks[j].kind != tNewline {
			continue
		}
		colon, indent, kase := p.toks[j-1], p.toks[j+1], p.toks[j+2]
		return colon.kind == tOp && colon.text == ":" && indent.kind == tIndent && kase.kind == tName && kase.text == "case"
	}
	return false
}

func (p *parser) matchStmt(sc *scope) {
	p.markKeyword(p.next())
	p.use(p.exprList(mode{}), sc)
	for p.tok().kind != tNewline && p.tok().kind != tEOF {
		p.next()
	}
	p.next()
	if p.tok().kind != tIndent {
		return
	}
	p.next()
	for p.tok().kind == tName && p.tok().text == "case" {
		t := p.next()
		p.markKeyword(t)
		pat := p.exprList(mode{pattern: true})
		if p.acceptKw("if") {
			p.use(p.test(mode{}), sc)
		}
		p.bindPattern(pat, sc, p.span(t.span.Start))
		p.suite(sc, false)
		for p.tok().kind == tNewline {
			p.next()
		}
	}
	if p.tok().kind == tDedent {
		p.next()
	}
}

func (p *parser) typeAlias(sc *scope, start int) {
	p.markKeyword(p.next())
	nt := p.next()
	d := p.f.NewDecl(sc.owner, code.KindTypeAlias, nt.text, nt.span, code.Span{Start: start, End: nt.span.End})
	if b := p.targetScope(sc, nt.text); b.names[nt.text] == nil {
		b.names[nt.text] = &binding{decl: d}
	}
	as := newScope(scAnnot, sc.owner, sc.refDecl, sc)
	if p.isOp("[") {
		p.typeParams(as, d)
	}
	if p.acceptOp("=") {
		if v := p.test(mode{}); v != nil {
			p.use(v, as)
			d.Type = p.text(v.span)
		}
	}
	d.Span.End = p.lastEnd
	p.simpleStmts(sc)
}

// dotted parses NAME ('.' NAME)*.
func (p *parser) dotted() []*tok {
	var segs []*tok
	for {
		nt := p.nameTok()
		if nt == nil {
			return segs
		}
		segs = append(segs, nt)
		if !p.isOp(".") || p.peek(1).kind != tName {
			return segs
		}
		p.next()
	}
}

func (p *parser) importRef(sc *scope, t *tok, n note) {
	r := p.newRef(sc, t.text, t.span, n)
	r.Import = true
}

func (p *parser) importStmt(sc *scope, start int) {
	p.next()
	for {
		segs := p.dotted()
		if len(segs) == 0 {
			return
		}
		module := ""
		for _, s := range segs {
			module = joinDotted(module, s.text)
			p.importRef(sc, s, note{kind: noteModule, imp: importRef{module: module}})
		}
		if p.acceptKw("as") {
			if alias := p.nameTok(); alias != nil {
				p.importAlias(sc, alias, start, importRef{module: module})
			}
		} else {
			ts := p.targetScope(sc, segs[0].text)
			if ts.names[segs[0].text] == nil {
				ts.names[segs[0].text] = &binding{imp: &importRef{module: segs[0].text}}
			}
		}
		if !p.acceptOp(",") {
			return
		}
	}
}

func (p *parser) importAlias(sc *scope, alias *tok, start int, target importRef) {
	ts := p.targetScope(sc, alias.text)
	if ts.names[alias.text] != nil {
		p.use(nameNode(alias), sc)
		return
	}
	d := p.f.NewDecl(ts.owner, code.KindImport, alias.text, alias.span, code.Span{Start: start, End: alias.span.End})
	p.declNote(d).imp = &target
	ts.names[alias.text] = &binding{decl: d}
	d.Type = target.module
	if target.name != "" {
		d.Type = joinDotted(target.module, target.name)
	}
}

func joinDotted(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "." + b
}

func (p *parser) fromStmt(sc *scope, start int) {
	p.next()
	level := 0
	for p.isOp(".") || p.isOp("...") {
		level += len(p.next().text)
	}
	segs := p.dotted()
	base, ok := "", true
	if level > 0 {
		parts := strings.Split(p.info.pkg, ".")
		if p.info.pkg == "" {
			parts = nil
		}
		if up := level - 1; up <= len(parts) {
			base = strings.Join(parts[:len(parts)-up], ".")
		} else {
			ok = false
		}
	}
	module := base
	for _, s := range segs {
		module = joinDotted(module, s.text)
		n := note{kind: noteModule, imp: importRef{module: module}}
		if !ok {
			n.imp.module = ""
		}
		p.importRef(sc, s, n)
	}
	if !ok {
		module = ""
	}
	if !p.acceptKw("import") {
		return
	}
	if p.acceptOp("*") {
		if sc.kind == scModule && module != "" {
			sc.stars = append(sc.stars, module)
		}
		return
	}
	paren := p.acceptOp("(")
	for {
		nt := p.nameTok()
		if nt == nil {
			break
		}
		target := importRef{module: module, name: nt.text}
		p.importRef(sc, nt, note{kind: noteImportName, imp: target})
		if p.acceptKw("as") {
			if alias := p.nameTok(); alias != nil {
				p.importAlias(sc, alias, start, target)
			}
		} else if ts := p.targetScope(sc, nt.text); ts.names[nt.text] == nil {
			ts.names[nt.text] = &binding{imp: &target}
		}
		if !p.acceptOp(",") {
			break
		}
	}
	if paren {
		p.acceptOp(")")
	}
}
