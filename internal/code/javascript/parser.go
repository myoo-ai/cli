package javascript

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// maxDepth bounds recursion on pathological nesting. If we go deeper, we
// skip token by token instead.
const maxDepth = 800

type declKey struct {
	start int
	kind  code.DeclKind
	name  string
}

// parser is an error-tolerant recursive descent parser over the significant
// tokens of a file. In build mode it creates the file's declarations and
// references. In resolve mode (build == false) it walks the same file
// again, finds the declarations and references we created earlier, and
// records the scopes and export tables that resolution needs, along with
// the shape of each member access.
type parser struct {
	f     *code.File
	src   []byte
	toks  []token
	match []int32 // matching bracket of ( [ { and template heads, -1 if none
	i     int
	ts    bool
	build bool
	decls map[declKey]*code.Decl
	refs  map[int]*code.Ref
	pr    *parseResult

	sc       *scope
	parent   *code.Decl // where new declarations attach
	refScope *code.Decl // Ref.Scope of new references
	thisDecl *code.Decl // what `this` refers to (class or object literal owner)
	ctor     *code.Decl // class whose JavaScript constructor is being parsed
	anon     int        // > 0 inside anonymous functions, where new declarations are local
	inGlobal bool       // inside `declare global { }`
	noIn     bool       // `in` is not an operator (for-statement heads)
	typeOwn  *code.Decl // owner for the members of the next object type literal
	depth    int

	// members lists the member names of JavaScript classes. We collect them
	// before parsing the class body, so `this.x = v` in a constructor can
	// declare a field only if there's no member x.
	members map[*code.Decl]map[string]bool
}

// declCtx carries what precedes a declaration.
type declCtx struct {
	start     int // span start: decorators, export, declare
	isDefault bool
	defSpan   code.Span // the `default` keyword of `export default`
}

type pctx struct {
	sc                         *scope
	parent, refScope, thisDecl *code.Decl
	ctor                       *code.Decl
	anon                       int
}

func (p *parser) save() pctx {
	return pctx{p.sc, p.parent, p.refScope, p.thisDecl, p.ctor, p.anon}
}

func (p *parser) restore(c pctx) {
	p.sc, p.parent, p.refScope, p.thisDecl, p.ctor, p.anon = c.sc, c.parent, c.refScope, c.thisDecl, c.ctor, c.anon
}

func (p *parser) push(kind scopeKind) *scope {
	p.sc = &scope{parent: p.sc, kind: kind}
	return p.sc
}

// newParser lexes f. In build mode it also fills f.Tokens, f.Comments and
// the module declaration.
func newParser(f *code.File, build bool) *parser {
	ts := f.Lang == code.TypeScript
	ext := strings.ToLower(f.Path[strings.LastIndexByte(f.Path, '.')+1:])
	jsx := ext != "ts" && ext != "mts" && ext != "cts"
	src := f.Src
	var markup []code.Comment
	if isComponent(ext) {
		// Component scripts are TypeScript or JavaScript without JSX. All we
		// need from the markup are its comments.
		jsx = false
		src, markup = maskComponent(f.Src)
	}
	l := lex(src, jsx, ts)
	if build {
		f.Tokens = publicTokens(l.toks)
		f.Comments = mergeComments(l.comments, markup)
		f.SetModule(moduleName(f.Path))
	}
	sig := make([]token, 0, len(l.toks))
	for _, t := range l.toks {
		if t.kind != code.TokComment {
			sig = append(sig, t)
		}
	}
	p := &parser{
		f: f, src: f.Src, toks: sig, ts: ts, build: build,
		pr:      newParseResult(),
		members: map[*code.Decl]map[string]bool{},
	}
	p.match = matchBrackets(sig, f.Src)
	if !build {
		p.decls = make(map[declKey]*code.Decl, len(f.Decls))
		for _, d := range f.Decls {
			p.decls[declKey{d.NameSpan.Start, d.Kind, d.Name}] = d
		}
		p.refs = make(map[int]*code.Ref, len(f.Refs))
		for _, r := range f.Refs {
			p.refs[r.Span.Start] = r
		}
	}
	p.sc = &scope{kind: scopeModule}
	p.pr.module = p.sc
	p.parent = f.Module
	p.refScope = f.Module
	return p
}

func (p *parser) run() {
	for p.i < len(p.toks) {
		p.statementSafe()
	}
}

// moduleName is the path without its extension (".d.ts" counts as one).
func moduleName(path string) string {
	lower := strings.ToLower(path)
	for _, ext := range []string{".d.ts", ".d.mts", ".d.cts"} {
		if strings.HasSuffix(lower, ext) {
			return path[:len(path)-len(ext)]
		}
	}
	if i := strings.LastIndexByte(path, '.'); i > strings.LastIndexByte(path, '/') {
		return path[:i]
	}
	return path
}

func matchBrackets(toks []token, src []byte) []int32 {
	match := make([]int32, len(toks))
	var stack []int32
	opener := func(t token) byte {
		if t.kind == code.TokString && t.sub == subTmplHead {
			return '`'
		}
		if t.kind == code.TokPunct && t.sub == subNone && t.end-t.start == 1 {
			switch src[t.start] {
			case '(':
				return ')'
			case '[':
				return ']'
			case '{':
				return '}'
			}
		}
		return 0
	}
	closer := func(t token) byte {
		if t.kind == code.TokString && t.sub == subTmplTail {
			return '`'
		}
		if t.kind == code.TokPunct && t.sub == subNone && t.end-t.start == 1 {
			switch c := src[t.start]; c {
			case ')', ']', '}':
				return c
			}
		}
		return 0
	}
	for i, t := range toks {
		match[i] = -1
		if opener(t) != 0 {
			stack = append(stack, int32(i))
			continue
		}
		c := closer(t)
		if c == 0 {
			continue
		}
		for j, s := range slices.Backward(stack) {
			if opener(toks[s]) == c {
				match[s] = int32(i)
				stack = stack[:j]
				break
			}
		}
	}
	return match
}

func (p *parser) at(i int) token {
	if i < 0 || i >= len(p.toks) {
		return token{start: len(p.src), end: len(p.src), pub: -1}
	}
	return p.toks[i]
}

func (p *parser) tok() token { return p.at(p.i) }

func (p *parser) peek(n int) token { return p.at(p.i + n) }

func (p *parser) eof() bool { return p.i >= len(p.toks) }

func (p *parser) text(t token) string { return string(p.src[t.start:t.end]) }

func (p *parser) isText(t token, s string) bool {
	return t.end-t.start == len(s) && string(p.src[t.start:t.end]) == s
}

func (p *parser) isOp(t token, s string) bool {
	return t.sub == subNone && (t.kind == code.TokPunct || t.kind == code.TokKeyword) && p.isText(t, s)
}

// isWord is for contextual keywords, which the lexer emits as identifiers.
func (p *parser) isWord(t token, s string) bool {
	return t.kind == code.TokIdent && t.sub == subNone && p.isText(t, s)
}

func (p *parser) is(s string) bool { return p.isOp(p.tok(), s) }

func (p *parser) word(s string) bool { return p.isWord(p.tok(), s) }

func (p *parser) eat(s string) bool {
	if p.is(s) {
		p.i++
		return true
	}
	return false
}

func (p *parser) prevEnd() int {
	if p.i == 0 {
		return 0
	}
	return p.at(p.i - 1).end
}

func (p *parser) semi() int {
	if p.is(";") {
		p.i++
	}
	return p.prevEnd()
}

func (p *parser) recover(open int) {
	if open >= 0 && open < len(p.match) {
		if m := int(p.match[open]); m > p.i {
			p.i = m
		}
	}
}

// listSeparator consumes the comma after a list item and reports whether
// the list goes on. If the item is malformed, we skip to the closing
// bracket and stop.
func (p *parser) listSeparator(open, start int, close string) bool {
	if p.eat(",") {
		return true
	}
	if p.i == start || !p.is(close) {
		p.recover(open)
	}
	return false
}

func isIdent(t token) bool { return t.kind == code.TokIdent && t.sub == subNone }

func isName(t token) bool {
	return (t.kind == code.TokIdent && (t.sub == subNone || t.sub == subPrivate)) || t.kind == code.TokKeyword
}

// isPropKey reports whether t can start a property name in classes, object
// literals and type literals.
func isPropKey(t token) bool {
	return isName(t) || t.kind == code.TokNumber || (t.kind == code.TokString && t.sub == subNone)
}

func (p *parser) setKind(i int, kind code.TokenKind) {
	if p.build && i < len(p.toks) && p.toks[i].pub >= 0 {
		p.f.Tokens[p.toks[i].pub].Kind = kind
	}
}

func (p *parser) kw() {
	p.setKind(p.i, code.TokKeyword)
	p.i++
}

// propName consumes a property key token and returns its name. For a string
// key the span leaves out the quotes, and numeric keys have no name.
func (p *parser) propName() (string, code.Span) {
	t := p.tok()
	p.i++
	if t.kind == code.TokKeyword {
		p.setKind(p.i-1, code.TokIdent)
	}
	span := code.Span{Start: t.start, End: t.end}
	switch t.kind {
	case code.TokNumber:
		return "", span
	case code.TokString:
		if span.End-span.Start >= 2 {
			span = code.Span{Start: span.Start + 1, End: span.End - 1}
		}
	}
	return string(p.src[span.Start:span.End]), span
}

func (p *parser) textFrom(start int) string {
	end := p.prevEnd()
	if end <= start {
		return ""
	}
	return strings.Join(strings.Fields(string(p.src[start:end])), " ")
}

func (p *parser) newDecl(kind code.DeclKind, name string, nameSpan code.Span, start int) *code.Decl {
	span := code.Span{Start: start, End: nameSpan.End}
	var d *code.Decl
	if p.build {
		d = p.f.NewDecl(p.parent, kind, name, nameSpan, span)
	} else if d = p.decls[declKey{nameSpan.Start, kind, name}]; d == nil {
		d = &code.Decl{Name: name, Kind: kind, File: p.f, NameSpan: nameSpan, Span: span, Parent: p.parent}
	}
	if p.anon > 0 {
		d.Local = true
	}
	if p.inGlobal && p.parent == p.f.Module {
		p.pr.globals = append(p.pr.globals, d)
	}
	return d
}

func (p *parser) declTok(kind code.DeclKind, t token, start int) *code.Decl {
	return p.newDecl(kind, p.text(t), code.Span{Start: t.start, End: t.end}, start)
}

func (p *parser) newRef(t token, m refMeta) *code.Ref {
	span := code.Span{Start: t.start, End: t.end}
	var r *code.Ref
	if p.build {
		r = p.f.NewRef(p.refScope, p.text(t), span)
	} else if r = p.refs[t.start]; r == nil {
		r = &code.Ref{Name: p.text(t), Span: span, File: p.f, Scope: p.refScope}
	}
	m.sc = p.sc
	p.pr.refs[r] = &m
	return r
}

// literalNames are identifiers that act like literals, so we don't record
// references to them.
var literalNames = map[string]bool{"undefined": true, "NaN": true, "Infinity": true}

// useRef records an identifier use that we resolve lexically.
func (p *parser) useRef(t token) *code.Ref {
	if literalNames[p.text(t)] {
		return nil
	}
	return p.newRef(t, refMeta{})
}

func (p *parser) typeUseRef(t token) *code.Ref {
	return p.newRef(t, refMeta{typePos: true})
}

// memberRef records the name after '.', using obj to know what we're
// looking into.
func (p *parser) memberRef(t token, obj exprInfo, typePos bool) *code.Ref {
	m := refMeta{typePos: typePos, thisDecl: p.thisDecl}
	switch {
	case obj.this:
		m.obj = objThis
	case obj.super:
		m.obj = objSuper
	case obj.require != "":
		m.obj, m.spec = objRequire, obj.require
	case obj.path && obj.ref != nil:
		m.obj, m.objRef = objRef, obj.ref
	case obj.newRef != nil:
		m.obj, m.objRef = objRef, obj.newRef
	default:
		m.obj = objOther
	}
	return p.newRef(t, m)
}

func (p *parser) namePath(typePos bool) exprInfo {
	t := p.tok()
	p.i++
	var ref *code.Ref
	if typePos {
		ref = p.typeUseRef(t)
	} else {
		ref = p.useRef(t)
	}
	return p.namePathRest(exprInfo{ref: ref, path: true}, typePos)
}

func (p *parser) namePathRest(info exprInfo, typePos bool) exprInfo {
	for p.is(".") && isName(p.peek(1)) {
		p.i++
		n := p.tok()
		p.i++
		info = exprInfo{ref: p.memberRef(n, info, typePos), path: true}
	}
	return info
}

func (p *parser) bindLexical(d *code.Decl) {
	if d != nil {
		p.sc.bind(d.Name, d)
	}
}

func (p *parser) statementSafe() {
	i := p.i
	p.statement()
	if p.i == i {
		p.i++
	}
}

func (p *parser) statement() {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.i++
		return
	}
	t := p.tok()
	start := t.start
	if _, ok := p.declaration(declCtx{start: start}); ok {
		return
	}
	switch t.kind {
	case code.TokPunct:
		switch {
		case p.is("{"):
			p.block(true)
			return
		case p.is(";"):
			p.i++
			return
		}
	case code.TokKeyword:
		if p.keywordStatement(t) {
			return
		}
	case code.TokIdent:
		if isIdent(t) && p.isOp(p.peek(1), ":") {
			p.i += 2
			p.statementSafe()
			return
		}
		if p.cjsExport() {
			return
		}
	}
	if p.ctorField() {
		return
	}
	p.expression()
	p.semi()
}

func (p *parser) keywordStatement(t token) bool {
	switch p.text(t) {
	case "if":
		p.i++
		p.parenExpr()
		p.statementSafe()
		if p.eat("else") {
			p.statementSafe()
		}
	case "for":
		p.forStatement()
	case "while", "with":
		p.i++
		p.parenExpr()
		p.statementSafe()
	case "do":
		p.i++
		p.statementSafe()
		if p.eat("while") {
			p.parenExpr()
		}
		p.semi()
	case "switch":
		p.switchStatement()
	case "try":
		p.tryStatement()
	case "return", "throw":
		p.i++
		if !p.stmtEndsAt(p.i) {
			p.expression()
		}
		p.semi()
	case "break", "continue":
		p.i++
		if n := p.tok(); isIdent(n) && !n.nl {
			p.i++
		}
		p.semi()
	case "debugger":
		p.i++
		p.semi()
	case "import":
		if n := p.peek(1); p.isOp(n, "(") || p.isOp(n, ".") {
			return false
		}
		p.importDecl()
	case "export":
		p.exportDecl(t.start)
	default:
		return false
	}
	return true
}

// block parses `{ statements }` and returns its span.
func (p *parser) block(newScope bool) code.Span {
	if !p.is("{") {
		return code.Span{}
	}
	start := p.tok().start
	p.i++
	var saved *scope
	if newScope {
		saved = p.sc
		p.push(scopeBlock)
	}
	for !p.eof() && !p.is("}") {
		p.statementSafe()
	}
	p.eat("}")
	if newScope {
		p.sc = saved
	}
	return code.Span{Start: start, End: p.prevEnd()}
}

func (p *parser) parenExpr() {
	if !p.is("(") {
		return
	}
	open := p.i
	p.i++
	p.expression()
	if !p.is(")") {
		p.recover(open)
	}
	p.eat(")")
}

func (p *parser) forStatement() {
	p.i++
	if p.word("await") {
		p.kw()
	}
	saved := p.sc
	p.push(scopeBlock)
	defer func() { p.sc = saved }()
	if !p.is("(") {
		p.statementSafe()
		return
	}
	open := p.i
	p.i++
	p.noIn = true
	t := p.tok()
	switch {
	case p.isOp(t, ";"):
	case p.isOp(t, "var"):
		p.varDecls(declCtx{start: t.start}, code.KindVariable, true, true)
	case p.isOp(t, "const"):
		p.varDecls(declCtx{start: t.start}, code.KindConstant, false, true)
	case p.isWord(t, "let") && (isIdent(p.peek(1)) || p.isOp(p.peek(1), "[") || p.isOp(p.peek(1), "{")):
		p.setKind(p.i, code.TokKeyword)
		p.varDecls(declCtx{start: t.start}, code.KindVariable, false, true)
	default:
		p.expression()
	}
	p.noIn = false
	if p.word("of") || p.is("in") {
		p.kw()
		p.assign()
	} else {
		for range 2 {
			if !p.eat(";") {
				break
			}
			if !p.is(";") && !p.is(")") {
				p.expression()
			}
		}
	}
	if !p.is(")") {
		p.recover(open)
	}
	p.eat(")")
	p.statementSafe()
}

func (p *parser) switchStatement() {
	p.i++
	p.parenExpr()
	if !p.is("{") {
		return
	}
	saved := p.sc
	p.push(scopeBlock)
	p.i++
	for !p.eof() && !p.is("}") {
		switch {
		case p.eat("case"):
			p.expression()
			p.eat(":")
		case p.eat("default"):
			p.eat(":")
		default:
			p.statementSafe()
		}
	}
	p.eat("}")
	p.sc = saved
}

func (p *parser) tryStatement() {
	p.i++
	p.block(true)
	if p.eat("catch") {
		saved := p.sc
		p.push(scopeBlock)
		if p.is("(") {
			open := p.i
			p.i++
			start := p.tok().start
			ds := p.bindingTarget(code.KindVariable, start, false)
			if p.ts && p.eat(":") {
				p.typ()
			}
			for _, d := range ds {
				d.Span.End = p.prevEnd()
			}
			if !p.is(")") {
				p.recover(open)
			}
			p.eat(")")
		}
		p.block(false)
		p.sc = saved
	}
	if p.eat("finally") {
		p.block(true)
	}
}

// declaration parses a declaration statement at the current token, if
// there is one, and returns the declarations it binds.
func (p *parser) declaration(ctx declCtx) ([]*code.Decl, bool) {
	t := p.tok()
	n := p.peek(1)
	one := func(d *code.Decl) ([]*code.Decl, bool) {
		if d == nil {
			return nil, true
		}
		return []*code.Decl{d}, true
	}
	switch {
	case p.isOp(t, "@"):
		p.decorators()
		if p.is("export") {
			p.exportDecl(ctx.start)
			return nil, true
		}
		return p.declaration(ctx)
	case p.isOp(t, "var"):
		return p.varDecls(ctx, code.KindVariable, true, false), true
	case p.isOp(t, "const"):
		if p.isOp(n, "enum") {
			p.i++
			return one(p.enumDecl(ctx))
		}
		return p.varDecls(ctx, code.KindConstant, false, false), true
	case p.isWord(t, "let") && (isIdent(n) || p.isOp(n, "[") || p.isOp(n, "{")):
		p.setKind(p.i, code.TokKeyword)
		return p.varDecls(ctx, code.KindVariable, false, false), true
	case p.isOp(t, "function"):
		return one(p.functionDecl(ctx))
	case p.isWord(t, "async") && p.isOp(n, "function") && !n.nl:
		p.kw()
		return one(p.functionDecl(ctx))
	case p.isOp(t, "class"):
		return one(p.class(ctx, nil, true))
	case p.isWord(t, "abstract") && p.isOp(n, "class") && !n.nl:
		p.kw()
		return one(p.class(ctx, nil, true))
	case p.isWord(t, "interface") && isIdent(n) && !n.nl:
		return one(p.interfaceDecl(ctx))
	case p.isWord(t, "type") && isIdent(n) && !n.nl && (p.isOp(p.peek(2), "=") || p.isOp(p.peek(2), "<")):
		return one(p.typeAlias(ctx))
	case p.isOp(t, "enum") && isIdent(n):
		return one(p.enumDecl(ctx))
	case p.isWord(t, "namespace") && isIdent(n) && !n.nl:
		return one(p.namespaceDecl(ctx))
	case p.isWord(t, "module") && !n.nl && (isIdent(n) || (n.kind == code.TokString && n.sub == subNone)) && (p.isOp(p.peek(2), "{") || p.isOp(p.peek(2), ".")):
		return one(p.namespaceDecl(ctx))
	case p.isWord(t, "global") && p.isOp(n, "{") && p.ts:
		p.kw()
		p.globalBlock()
		return nil, true
	case p.isWord(t, "declare") && !n.nl && (n.kind == code.TokKeyword || isIdent(n)) && !p.isOp(n, "in") && !p.isOp(n, "instanceof"):
		i := p.i
		p.kw()
		if ds, ok := p.declaration(ctx); ok {
			return ds, true
		}
		p.i = i
	}
	return nil, false
}

func (p *parser) globalBlock() {
	saved := p.inGlobal
	p.inGlobal = true
	p.i++
	for !p.eof() && !p.is("}") {
		p.statementSafe()
	}
	p.eat("}")
	p.inGlobal = saved
}

// varDecls parses `var|let|const declarator, ...` with the keyword at the
// current token. If bindVar is set, names bind into the function scope.
func (p *parser) varDecls(ctx declCtx, kind code.DeclKind, bindVar, inFor bool) []*code.Decl {
	p.setKind(p.i, code.TokKeyword)
	p.i++
	var all, last []*code.Decl
	start := ctx.start
	for !p.eof() {
		last = p.declarator(kind, start, bindVar)
		all = append(all, last...)
		if !p.eat(",") {
			break
		}
		start = p.tok().start
	}
	if !inFor && p.is(";") {
		p.i++
		for _, d := range last {
			d.Span.End = p.prevEnd()
		}
	}
	return all
}

func (p *parser) bindVarOrLexical(d *code.Decl, bindVar bool) {
	if bindVar {
		p.sc.varScope().bind(d.Name, d)
	} else {
		p.sc.bind(d.Name, d)
	}
}

func (p *parser) declarator(kind code.DeclKind, start int, bindVar bool) []*code.Decl {
	t := p.tok()
	if !isIdent(t) {
		if !p.is("{") && !p.is("[") {
			return nil
		}
		if m := p.match[p.i]; p.is("{") && m > 0 && p.requireAfter(int(m)+2) {
			p.requireDestructure(start)
			return nil
		}
		ds := p.bindingTarget(kind, start, bindVar)
		if p.ts && p.eat(":") {
			p.typ()
		}
		if p.eat("=") {
			p.assign()
		}
		for _, d := range ds {
			d.Span.End = p.prevEnd()
		}
		return ds
	}
	p.i++
	p.eat("!")
	var typ string
	var tref *code.Ref
	if p.ts && p.eat(":") {
		s := p.tok().start
		tref = p.typ().ref
		typ = p.textFrom(s)
	}
	var d *code.Decl
	if !p.eat("=") {
		d = p.declTok(kind, t, start)
		p.bindVarOrLexical(d, bindVar)
	} else {
		switch {
		case p.requireAfter(p.i):
			d = p.declTok(code.KindImport, t, start)
			p.bindVarOrLexical(d, bindVar)
			p.pr.aliases[d] = alias{spec: p.requireCall(), name: "*"}
		case p.functionAhead():
			d = p.declTok(code.KindFunction, t, start)
			p.bindVarOrLexical(d, bindVar)
			p.functionValue(d)
		case p.is("class"):
			d = p.declTok(code.KindClass, t, start)
			p.bindVarOrLexical(d, bindVar)
			p.class(declCtx{start: start}, d, false)
		case p.is("{"):
			d = p.declTok(kind, t, start)
			p.bindVarOrLexical(d, bindVar)
			p.objectLiteral(d, false)
			p.continueExpr()
		default:
			d = p.declTok(kind, t, start)
			p.bindVarOrLexical(d, bindVar)
			info := p.assign()
			if typ == "" && info.newText != "" {
				typ, tref = info.newText, info.newRef
			}
		}
	}
	if typ != "" {
		d.Type = typ
	}
	if tref != nil {
		p.pr.typeRef[d] = tref
	}
	d.Span.End = p.prevEnd()
	return []*code.Decl{d}
}

// requireAfter reports whether the tokens at i are `require ( 'spec' )` and
// the initializer ends right after it.
func (p *parser) requireAfter(i int) bool {
	if !p.isWord(p.at(i), "require") || !p.isOp(p.at(i+1), "(") {
		return false
	}
	s := p.at(i + 2)
	if s.kind != code.TokString || (s.sub != subNone && s.sub != subTmplFull) || !p.isOp(p.at(i+3), ")") {
		return false
	}
	n := p.at(i + 4)
	return i+4 >= len(p.toks) || n.nl || p.isOp(n, ";") || p.isOp(n, ",") || p.isOp(n, "}") || p.isOp(n, ")")
}

// requireCall consumes `require ( 'spec' )` and returns the specifier.
func (p *parser) requireCall() string {
	if r := p.useRef(p.tok()); r != nil {
		r.Call = true
	}
	spec := unquote(p.text(p.peek(2)))
	p.pr.esm = true
	p.i += 4
	return spec
}

func unquote(s string) string {
	if len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return s
}

// requireDestructure parses `{ a, b: c } = require('spec')` like an import,
// so `a` binds to the export a and `c` is an alias of export b.
func (p *parser) requireDestructure(start int) {
	open := p.i
	closeIdx := int(p.match[open])
	spec := unquote(p.text(p.at(closeIdx + 4)))
	p.pr.esm = true
	p.i++
	type item struct {
		name, local token
		aliased     bool
	}
	var items []item
	for p.i < closeIdx {
		st := p.i
		t := p.tok()
		if isPropKey(t) {
			p.i++
			if p.eat(":") && isIdent(p.tok()) {
				items = append(items, item{name: t, local: p.tok(), aliased: true})
				p.i++
			} else if isIdent(t) {
				items = append(items, item{name: t, local: t})
			}
			if p.eat("=") {
				p.assign()
			}
		}
		if !p.eat(",") && p.i == st {
			p.i++
		}
	}
	p.i = closeIdx + 1
	p.eat("=")
	p.requireCall()
	end := p.prevEnd()
	for _, it := range items {
		name := p.text(it.name)
		if it.name.kind == code.TokString {
			name = unquote(name)
		}
		if isName(it.name) && it.name.kind != code.TokKeyword {
			r := p.newRef(it.name, refMeta{spec: spec})
			r.Import = true
		}
		p.bindNamedImport(it.local, name, spec, start, end, it.aliased)
	}
}

// bindingTarget parses an identifier or a destructuring pattern and
// declares the names it binds.
func (p *parser) bindingTarget(kind code.DeclKind, start int, bindVar bool) []*code.Decl {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.i++
		return nil
	}
	t := p.tok()
	switch {
	case isIdent(t):
		p.i++
		d := p.declTok(kind, t, start)
		p.bindVarOrLexical(d, bindVar)
		return []*code.Decl{d}
	case p.isOp(t, "{"), p.isOp(t, "["):
		return p.bindingPattern(kind, start, bindVar, p.isOp(t, "{"))
	}
	return nil
}

func (p *parser) bindingPattern(kind code.DeclKind, start int, bindVar, object bool) []*code.Decl {
	open := p.i
	p.i++
	close := "]"
	if object {
		close = "}"
	}
	var ds []*code.Decl
	for !p.eof() && !p.is(close) {
		st := p.i
		if object {
			ds = append(ds, p.bindingProperty(kind, start, bindVar)...)
		} else {
			if p.eat(",") {
				continue
			}
			p.eat("...")
			ds = append(ds, p.bindingTarget(kind, start, bindVar)...)
		}
		if p.eat("=") {
			p.assign()
		}
		if !p.eat(",") && (p.i == st || !p.is(close)) {
			p.recover(open)
			break
		}
	}
	p.eat(close)
	return ds
}

func (p *parser) bindingProperty(kind code.DeclKind, start int, bindVar bool) []*code.Decl {
	switch {
	case p.eat("..."):
		return p.bindingTarget(kind, start, bindVar)
	case p.is("["):
		p.computedKey()
		if p.eat(":") {
			return p.bindingTarget(kind, start, bindVar)
		}
	case isPropKey(p.tok()):
		k := p.tok()
		if p.isOp(p.peek(1), ":") {
			p.propName()
			p.i++
			return p.bindingTarget(kind, start, bindVar)
		}
		if isIdent(k) {
			return p.bindingTarget(kind, start, bindVar)
		}
		p.i++
	}
	return nil
}

func (p *parser) computedKey() {
	open := p.i
	p.i++
	p.assign()
	if !p.is("]") {
		p.recover(open)
	}
	p.eat("]")
}

func (p *parser) decorators() {
	for p.is("@") {
		p.decorator()
	}
}

// decorator parses `@expr`, where expr is a path that may be called.
func (p *parser) decorator() {
	p.i++
	t := p.tok()
	if !isIdent(t) {
		if p.is("(") {
			p.parenExpr()
		}
		return
	}
	info := p.namePath(false)
	if info.ref != nil {
		info.ref.Call = true
	}
	if p.ts && p.is("<") {
		p.typeArgs()
	}
	if p.is("(") {
		p.args()
	}
}

type fnKind uint8

const (
	fnPlain  fnKind = iota // function declaration or expression: rebinds this
	fnMethod               // class or object literal method
	fnArrow                // arrow function: keeps this
)

func (p *parser) functionDecl(ctx declCtx) *code.Decl {
	p.i++
	p.eat("*")
	t := p.tok()
	var d *code.Decl
	switch {
	case isIdent(t):
		p.i++
		d = p.declTok(code.KindFunction, t, ctx.start)
		p.bindLexical(d)
	case ctx.isDefault:
		d = p.newDecl(code.KindFunction, "default", ctx.defSpan, ctx.start)
	}
	p.functionRest(d, fnPlain, nil, "")
	if p.is(";") && (d == nil || d.Body.Empty()) {
		p.i++
	}
	if d != nil {
		d.Span.End = p.prevEnd()
	}
	return d
}

// functionAhead reports whether a function or arrow function expression
// starts at the current token.
func (p *parser) functionAhead() bool {
	t := p.tok()
	if p.isOp(t, "function") {
		return true
	}
	if n := p.peek(1); p.isWord(t, "async") && p.isOp(n, "function") && !n.nl {
		return true
	}
	return p.arrowAhead()
}

func (p *parser) functionValue(d *code.Decl) {
	if p.word("async") && p.isOp(p.peek(1), "function") {
		p.kw()
	}
	if !p.is("function") {
		p.arrowFn(d)
		return
	}
	p.i++
	p.eat("*")
	self := ""
	if t := p.tok(); isIdent(t) {
		self = p.text(t)
		p.i++
	}
	p.functionRest(d, fnPlain, nil, self)
}

func (p *parser) functionExpr() {
	start := p.tok().start
	p.i++
	p.eat("*")
	var d *code.Decl
	if t := p.tok(); isIdent(t) {
		p.i++
		d = p.declTok(code.KindFunction, t, start)
	}
	self := ""
	if d != nil {
		self = d.Name
	}
	p.functionRest(d, fnPlain, nil, self)
	if d != nil {
		d.Span.End = p.prevEnd()
	}
}

// functionRest parses type parameters, parameters, return type and body.
// owner gets the TypeScript parameter properties of a constructor. self is
// the name of a function expression, which is only visible inside it.
func (p *parser) functionRest(d *code.Decl, kind fnKind, owner *code.Decl, self string) {
	saved := p.save()
	if d != nil {
		p.parent, p.refScope = d, d
	}
	if d == nil || !d.Callable() {
		p.anon++
	}
	if kind == fnPlain {
		p.thisDecl = nil
	}
	p.ctor = nil
	if d != nil && d.Kind == code.KindConstructor && !p.ts {
		p.ctor = owner
	}
	p.push(scopeFunction)
	if self != "" && d != nil {
		p.sc.bind(self, d)
	}
	if p.is("<") {
		p.typeParameterList(true)
	}
	if p.is("(") {
		p.params(owner, false)
	}
	p.functionReturnType(d)
	if p.is("{") {
		body := p.block(false)
		if d != nil {
			d.Body = body
		}
	}
	p.restore(saved)
}

func (p *parser) arrowFn(d *code.Decl) {
	if p.word("async") && !p.isOp(p.peek(1), "=>") {
		p.kw()
	}
	saved := p.save()
	if d != nil {
		p.parent, p.refScope = d, d
	} else {
		p.anon++
	}
	p.push(scopeArrow)
	if p.is("<") {
		p.typeParameterList(true)
	}
	if t := p.tok(); isIdent(t) {
		p.i++
		p.bindLexical(p.declTok(code.KindParameter, t, t.start))
	} else if p.is("(") {
		p.params(nil, false)
	}
	p.functionReturnType(d)
	p.eat("=>")
	if p.is("{") {
		body := p.block(false)
		if d != nil {
			d.Body = body
		}
	} else {
		noIn := p.noIn
		p.noIn = false
		p.assign()
		p.noIn = noIn
	}
	p.restore(saved)
}

func (p *parser) functionReturnType(d *code.Decl) {
	if !p.ts || !p.eat(":") {
		return
	}
	s := p.tok().start
	p.returnType(true)
	if d != nil {
		d.Type = p.textFrom(s)
	}
}

// params parses a parameter list. If typeOnly is set (signatures and
// function types), we don't declare anything.
func (p *parser) params(owner *code.Decl, typeOnly bool) {
	open := p.i
	p.i++
	for !p.eof() && !p.is(")") {
		st := p.i
		p.param(owner, typeOnly)
		if !p.listSeparator(open, st, ")") {
			break
		}
	}
	p.eat(")")
}

var paramModifiers = map[string]bool{"public": true, "private": true, "protected": true, "readonly": true, "override": true}

func (p *parser) param(owner *code.Decl, typeOnly bool) {
	start := p.tok().start
	p.decorators()
	property := false
	for t := p.tok(); isIdent(t) && paramModifiers[p.text(t)]; t = p.tok() {
		n := p.peek(1)
		if !isIdent(n) && !p.isOp(n, "{") && !p.isOp(n, "[") && !p.isOp(n, "...") && !isParamModifier(p, n) {
			break
		}
		p.kw()
		property = true
	}
	if p.is("this") {
		p.i++
		if p.eat(":") {
			p.typ()
		}
		return
	}
	p.eat("...")
	t := p.tok()
	var ds []*code.Decl
	switch {
	case typeOnly:
		if p.is("{") || p.is("[") {
			p.recover(p.i)
		}
		p.i++
	case isIdent(t) && property && owner != nil:
		p.i++
		saved := p.parent
		p.parent = owner
		d := p.declTok(code.KindField, t, start)
		p.parent = saved
		p.sc.bind(d.Name, d)
		p.addMember(owner, d.Name)
		ds = append(ds, d)
	default:
		ds = p.bindingTarget(code.KindParameter, start, false)
	}
	p.eat("?")
	if p.ts && p.eat(":") {
		s := p.tok().start
		tref := p.typ().ref
		if len(ds) == 1 && isIdent(t) {
			ds[0].Type = p.textFrom(s)
			if tref != nil {
				p.pr.typeRef[ds[0]] = tref
			}
		}
	}
	if p.eat("=") {
		info := p.assign()
		if len(ds) == 1 && ds[0].Type == "" && info.newText != "" {
			ds[0].Type = info.newText
			p.pr.typeRef[ds[0]] = info.newRef
		}
	}
	for _, d := range ds {
		d.Span.End = p.prevEnd()
	}
}

func isParamModifier(p *parser, t token) bool {
	return isIdent(t) && paramModifiers[p.text(t)]
}

func (p *parser) addMember(cls *code.Decl, name string) {
	if cls == nil {
		return
	}
	m := p.members[cls]
	if m == nil {
		m = map[string]bool{}
		p.members[cls] = m
	}
	m[name] = true
}

func (p *parser) isGT() bool {
	t := p.tok()
	return t.kind == code.TokPunct && t.sub == subNone && p.src[t.start] == '>'
}

// eatGT consumes a single '>' character. It has to split tokens like ">>",
// which can close two type argument lists at once.
func (p *parser) eatGT() {
	if !p.isGT() {
		return
	}
	t := &p.toks[p.i]
	if t.end-t.start == 1 {
		p.i++
		return
	}
	t.start++
}

func (p *parser) class(ctx declCtx, d *code.Decl, bind bool) *code.Decl {
	p.i++
	inner := ""
	if t := p.tok(); isIdent(t) && !p.isWord(t, "implements") {
		p.i++
		inner = p.text(t)
		if d == nil {
			d = p.declTok(code.KindClass, t, ctx.start)
			if bind {
				p.bindLexical(d)
			}
		}
	} else if d == nil && ctx.isDefault {
		d = p.newDecl(code.KindClass, "default", ctx.defSpan, ctx.start)
	}
	saved := p.save()
	p.push(scopeClass)
	if d != nil {
		p.parent = d
		p.bindLexical(d)
		if inner != "" && inner != d.Name {
			p.sc.bind(inner, d)
		}
	}
	if p.is("<") {
		p.typeParameterList(true)
	}
	var bases []string
	var baseRefs []*code.Ref
	if p.eat("extends") {
		s := p.tok().start
		info := p.callChain()
		if p.ts && p.is("<") {
			p.typeArgs()
		}
		bases = append(bases, p.textFrom(s))
		if info.path && info.ref != nil {
			baseRefs = append(baseRefs, info.ref)
		}
	}
	if p.word("implements") {
		p.kw()
		bases, baseRefs = p.baseTypes(bases, baseRefs)
	}
	if d != nil {
		if len(bases) > 0 {
			d.Bases = bases
		}
		p.pr.bases[d] = baseRefs
	}
	if p.is("{") {
		if d != nil {
			p.refScope = d
		} else {
			p.anon++
		}
		p.thisDecl = d
		body := p.classBody(d)
		if d != nil {
			d.Body = body
		}
	}
	p.restore(saved)
	if d != nil {
		d.Span.End = p.prevEnd()
	}
	return d
}

func (p *parser) baseTypes(bases []string, refs []*code.Ref) ([]string, []*code.Ref) {
	for !p.eof() {
		s := p.tok().start
		if ti := p.typ(); ti.ref != nil {
			refs = append(refs, ti.ref)
		}
		if s < p.prevEnd() {
			bases = append(bases, p.textFrom(s))
		}
		if !p.eat(",") {
			break
		}
	}
	return bases, refs
}

func (p *parser) classBody(cls *code.Decl) code.Span {
	start := p.tok().start
	open := p.i
	if !p.ts && cls != nil {
		p.collectMembers(cls, open)
	}
	p.i++
	for !p.eof() && !p.is("}") {
		st := p.i
		p.classMember(cls)
		if p.i == st {
			p.i++
		}
	}
	p.eat("}")
	return code.Span{Start: start, End: p.prevEnd()}
}

// collectMembers records the names declared directly in the class body that
// opens at open. We skip nested brackets.
func (p *parser) collectMembers(cls *code.Decl, open int) {
	end := int(p.match[open])
	if end < 0 {
		end = len(p.toks)
	}
	for i := open + 1; i < end; i++ {
		t := p.at(i)
		if m := p.match[i]; m > 0 {
			i = int(m)
			continue
		}
		n := p.at(i + 1)
		if isName(t) && (p.isOp(n, "(") || p.isOp(n, "=") || p.isOp(n, ";")) {
			p.addMember(cls, p.text(t))
		}
	}
}

var classModifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "static": true, "readonly": true,
	"abstract": true, "override": true, "declare": true, "accessor": true, "async": true,
	"get": true, "set": true,
}

func (p *parser) classMember(cls *code.Decl) {
	start := p.tok().start
	if p.eat(";") {
		return
	}
	p.decorators()
	accessor := false
	for t := p.tok(); isIdent(t) && classModifiers[p.text(t)]; t = p.tok() {
		n := p.peek(1)
		if !isPropKey(n) && !p.isOp(n, "[") && !p.isOp(n, "*") && !(p.isOp(n, "{") && p.isText(t, "static")) {
			break
		}
		if p.isText(t, "get") || p.isText(t, "set") {
			accessor = true
		}
		p.kw()
		if p.isText(t, "static") && p.is("{") {
			saved := p.save()
			p.push(scopeFunction)
			p.block(false)
			p.restore(saved)
			return
		}
	}
	p.eat("*")
	var name string
	var nameSpan code.Span
	t := p.tok()
	switch {
	case p.isOp(t, "[") && p.ts && isIdent(p.peek(1)) && p.isOp(p.peek(2), ":"):
		p.indexSignature()
		p.semi()
		return
	case p.isOp(t, "["):
		p.computedKey()
	case isPropKey(t):
		name, nameSpan = p.propName()
	default:
		return
	}
	p.eat("?")
	p.eat("!")
	if p.is("(") || p.is("<") {
		p.classMethod(cls, name, nameSpan, start, t, accessor)
		return
	}
	p.classField(cls, name, nameSpan, start)
}

func (p *parser) classMethod(cls *code.Decl, name string, nameSpan code.Span, start int, t token, accessor bool) {
	kind := code.KindMethod
	switch {
	case name == "constructor" && t.kind != code.TokString:
		kind = code.KindConstructor
	case accessor:
		kind = code.KindProperty
	}
	d := p.namedClassMember(cls, kind, name, nameSpan, start)
	owner := cls
	if kind != code.KindConstructor {
		owner = nil
	}
	p.functionRest(d, fnMethod, owner, "")
	end := p.semi()
	if d != nil {
		d.Span.End = end
	}
}

func (p *parser) namedClassMember(cls *code.Decl, kind code.DeclKind, name string, nameSpan code.Span, start int) *code.Decl {
	if cls == nil || name == "" {
		return nil
	}
	d := p.memberDecl(cls, kind, name, nameSpan, start)
	p.addMember(cls, name)
	return d
}

func (p *parser) classField(cls *code.Decl, name string, nameSpan code.Span, start int) {
	var typ string
	var tref *code.Ref
	if p.ts && p.eat(":") {
		s := p.tok().start
		tref = p.typ().ref
		typ = p.textFrom(s)
	}
	isFn := p.is("=") && p.peekFunction()
	kind := code.KindField
	if isFn {
		kind = code.KindMethod
	}
	d := p.namedClassMember(cls, kind, name, nameSpan, start)
	if p.eat("=") {
		if isFn {
			p.functionValue(d)
		} else {
			info := p.assign()
			if typ == "" && info.newText != "" {
				typ, tref = info.newText, info.newRef
			}
		}
	}
	end := p.semi()
	if d != nil {
		d.Span.End = end
		if d.Kind == code.KindField {
			d.Type = typ
			if tref != nil {
				p.pr.typeRef[d] = tref
			}
		}
	}
}

func (p *parser) peekFunction() bool {
	p.i++
	ok := p.functionAhead()
	p.i--
	return ok
}

func (p *parser) indexSignature() {
	open := p.i
	p.i += 2
	p.i++
	p.typ()
	if !p.is("]") {
		p.recover(open)
	}
	p.eat("]")
	p.eat("?")
	if p.eat(":") {
		p.typ()
	}
}

// ctorField declares a field for `this.x = value` in a JavaScript
// constructor if the class has no member x.
func (p *parser) ctorField() bool {
	cls := p.ctor
	if cls == nil || !p.is("this") || !p.isOp(p.peek(1), ".") {
		return false
	}
	n := p.peek(2)
	if !isName(n) || !p.isOp(p.peek(3), "=") || p.members[cls][p.text(n)] {
		return false
	}
	start := p.tok().start
	p.i += 3
	saved := p.parent
	p.parent = cls
	d := p.declTok(code.KindField, n, start)
	p.parent = saved
	p.addMember(cls, d.Name)
	p.i++
	info := p.assign()
	if info.newText != "" {
		d.Type = info.newText
		p.pr.typeRef[d] = info.newRef
	}
	d.Span.End = p.semi()
	return true
}

func (p *parser) interfaceDecl(ctx declCtx) *code.Decl {
	d, saved := p.typeDeclaration(ctx, code.KindInterface)
	if p.eat("extends") {
		bases, refs := p.baseTypes(nil, nil)
		if len(bases) > 0 {
			d.Bases = bases
		}
		p.pr.bases[d] = refs
	}
	if p.is("{") {
		p.refScope = d
		d.Body = p.typeMembers(d)
	}
	p.restore(saved)
	d.Span.End = p.prevEnd()
	return d
}

func (p *parser) typeAlias(ctx declCtx) *code.Decl {
	d, saved := p.typeDeclaration(ctx, code.KindTypeAlias)
	if p.eat("=") {
		if p.is("{") {
			p.typeOwn = d
		}
		if ti := p.typ(); ti.ref != nil {
			p.pr.typeRef[d] = ti.ref
		}
		p.typeOwn = nil
	}
	p.restore(saved)
	d.Span.End = p.semi()
	return d
}

func (p *parser) typeDeclaration(ctx declCtx, kind code.DeclKind) (*code.Decl, pctx) {
	p.kw()
	t := p.tok()
	p.i++
	d := p.declTok(kind, t, ctx.start)
	p.bindLexical(d)
	saved := p.save()
	p.push(scopeClass)
	p.parent = d
	if p.is("<") {
		p.typeParameterList(true)
	}
	return d, saved
}

func (p *parser) enumDecl(ctx declCtx) *code.Decl {
	p.i++
	t := p.tok()
	if !isIdent(t) {
		return nil
	}
	p.i++
	d := p.declTok(code.KindEnum, t, ctx.start)
	p.bindLexical(d)
	if p.is("{") {
		saved := p.save()
		p.push(scopeBlock)
		p.parent, p.refScope = d, d
		start := p.tok().start
		open := p.i
		p.i++
		for !p.eof() && !p.is("}") {
			st := p.i
			p.enumMember()
			if !p.eat(",") && (p.i == st || !p.is("}")) {
				p.recover(open)
				break
			}
		}
		p.eat("}")
		d.Body = code.Span{Start: start, End: p.prevEnd()}
		p.restore(saved)
	}
	d.Span.End = p.prevEnd()
	return d
}

func (p *parser) enumMember() {
	m := p.tok()
	if !isPropKey(m) {
		if p.is("[") {
			p.recover(p.i)
			p.i++
		}
		return
	}
	name, span := p.propName()
	if name == "" {
		return
	}
	md := p.newDecl(code.KindEnumMember, name, span, m.start)
	p.sc.bind(md.Name, md)
	if p.eat("=") {
		p.assign()
	}
	md.Span.End = p.prevEnd()
}

func (p *parser) namespaceDecl(ctx declCtx) *code.Decl {
	p.kw()
	saved := p.save()
	var chain []*code.Decl
	if t := p.tok(); t.kind == code.TokString {
		p.i++
		d := p.newDecl(code.KindNamespace, unquote(p.text(t)), code.Span{Start: t.start, End: t.end}, ctx.start)
		chain = append(chain, d)
		p.parent = d
		p.push(scopeNamespace)
	} else {
		for isIdent(p.tok()) {
			t := p.tok()
			p.i++
			d := p.declTok(code.KindNamespace, t, ctx.start)
			p.bindLexical(d)
			chain = append(chain, d)
			p.parent = d
			p.push(scopeNamespace)
			if !p.eat(".") {
				break
			}
		}
	}
	if len(chain) == 0 {
		p.restore(saved)
		return nil
	}
	var body code.Span
	if p.is("{") {
		p.refScope = p.parent
		p.thisDecl = nil
		body = p.block(false)
	} else {
		p.semi()
	}
	p.restore(saved)
	for _, d := range chain {
		d.Body = body
		d.Span.End = p.prevEnd()
	}
	return chain[0]
}

type importItem struct {
	name    token // imported name, zero for default and namespace imports
	local   token
	kind    byte // 'd' default, 'n' namespace, 's' specifier
	aliased bool
}

func (p *parser) importDecl() {
	start := p.tok().start
	p.i++
	p.pr.esm = true
	if t := p.tok(); (p.isWord(t, "type") || p.isWord(t, "typeof")) && !p.isWord(p.peek(1), "from") && !p.isOp(p.peek(1), ",") && !p.isOp(p.peek(1), "=") {
		p.kw()
	}
	if t := p.tok(); t.kind == code.TokString {
		p.i++
		p.importAttributes()
		p.semi()
		return
	}
	if t := p.tok(); isIdent(t) && p.isOp(p.peek(1), "=") {
		p.importEquals(start, t)
		return
	}
	var items []importItem
	if t := p.tok(); isIdent(t) {
		items = append(items, importItem{local: t, kind: 'd'})
		p.i++
		p.eat(",")
	}
	if p.is("*") {
		p.i++
		if p.word("as") {
			p.kw()
		}
		if t := p.tok(); isIdent(t) {
			items = append(items, importItem{local: t, kind: 'n'})
			p.i++
		}
	}
	if p.is("{") {
		items = append(items, p.specifiers()...)
	}
	spec := ""
	if p.word("from") {
		p.kw()
	}
	if t := p.tok(); t.kind == code.TokString {
		spec = unquote(p.text(t))
		p.i++
	}
	p.importAttributes()
	end := p.semi()
	for _, it := range items {
		switch it.kind {
		case 'd', 'n':
			d := p.declTok(code.KindImport, it.local, start)
			d.Span.End = end
			p.sc.bind(d.Name, d)
			name := "default"
			if it.kind == 'n' {
				name = "*"
			}
			p.pr.aliases[d] = alias{spec: spec, name: name}
		default:
			name := p.text(it.name)
			if it.name.kind == code.TokString {
				name = unquote(name)
			} else if it.name.kind != code.TokKeyword {
				r := p.newRef(it.name, refMeta{spec: spec})
				r.Import = true
			}
			p.bindNamedImport(it.local, name, spec, start, end, it.aliased)
		}
	}
}

func (p *parser) bindNamedImport(local token, name, spec string, start, end int, aliased bool) {
	if !aliased {
		p.sc.bindImport(name, importBinding{spec: spec, name: name})
		return
	}
	d := p.declTok(code.KindImport, local, start)
	d.Span.End = end
	p.sc.bind(d.Name, d)
	p.pr.aliases[d] = alias{spec: spec, name: name}
}

// specifiers parses `{ a, b as c, type d }` of an import or export.
func (p *parser) specifiers() []importItem {
	open := p.i
	p.i++
	var items []importItem
	for !p.eof() && !p.is("}") {
		st := p.i
		if p.word("type") && (isPropKey(p.peek(1))) && !p.isOp(p.peek(1), ",") && !p.isWord(p.peek(1), "as") {
			p.kw()
		}
		if t := p.tok(); isPropKey(t) {
			p.i++
			it := importItem{name: t, local: t, kind: 's'}
			if p.word("as") {
				p.kw()
				if l := p.tok(); isPropKey(l) {
					it.local, it.aliased = l, true
					p.i++
				}
			}
			items = append(items, it)
		}
		if !p.eat(",") && (p.i == st || !p.is("}")) {
			p.recover(open)
			break
		}
	}
	p.eat("}")
	return items
}

func (p *parser) importAttributes() {
	if (p.word("with") || p.is("with") || p.word("assert")) && p.isOp(p.peek(1), "{") && !p.peek(0).nl {
		p.i++
		p.recover(p.i)
		p.i++
	}
}

// importEquals parses TypeScript `import a = require('x')` and
// `import a = N.b`.
func (p *parser) importEquals(start int, name token) *code.Decl {
	p.i += 2
	d := p.declTok(code.KindImport, name, start)
	p.sc.bind(d.Name, d)
	if p.requireAfter(p.i) {
		p.pr.aliases[d] = alias{spec: p.requireCall(), name: "*"}
	} else if t := p.tok(); isIdent(t) {
		info := p.namePath(false)
		p.pr.aliases[d] = alias{ref: info.ref}
	}
	d.Span.End = p.semi()
	return d
}

// exportDecl parses an export statement. start includes any leading
// decorators.
func (p *parser) exportDecl(start int) {
	p.i++
	p.pr.esm = true
	top := p.parent == p.f.Module && !p.inGlobal
	switch {
	case p.is("default"):
		def := p.tok()
		p.i++
		p.exportDefault(start, def, top)
	case p.is("="):
		p.i++
		if t := p.tok(); isIdent(t) && top {
			p.pr.exports["default"] = export{sc: p.sc, name: p.text(t)}
			p.pr.cjsWhole = true
		}
		p.assign()
		p.semi()
	case p.is("*"):
		p.i++
		var local token
		if p.word("as") {
			p.kw()
			local = p.tok()
			p.i++
		}
		if p.word("from") {
			p.kw()
		}
		spec := ""
		if t := p.tok(); t.kind == code.TokString {
			spec = unquote(p.text(t))
			p.i++
		}
		p.importAttributes()
		end := p.semi()
		if local.end > local.start && isPropKey(local) {
			d := p.declTok(code.KindImport, local, start)
			d.Span.End = end
			p.pr.aliases[d] = alias{spec: spec, name: "*"}
			if top {
				p.pr.exports[d.Name] = export{decl: d}
			}
		} else if top && spec != "" {
			p.pr.stars = append(p.pr.stars, spec)
		}
	case p.word("as") && p.isWord(p.peek(1), "namespace"):
		p.kw()
		p.kw()
		p.i++
		p.semi()
	case p.is("{") || (p.word("type") && p.isOp(p.peek(1), "{")):
		if !p.is("{") {
			p.kw()
		}
		p.exportClause(start, top)
	case p.is("import"):
		p.i++
		if t := p.tok(); isIdent(t) && p.isOp(p.peek(1), "=") {
			if d := p.importEquals(start, t); top {
				p.pr.exports[d.Name] = export{decl: d}
			}
		}
	default:
		ds, _ := p.declaration(declCtx{start: start})
		if top {
			for _, d := range ds {
				p.pr.exports[d.Name] = export{decl: d}
			}
		}
	}
}

func (p *parser) exportClause(start int, top bool) {
	items := p.specifiers()
	spec := ""
	if p.word("from") {
		p.kw()
		if t := p.tok(); t.kind == code.TokString {
			spec = unquote(p.text(t))
			p.i++
		}
	}
	p.importAttributes()
	end := p.semi()
	for _, it := range items {
		name := p.text(it.name)
		if it.name.kind == code.TokString {
			name = unquote(name)
		}
		if it.name.kind != code.TokKeyword && it.name.kind != code.TokString {
			m := refMeta{spec: spec}
			r := p.newRef(it.name, m)
			r.Import = true
		}
		var e export
		if spec != "" {
			e = export{spec: spec, name: name}
		} else {
			e = export{sc: p.sc, name: name}
		}
		exported := name
		if it.aliased {
			exported = p.text(it.local)
			if it.local.kind == code.TokString {
				exported = unquote(exported)
			} else {
				d := p.declTok(code.KindImport, it.local, start)
				d.Span.End = end
				p.pr.aliases[d] = alias{spec: e.spec, sc: e.sc, name: name}
				e = export{decl: d}
			}
		}
		if top {
			p.pr.exports[exported] = e
		}
	}
}

func (p *parser) exportDefault(start int, def token, top bool) {
	ctx := declCtx{start: start, isDefault: true, defSpan: code.Span{Start: def.start, End: def.end}}
	record := func(d *code.Decl) {
		if top && d != nil {
			p.pr.exports["default"] = export{decl: d}
		}
	}
	t := p.tok()
	n := p.peek(1)
	switch {
	case p.isOp(t, "function"), p.isOp(t, "class"), p.isOp(t, "@"),
		p.isWord(t, "async") && p.isOp(n, "function"),
		p.isWord(t, "abstract") && p.isOp(n, "class"),
		p.isWord(t, "interface") && isIdent(n):
		ds, _ := p.declaration(ctx)
		if len(ds) > 0 {
			record(ds[0])
		}
		return
	case isIdent(t) && (p.isOp(n, ";") || p.isOp(n, "}") || n.nl || p.i+1 >= len(p.toks)) && !p.isOp(n, "=>"):
		p.i++
		p.useRef(t)
		if top {
			p.pr.exports["default"] = export{sc: p.sc, name: p.text(t)}
		}
		p.semi()
		return
	}
	var d *code.Decl
	switch {
	case p.functionAhead():
		d = p.newDecl(code.KindFunction, "default", ctx.defSpan, start)
		p.functionValue(d)
	case p.is("{"):
		d = p.newDecl(code.KindConstant, "default", ctx.defSpan, start)
		p.objectLiteral(d, false)
		p.continueExpr()
	default:
		d = p.newDecl(code.KindConstant, "default", ctx.defSpan, start)
		if info := p.assign(); info.newText != "" {
			d.Type = info.newText
			p.pr.typeRef[d] = info.newRef
		}
	}
	d.Span.End = p.semi()
	record(d)
}

// cjsExport handles `module.exports = v`, `module.exports.x = v` and
// `exports.x = v` statements at module level.
func (p *parser) cjsExport() bool {
	if p.refScope != p.f.Module || p.anon > 0 {
		return false
	}
	t := p.tok()
	var name token
	var eq int
	switch {
	case p.isWord(t, "module") && p.isOp(p.peek(1), ".") && p.isWord(p.peek(2), "exports"):
		switch {
		case p.isOp(p.peek(3), "="):
			eq = p.i + 3
		case p.isOp(p.peek(3), ".") && isName(p.peek(4)) && p.isOp(p.peek(5), "="):
			name, eq = p.peek(4), p.i+5
		default:
			return false
		}
	case p.isWord(t, "exports") && p.isOp(p.peek(1), ".") && isName(p.peek(2)) && p.isOp(p.peek(3), "="):
		name, eq = p.peek(2), p.i+3
	default:
		return false
	}
	p.pr.esm = true
	start := t.start
	obj := exprInfo{ref: p.useRef(t), path: true}
	if p.isWord(t, "module") {
		p.memberRef(p.peek(2), obj, false)
	}
	p.i = eq + 1
	if name.end > name.start {
		kind := code.KindVariable
		switch {
		case p.functionAhead():
			kind = code.KindFunction
		case p.is("class"):
			kind = code.KindClass
		}
		d := p.declTok(kind, name, start)
		p.pr.exports[d.Name] = export{decl: d}
		switch kind {
		case code.KindFunction:
			p.functionValue(d)
		case code.KindClass:
			p.class(declCtx{start: start}, d, false)
		default:
			if info := p.assign(); info.newText != "" {
				d.Type = info.newText
				p.pr.typeRef[d] = info.newRef
			}
		}
		d.Span.End = p.semi()
		return true
	}
	p.pr.cjsWhole = true
	v := p.tok()
	switch {
	case p.is("{"):
		p.objectLiteral(nil, true)
	case isIdent(v) && (p.stmtEndsAt(p.i + 1)):
		p.i++
		p.useRef(v)
		p.pr.exports["default"] = export{sc: p.sc, name: p.text(v)}
	case p.is("class") && isIdent(p.peek(1)):
		if d := p.class(declCtx{start: start}, nil, true); d != nil {
			p.pr.exports["default"] = export{decl: d}
		}
	case p.is("function") && isIdent(p.peek(1)):
		if d := p.functionDecl(declCtx{start: start}); d != nil {
			p.pr.exports["default"] = export{decl: d}
		}
	default:
		p.assign()
	}
	p.semi()
	return true
}

// stmtEndsAt reports whether the statement ends before token i. We need
// this for restricted productions, like `return` followed by a newline.
func (p *parser) stmtEndsAt(i int) bool {
	t := p.at(i)
	return i >= len(p.toks) || t.nl || p.isOp(t, ";") || p.isOp(t, "}")
}
