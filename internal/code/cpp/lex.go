package cpp

import "github.com/myoo-ai/cli/internal/code"

type tok struct {
	kind code.TokenKind
	s, e int
	dir  int // 1-based preprocessor directive number, 0 outside directives
}

// A directive covers tokens toks[first:last] of the lexer output, '#' included,
// and runs over continuation lines.
type directive struct {
	span        code.Span
	first, last int
}

type lexer struct {
	src      []byte
	i        int
	cpp      bool // we only lex raw strings in C++
	keywords map[string]bool
	toks     []tok
	comments []code.Comment
	dirs     []directive
}

// lex splits src into tokens, comments included, plus comments and directives.
func lex(src []byte, cpp bool) *lexer {
	l := &lexer{src: src, cpp: cpp, keywords: cKeywords}
	if cpp {
		l.keywords = cppKeywords
	}
	bol := true
	for l.i < len(src) {
		c := src[l.i]
		switch {
		case c == '\n':
			bol = true
			l.i++
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.i++
			continue
		case c == '\\' && l.splice():
			continue
		case c == '#' && bol:
			l.directive()
			continue
		}
		bol = false
		l.token(0)
	}
	return l
}

// splice skips a backslash-newline line continuation at l.i.
func (l *lexer) splice() bool {
	j := l.i + 1
	if j < len(l.src) && l.src[j] == '\r' {
		j++
	}
	if j < len(l.src) && l.src[j] == '\n' {
		l.i = j + 1
		return true
	}
	return false
}

func (l *lexer) directive() {
	n := len(l.dirs) + 1
	first := len(l.toks)
	start := l.i
	l.emit(code.TokPunct, l.i, l.i+1, n)
	l.i++
	name, named := "", false
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == '\n' {
			break
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v' {
			l.i++
			continue
		}
		if c == '\\' && l.splice() {
			continue
		}
		if c == '<' && (name == "include" || name == "include_next" || name == "import") && len(l.toks)-first == 2 {
			j := l.i + 1
			for j < len(l.src) && l.src[j] != '>' && l.src[j] != '\n' {
				j++
			}
			if j < len(l.src) && l.src[j] == '>' {
				j++
			}
			l.emit(code.TokString, l.i, j, n)
			l.i = j
			continue
		}
		l.token(n)
		if t := &l.toks[len(l.toks)-1]; !named && t.kind != code.TokComment {
			named = true
			if t.kind == code.TokIdent || t.kind == code.TokKeyword {
				t.kind = code.TokKeyword
				name = string(l.src[t.s:t.e])
			}
		}
	}
	end := start + 1
	if last := len(l.toks); last > first {
		end = max(end, l.toks[last-1].e)
	}
	l.dirs = append(l.dirs, directive{span: code.Span{Start: start, End: end}, first: first, last: len(l.toks)})
}

func (l *lexer) emit(kind code.TokenKind, s, e, dir int) {
	l.toks = append(l.toks, tok{kind: kind, s: s, e: e, dir: dir})
}

func (l *lexer) comment(kind code.CommentKind, s, e, dir int) {
	l.emit(code.TokComment, s, e, dir)
	l.comments = append(l.comments, code.Comment{Kind: kind, Span: code.Span{Start: s, End: e}})
}

// token lexes a token or comment starting at l.i, which has to be non-blank.
func (l *lexer) token(dir int) {
	src, s := l.src, l.i
	c := src[s]
	next := byte(0)
	if s+1 < len(src) {
		next = src[s+1]
	}
	switch {
	case c == '/' && next == '/':
		e := s + 2
		for e < len(src) && src[e] != '\n' {
			if src[e] == '\\' && e+1 < len(src) && src[e+1] == '\n' {
				e++
			}
			e++
		}
		if e > s && src[e-1] == '\r' {
			e--
		}
		kind := code.LineComment
		text := src[s:e]
		if len(text) >= 3 && (text[2] == '!' || text[2] == '/' && (len(text) == 3 || text[3] != '/')) {
			kind = code.DocComment
		}
		l.comment(kind, s, e, dir)
		l.i = e
	case c == '/' && next == '*':
		e := s + 2
		for e < len(src) && !(src[e] == '*' && e+1 < len(src) && src[e+1] == '/') {
			e++
		}
		e = min(e+2, len(src))
		kind := code.BlockComment
		if e-s > 4 && (src[s+2] == '*' && src[s+3] != '*' || src[s+2] == '!') {
			kind = code.DocComment
		}
		l.comment(kind, s, e, dir)
		l.i = e
	case isIdentStart(c):
		e := s + 1
		for e < len(src) && isIdentChar(src[e]) {
			e++
		}
		word := src[s:e]
		if e < len(src) && (src[e] == '"' || src[e] == '\'') {
			if src[e] == '"' && l.cpp && rawPrefixes[string(word)] {
				l.i = l.raw(s, e+1)
				l.emit(code.TokString, s, l.i, dir)
				return
			}
			if stringPrefixes[string(word)] {
				l.i = l.quoted(e)
				l.emit(code.TokString, s, l.i, dir)
				return
			}
		}
		kind := code.TokIdent
		if l.keywords[string(word)] {
			kind = code.TokKeyword
		}
		l.emit(kind, s, e, dir)
		l.i = e
	case isDigit(c) || c == '.' && isDigit(next):
		l.i = l.number(s)
		l.emit(code.TokNumber, s, l.i, dir)
	case c == '"' || c == '\'':
		l.i = l.quoted(s)
		l.emit(code.TokString, s, l.i, dir)
	default:
		n := code.PunctLen(src[s:], puncts3, puncts2)
		l.emit(code.TokPunct, s, s+n, dir)
		l.i = s + n
	}
}

// quoted returns the end of the string or char literal whose quote is at q,
// including a user-defined literal suffix. If the literal isn't terminated it
// ends at the end of the line.
func (l *lexer) quoted(q int) int {
	src := l.src
	quote := src[q]
	e := q + 1
	for e < len(src) {
		c := src[e]
		if c == '\\' {
			e += 2
			continue
		}
		if c == '\n' {
			if e > q && src[e-1] == '\r' {
				e--
			}
			return min(e, len(src))
		}
		e++
		if c == quote {
			return l.suffix(e)
		}
	}
	return len(src)
}

// raw returns the end of the raw string whose opening quote ends at body, just
// after R". If the delimiter is malformed we treat it as a regular string.
func (l *lexer) raw(s, body int) int {
	src := l.src
	d := body
	for d < len(src) && d-body <= 16 && src[d] != '(' {
		switch src[d] {
		case ' ', ')', '\\', '\t', '\n', '"':
			return l.quoted(body - 1)
		}
		d++
	}
	if d >= len(src) || src[d] != '(' {
		return l.quoted(body - 1)
	}
	delim := string(src[body:d])
	closing := ")" + delim + "\""
	for e := d + 1; e+len(closing) <= len(src); e++ {
		if src[e] == ')' && string(src[e:e+len(closing)]) == closing {
			return l.suffix(e + len(closing))
		}
	}
	return len(src)
}

// suffix extends a literal ending at e by a user-defined literal suffix.
func (l *lexer) suffix(e int) int {
	if e < len(l.src) && isIdentStart(l.src[e]) {
		for e < len(l.src) && isIdentChar(l.src[e]) {
			e++
		}
	}
	return e
}

func (l *lexer) number(s int) int {
	src := l.src
	hex := s+1 < len(src) && src[s] == '0' && (src[s+1] == 'x' || src[s+1] == 'X')
	e := s + 1
	for e < len(src) {
		c := src[e]
		switch {
		case isIdentChar(c) || c == '.':
			e++
		case c == '\'' && e+1 < len(src) && isIdentChar(src[e+1]):
			e++
		case (c == '+' || c == '-') && isExponent(src[e-1], hex):
			e++
		default:
			return e
		}
	}
	return e
}

func isExponent(c byte, hex bool) bool {
	if hex {
		return c == 'p' || c == 'P'
	}
	return c == 'e' || c == 'E' || c == 'p' || c == 'P'
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$' || c >= 0x80
}

func isIdentChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

var puncts3 = []string{"...", "<<=", ">>=", "<=>", "->*"}

var puncts2 = []string{
	"::", "->", "++", "--", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", ".*", "##",
}

var stringPrefixes = map[string]bool{"u8": true, "u": true, "U": true, "L": true}

var rawPrefixes = map[string]bool{"R": true, "u8R": true, "uR": true, "UR": true, "LR": true}

var cWords = []string{
	"auto", "break", "case", "char", "const", "continue", "default", "do", "double",
	"else", "enum", "extern", "float", "for", "goto", "if", "inline", "int", "long",
	"register", "restrict", "return", "short", "signed", "sizeof", "static", "struct",
	"switch", "typedef", "union", "unsigned", "void", "volatile", "while",
	"_Alignas", "_Alignof", "_Atomic", "_Bool", "_Complex", "_Generic", "_Imaginary",
	"_Noreturn", "_Static_assert", "_Thread_local",
	"alignas", "alignof", "bool", "constexpr", "false", "nullptr", "static_assert",
	"thread_local", "true", "typeof", "typeof_unqual",
}

var cKeywords = code.StringSet(cWords...)

var cppKeywords = code.StringSet(append(cWords[:len(cWords):len(cWords)],
	"asm", "catch", "char8_t", "char16_t", "char32_t", "class", "concept", "consteval",
	"constinit", "const_cast", "co_await", "co_return", "co_yield", "decltype", "delete",
	"dynamic_cast", "explicit", "export", "friend", "mutable", "namespace", "new",
	"noexcept", "operator", "private", "protected", "public", "reinterpret_cast",
	"requires", "static_cast", "template", "this", "throw", "try", "typeid", "typename",
	"using", "virtual", "wchar_t",
)...)
