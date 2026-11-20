package clike

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// tok is a token as the parser sees it. Comments and C# preprocessor lines go
// into File.Tokens but don't reach the parser.
type tok struct {
	kind code.TokenKind
	s, e int
	text string
	// Index into File.Tokens, so the parser can reclassify contextual
	// keywords. It's -1 for tokens lexed from interpolation holes.
	ft int
	// expression parts of a C# interpolated string
	holes []code.Span
	// set for a C# verbatim identifier like @class, whose name starts at s+1
	at bool
}

var javaKeywords = code.StringSet(
	"abstract", "assert", "boolean", "break", "byte", "case", "catch", "char", "class", "const",
	"continue", "default", "do", "double", "else", "enum", "extends", "final", "finally", "float",
	"for", "goto", "if", "implements", "import", "instanceof", "int", "interface", "long", "native",
	"new", "package", "private", "protected", "public", "return", "short", "static", "strictfp",
	"super", "switch", "synchronized", "this", "throw", "throws", "transient", "try", "void",
	"volatile", "while", "true", "false", "null",
)

var csKeywords = code.StringSet(
	"abstract", "as", "base", "bool", "break", "byte", "case", "catch", "char", "checked", "class",
	"const", "continue", "decimal", "default", "delegate", "do", "double", "else", "enum", "event",
	"explicit", "extern", "false", "finally", "fixed", "float", "for", "foreach", "goto", "if",
	"implicit", "in", "int", "interface", "internal", "is", "lock", "long", "namespace", "new",
	"null", "object", "operator", "out", "override", "params", "private", "protected", "public",
	"readonly", "ref", "return", "sbyte", "sealed", "short", "sizeof", "stackalloc", "static",
	"string", "struct", "switch", "this", "throw", "true", "try", "typeof", "uint", "ulong",
	"unchecked", "unsafe", "ushort", "using", "virtual", "void", "volatile", "while",
)

// Multi-character operators. We always lex '>' as a single token so the end
// of nested type argument lists can't merge into a shift operator.
var punct3 = code.StringSet("...", "<<=", "??=")
var punct2 = code.StringSet(
	"->", "=>", "::", "++", "--", "&&", "||", "==", "!=", "<=", ">=", "+=", "-=", "*=", "/=",
	"%=", "&=", "|=", "^=", "<<", "??", "?.", "..",
)

type lexer struct {
	src string
	cs  bool
	i   int
	end int // scanning has to stay below end
}

// lex tokenizes f, fills f.Tokens and f.Comments, and returns the tokens the
// parser should see.
func lex(f *code.File, src string, cs bool) []tok {
	lx := &lexer{src: src, cs: cs, end: len(src)}
	if strings.HasPrefix(src, "\uFEFF") {
		lx.i = 3
	}
	toks := make([]tok, 0, len(src)/5)
	bol := true
	ppEnd := -1
	for {
		for lx.i < len(src) {
			c := src[lx.i]
			if c == '\n' {
				bol = true
			} else if c != ' ' && c != '\t' && c != '\r' && c != '\f' && c != '\v' {
				break
			}
			lx.i++
		}
		if ppEnd >= 0 && lx.i >= ppEnd {
			ppEnd = -1
			lx.end = len(src)
		}
		if lx.i >= len(src) {
			break
		}
		if cs && bol && ppEnd < 0 && src[lx.i] == '#' {
			ppEnd = lx.i
			for ppEnd < len(src) && src[ppEnd] != '\n' {
				ppEnd++
			}
			lx.end = ppEnd
			f.Tokens = append(f.Tokens, code.Token{Kind: code.TokPunct, Span: code.Span{Start: lx.i, End: lx.i + 1}})
			lx.i++
			for lx.i < ppEnd && (src[lx.i] == ' ' || src[lx.i] == '\t') {
				lx.i++
			}
			if s := lx.i; s < ppEnd && isASCIILetter(src[s]) {
				for lx.i < ppEnd && isASCIILetter(src[lx.i]) {
					lx.i++
				}
				f.Tokens = append(f.Tokens, code.Token{Kind: code.TokKeyword, Span: code.Span{Start: s, End: lx.i}})
			}
			bol = false
			continue
		}
		bol = false
		t, ck := lx.next()
		if t.kind == 0 {
			break
		}
		t.ft = len(f.Tokens)
		f.Tokens = append(f.Tokens, code.Token{Kind: t.kind, Span: code.Span{Start: t.s, End: t.e}})
		switch {
		case t.kind == code.TokComment:
			f.Comments = append(f.Comments, code.Comment{Kind: ck, Span: code.Span{Start: t.s, End: t.e}})
		case ppEnd < 0:
			toks = append(toks, t)
		}
	}
	return toks
}

// lexRange tokenizes the C# interpolation hole src[start:end] without
// recording anything in a file. Comments are dropped.
func lexRange(src string, start, end int) []tok {
	lx := &lexer{src: src, cs: true, i: start, end: end}
	var toks []tok
	for {
		lx.skipSpace()
		t, _ := lx.next()
		if t.kind == 0 {
			return toks
		}
		if t.kind != code.TokComment {
			t.ft = -1
			toks = append(toks, t)
		}
	}
}

func (lx *lexer) skipSpace() {
	for lx.i < lx.end {
		switch lx.src[lx.i] {
		case ' ', '\t', '\r', '\n', '\f', '\v':
			lx.i++
		default:
			return
		}
	}
}

func (lx *lexer) at(i int) byte {
	if i < lx.end {
		return lx.src[i]
	}
	return 0
}

// next scans a token starting at lx.i, which can't be whitespace. It returns
// a zero tok at the end of input.
func (lx *lexer) next() (tok, code.CommentKind) {
	start := lx.i
	if start >= lx.end {
		return tok{}, 0
	}
	c := lx.src[start]
	mk := func(kind code.TokenKind) tok {
		return tok{kind: kind, s: start, e: lx.i, text: lx.src[start:lx.i]}
	}
	switch {
	case c == '/' && lx.at(start+1) == '/':
		for lx.i < lx.end && lx.src[lx.i] != '\n' {
			lx.i++
		}
		if lx.i > start && lx.src[lx.i-1] == '\r' {
			lx.i--
		}
		ck := code.LineComment
		if lx.at(start+2) == '/' && lx.at(start+3) != '/' {
			ck = code.DocComment
		}
		return mk(code.TokComment), ck
	case c == '/' && lx.at(start+1) == '*':
		ck := code.BlockComment
		if lx.at(start+2) == '*' && lx.at(start+3) != '/' {
			ck = code.DocComment
		}
		if j := strings.Index(lx.src[start+2:lx.end], "*/"); j >= 0 {
			lx.i = start + 2 + j + 2
		} else {
			lx.i = lx.end
		}
		return mk(code.TokComment), ck
	case c == '"':
		if lx.cs {
			t := mk(code.TokString)
			lx.csString(&t)
			return t, 0
		}
		lx.javaString()
		return mk(code.TokString), 0
	case c == '\'':
		lx.charLiteral()
		return mk(code.TokString), 0
	case lx.cs && (c == '@' || c == '$'):
		return lx.prefixedToken(), 0
	case isDigit(c) || (c == '.' && isDigit(lx.at(start+1))):
		lx.scanNumber()
		return mk(code.TokNumber), 0
	case lx.identStart(start):
		lx.scanIdent()
		t := mk(code.TokIdent)
		if (lx.cs && csKeywords[t.text]) || (!lx.cs && javaKeywords[t.text]) {
			t.kind = code.TokKeyword
		}
		return t, 0
	}
	if c >= utf8.RuneSelf {
		_, size := utf8.DecodeRuneInString(lx.src[start:lx.end])
		lx.i += size
		return mk(code.TokPunct), 0
	}
	if start+3 <= lx.end && punct3[lx.src[start:start+3]] {
		lx.i += 3
	} else if start+2 <= lx.end && punct2[lx.src[start:start+2]] && !(c == '?' && lx.at(start+1) == '.' && isDigit(lx.at(start+2))) {
		lx.i += 2
	} else {
		lx.i++
	}
	return mk(code.TokPunct), 0
}

func (lx *lexer) prefixedToken() tok {
	start := lx.i
	j := start
	for j < lx.end && (lx.src[j] == '@' || lx.src[j] == '$') {
		j++
	}
	t := tok{kind: code.TokString, s: start}
	if lx.at(j) == '"' {
		lx.csString(&t)
		return t
	}
	t.kind = code.TokPunct
	lx.i = start + 1
	if lx.src[start] == '@' && j == start+1 && j < lx.end && lx.identStart(j) {
		lx.i = j
		lx.scanIdent()
		t.kind, t.at = code.TokIdent, true
	}
	t.e, t.text = lx.i, lx.src[start:lx.i]
	return t
}

func (lx *lexer) charLiteral() {
	lx.i++
	for lx.i < lx.end {
		ch := lx.src[lx.i]
		if ch == '\\' {
			lx.i += 2
			continue
		}
		if ch == '\n' {
			break
		}
		lx.i++
		if ch == '\'' {
			break
		}
	}
	lx.i = min(lx.i, lx.end)
}

func (lx *lexer) javaString() {
	start := lx.i
	if strings.HasPrefix(lx.src[start:lx.end], `"""`) {
		lx.i += 3
		for lx.i < lx.end {
			if lx.src[lx.i] == '\\' {
				lx.i += 2
				continue
			}
			if strings.HasPrefix(lx.src[lx.i:lx.end], `"""`) {
				lx.i += 3
				return
			}
			lx.i++
		}
		lx.i = lx.end
		return
	}
	lx.i++
	for lx.i < lx.end {
		switch lx.src[lx.i] {
		case '\\':
			lx.i += 2
			continue
		case '\n':
			return
		case '"':
			lx.i++
			return
		}
		lx.i++
	}
	lx.i = lx.end
}

// csString scans a C# string literal with any mix of '$' and '@' prefixes,
// raw strings included, and records the interpolation holes in t.
func (lx *lexer) csString(t *tok) {
	dollars, verbatim := 0, false
	for lx.i < lx.end && (lx.src[lx.i] == '$' || lx.src[lx.i] == '@') {
		if lx.src[lx.i] == '$' {
			dollars++
		} else {
			verbatim = true
		}
		lx.i++
	}
	quotes := 0
	for lx.i+quotes < lx.end && lx.src[lx.i+quotes] == '"' {
		quotes++
	}
	defer func() {
		lx.i = min(lx.i, lx.end)
		t.e = lx.i
		t.text = lx.src[t.s:t.e]
	}()
	switch {
	case !verbatim && quotes >= 3:
		lx.rawString(t, quotes, dollars)
	case !verbatim && quotes == 2:
		lx.i += 2
	default:
		lx.quotedString(t, verbatim, dollars > 0)
	}
}

func (lx *lexer) rawString(t *tok, quotes, dollars int) {
	lx.i += quotes
	for lx.i < lx.end {
		ch := lx.src[lx.i]
		if ch == '"' {
			run := lx.run('"')
			lx.i += run
			if run >= quotes {
				return
			}
			continue
		}
		if dollars > 0 && ch == '{' {
			run := lx.run('{')
			lx.i += run
			if run >= dollars {
				lx.hole(t, dollars)
			}
			continue
		}
		lx.i++
	}
}

func (lx *lexer) quotedString(t *tok, verbatim, interpolated bool) {
	lx.i++
	for lx.i < lx.end {
		ch := lx.src[lx.i]
		switch {
		case verbatim && ch == '"' && lx.at(lx.i+1) == '"':
			lx.i += 2
		case !verbatim && ch == '\\':
			lx.i += 2
		case !verbatim && ch == '\n':
			return
		case ch == '"':
			lx.i++
			return
		case interpolated && ch == '{' && lx.at(lx.i+1) == '{', interpolated && ch == '}' && lx.at(lx.i+1) == '}':
			lx.i += 2
		case interpolated && ch == '{':
			lx.i++
			lx.hole(t, 1)
		default:
			lx.i++
		}
	}
}

func (lx *lexer) run(c byte) int {
	n := 0
	for lx.i+n < lx.end && lx.src[lx.i+n] == c {
		n++
	}
	return n
}

// hole scans an interpolation hole whose expression starts at lx.i. It
// records the expression span, minus alignment and format, and leaves lx.i
// after the closing braces.
func (lx *lexer) hole(t *tok, closing int) {
	sub := &lexer{src: lx.src, cs: true, i: lx.i, end: lx.end}
	start, exprEnd, depth := lx.i, -1, 0
	for {
		sub.skipSpace()
		pt, _ := sub.next()
		if pt.kind == 0 {
			lx.i = lx.end
			break
		}
		if pt.kind != code.TokPunct {
			continue
		}
		switch pt.text {
		case "(", "[", "{":
			depth++
			continue
		case ")", "]":
			depth = max(depth-1, 0)
			continue
		case ",", ":":
			if depth > 0 || exprEnd >= 0 {
				continue
			}
			exprEnd = pt.s
			if pt.text == ":" {
				for sub.i < sub.end && sub.src[sub.i] != '}' {
					sub.i++
				}
			}
			continue
		case "}":
			if depth > 0 {
				depth--
				continue
			}
		default:
			continue
		}
		if exprEnd < 0 {
			exprEnd = pt.s
		}
		lx.i = pt.s
		for n := 0; n < closing && lx.i < lx.end && lx.src[lx.i] == '}'; n++ {
			lx.i++
		}
		break
	}
	if exprEnd < 0 {
		exprEnd = lx.i
	}
	if exprEnd > start {
		t.holes = append(t.holes, code.Span{Start: start, End: exprEnd})
	}
}

func (lx *lexer) scanNumber() {
	hex := lx.src[lx.i] == '0' && (lx.at(lx.i+1)|0x20) == 'x'
	dot := false
	for lx.i < lx.end {
		c := lx.src[lx.i]
		switch {
		case isDigit(c) || isASCIILetter(c) || c == '_':
			lx.i++
			if (!hex && (c|0x20) == 'e') || (hex && (c|0x20) == 'p') {
				if n := lx.at(lx.i); n == '+' || n == '-' {
					lx.i++
				}
			}
		case c == '.' && !dot && !hex && isDigit(lx.at(lx.i+1)):
			dot = true
			lx.i++
		default:
			return
		}
	}
}

func (lx *lexer) identStart(i int) bool {
	c := lx.src[i]
	if isASCIILetter(c) || c == '_' || (c == '$' && !lx.cs) {
		return true
	}
	if c == '\\' {
		return !lx.cs && lx.unicodeEscape(i) > 0
	}
	if c < utf8.RuneSelf {
		return false
	}
	r, _ := utf8.DecodeRuneInString(lx.src[i:lx.end])
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

// unicodeEscape returns the length of a Java \uXXXX escape at i, or 0.
func (lx *lexer) unicodeEscape(i int) int {
	j := i + 1
	if lx.at(j) != 'u' {
		return 0
	}
	for lx.at(j) == 'u' {
		j++
	}
	for k := range 4 {
		if !isHex(lx.at(j + k)) {
			return 0
		}
	}
	return j + 4 - i
}

func (lx *lexer) scanIdent() {
	for lx.i < lx.end {
		c := lx.src[lx.i]
		switch {
		case isASCIILetter(c) || isDigit(c) || c == '_' || (c == '$' && !lx.cs):
			lx.i++
		case c == '\\' && !lx.cs:
			n := lx.unicodeEscape(lx.i)
			if n == 0 {
				return
			}
			lx.i += n
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(lx.src[lx.i:lx.end])
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nl, unicode.Pc) {
				return
			}
			lx.i += size
		default:
			return
		}
	}
}

func isDigit(c byte) bool       { return c >= '0' && c <= '9' }
func isASCIILetter(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
func isHex(c byte) bool         { return isDigit(c) || ((c|0x20) >= 'a' && (c|0x20) <= 'f') }

// decodeEscapes resolves Java \uXXXX escapes in an identifier.
func decodeEscapes(s string) string {
	if !strings.Contains(s, `\u`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == 'u' {
			j := i + 1
			for j < len(s) && s[j] == 'u' {
				j++
			}
			if j+4 <= len(s) {
				if r, ok := hexRune(s[j : j+4]); ok {
					b.WriteRune(r)
					i = j + 4
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func hexRune(s string) (rune, bool) {
	var r rune
	for _, h := range s {
		switch {
		case h >= '0' && h <= '9':
			r = r*16 + h - '0'
		case h|0x20 >= 'a' && h|0x20 <= 'f':
			r = r*16 + (h | 0x20) - 'a' + 10
		default:
			return 0, false
		}
	}
	return r, true
}
