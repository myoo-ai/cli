package rust

import (
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// Lifetimes and labels like 'a are TokIdent tokens with a leading quote. Raw
// identifiers keep their r# prefix.
type tok struct {
	kind code.TokenKind
	s, e int
}

func (t tok) token() code.Token {
	return code.Token{Kind: t.kind, Span: code.Span{Start: t.s, End: t.e}}
}

type lexer struct {
	src      []byte
	i        int
	toks     []tok
	comments []code.Comment
}

// lex splits src into tokens, comments included, plus a list of comments.
func lex(src []byte) *lexer {
	l := &lexer{src: src}
	if len(src) > 2 && src[0] == '#' && src[1] == '!' && src[2] != '[' {
		// A shebang line. The compiler skips it, so we do too.
		for l.i < len(src) && src[l.i] != '\n' {
			l.i++
		}
		l.comment(code.LineComment, 0, l.i)
	}
	for l.i < len(src) {
		switch c := src[l.i]; c {
		case ' ', '\t', '\r', '\n', '\f', '\v':
			l.i++
		default:
			l.token()
		}
	}
	return l
}

func (l *lexer) emit(kind code.TokenKind, s, e int) {
	l.toks = append(l.toks, tok{kind: kind, s: s, e: e})
	l.i = e
}

func (l *lexer) comment(kind code.CommentKind, s, e int) {
	l.toks = append(l.toks, tok{kind: code.TokComment, s: s, e: e})
	l.comments = append(l.comments, code.Comment{Kind: kind, Span: code.Span{Start: s, End: e}})
	l.i = e
}

func (l *lexer) at(i int) byte {
	if i < len(l.src) {
		return l.src[i]
	}
	return 0
}

// token lexes a token or comment that starts at the non-blank byte l.i.
func (l *lexer) token() {
	src, s := l.src, l.i
	c, next := src[s], l.at(s+1)
	switch {
	case c == '/' && next == '/':
		e := s + 2
		for e < len(src) && src[e] != '\n' {
			e++
		}
		if src[e-1] == '\r' {
			e--
		}
		kind := code.LineComment
		// /// and //! are doc comments, //// is a plain comment.
		if third := l.at(s + 2); s+2 < e && (third == '!' || third == '/' && (s+3 >= e || src[s+3] != '/')) {
			kind = code.DocComment
		}
		l.comment(kind, s, e)
	case c == '/' && next == '*':
		e, depth := s+2, 1
		for e < len(src) && depth > 0 {
			switch {
			case src[e] == '/' && l.at(e+1) == '*':
				depth++
				e += 2
			case src[e] == '*' && l.at(e+1) == '/':
				depth--
				e += 2
			default:
				e++
			}
		}
		kind := code.BlockComment
		// /** */ and /*! */ are doc comments, /**/ and /*** */ are not.
		if third := l.at(s + 2); third == '!' || third == '*' && e-s > 4 && l.at(s+3) != '*' {
			kind = code.DocComment
		}
		l.comment(kind, s, e)
	case c == '\'':
		l.quote(s)
	case c == '"':
		l.emit(code.TokString, s, l.quoted(s))
	case isDigit(c):
		l.emit(code.TokNumber, s, l.number(s))
	case isIdentStart(c) || c >= 0x80:
		l.word(s)
	default:
		l.emit(code.TokPunct, s, s+code.PunctLen(src[s:], puncts3, puncts2))
	}
}

// word lexes an identifier, keyword, raw identifier or prefixed literal.
func (l *lexer) word(s int) {
	src := l.src
	e := s
	for e < len(src) {
		r, size := utf8.DecodeRune(src[e:])
		if !isIdentRune(r) {
			break
		}
		e += size
	}
	if e == s {
		// A byte that can't start a token, like a stray non-letter rune.
		_, size := utf8.DecodeRune(src[s:])
		l.emit(code.TokPunct, s, s+size)
		return
	}
	prefix := string(src[s:e])
	switch nx := l.at(e); {
	case nx == '"' && (prefix == "b" || prefix == "c"):
		l.emit(code.TokString, s, l.quoted(e))
		return
	case nx == '\'' && prefix == "b":
		l.emit(code.TokString, s, l.quoted(e))
		return
	case (nx == '"' || nx == '#') && (prefix == "r" || prefix == "br" || prefix == "cr"):
		if end, ok := l.raw(e); ok {
			l.emit(code.TokString, s, end)
			return
		}
		if prefix == "r" && nx == '#' && isIdentStart(l.at(e+1)) {
			// A raw identifier such as r#type.
			k := e + 1
			for k < len(src) && isIdentChar(src[k]) {
				k++
			}
			l.emit(code.TokIdent, s, k)
			return
		}
	}
	kind := code.TokIdent
	if keywords[prefix] {
		kind = code.TokKeyword
	}
	l.emit(kind, s, e)
}

// raw returns the end of the raw string if its hashes or quote start at i.
func (l *lexer) raw(i int) (int, bool) {
	src := l.src
	hashes := 0
	for i+hashes < len(src) && src[i+hashes] == '#' {
		hashes++
	}
	q := i + hashes
	if l.at(q) != '"' {
		return 0, false
	}
	for e := q + 1; e < len(src); e++ {
		if src[e] != '"' {
			continue
		}
		n := 0
		for n < hashes && e+1+n < len(src) && src[e+1+n] == '#' {
			n++
		}
		if n == hashes {
			return l.suffix(e + 1 + n), true
		}
	}
	return len(src), true
}

// quote lexes a char literal like 'a', '\n' or '\u{1F600}', or a lifetime or
// label like 'a or 'static.
func (l *lexer) quote(s int) {
	src := l.src
	if l.at(s+1) == '\\' {
		l.emit(code.TokString, s, l.quoted(s))
		return
	}
	if s+1 < len(src) {
		r, size := utf8.DecodeRune(src[s+1:])
		if l.at(s+1+size) == '\'' && r != '\n' {
			l.emit(code.TokString, s, l.suffix(s+2+size))
			return
		}
	}
	e := s + 1
	for e < len(src) {
		r, size := utf8.DecodeRune(src[e:])
		if !isIdentRune(r) {
			break
		}
		e += size
	}
	if e == s+1 {
		l.emit(code.TokPunct, s, s+1)
		return
	}
	l.emit(code.TokIdent, s, e)
}

// quoted returns the end of the string or char literal that opens at q.
// Strings can span lines, but an unterminated char literal stops at the
// end of the line.
func (l *lexer) quoted(q int) int {
	src := l.src
	quote := src[q]
	for e := q + 1; e < len(src); e++ {
		switch src[e] {
		case '\\':
			e++
		case '\n':
			if quote == '\'' {
				return e
			}
		case quote:
			return l.suffix(e + 1)
		}
	}
	return len(src)
}

// suffix extends a literal ending at e by a suffix such as 1u8 or "x"s.
func (l *lexer) suffix(e int) int {
	if isIdentStart(l.at(e)) {
		for e < len(l.src) && isIdentChar(l.src[e]) {
			e++
		}
	}
	return e
}

// number lexes an integer or float literal with underscores, a base prefix,
// an exponent and a type suffix, like 1_000u32, 0x_FFi64, 1e-3_f64 or 2.5.
func (l *lexer) number(s int) int {
	src := l.src
	e := s + 1
	hex := src[s] == '0' && (l.at(s+1) == 'x' || l.at(s+1) == 'X')
	if src[s] == '0' && (hex || l.at(s+1) == 'o' || l.at(s+1) == 'b') {
		e = s + 2
	}
	for e < len(src) {
		c := src[e]
		switch {
		case isIdentChar(c):
			if !hex && (c == 'e' || c == 'E') && (l.at(e+1) == '+' || l.at(e+1) == '-') && isDigit(l.at(e+2)) {
				e += 2
			}
			e++
		case c == '.' && isDigit(l.at(e+1)) && !hex:
			e++
		case c == '.' && l.at(e+1) != '.' && !isIdentStart(l.at(e+1)) && !hex:
			// 1. is a float literal, but 1..2 is a range and 1.max(2) a call.
			return e + 1
		default:
			return e
		}
	}
	return e
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isIdentChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentRune(r rune) bool {
	if r < 0x80 {
		return isIdentChar(byte(r))
	}
	return r != utf8.RuneError && (r >= 0xAA && isLetterRune(r))
}

// isLetterRune approximates XID_Continue for non-ASCII runes. We take all of
// them except punctuation, symbols and spaces of the common blocks.
func isLetterRune(r rune) bool {
	switch {
	case r >= 0x2000 && r <= 0x2BFF, r >= 0x3000 && r <= 0x303F, r >= 0x1F000, r == 0xA0, r == 0xFEFF:
		return false
	}
	return true
}

var puncts3 = []string{"...", "..=", "<<=", ">>="}

var puncts2 = []string{
	"::", "->", "=>", "==", "!=", "<=", ">=", "&&", "||", "+=", "-=", "*=", "/=", "%=",
	"^=", "&=", "|=", "<<", ">>", "..",
}

var keywords = code.StringSet(
	"as", "async", "await", "break", "const", "continue", "crate", "dyn", "else", "enum",
	"extern", "false", "fn", "for", "if", "impl", "in", "let", "loop", "match", "mod", "move",
	"mut", "pub", "ref", "return", "self", "Self", "static", "struct", "super", "trait", "true",
	"type", "unsafe", "use", "where", "while",
)
