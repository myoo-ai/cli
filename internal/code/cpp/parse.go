package cpp

import (
	"github.com/myoo-ai/cli/internal/code"
)

type scopeKind uint8

const (
	scopeFile scopeKind = iota
	scopeNamespace
	scopeClass
	scopeFunc
	scopeBlock
	scopeMacro
)

// scope is a lexical scope the parser records for the resolver.
type scope struct {
	kind   scopeKind
	parent *scope
	decl   *code.Decl // nil for blocks
	// The innermost declaration with a body. Declarations made in the scope
	// get it as Parent, references as Scope.
	owner *code.Decl
	nsKey string // namespace path of file and namespace scopes, "" for the global one
	names map[string][]*code.Decl
	// Lambda parameters that got no declaration. They shadow outer names but
	// don't resolve to anything.
	hidden     map[string]bool
	usingNS    []string             // "a::b" for using namespace a::b
	usingDecls map[string]*code.Ref // b to its reference in using a::b;
	anon       bool                 // anonymous namespace or inside one
	// inline namespaces also publish their members in the parent namespace.
	inline bool
}

func newScope(kind scopeKind, parent *scope, decl, owner *code.Decl) *scope {
	s := &scope{kind: kind, parent: parent, decl: decl, owner: owner, names: map[string][]*code.Decl{}}
	if parent != nil {
		s.nsKey, s.anon = parent.nsKey, parent.anon
	}
	return s
}

func (s *scope) add(d *code.Decl) {
	if d != nil && d.Name != "" {
		s.names[d.Name] = append(s.names[d.Name], d)
	}
}

func (s *scope) hide(name string) {
	if s.hidden == nil {
		s.hidden = map[string]bool{}
	}
	s.hidden[name] = true
}

type refKind uint8

const (
	refPlain   refKind = iota
	refScoped          // after `::`, qualified by prev
	refGlobal          // `::name`
	refMember          // after `.` or `->`, object is prev
	refResult          // after `f().` or `f()->`, object is the result of prev
	refElem            // after `a[i].`, object is an element of prev
	refThis            // `this->name`
	refUnknown         // member of an object the parser can't name
	refInclude         // the path of an #include
)

type refInfo struct {
	sc    *scope
	kind  refKind
	prev  *code.Ref
	arity int  // argument count of calls, -1 otherwise
	arrow bool // member access through ->
}

type declInfo struct {
	sc        *scope // scope the declaration was made in
	def       bool   // has a body, or is a variable that isn't extern
	fileLocal bool   // static at namespace scope or inside an anonymous namespace
	arity     int    // parameter count of functions, -1 if unknown
	variadic  bool
	scoped    bool      // enum class
	nsKey     string    // namespace path of namespace declarations
	body      *scope    // class body or function scope
	init      *code.Ref // callee or type of the initializer, to deduce auto
	rangeOf   *code.Ref // range of a range-for variable, to deduce auto
}

type include struct {
	ref    *code.Ref
	path   string
	system bool
}

type fileInfo struct {
	f        *code.File
	root     *scope
	refs     map[*code.Ref]*refInfo
	decls    map[*code.Decl]*declInfo
	includes []include
	macros   []*code.Decl
	nsScopes []*scope // file scope and named namespace scopes
}

// parser walks the code tokens of a file, with comments, directives and
// inactive #else branches removed.
type parser struct {
	f     *code.File
	src   string // token texts are substrings of it
	cpp   bool
	t     []tok
	match []int // index of the matching bracket, -1 if unmatched
	i     int
	fi    *fileInfo
	// The references made for each token index, used to chain qualified names
	// and member accesses.
	refAt map[int]*code.Ref
	tmpl  *tmplInfo
}

func parseFile(f *code.File) *fileInfo {
	cpp := f.Lang == code.CPP
	l := lex(f.Src, cpp)
	f.Tokens = make([]code.Token, len(l.toks))
	for i, t := range l.toks {
		f.Tokens[i] = code.Token{Kind: t.kind, Span: code.Span{Start: t.s, End: t.e}}
	}
	f.Comments = l.comments
	mod := f.SetModule(moduleName(f.Path))
	fi := &fileInfo{f: f, refs: map[*code.Ref]*refInfo{}, decls: map[*code.Decl]*declInfo{}}
	fi.root = newScope(scopeFile, nil, mod, mod)
	fi.nsScopes = append(fi.nsScopes, fi.root)
	p := &parser{f: f, src: string(f.Src), cpp: cpp, fi: fi, refAt: map[int]*code.Ref{}}
	p.t = activeTokens(l)
	p.matchBrackets()
	p.directives(l)
	p.items(fi.root, len(p.t))
	return fi
}

func (p *parser) text(i int) string {
	if i < 0 || i >= len(p.t) {
		return ""
	}
	return p.src[p.t[i].s:p.t[i].e]
}

func (p *parser) is(i int, s string) bool {
	return i >= 0 && i < len(p.t) && p.src[p.t[i].s:p.t[i].e] == s
}

func (p *parser) kind(i int) code.TokenKind {
	if i < 0 || i >= len(p.t) {
		return 0
	}
	return p.t[i].kind
}

func (p *parser) ident(i int) bool { return p.kind(i) == code.TokIdent }

func (p *parser) span(i int) code.Span { return code.Span{Start: p.t[i].s, End: p.t[i].e} }

// spanOf covers the tokens from..to inclusive, clamped to the file's tokens.
func (p *parser) spanOf(from, to int) code.Span {
	n := len(p.t)
	if n == 0 {
		return code.Span{}
	}
	from, to = min(max(from, 0), n-1), min(to, n-1)
	if from > to {
		to = from
	}
	return code.Span{Start: p.t[from].s, End: p.t[to].e}
}

// closeOf returns the index of the bracket matching the opener at i,
// clamped to end.
func (p *parser) closeOf(i, end int) int {
	if i < 0 || i >= len(p.match) {
		return end
	}
	if m := p.match[i]; m >= 0 && m < end {
		return m
	}
	return end
}

// matchBrackets pairs (), [] and {}. A closing brace also closes any bracket
// still open inside its block, but stray ) and ] stay unmatched.
func (p *parser) matchBrackets() {
	p.match = make([]int, len(p.t))
	var stack []int
	for i, t := range p.t {
		p.match[i] = -1
		if t.kind != code.TokPunct || t.e-t.s != 1 {
			continue
		}
		switch c := p.src[t.s]; c {
		case '(', '[', '{':
			stack = append(stack, i)
		case ')', ']':
			open := byte('(')
			if c == ']' {
				open = '['
			}
			if n := len(stack); n > 0 && p.src[p.t[stack[n-1]].s] == open {
				p.match[stack[n-1]], p.match[i] = i, stack[n-1]
				stack = stack[:n-1]
			}
		case '}':
			for n := len(stack); n > 0; n = len(stack) {
				top := stack[n-1]
				stack = stack[:n-1]
				if p.src[p.t[top].s] == '{' {
					p.match[top], p.match[i] = i, top
					break
				}
			}
		}
	}
}

func (p *parser) newRef(sc *scope, i int, kind refKind, prev *code.Ref) *code.Ref {
	r := p.f.NewRef(sc.owner, p.text(i), p.span(i))
	p.fi.refs[r] = &refInfo{sc: sc, kind: kind, prev: prev, arity: -1}
	p.refAt[i] = r
	return r
}

func (p *parser) newDecl(sc *scope, kind code.DeclKind, nameTok int, span code.Span) *code.Decl {
	d := p.f.NewDecl(sc.owner, kind, p.text(nameTok), p.span(nameTok), span)
	p.fi.decls[d] = &declInfo{sc: sc, arity: -1}
	return d
}

func (p *parser) info(d *code.Decl) *declInfo { return p.fi.decls[d] }
