package javascript

import (
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// sub refines code.TokenKind for the parser. It marks template literal
// parts, regex literals and the JSX tokens the lexer recognises.
type sub uint8

const (
	subNone sub = iota
	subRegex
	subTmplFull   // `text` without substitutions
	subTmplHead   // `text${
	subTmplMiddle // }text${
	subTmplTail   // }text`
	subPrivate    // #name
	subJSXOpen    // "<" that starts a JSX tag
	subJSXClose   // "</" that starts a JSX closing tag
	subJSXEnd     // ">" that ends a JSX tag
	subJSXSelf    // "/>" that ends a self-closing JSX tag
	subJSXName    // tag or attribute name, may contain '-'
	subJSXText    // text between JSX tags, trimmed
	subJSXString  // attribute string, no escapes
)

// token is a lexical token. We split template literals into their parts so
// the parser can see the embedded expressions. The public code.File.Tokens
// stream merges them back into a single TokString.
type token struct {
	kind  code.TokenKind
	sub   sub
	nl    bool // a line terminator precedes the token
	start int
	end   int
	pub   int32 // index into the public token stream, -1 inside templates
}

type frameKind uint8

const (
	frameBrace       frameKind = iota // { ... } in ordinary code
	frameTemplate                     // ${ ... } inside a template literal
	frameJSXAttr                      // { ... } in a JSX attribute
	frameJSXChild                     // { ... } between JSX children
	frameJSXTag                       // inside < ... > of a JSX tag
	frameJSXClosing                   // inside </ ... > of a JSX closing tag
	frameJSXChildren                  // between the tags of a JSX element
)

// keywords are the reserved words of ECMAScript. Contextual keywords (let,
// async, type, interface, ...) get lexed as identifiers, and the parser
// promotes them where they act as keywords.
var keywords = code.StringSet(
	"break", "case", "catch", "class", "const",
	"continue", "debugger", "default", "delete", "do",
	"else", "enum", "export", "extends", "false",
	"finally", "for", "function", "if", "import",
	"in", "instanceof", "new", "null", "return",
	"super", "switch", "this", "throw", "true",
	"try", "typeof", "var", "void", "while", "with",
)

// punctuators lists multi-character operators by first byte, longest first.
var punctuators = func() [128][]string {
	var t [128][]string
	for _, p := range []string{
		">>>=", "...", "===", "!==", "**=", "<<=", ">>=", ">>>", "&&=", "||=", "??=",
		"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "+=", "-=",
		"*=", "%=", "&=", "|=", "^=", "<<", ">>", "**",
	} {
		t[p[0]] = append(t[p[0]], p)
	}
	return t
}()

type lexer struct {
	src      []byte
	pos      int
	jsx      bool // JSX is enabled (.js, .jsx, .mjs, .cjs, .tsx)
	ts       bool
	toks     []token // every token, comments included
	comments []code.Comment
	frames   []frameKind
	nl       bool // a line terminator was seen since the last significant token
	last     int  // index of the last significant token in toks, -1 if none
	// parens records for each open '(' if it follows if, while, for or
	// with. ctrlClose is set if the last ')' closed such a condition, because
	// a '/' after it has to start a regex: `if (x) /re/.test(s)`.
	parens    []bool
	ctrlClose bool
}

// lex tokenizes src. It can't fail, because on malformed input we produce
// best-effort tokens up to the end of src.
func lex(src []byte, jsx, ts bool) *lexer {
	l := &lexer{src: src, jsx: jsx, ts: ts, last: -1, toks: make([]token, 0, len(src)/4)}
	if len(src) >= 2 && src[0] == '#' && src[1] == '!' {
		l.lineComment(0, code.LineComment)
	}
	for l.pos < len(l.src) {
		switch l.top() {
		case frameJSXTag, frameJSXClosing:
			l.lexJSXTag()
		case frameJSXChildren:
			l.lexJSXChildren()
		default:
			l.lexCode()
		}
	}
	return l
}

func (l *lexer) top() frameKind {
	if len(l.frames) == 0 {
		return frameBrace
	}
	return l.frames[len(l.frames)-1]
}

func (l *lexer) push(k frameKind) { l.frames = append(l.frames, k) }

func (l *lexer) pop() frameKind {
	if len(l.frames) == 0 {
		return frameBrace
	}
	k := l.frames[len(l.frames)-1]
	l.frames = l.frames[:len(l.frames)-1]
	return k
}

func (l *lexer) emit(kind code.TokenKind, s sub, start, end int) {
	l.toks = append(l.toks, token{kind: kind, sub: s, nl: l.nl, start: start, end: end, pub: -1})
	l.last = len(l.toks) - 1
	l.nl = false
	l.pos = end
}

func (l *lexer) comment(kind code.CommentKind, start, end int) {
	l.toks = append(l.toks, token{kind: code.TokComment, start: start, end: end, pub: -1})
	l.comments = append(l.comments, code.Comment{Kind: kind, Span: code.Span{Start: start, End: end}})
	l.pos = end
}

func (l *lexer) lineComment(start int, kind code.CommentKind) {
	end := start
	for end < len(l.src) && !isLineEnd(l.src, end) {
		end++
	}
	l.comment(kind, start, end)
}

func (l *lexer) blockComment(start int) {
	end := len(l.src)
	for i := start + 2; i+1 < len(l.src); i++ {
		if l.src[i] == '*' && l.src[i+1] == '/' {
			end = i + 2
			break
		}
	}
	for i := start; i < end; i++ {
		if l.src[i] == '\n' || l.src[i] == '\r' {
			l.nl = true
			break
		}
	}
	kind := code.BlockComment
	if end-start > 4 && l.src[start+2] == '*' && l.src[start+3] != '/' {
		kind = code.DocComment
	}
	l.comment(kind, start, end)
}

func (l *lexer) skipSpace() bool {
	start := l.pos
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n' || c == '\r':
			l.nl = true
			l.pos++
		case c == ' ' || c == '\t' || c == '\v' || c == '\f':
			l.pos++
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRune(l.src[l.pos:])
			if r == '\u2028' || r == '\u2029' {
				l.nl = true
			} else if r != '\ufeff' && !unicode.IsSpace(r) {
				return l.pos > start
			}
			l.pos += size
		default:
			return l.pos > start
		}
	}
	return l.pos > start
}

// lexTrivia consumes a single whitespace run or comment before a code or JSX
// token.
func (l *lexer) lexTrivia(doc bool) bool {
	if l.skipSpace() || l.pos >= len(l.src) {
		return true
	}
	start := l.pos
	if l.src[start] != '/' {
		return false
	}
	switch at(l.src, start+1) {
	case '/':
		kind := code.LineComment
		if doc && at(l.src, start+2) == '/' && at(l.src, start+3) != '/' {
			kind = code.DocComment
		}
		l.lineComment(start, kind)
	case '*':
		l.blockComment(start)
	default:
		return false
	}
	return true
}

func (l *lexer) lexCode() {
	if l.lexTrivia(true) {
		return
	}
	src, start := l.src, l.pos
	c := src[start]
	switch {
	case c == '<' && hasPrefix(src, start, "<!--"):
		l.lineComment(start, code.LineComment)
	case c == '-' && hasPrefix(src, start, "-->") && (l.nl || l.last < 0):
		l.lineComment(start, code.LineComment)
	case c == '/':
		if l.regexAllowed() {
			l.regex(start)
		} else if at(src, start+1) == '=' {
			l.emit(code.TokPunct, subNone, start, start+2)
		} else {
			l.emit(code.TokPunct, subNone, start, start+1)
		}
	case c == '`':
		l.template(start, start+1, true)
	case c == '\'' || c == '"':
		l.str(start)
	case isDigit(c) || (c == '.' && isDigit(at(src, start+1))):
		l.number(start)
	case c == '#' && isIdentStartAt(src, start+1):
		l.emit(code.TokIdent, subPrivate, start, identEnd(src, start+1))
	case isIdentStartAt(src, start):
		end := identEnd(src, start)
		kind := code.TokIdent
		if keywords[string(src[start:end])] {
			kind = code.TokKeyword
		}
		l.emit(kind, subNone, start, end)
	case c == '<' && l.jsx && l.regexAllowed() && l.jsxStartsAt(start):
		l.emit(code.TokPunct, subJSXOpen, start, start+1)
		l.push(frameJSXTag)
	case c == '(':
		ctrl := false
		if l.last >= 0 {
			if t := l.toks[l.last]; t.kind == code.TokKeyword {
				switch string(src[t.start:t.end]) {
				case "if", "while", "for", "with":
					ctrl = true
				}
			}
		}
		l.parens = append(l.parens, ctrl)
		l.emit(code.TokPunct, subNone, start, start+1)
	case c == ')':
		l.ctrlClose = false
		if n := len(l.parens); n > 0 {
			l.ctrlClose = l.parens[n-1]
			l.parens = l.parens[:n-1]
		}
		l.emit(code.TokPunct, subNone, start, start+1)
	case c == '{':
		l.emit(code.TokPunct, subNone, start, start+1)
		l.push(frameBrace)
	case c == '}':
		if l.pop() == frameTemplate {
			l.template(start, start+1, false)
			return
		}
		l.emit(code.TokPunct, subNone, start, start+1)
	default:
		l.punct(start)
	}
}

func (l *lexer) punct(start int) {
	if c := l.src[start]; c < utf8.RuneSelf {
		for _, p := range punctuators[c] {
			if hasPrefix(l.src, start, p) {
				if p == "?." && isDigit(at(l.src, start+2)) {
					continue
				}
				l.emit(code.TokPunct, subNone, start, start+len(p))
				return
			}
		}
	}
	_, size := utf8.DecodeRune(l.src[start:])
	l.emit(code.TokPunct, subNone, start, start+size)
}

// regexAllowed reports whether a '/' (or a JSX '<') at the current position
// starts a literal instead of an operator. We decide this from the previous
// significant token.
func (l *lexer) regexAllowed() bool {
	if l.last < 0 {
		return true
	}
	t := l.toks[l.last]
	text := string(l.src[t.start:t.end])
	switch t.kind {
	case code.TokPunct:
		switch t.sub {
		case subJSXEnd, subJSXSelf:
			return false
		}
		switch text {
		case ")":
			return l.ctrlClose
		case "]", "++", "--":
			return false
		}
		return true
	case code.TokIdent:
		return t.sub == subNone && (text == "await" || text == "yield")
	case code.TokKeyword:
		switch text {
		case "this", "super", "null", "true", "false":
			return false
		}
		return true
	case code.TokString:
		return t.sub == subTmplHead || t.sub == subTmplMiddle
	}
	return false
}

// jsxStartsAt reports whether the '<' at start opens a JSX element. In TSX,
// a generic arrow function like `<T,>(x) => x` or `<T extends U>` isn't JSX.
func (l *lexer) jsxStartsAt(start int) bool {
	next := at(l.src, start+1)
	if next == '>' {
		return true
	}
	if !isIdentStartAt(l.src, start+1) {
		return false
	}
	if !l.ts {
		return true
	}
	i := identEnd(l.src, start+1)
	for i < len(l.src) && (l.src[i] == ' ' || l.src[i] == '\t') {
		i++
	}
	if at(l.src, i) == ',' {
		return false
	}
	if hasPrefix(l.src, i, "extends") {
		j := i + len("extends")
		if c := at(l.src, j); c == ' ' || c == '\t' || c == '\n' {
			for j < len(l.src) && (l.src[j] == ' ' || l.src[j] == '\t' || l.src[j] == '\n') {
				j++
			}
			return at(l.src, j) == '='
		}
	}
	return true
}

func (l *lexer) regex(start int) {
	src := l.src
	i := start + 1
	inClass := false
	for i < len(src) && !isLineEnd(src, i) {
		c := src[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '[' {
			inClass = true
		} else if c == ']' {
			inClass = false
		} else if c == '/' && !inClass {
			i++
			for i < len(src) && isIdentPartAt(src, i) {
				i += runeLen(src, i)
			}
			break
		}
		i++
	}
	l.emit(code.TokString, subRegex, start, min(i, len(src)))
}

func (l *lexer) str(start int) {
	src := l.src
	q := src[start]
	i := start + 1
	for i < len(src) {
		c := src[i]
		if c == '\\' {
			i++
			if i < len(src) && src[i] == '\r' && at(src, i+1) == '\n' {
				i++
			}
			i++
			continue
		}
		if c == q {
			i++
			break
		}
		if c == '\n' || c == '\r' {
			break
		}
		i++
	}
	l.emit(code.TokString, subNone, start, min(i, len(src)))
}

// template lexes a template literal part that starts at start. i is the
// first byte after the opening '`' or '}'.
func (l *lexer) template(start, i int, head bool) {
	src := l.src
	for i < len(src) {
		switch src[i] {
		case '\\':
			i += 2
			continue
		case '`':
			s := subTmplTail
			if head {
				s = subTmplFull
			}
			l.emit(code.TokString, s, start, i+1)
			return
		case '$':
			if at(src, i+1) == '{' {
				s := subTmplMiddle
				if head {
					s = subTmplHead
				}
				l.emit(code.TokString, s, start, i+2)
				l.push(frameTemplate)
				return
			}
		}
		i++
	}
	s := subTmplTail
	if head {
		s = subTmplFull
	}
	l.emit(code.TokString, s, start, len(src))
}

func (l *lexer) number(start int) {
	src := l.src
	i := start
	if src[i] == '0' && i+1 < len(src) {
		switch src[i+1] | 0x20 {
		case 'x', 'o', 'b':
			i += 2
			for i < len(src) && (isHex(src[i]) || src[i] == '_') {
				i++
			}
			if at(src, i) == 'n' {
				i++
			}
			l.emit(code.TokNumber, subNone, start, i)
			return
		}
	}
	for i < len(src) && (isDigit(src[i]) || src[i] == '_') {
		i++
	}
	if at(src, i) == '.' {
		i++
		for i < len(src) && (isDigit(src[i]) || src[i] == '_') {
			i++
		}
	}
	if c := at(src, i); c == 'e' || c == 'E' {
		j := i + 1
		if c := at(src, j); c == '+' || c == '-' {
			j++
		}
		if isDigit(at(src, j)) {
			i = j
			for i < len(src) && (isDigit(src[i]) || src[i] == '_') {
				i++
			}
		}
	}
	if at(src, i) == 'n' {
		i++
	}
	l.emit(code.TokNumber, subNone, start, i)
}

func (l *lexer) lexJSXTag() {
	if l.lexTrivia(false) {
		return
	}
	src, start := l.src, l.pos
	c := src[start]
	switch {
	case c == '/' && at(src, start+1) == '>':
		l.emit(code.TokPunct, subJSXSelf, start, start+2)
		l.pop()
	case c == '>':
		l.emit(code.TokPunct, subJSXEnd, start, start+1)
		if l.pop() == frameJSXTag {
			l.push(frameJSXChildren)
		}
	case c == '{':
		l.emit(code.TokPunct, subNone, start, start+1)
		l.push(frameJSXAttr)
	case c == '"' || c == '\'':
		i := start + 1
		for i < len(src) && src[i] != c {
			i++
		}
		l.emit(code.TokString, subJSXString, start, min(i+1, len(src)))
	case c == '<':
		l.emit(code.TokPunct, subJSXOpen, start, start+1)
		l.push(frameJSXTag)
	case isIdentStartAt(src, start):
		i := start
		for i < len(src) && (isIdentPartAt(src, i) || src[i] == '-') {
			i += runeLen(src, i)
		}
		l.emit(code.TokIdent, subJSXName, start, i)
	default:
		_, size := utf8.DecodeRune(src[start:])
		l.emit(code.TokPunct, subNone, start, start+size)
	}
}

func (l *lexer) lexJSXChildren() {
	src, start := l.src, l.pos
	i := start
	for i < len(src) && src[i] != '<' && src[i] != '{' {
		i++
	}
	if i > start {
		s, e := start, i
		for s < e && isSpaceByte(src[s]) {
			s++
		}
		for e > s && isSpaceByte(src[e-1]) {
			e--
		}
		if e > s {
			l.toks = append(l.toks, token{kind: code.TokString, sub: subJSXText, start: s, end: e, pub: -1})
			l.last = len(l.toks) - 1
		}
		l.pos = i
		return
	}
	switch {
	case src[i] == '{':
		l.emit(code.TokPunct, subNone, i, i+1)
		l.push(frameJSXChild)
	case at(src, i+1) == '/':
		l.emit(code.TokPunct, subJSXClose, i, i+2)
		l.pop()
		l.push(frameJSXClosing)
	default:
		l.emit(code.TokPunct, subJSXOpen, i, i+1)
		l.push(frameJSXTag)
	}
}

// publicTokens merges template literal parts, plus everything between them,
// into single TokString tokens. It also records each token's public index.
func publicTokens(toks []token) []code.Token {
	out := make([]code.Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.sub != subTmplHead {
			toks[i].pub = int32(len(out))
			out = append(out, code.Token{Kind: t.kind, Span: code.Span{Start: t.start, End: t.end}})
			continue
		}
		depth, end, j := 0, t.end, i
		for ; j < len(toks); j++ {
			end = toks[j].end
			if toks[j].sub == subTmplHead {
				depth++
			} else if toks[j].sub == subTmplTail {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		out = append(out, code.Token{Kind: code.TokString, Span: code.Span{Start: t.start, End: end}})
		i = j
	}
	return out
}

func at(src []byte, i int) byte {
	if i < 0 || i >= len(src) {
		return 0
	}
	return src[i]
}

func hasPrefix(src []byte, i int, p string) bool {
	return len(src)-i >= len(p) && string(src[i:i+len(p)]) == p
}

func isLineEnd(src []byte, i int) bool {
	c := src[i]
	if c == '\n' || c == '\r' {
		return true
	}
	return c == 0xe2 && at(src, i+1) == 0x80 && (at(src, i+2) == 0xa8 || at(src, i+2) == 0xa9)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func runeLen(src []byte, i int) int {
	if src[i] < utf8.RuneSelf {
		return 1
	}
	_, size := utf8.DecodeRune(src[i:])
	return size
}

func isIdentStartAt(src []byte, i int) bool {
	if i >= len(src) {
		return false
	}
	c := src[i]
	if c < utf8.RuneSelf {
		return c == '$' || c == '_' || c == '\\' || (c|0x20 >= 'a' && c|0x20 <= 'z')
	}
	r, _ := utf8.DecodeRune(src[i:])
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

func isIdentPartAt(src []byte, i int) bool {
	if i >= len(src) {
		return false
	}
	c := src[i]
	if c < utf8.RuneSelf {
		return c == '$' || c == '_' || c == '\\' || isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'z')
	}
	r, _ := utf8.DecodeRune(src[i:])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nl, unicode.Pc) || r == '\u200c' || r == '\u200d'
}

// identEnd returns the end of the identifier at i. Identifiers can contain
// \uXXXX and \u{...} escapes.
func identEnd(src []byte, i int) int {
	for i < len(src) && isIdentPartAt(src, i) {
		if src[i] == '\\' {
			i = identEscapeEnd(src, i+1)
			continue
		}
		i += runeLen(src, i)
	}
	return i
}

func identEscapeEnd(src []byte, i int) int {
	if at(src, i) != 'u' {
		return min(i, len(src))
	}
	i++
	if at(src, i) != '{' {
		return min(i+4, len(src))
	}
	for i < len(src) && src[i] != '}' {
		i++
	}
	return min(i+1, len(src))
}
