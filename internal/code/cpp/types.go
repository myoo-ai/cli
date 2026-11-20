package cpp

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

var builtinTypes = code.StringSet(
	"void", "char", "short", "int", "long", "float", "double", "signed", "unsigned",
	"bool", "_Bool", "wchar_t", "char8_t", "char16_t", "char32_t", "_Complex",
	"_Imaginary", "auto",
)

// specKeywords are declaration specifiers other than types and cv
// qualifiers. The identifiers in here are common compiler extensions.
var specKeywords = code.StringSet(
	"static", "extern", "inline", "virtual", "explicit", "constexpr", "consteval",
	"constinit", "friend", "mutable", "register", "thread_local", "_Thread_local",
	"typedef", "_Noreturn", "__inline", "__inline__", "__forceinline", "__extension__",
)

var cvKeywords = code.StringSet("const", "volatile", "restrict", "__restrict", "__restrict__")

// attrWords take a parenthesized argument, and we skip them like attributes.
var attrWords = code.StringSet("__attribute__", "__declspec", "alignas", "_Alignas", "__asm__", "__asm")

// declKeywords start a declaration statement.
var declKeywords = code.StringSet(
	"void", "char", "short", "int", "long", "float", "double", "signed", "unsigned",
	"bool", "_Bool", "wchar_t", "char8_t", "char16_t", "char32_t", "_Complex", "auto",
	"static", "extern", "inline", "constexpr", "consteval", "constinit", "mutable",
	"register", "thread_local", "_Thread_local", "typedef", "const", "volatile",
	"struct", "class", "union", "enum", "typename", "decltype", "typeof", "using",
	"template", "static_assert", "_Static_assert", "_Atomic", "namespace",
)

// macroLike reports an all-caps identifier like EXPORT_API.
func macroLike(s string) bool {
	letter := false
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			letter = true
		case c == '_' || c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return letter && len(s) > 1
}

// newline reports whether there's a line break between token i and the one
// before it.
func (p *parser) newline(i int) bool {
	if i <= 0 || i >= len(p.t) {
		return true
	}
	return strings.Contains(p.src[p.t[i-1].e:p.t[i].s], "\n")
}

// compact renders tokens [from, to) without layout. We only put a space
// between two word tokens.
func (p *parser) compact(from, to int) string {
	var b strings.Builder
	prevWord := false
	for i := from; i < to && i < len(p.t); i++ {
		k := p.t[i].kind
		word := k == code.TokIdent || k == code.TokKeyword || k == code.TokNumber
		if word && prevWord {
			b.WriteByte(' ')
		}
		b.WriteString(p.src[p.t[i].s:p.t[i].e])
		prevWord = word
	}
	return b.String()
}

// typeName records references for the name starting at i, which can be
// qualified and templated, and returns the index after it.
func (p *parser) typeName(sc *scope, i, end int) int {
	from := i
	if p.is(i, "::") {
		i++
	}
	for i < end {
		if p.is(i, "template") {
			i++
		}
		if !p.ident(i) {
			break
		}
		kind, prev := p.qualifier(i, from)
		p.newRef(sc, i, kind, prev)
		i++
		if p.is(i, "<") {
			if e := p.templateArgsEnd(i, end, false); e >= 0 {
				p.scanType(sc, i+1, e)
				i = e + 1
			}
		}
		if !p.is(i, "::") || p.is(i+1, "*") || p.is(i+1, "~") || p.is(i+1, "operator") {
			break
		}
		i++
	}
	return i
}

// skipTypeName returns the index after the qualified name at i, or -1. It
// doesn't record anything and is strict about template arguments.
func (p *parser) skipTypeName(i, end int) int {
	if p.is(i, "::") {
		i++
	}
	start := i
	for i < end && p.ident(i) {
		i++
		if p.is(i, "<") {
			e := p.templateArgsEnd(i, end, true)
			if e < 0 {
				break
			}
			i = e + 1
		}
		if !p.is(i, "::") || !p.ident(i+1) {
			break
		}
		i++
	}
	if i == start {
		return -1
	}
	return i
}

// scanType records references for a type-id or similar region [from, to).
func (p *parser) scanType(sc *scope, from, to int) {
	to = min(to, len(p.t))
	for i := from; i < to; i++ {
		switch {
		case p.ident(i):
			if literalNames[p.text(i)] {
				continue
			}
			kind, prev := p.qualifier(i, from)
			p.newRef(sc, i, kind, prev)
			if p.is(i+1, "<") {
				if e := p.templateArgsEnd(i+1, to, false); e >= 0 {
					p.scanType(sc, i+2, e)
					i = e
				}
			}
		case p.is(i, "(") || p.is(i, "[") || p.is(i, "{"):
			m := p.closeOf(i, to)
			p.scanExpr(sc, i+1, m)
			i = m
		}
	}
}

// ctorAhead reports whether a constructor or destructor declarator starts
// at i: `Name(` inside class Name, `A::A(`, `A::~A(` or `~A(`.
func (p *parser) ctorAhead(sc *scope, i, end int) bool {
	if p.is(i, "~") {
		return true
	}
	if sc.kind == scopeClass && sc.decl != nil && p.ident(i) && p.text(i) == sc.decl.Name && p.is(i+1, "(") {
		return true
	}
	var comps []string
	if p.is(i, "::") {
		i++
	}
	for i < end && p.ident(i) {
		comps = append(comps, p.text(i))
		i++
		if p.is(i, "<") {
			e := p.templateArgsEnd(i, end, false)
			if e < 0 {
				return false
			}
			i = e + 1
		}
		if !p.is(i, "::") {
			break
		}
		i++
		if p.is(i, "~") || p.is(i, "operator") {
			return true
		}
	}
	n := len(comps)
	return n >= 2 && comps[n-1] == comps[n-2] && p.is(i, "(")
}

// skipAttributes skips [[...]], __attribute__((...)) and similar at p.i.
func (p *parser) skipAttributes(end int) {
	for p.i < end {
		switch {
		case p.is(p.i, "[") && p.is(p.i+1, "["):
			p.i = p.closeOf(p.i, end) + 1
		case attrWords[p.text(p.i)] && p.is(p.i+1, "("):
			p.i = p.closeOf(p.i+1, end) + 1
		default:
			return
		}
	}
}
