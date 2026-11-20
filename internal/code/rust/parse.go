package rust

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type scopeKind uint8

const (
	scopeModule scopeKind = iota // file module or inline mod
	scopeItem                    // struct, enum, trait or impl with its generics
	scopeFunc
	scopeBlock
)

// scope is a lexical scope. The parser records these so the resolver can
// look names up later.
type scope struct {
	kind   scopeKind
	parent *scope
	// Declarations made in this scope get it as Parent, references as Scope.
	owner *code.Decl
	mod   *code.Decl // file module or inline mod the scope belongs to
	// items and generic parameters, which are visible in the whole scope
	items map[string][]*code.Decl
	// Bindings in declaration order. A binding is visible from its
	// declInfo.visible on, and a later binding shadows an earlier one.
	locals []*code.Decl
	// Names a use declaration brings in, mapped to the reference to the last
	// segment of the imported path.
	imports map[string]*code.Ref
	globs   []glob
	// Generic parameters of impl blocks. They get no declaration, but they
	// still shadow outer names, so looking one up resolves to nothing.
	hidden   map[string]bool
	selfType *code.Ref  // type an impl block implements
	selfDecl *code.Decl // trait of a trait body
	selfImpl bool       // the scope is an impl block
}

func newScope(kind scopeKind, parent *scope, owner *code.Decl) *scope {
	s := &scope{kind: kind, parent: parent, owner: owner, items: map[string][]*code.Decl{}}
	if parent != nil {
		s.mod = parent.mod
	}
	return s
}

func (s *scope) addItem(d *code.Decl) {
	if d != nil && d.Name != "" && d.Name != "_" {
		s.items[d.Name] = append(s.items[d.Name], d)
	}
}

func (s *scope) addImport(name string, r *code.Ref) {
	if name == "" || name == "_" || r == nil {
		return
	}
	if s.imports == nil {
		s.imports = map[string]*code.Ref{}
	}
	if _, ok := s.imports[name]; !ok {
		s.imports[name] = r
	}
}

type refKind uint8

const (
	refPlain   refKind = iota // a name looked up in the lexical scopes
	refSeg                    // a path segment after `::`, qualified by prev
	refPrefix                 // the segment after crate::, self::, super:: or Self::
	refMember                 // after `.`, member of obj
	refField                  // field name in a struct literal or pattern of obj
	refMacro                  // name of a macro invocation
	refUse                    // first segment of a use path
	refCrate                  // extern crate name
	refModFile                // name of `mod x;`, linked by the module tree
)

// object is what a member access or struct literal applies to.
type object struct {
	ref  *code.Ref // a named value, or the callee when call is set
	call bool      // the result of calling ref
	self bool      // the self value or Self type
}

type refInfo struct {
	sc     *scope
	kind   refKind
	prev   *code.Ref
	prefix string // "crate", "self", "super" or "Self" for refPrefix
	supers int    // number of super segments
	obj    object
}

type declInfo struct {
	sc      *scope
	visible int       // offset from which a local binding is visible, -1 for items
	typeRef *code.Ref // head of the declared type or result type
	selfRet bool      // the declared type is Self
	init    object    // initializer of a binding without a type
	alias   *code.Ref // imported entity of an alias import
}

type implInfo struct {
	self    *code.Ref
	trait   *code.Ref
	traitAs string // the trait path as written, generics left out
	members []*code.Decl
}

// glob is a `use path::*` import of the path ending in ref. If the path is
// only keywords, like `use super::*`, prefix and supers hold them.
type glob struct {
	ref    *code.Ref
	prefix string
	supers int
}

// modLink is a `mod name;` declaration. The body lives in another file.
type modLink struct {
	ref    *code.Ref
	parent *code.Decl // the module that declares it
	inline []string   // names of the inline mods between the file and the declaration
	path   string     // the #[path] attribute, if any
}

type fileInfo struct {
	f       *code.File
	layout  layout
	root    *scope
	refs    map[*code.Ref]*refInfo
	decls   map[*code.Decl]*declInfo
	impls   []*implInfo
	links   []modLink
	modScop map[*code.Decl]*scope // module scope of the file and of each inline mod
	modUp   map[*code.Decl]*code.Decl
}

// parser walks the code tokens of a file, with the comments stripped out.
type parser struct {
	f     *code.File
	src   []byte
	t     []tok
	match []int // index of the matching bracket, -1 when unmatched
	i     int
	fi    *fileInfo
	// Set if the first `>` of a `>>` token closed a generic argument list.
	// The next close then has to consume the token.
	halfGt   bool
	inline   []string // inline mod names around the current position
	pathAttr string   // #[path] value of the item being parsed
}

func parseFile(f *code.File) *fileInfo {
	l := lex(f.Src)
	t := code.SetTokens(f, l.toks, tok.token)
	f.Comments = l.comments
	ly := fileLayout(f.Path, f.Abs)
	mod := f.SetModule(ly.module)
	fi := &fileInfo{
		f: f, layout: ly, refs: map[*code.Ref]*refInfo{}, decls: map[*code.Decl]*declInfo{},
		modScop: map[*code.Decl]*scope{}, modUp: map[*code.Decl]*code.Decl{},
	}
	fi.root = newScope(scopeModule, nil, mod)
	fi.root.mod = mod
	fi.modScop[mod] = fi.root
	p := &parser{f: f, src: f.Src, t: t, fi: fi}
	p.match = matchBrackets(p)
	p.items(fi.root, len(t), nil)
	return fi
}

// matchBrackets pairs (), [] and {}. We skip a closer that has no opener.
// If a closer matches an outer opener, it closes the inner ones too.
func matchBrackets(p *parser) []int {
	m := make([]int, len(p.t))
	var stack []int
	for i := range p.t {
		m[i] = -1
		if p.t[i].kind != code.TokPunct {
			continue
		}
		switch c := p.src[p.t[i].s]; c {
		case '(', '[', '{':
			stack = append(stack, i)
		case ')', ']', '}':
			open := map[byte]byte{')': '(', ']': '[', '}': '{'}[c]
			k := len(stack) - 1
			for k >= 0 && p.src[p.t[stack[k]].s] != open {
				k--
			}
			if k < 0 {
				continue
			}
			m[i], m[stack[k]] = stack[k], i
			stack = stack[:k]
		}
	}
	return m
}

func (p *parser) text(i int) string {
	if i < 0 || i >= len(p.t) {
		return ""
	}
	return string(p.src[p.t[i].s:p.t[i].e])
}

func (p *parser) cur() string { return p.text(p.i) }

func (p *parser) is(i int, s string) bool { return p.text(i) == s }

func (p *parser) span(i int) code.Span {
	if i >= len(p.t) {
		return code.Span{Start: len(p.src), End: len(p.src)}
	}
	return code.Span{Start: p.t[i].s, End: p.t[i].e}
}

// startOf and endOf return the offsets of token i, clamped to the file.
func (p *parser) startOf(i int) int { return p.span(i).Start }

func (p *parser) endOf(i int) int {
	if i <= 0 {
		return 0
	}
	return p.span(min(i, len(p.t)) - 1).End
}

// isIdent reports whether token i is an identifier other than a lifetime.
func (p *parser) isIdent(i int) bool {
	return i < len(p.t) && p.t[i].kind == code.TokIdent && p.src[p.t[i].s] != '\''
}

func (p *parser) isLifetime(i int) bool {
	return i < len(p.t) && p.t[i].kind == code.TokIdent && p.src[p.t[i].s] == '\''
}

// name returns the identifier of token i without an r# prefix.
func (p *parser) name(i int) string { return strings.TrimPrefix(p.text(i), "r#") }

// closeOf returns the index of the bracket that closes the one at i, or end
// if that's past end. An unmatched ( or [ stops before the next brace or
// semicolon, so a missing parenthesis doesn't swallow the rest of the file.
func (p *parser) closeOf(i, end int) int {
	if c := p.match[i]; c > i {
		return min(c, end)
	}
	if p.is(i, "(") || p.is(i, "[") {
		for k := i + 1; k < end; k++ {
			switch p.text(k) {
			case ")", "]":
				return k
			case "{", "}", ";":
				return k - 1
			}
		}
	}
	return end
}

func (p *parser) skipBracket(end int) {
	p.i = min(p.closeOf(p.i, end)+1, end)
}

func (p *parser) newRef(sc *scope, i int, kind refKind) *code.Ref {
	r := p.f.NewRef(sc.owner, p.name(i), p.span(i))
	p.fi.refs[r] = &refInfo{sc: sc, kind: kind}
	return r
}

func (p *parser) newDecl(sc *scope, kind code.DeclKind, i, start int) *code.Decl {
	d := p.f.NewDecl(sc.owner, kind, p.name(i), p.span(i), code.Span{Start: start, End: p.span(i).End})
	p.fi.decls[d] = &declInfo{sc: sc, visible: -1}
	return d
}

// binding declares a pattern binding. Bindings are local even at module
// level, for example in a closure in a static initializer.
func (p *parser) binding(sc *scope, kind code.DeclKind, i, visible int) *code.Decl {
	d := p.newDecl(sc, kind, i, p.span(i).Start)
	d.Local = true
	p.fi.decls[d].visible = visible
	sc.locals = append(sc.locals, d)
	return d
}

// finish sets the span end of d to the end of the token before p.i.
func (p *parser) finish(d *code.Decl) {
	if d != nil {
		d.Span.End = max(d.Span.End, p.endOf(p.i))
	}
}

// attrs skips outer and inner attributes at p.i, remembering a #[path].
func (p *parser) attrs(end int) {
	for p.i < end && p.is(p.i, "#") {
		j := p.i + 1
		if p.is(j, "!") {
			j++
		}
		if !p.is(j, "[") {
			return
		}
		cl := p.closeOf(j, end)
		if p.is(j+1, "path") && p.is(j+2, "=") && j+3 < cl && p.t[j+3].kind == code.TokString {
			p.pathAttr = strings.Trim(p.text(j+3), `"`)
		}
		p.i = min(cl+1, end)
	}
}

// visibility skips pub, pub(crate), pub(in path).
func (p *parser) visibility(end int) {
	if p.i < end && p.is(p.i, "pub") {
		p.i++
		if p.is(p.i, "(") {
			p.skipBracket(end)
		}
	}
}

// items parses the items of a module or block, extern blocks included, up to
// end. impl is set inside impl and trait bodies.
func (p *parser) items(sc *scope, end int, impl *implInfo) {
	for p.i < end {
		start := p.i
		if !p.item(sc, end, impl) {
			p.i++
		}
		if p.i <= start {
			p.i = start + 1
		}
	}
}

// itemStarts reports whether an item starts at token i, as opposed to a
// statement.
func (p *parser) itemStarts(i int) bool {
	switch p.text(i) {
	case "fn", "struct", "enum", "trait", "impl", "mod", "use", "static", "type", "pub", "#":
		return true
	case "const":
		return !p.is(i+1, "{") && !p.is(i+1, "|") && !p.is(i+1, "move")
	case "unsafe":
		return !p.is(i+1, "{")
	case "async":
		return p.is(i+1, "fn") || p.is(i+1, "unsafe")
	case "extern":
		return true
	case "union", "auto", "default", "safe":
		return p.isIdent(i+1) || p.is(i+1, "fn") || p.is(i+1, "trait") || p.is(i+1, "unsafe")
	case "macro_rules":
		return p.is(i+1, "!")
	}
	return false
}

// item parses the item at p.i and reports whether it found one.
func (p *parser) item(sc *scope, end int, impl *implInfo) bool {
	start := p.i
	p.pathAttr = ""
	p.attrs(end)
	p.visibility(end)
	offset := p.startOf(start)
	for p.i < end {
		switch p.cur() {
		case "const":
			if p.is(p.i+1, "fn") || p.is(p.i+1, "unsafe") || p.is(p.i+1, "async") || p.is(p.i+1, "extern") {
				p.i++
				continue
			}
			p.constItem(sc, end, offset, code.KindConstant, impl)
			return true
		case "async", "unsafe", "default", "safe", "auto":
			if p.cur() == "unsafe" && p.is(p.i+1, "{") {
				return p.i > start
			}
			p.i++
			continue
		case "extern":
			if p.externItem(sc, end) {
				return true
			}
			continue
		case "fn":
			p.fn(sc, end, offset, impl)
			return true
		case "struct":
			p.structItem(sc, end, offset)
			return true
		case "union":
			if !p.isIdent(p.i + 1) {
				return false
			}
			p.structItem(sc, end, offset)
			return true
		case "enum":
			p.enumItem(sc, end, offset)
			return true
		case "trait":
			p.traitItem(sc, end, offset)
			return true
		case "impl":
			p.implItem(sc, end)
			return true
		case "type":
			p.typeItem(sc, end, offset, impl)
			return true
		case "static":
			p.constItem(sc, end, offset, code.KindVariable, impl)
			return true
		case "mod":
			p.modItem(sc, end, offset)
			return true
		case "use":
			p.i++
			p.useTree(sc, end, nil, "", 0, false)
			if p.is(p.i, ";") {
				p.i++
			}
			return true
		case "macro_rules":
			if !p.is(p.i+1, "!") {
				return false
			}
			p.macroRules(sc, end, offset)
			return true
		default:
			if p.isIdent(p.i) && sc.kind == scopeModule && p.itemMacro(sc, end) {
				return true
			}
			// Attributes or a visibility before something else, such as
			// an expression statement with an attribute.
			return p.i > start
		}
	}
	return p.i > start
}

// externItem parses `extern crate` or an `extern "C" {}` block at p.i. For
// an extern fn it skips the ABI and returns false, so the caller can parse
// the fn next.
func (p *parser) externItem(sc *scope, end int) bool {
	switch {
	case p.is(p.i+1, "crate"):
		p.externCrate(sc, end)
		return true
	case p.t[min(p.i+1, len(p.t)-1)].kind == code.TokString:
		p.i += 2
	default:
		p.i++
	}
	if !p.is(p.i, "{") {
		return false
	}
	cl := p.closeOf(p.i, end)
	p.i++
	p.items(sc, cl, nil)
	p.i = min(cl+1, end)
	return true
}

// itemMacro parses a macro invocation in item position, like lazy_static! { }.
func (p *parser) itemMacro(sc *scope, end int) bool {
	if !p.macroPath(sc, end) {
		return false
	}
	p.exprGroup(sc, end)
	if p.is(p.i, ";") {
		p.i++
	}
	return true
}

// macroPath parses `a::b!` at p.i and records its references. It leaves p.i
// on the bracket.
func (p *parser) macroPath(sc *scope, end int) bool {
	j := p.i
	for p.isIdent(j) && p.is(j+1, "::") {
		j += 2
	}
	if j >= end || !p.isIdent(j) || !p.is(j+1, "!") || !(p.is(j+2, "(") || p.is(j+2, "[") || p.is(j+2, "{")) {
		return false
	}
	var prev *code.Ref
	for ; p.i <= j; p.i += 2 {
		kind := refSeg
		switch {
		case prev == nil && p.i == j:
			kind = refMacro
		case prev == nil:
			kind = refPlain
		}
		r := p.newRef(sc, p.i, kind)
		p.fi.refs[r].prev = prev
		prev = r
	}
	prev.Call = true
	p.i = j + 2
	return true
}

// exprGroup scans the bracketed group at p.i as expressions. Besides
// parentheses and array literals we use it for macro arguments, which is
// good enough for format!, vec!, assert! and most other macros.
func (p *parser) exprGroup(sc *scope, end int) {
	if p.i >= end {
		return
	}
	cl := p.closeOf(p.i, end)
	p.i++
	p.exprList(sc, cl)
	p.i = min(cl+1, end)
}

// generics parses <...> parameters at p.i and declares them under owner. If
// owner is nil, as for impl blocks, we hide them in sc.
func (p *parser) generics(sc *scope, owner *code.Decl, end int) {
	if !p.is(p.i, "<") {
		return
	}
	p.i++
	for p.i < end && !p.angleClose() {
		start := p.i
		switch {
		case p.isLifetime(p.i):
			p.i++
			if p.is(p.i, ":") {
				p.i++
				for p.isLifetime(p.i) || p.is(p.i, "+") {
					p.i++
				}
			}
		case p.is(p.i, "const") && p.isIdent(p.i+1):
			p.i++
			p.typeParam(sc, owner)
			if p.is(p.i, ":") {
				p.i++
				p.typ(sc, end)
			}
			if p.is(p.i, "=") {
				p.i++
				p.genericArg(sc, end)
			}
		case p.isIdent(p.i):
			p.typeParam(sc, owner)
			if p.is(p.i, ":") {
				p.i++
				p.bounds(sc, end)
			}
			if p.is(p.i, "=") {
				p.i++
				p.typ(sc, end)
			}
		}
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
	p.closeAngle()
}

func (p *parser) typeParam(sc *scope, owner *code.Decl) {
	if owner == nil {
		if sc.hidden == nil {
			sc.hidden = map[string]bool{}
		}
		sc.hidden[p.name(p.i)] = true
		p.i++
		return
	}
	d := p.f.NewDecl(owner, code.KindTypeParameter, p.name(p.i), p.span(p.i), p.span(p.i))
	d.Local = true
	p.fi.decls[d] = &declInfo{sc: sc, visible: -1}
	sc.addItem(d)
	p.i++
}

func (p *parser) angleClose() bool {
	switch p.cur() {
	case ">", ">>", ">=", ">>=":
		return true
	}
	return p.halfGt
}

// closeAngle consumes the `>` closing a generic list, splitting `>>`.
func (p *parser) closeAngle() {
	if p.halfGt {
		p.halfGt = false
		p.i++
		return
	}
	switch p.cur() {
	case ">", ">=":
		p.i++
	case ">>", ">>=":
		p.halfGt = true
	}
}

// where parses a where clause at p.i up to a body or semicolon.
func (p *parser) where(sc *scope, end int) {
	if !p.is(p.i, "where") {
		return
	}
	p.i++
	for p.i < end && !p.is(p.i, "{") && !p.is(p.i, ";") && !p.is(p.i, "=") {
		start := p.i
		if p.is(p.i, "for") && p.is(p.i+1, "<") {
			p.i++
			p.skipAngles(end)
		}
		if p.isLifetime(p.i) {
			p.i++
		} else {
			p.typ(sc, end)
		}
		if p.is(p.i, ":") {
			p.i++
			p.bounds(sc, end)
		}
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
}

// skipAngles skips a <...> group that has no references to record, like
// for<'a>.
func (p *parser) skipAngles(end int) {
	depth := 0
	for p.i < end {
		switch p.cur() {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		}
		p.i++
		if depth <= 0 {
			return
		}
	}
}

func (p *parser) fn(sc *scope, end, offset int, impl *implInfo) {
	p.i++
	if !p.isIdent(p.i) {
		return
	}
	kind := code.KindFunction
	if impl != nil {
		kind = code.KindMethod
	}
	d := p.newDecl(sc, kind, p.i, offset)
	p.assoc(sc, d, impl)
	p.i++
	fs := newScope(scopeFunc, sc, d)
	p.generics(fs, d, end)
	if p.is(p.i, "(") {
		cl := p.closeOf(p.i, end)
		p.i++
		p.params(fs, cl)
		p.i = min(cl+1, end)
	}
	if p.is(p.i, "->") {
		p.i++
		if p.is(p.i, "Self") && !p.is(p.i+1, "::") {
			p.fi.decls[d].selfRet = true
		}
		start := p.i
		p.fi.decls[d].typeRef = p.typ(fs, end)
		d.Type = p.typeText(start, p.i)
	}
	p.where(fs, end)
	if p.is(p.i, "{") {
		cl := p.closeOf(p.i, end)
		d.Body = code.Span{Start: p.startOf(p.i), End: p.endOf(cl + 1)}
		p.block(fs, cl)
		p.i = min(cl+1, end)
	} else if p.is(p.i, ";") {
		p.i++
	}
	p.finish(d)
}

// list calls elem for each comma separated element up to cl. If elem doesn't
// consume the whole element, we skip the rest of it.
func (p *parser) list(cl int, elem func()) {
	for p.i < cl {
		start := p.i
		elem()
		for p.i < cl && !p.is(p.i, ",") {
			p.i++
		}
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
}

func (p *parser) params(fs *scope, cl int) {
	p.list(cl, func() {
		p.attrs(cl)
		j := p.i
		for p.is(j, "&") || p.is(j, "&&") || p.isLifetime(j) || p.is(j, "mut") {
			j++
		}
		switch {
		case p.is(j, "self"):
			p.i = j + 1
			if p.is(p.i, ":") {
				p.i++
				p.typ(fs, cl)
			}
		case p.is(p.i, "..."):
			p.i++
		default:
			p.typedPattern(fs, cl, stopParam, code.KindParameter)
		}
	})
}

// typedPattern parses a parameter or let pattern with an optional `: Type`
// and returns its bindings. If there's a single binding, it gets the type.
// Let bindings stay invisible until the caller knows where the statement
// ends.
func (p *parser) typedPattern(sc *scope, end int, stops map[string]bool, kind code.DeclKind) []*code.Decl {
	var decls []*code.Decl
	p.pattern(sc, end, stops, func(i int) {
		visible := p.span(i).Start
		if kind == code.KindVariable {
			visible = len(p.src)
		}
		decls = append(decls, p.binding(sc, kind, i, visible))
	})
	if !p.is(p.i, ":") {
		return decls
	}
	p.i++
	ts := p.i
	head := p.typ(sc, end)
	if len(decls) == 1 {
		decls[0].Type = p.typeText(ts, p.i)
		p.fi.decls[decls[0]].typeRef = head
		decls[0].Span.End = p.endOf(p.i)
	}
	return decls
}

// typeText returns the source of tokens [i, j) with whitespace runs
// collapsed.
func (p *parser) typeText(i, j int) string {
	if j <= i || i >= len(p.t) {
		return ""
	}
	return strings.Join(strings.Fields(string(p.src[p.startOf(i):p.endOf(j)])), " ")
}

// typeHeader declares the struct, union, enum or trait named after p.i and
// parses its generics. It returns the declaration and its body scope, or nil
// if no name follows.
func (p *parser) typeHeader(sc *scope, end, offset int, kind code.DeclKind) (*code.Decl, *scope) {
	p.i++
	if !p.isIdent(p.i) {
		return nil, nil
	}
	d := p.newDecl(sc, kind, p.i, offset)
	sc.addItem(d)
	p.i++
	is := newScope(scopeItem, sc, d)
	is.selfDecl = d
	p.generics(is, d, end)
	return d, is
}

func (p *parser) structItem(sc *scope, end, offset int) {
	d, is := p.typeHeader(sc, end, offset, code.KindStruct)
	if d == nil {
		return
	}
	p.where(is, end)
	if p.is(p.i, "{") {
		d.Body = code.Span{Start: p.startOf(p.i), End: p.endOf(p.closeOf(p.i, end) + 1)}
	}
	p.structBody(is, d, end)
	p.where(is, end)
	if p.is(p.i, ";") {
		p.i++
	}
	p.finish(d)
}

// structBody parses the `{ named: T }` or `(T, U)` fields of a struct or
// variant at p.i, if any.
func (p *parser) structBody(is *scope, owner *code.Decl, end int) {
	if !p.is(p.i, "{") && !p.is(p.i, "(") {
		return
	}
	named := p.is(p.i, "{")
	cl := p.closeOf(p.i, end)
	p.i++
	p.list(cl, func() {
		p.attrs(cl)
		fstart := p.startOf(p.i)
		p.visibility(cl)
		if !named {
			p.typ(is, cl)
			return
		}
		if !p.isIdent(p.i) || !p.is(p.i+1, ":") {
			return
		}
		f := p.f.NewDecl(owner, code.KindField, p.name(p.i), p.span(p.i), code.Span{Start: fstart, End: p.span(p.i).End})
		p.fi.decls[f] = &declInfo{sc: is, visible: -1}
		p.i += 2
		ts := p.i
		p.fi.decls[f].typeRef = p.typ(is, cl)
		f.Type = p.typeText(ts, p.i)
		p.finish(f)
	})
	p.i = min(cl+1, end)
}

func (p *parser) enumItem(sc *scope, end, offset int) {
	d, is := p.typeHeader(sc, end, offset, code.KindEnum)
	if d == nil {
		return
	}
	p.where(is, end)
	if p.is(p.i, "{") {
		cl := p.closeOf(p.i, end)
		d.Body = code.Span{Start: p.startOf(p.i), End: p.endOf(cl + 1)}
		p.i++
		p.list(cl, func() { p.variant(is, d, cl) })
		p.i = min(cl+1, end)
	}
	p.finish(d)
}

// variant parses one enum variant up to its comma.
func (p *parser) variant(is *scope, enum *code.Decl, cl int) {
	start := p.startOf(p.i)
	p.attrs(cl)
	p.visibility(cl)
	if !p.isIdent(p.i) {
		return
	}
	v := p.f.NewDecl(enum, code.KindEnumMember, p.name(p.i), p.span(p.i), code.Span{Start: start, End: p.span(p.i).End})
	p.fi.decls[v] = &declInfo{sc: is, visible: -1}
	p.i++
	p.structBody(is, v, cl)
	if p.is(p.i, "=") {
		p.i++
		p.expr(is, cl, stopComma, false, false)
	}
	p.finish(v)
}

func (p *parser) traitItem(sc *scope, end, offset int) {
	d, is := p.typeHeader(sc, end, offset, code.KindInterface)
	if d == nil {
		return
	}
	if p.is(p.i, ":") {
		p.i++
		for p.i < end && !p.is(p.i, "{") && !p.is(p.i, "where") && !p.is(p.i, ";") {
			start := p.i
			if p.isLifetime(p.i) || p.is(p.i, "+") || p.is(p.i, "?") {
				p.i++
				continue
			}
			p.typ(is, end)
			if base := p.typeText(start, p.i); base != "" {
				d.Bases = append(d.Bases, stripGenerics(base))
			}
			if p.i == start {
				p.i++
			}
		}
	}
	p.where(is, end)
	if p.is(p.i, "{") {
		cl := p.closeOf(p.i, end)
		d.Body = code.Span{Start: p.startOf(p.i), End: p.endOf(cl + 1)}
		p.i++
		p.items(is, cl, &implInfo{})
		p.i = min(cl+1, end)
	} else if p.is(p.i, "=") {
		p.i++
		p.bounds(is, end)
	}
	if p.is(p.i, ";") {
		p.i++
	}
	p.finish(d)
}

// stripGenerics drops the generic arguments of a path as written.
func stripGenerics(s string) string {
	if i := strings.IndexByte(s, '<'); i > 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(strings.TrimSpace(s), "::")
}

func (p *parser) implItem(sc *scope, end int) {
	p.i++
	is := newScope(scopeItem, sc, sc.owner)
	is.selfImpl = true
	if p.is(p.i, "<") {
		p.generics(is, nil, end)
	}
	if p.is(p.i, "const") {
		p.i++
	}
	if p.is(p.i, "!") {
		p.i++
	}
	impl := &implInfo{}
	start := p.i
	first := p.typ(is, end)
	if p.is(p.i, "for") {
		impl.trait = first
		impl.traitAs = stripGenerics(p.typeText(start, p.i))
		p.i++
		first = p.typ(is, end)
	}
	impl.self = first
	is.selfType = first
	p.where(is, end)
	if p.is(p.i, "{") {
		cl := p.closeOf(p.i, end)
		p.i++
		p.items(is, cl, impl)
		p.i = min(cl+1, end)
	}
	p.fi.impls = append(p.fi.impls, impl)
}

// typeItem parses `type Name<T> = Type;`, including associated types.
func (p *parser) typeItem(sc *scope, end, offset int, impl *implInfo) {
	p.i++
	if !p.isIdent(p.i) {
		return
	}
	d := p.newDecl(sc, code.KindTypeAlias, p.i, offset)
	p.assoc(sc, d, impl)
	p.i++
	is := newScope(scopeItem, sc, d)
	p.generics(is, d, end)
	if p.is(p.i, ":") {
		p.i++
		p.bounds(is, end)
	}
	p.where(is, end)
	if p.is(p.i, "=") {
		p.i++
		ts := p.i
		p.fi.decls[d].typeRef = p.typ(is, end)
		d.Type = p.typeText(ts, p.i)
	}
	p.where(is, end)
	if p.is(p.i, ";") {
		p.i++
	}
	p.finish(d)
}

// constItem parses const and static items, including associated consts.
func (p *parser) constItem(sc *scope, end, offset int, kind code.DeclKind, impl *implInfo) {
	p.i++
	if p.is(p.i, "mut") {
		p.i++
	}
	var d *code.Decl
	if p.isIdent(p.i) && p.text(p.i) != "_" {
		d = p.newDecl(sc, kind, p.i, offset)
		p.assoc(sc, d, impl)
	}
	p.i++
	if p.is(p.i, ":") {
		p.i++
		ts := p.i
		head := p.typ(sc, end)
		if d != nil {
			p.fi.decls[d].typeRef = head
			d.Type = p.typeText(ts, p.i)
		}
	}
	if p.is(p.i, "=") {
		p.i++
		p.expr(sc, end, stopSemi, false, false)
	}
	if p.is(p.i, ";") {
		p.i++
	}
	p.finish(d)
}

func (p *parser) modItem(sc *scope, end, offset int) {
	p.i++
	if !p.isIdent(p.i) {
		return
	}
	if !p.is(p.i+1, "{") {
		r := p.newRef(sc, p.i, refModFile)
		r.Import = true
		p.fi.links = append(p.fi.links, modLink{ref: r, parent: sc.mod, inline: slices.Clone(p.inline), path: p.pathAttr})
		p.i++
		if p.is(p.i, ";") {
			p.i++
		}
		return
	}
	d := p.newDecl(sc, code.KindNamespace, p.i, offset)
	sc.addItem(d)
	p.i++
	ms := newScope(scopeModule, sc, d)
	ms.mod = d
	p.fi.modScop[d] = ms
	p.fi.modUp[d] = sc.mod
	cl := p.closeOf(p.i, end)
	d.Body = code.Span{Start: p.startOf(p.i), End: p.endOf(cl + 1)}
	p.i++
	p.inline = append(p.inline, d.Name)
	p.items(ms, cl, nil)
	p.inline = p.inline[:len(p.inline)-1]
	p.i = min(cl+1, end)
	p.finish(d)
}

func (p *parser) externCrate(sc *scope, end int) {
	p.i += 2
	if !p.isIdent(p.i) && !p.is(p.i, "self") {
		return
	}
	var r *code.Ref
	if p.isIdent(p.i) {
		r = p.newRef(sc, p.i, refCrate)
		r.Import = true
	}
	p.i++
	name := ""
	if r != nil {
		name = r.Name
	}
	if p.is(p.i, "as") && p.isIdent(p.i+1) {
		p.i++
		name = ""
		if p.text(p.i) != "_" {
			d := p.newDecl(sc, code.KindImport, p.i, p.startOf(p.i))
			p.fi.decls[d].alias = r
			sc.addItem(d)
		}
		p.i++
	}
	sc.addImport(name, r)
	if p.is(p.i, ";") {
		p.i++
	}
}

// useTree parses a use tree at p.i. prev is the reference to the last
// segment of the prefix path. If the path so far is only crate, self, super
// or Self, kw and supers hold that prefix.
func (p *parser) useTree(sc *scope, end int, prev *code.Ref, kw string, supers int, nested bool) {
	if p.is(p.i, "::") {
		p.i++
	}
	for p.i < end {
		switch {
		case p.is(p.i, "{"):
			cl := p.closeOf(p.i, end)
			p.i++
			for p.i < cl {
				start := p.i
				p.useTree(sc, cl, prev, kw, supers, true)
				if p.is(p.i, ",") {
					p.i++
				}
				if p.i == start {
					p.i++
				}
			}
			p.i = min(cl+1, end)
			return
		case p.is(p.i, "*"):
			p.i++
			sc.globs = append(sc.globs, glob{ref: prev, prefix: kw, supers: supers})
			return
		case p.is(p.i, "self") && nested && !p.is(p.i+1, "::"):
			// `use a::{self}` imports a itself.
			p.i++
			name := ""
			if prev != nil {
				name = prev.Name
			}
			name = p.useAlias(sc, prev, name)
			sc.addImport(name, prev)
			return
		case p.is(p.i, "crate") || p.is(p.i, "self") || p.is(p.i, "super") || p.is(p.i, "Self"):
			if p.is(p.i, "super") {
				supers++
				if kw == "" {
					kw = "super"
				}
			} else {
				kw = p.cur()
			}
			p.i++
		case p.isIdent(p.i):
			kind := refSeg
			switch {
			case prev == nil && kw != "":
				kind = refPrefix
			case prev == nil:
				kind = refUse
			}
			r := p.newRef(sc, p.i, kind)
			r.Import = true
			ri := p.fi.refs[r]
			ri.prev, ri.prefix, ri.supers = prev, kw, supers
			prev = r
			p.i++
			if !p.is(p.i, "::") {
				name := p.useAlias(sc, r, r.Name)
				sc.addImport(name, r)
				return
			}
		default:
			return
		}
		if !p.is(p.i, "::") {
			return
		}
		p.i++
	}
}

// useAlias parses `as name` after an imported path and declares the alias.
// It returns the name the import binds.
func (p *parser) useAlias(sc *scope, target *code.Ref, name string) string {
	if !p.is(p.i, "as") {
		return name
	}
	p.i++
	if !p.isIdent(p.i) {
		return ""
	}
	// The alias declaration resolves the name, so the import itself doesn't
	// bind anything.
	if p.text(p.i) != "_" {
		d := p.newDecl(sc, code.KindImport, p.i, p.startOf(p.i))
		p.fi.decls[d].alias = target
		sc.addItem(d)
	}
	p.i++
	return ""
}

// assoc registers d as an item of sc, or as an associated item of an impl
// or trait. Those can only be reached by a path through the type.
func (p *parser) assoc(sc *scope, d *code.Decl, impl *implInfo) {
	switch {
	case impl == nil:
		sc.addItem(d)
	case impl.self != nil && sc.selfImpl:
		d.Receiver = impl.self.Name
		impl.members = append(impl.members, d)
	}
}

// macroRules declares a macro_rules! macro and skips its body.
func (p *parser) macroRules(sc *scope, end, offset int) {
	p.i += 2
	if !p.isIdent(p.i) {
		return
	}
	d := p.newDecl(sc, code.KindMacro, p.i, offset)
	sc.addItem(d)
	p.i++
	if p.is(p.i, "{") || p.is(p.i, "(") || p.is(p.i, "[") {
		cl := p.closeOf(p.i, end)
		d.Body = code.Span{Start: p.startOf(p.i), End: p.endOf(cl + 1)}
		p.i = min(cl+1, end)
	}
	if p.is(p.i, ";") {
		p.i++
	}
	p.finish(d)
}
