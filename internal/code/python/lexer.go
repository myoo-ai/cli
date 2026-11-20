package python

import (
	"bytes"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

type tokKind uint8

const (
	tName tokKind = iota + 1
	tKeyword
	tNumber
	tString
	tOp
	tNewline
	tIndent
	tDedent
	tEOF
)

// tok is a parser token. Comments aren't parser tokens. NEWLINE, INDENT,
// DEDENT and EOF are synthetic, so they have no File.Tokens entry.
type tok struct {
	kind tokKind
	text string // names, keywords and operators
	span code.Span
	// fields are the replacement-field expressions of an f-string or
	// t-string, including fields nested in format specs.
	fields []code.Span
	code   int // index into File.Tokens, -1 if we didn't emit the token
}

var keywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true,
	"async": true, "await": true, "break": true, "class": true, "continue": true,
	"def": true, "del": true, "elif": true, "else": true, "except": true,
	"finally": true, "for": true, "from": true, "global": true, "if": true,
	"import": true, "in": true, "is": true, "lambda": true, "nonlocal": true,
	"not": true, "or": true, "pass": true, "raise": true, "return": true,
	"try": true, "while": true, "with": true, "yield": true,
}

var ops3 = []string{"**=", "//=", ">>=", "<<=", "..."}

var ops2 = []string{
	"->", ":=", "**", "//", "<<", ">>", "<=",
	">=", "==", "!=", "+=", "-=", "*=", "/=",
	"%=", "&=", "|=", "^=", "@=",
}

type lexer struct {
	src      []byte
	pos, end int
	emit     bool // record File tokens and comments
	nested   bool // lexing an f-string field, which is always inside brackets
	toks     []tok
	code     []code.Token
	comments []code.Comment

	indents       []int
	depth         int
	lineStart     bool
	pending       bool // the current logical line has tokens
	bracketIndent int  // indentation of the line that opened the outermost bracket
}

// lex tokenizes src[start:end]. In nested mode, used for an f-string
// replacement field, newlines can't end a logical line and we don't track
// indentation.
func lex(src []byte, start, end int, emit, nested bool) *lexer {
	l := &lexer{src: src, pos: start, end: end, emit: emit, nested: nested, indents: []int{0}, lineStart: !nested}
	if nested {
		l.depth = 1
	}
	l.run()
	return l
}

func (l *lexer) run() {
	for l.pos < l.end {
		if l.lineStart {
			l.lineStart = false
			l.indent()
			continue
		}
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.newline()
		case c == ' ' || c == '\t' || c == '\f' || c == '\r':
			l.pos++
		case c == '#':
			l.comment()
		case c == '\\' && l.continuation(l.pos+1) > 0:
			l.pos += 1 + l.continuation(l.pos+1)
		case c == '"' || c == '\'':
			l.string(l.pos, l.pos)
		case isDigit(c) || (c == '.' && l.pos+1 < l.end && isDigit(l.src[l.pos+1])):
			l.number()
		case l.identStart(l.pos):
			l.name()
		default:
			l.op()
		}
	}
	l.finish()
}

// continuation returns the length of the line break at i, or 0.
func (l *lexer) continuation(i int) int {
	switch {
	case i < l.end && l.src[i] == '\n':
		return 1
	case i+1 < l.end && l.src[i] == '\r' && l.src[i+1] == '\n':
		return 2
	}
	return 0
}

func (l *lexer) add(kind tokKind, start, end int, ck code.TokenKind) *tok {
	t := tok{kind: kind, span: code.Span{Start: start, End: end}, code: -1}
	if kind == tName || kind == tKeyword || kind == tOp {
		t.text = string(l.src[start:end])
	}
	if l.emit && ck != 0 {
		t.code = len(l.code)
		l.code = append(l.code, code.Token{Kind: ck, Span: t.span})
	}
	if ck != 0 {
		l.pending = true
	}
	l.toks = append(l.toks, t)
	return &l.toks[len(l.toks)-1]
}

// indent measures the indentation at the start of a physical line and
// emits INDENT/DEDENT tokens. We ignore blank and comment-only lines. If a
// dedent doesn't match an outer level, we snap to the nearest enclosing one.
func (l *lexer) indent() {
	if l.depth > 0 {
		return
	}
	col, i := 0, l.pos
loop:
	for ; i < l.end; i++ {
		switch l.src[i] {
		case ' ':
			col++
		case '\t':
			col = (col/8 + 1) * 8
		case '\f':
			col = 0
		default:
			break loop
		}
	}
	l.pos = i
	if i >= l.end {
		return
	}
	switch l.src[i] {
	case '\n', '\r', '#':
		return
	case '\\':
		if l.continuation(i+1) > 0 {
			return
		}
	}
	top := l.indents[len(l.indents)-1]
	if col > top {
		l.indents = append(l.indents, col)
		l.add(tIndent, i, i, 0)
		return
	}
	for col < top && len(l.indents) > 1 {
		l.indents = l.indents[:len(l.indents)-1]
		top = l.indents[len(l.indents)-1]
		l.add(tDedent, i, i, 0)
	}
}

func (l *lexer) newline() {
	if l.depth > 0 && !l.nested && l.statementStartsAt(l.pos+1) {
		l.depth = 0
	}
	if l.depth == 0 {
		if l.pending {
			l.add(tNewline, l.pos, l.pos+1, 0)
			l.pending = false
		}
		l.lineStart = true
	}
	l.pos++
}

// statementStartsAt is how we recover from unbalanced brackets. A `def` or
// `class` at or left of the indentation of the line that opened the bracket
// has to end the bracketed region.
func (l *lexer) statementStartsAt(i int) bool {
	col := 0
	for ; i < l.end && (l.src[i] == ' ' || l.src[i] == '\t'); i++ {
		col++
	}
	if col > l.bracketIndent {
		return false
	}
	for _, kw := range []string{"def", "class", "async def"} {
		j := i + len(kw)
		if bytes.HasPrefix(l.src[i:l.end], []byte(kw)) && j < l.end && (l.src[j] == ' ' || l.src[j] == '\t') {
			return true
		}
	}
	return false
}

func (l *lexer) comment() {
	start := l.pos
	for l.pos < l.end && l.src[l.pos] != '\n' {
		l.pos++
	}
	end := l.pos
	if end > start && l.src[end-1] == '\r' {
		end--
	}
	if l.emit {
		sp := code.Span{Start: start, End: end}
		l.code = append(l.code, code.Token{Kind: code.TokComment, Span: sp})
		l.comments = append(l.comments, code.Comment{Kind: code.LineComment, Span: sp})
	}
}

func (l *lexer) finish() {
	if l.pending {
		l.add(tNewline, l.end, l.end, 0)
		l.pending = false
	}
	for len(l.indents) > 1 {
		l.indents = l.indents[:len(l.indents)-1]
		l.add(tDedent, l.end, l.end, 0)
	}
	l.add(tEOF, l.end, l.end, 0)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (l *lexer) identStart(i int) bool {
	c := l.src[i]
	if c < utf8.RuneSelf {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	r, _ := utf8.DecodeRune(l.src[i:l.end])
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

func (l *lexer) identEnd(i int) int {
	for i < l.end {
		c := l.src[i]
		if c < utf8.RuneSelf {
			if c == '_' || isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				i++
				continue
			}
			return i
		}
		r, size := utf8.DecodeRune(l.src[i:l.end])
		if r == utf8.RuneError || !(unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.In(r, unicode.Mn, unicode.Mc, unicode.Pc, unicode.Nl)) {
			return i
		}
		i += size
	}
	return i
}

func (l *lexer) name() {
	start := l.pos
	l.pos = l.identEnd(l.pos)
	if l.pos < l.end && (l.src[l.pos] == '"' || l.src[l.pos] == '\'') && isStringPrefix(l.src[start:l.pos]) {
		l.string(start, l.pos)
		return
	}
	if keywords[string(l.src[start:l.pos])] {
		l.add(tKeyword, start, l.pos, code.TokKeyword)
		return
	}
	l.add(tName, start, l.pos, code.TokIdent)
}

// isStringPrefix reports whether p is a valid string prefix. That's r, u,
// b, f or t, or the combinations br, fr and tr in any order and case.
func isStringPrefix(p []byte) bool {
	if len(p) == 0 || len(p) > 2 {
		return false
	}
	var r, other int
	for _, c := range bytes.ToLower(p) {
		switch c {
		case 'r':
			r++
		case 'u', 'b', 'f', 't':
			other++
		default:
			return false
		}
	}
	if len(p) == 1 {
		return true
	}
	return r == 1 && other == 1 && !bytes.ContainsAny(bytes.ToLower(p), "u")
}

func (l *lexer) number() {
	start, i := l.pos, l.pos
	digits := func() {
		for i < l.end && (isDigit(l.src[i]) || l.src[i] == '_') {
			i++
		}
	}
	if l.src[i] == '0' && i+1 < l.end && bytes.IndexByte([]byte("xXoObB"), l.src[i+1]) >= 0 {
		i += 2
		for i < l.end && (isHexDigit(l.src[i]) || l.src[i] == '_') {
			i++
		}
	} else {
		digits()
		if i < l.end && l.src[i] == '.' {
			i++
			digits()
		}
		if i < l.end && (l.src[i] == 'e' || l.src[i] == 'E') {
			j := i + 1
			if j < l.end && (l.src[j] == '+' || l.src[j] == '-') {
				j++
			}
			if j < l.end && isDigit(l.src[j]) {
				i = j
				digits()
			}
		}
		if i < l.end && (l.src[i] == 'j' || l.src[i] == 'J') {
			i++
		}
	}
	l.pos = i
	l.add(tNumber, start, i, code.TokNumber)
}

func (l *lexer) op() {
	start := l.pos
	n := code.PunctLen(l.src[start:l.end], ops3, ops2)
	if l.src[start] >= utf8.RuneSelf {
		_, n = utf8.DecodeRune(l.src[start:l.end])
	}
	l.pos += n
	switch l.src[start] {
	case '(', '[', '{':
		if l.depth == 0 {
			l.bracketIndent = l.indents[len(l.indents)-1]
		}
		l.depth++
	case ')', ']', '}':
		floor := 0
		if l.nested {
			floor = 1
		}
		if l.depth > floor {
			l.depth--
		}
	}
	l.add(tOp, start, l.pos, code.TokPunct)
}

// string lexes the literal with its prefix at start and its opening quote
// at q.
func (l *lexer) string(start, q int) {
	end, fields := l.scanString(start, q)
	l.pos = end
	t := l.add(tString, start, end, code.TokString)
	t.fields = fields
}

// scanString returns the end of the literal with its prefix at start and
// opening quote at q. For f-strings it also returns the replacement fields.
func (l *lexer) scanString(start, q int) (int, []code.Span) {
	prefix := bytes.ToLower(l.src[start:q])
	raw := bytes.IndexByte(prefix, 'r') >= 0
	format := bytes.IndexByte(prefix, 'f') >= 0 || bytes.IndexByte(prefix, 't') >= 0
	quote := l.src[q]
	triple := q+2 < l.end && l.src[q+1] == quote && l.src[q+2] == quote
	i := q + 1
	if triple {
		i = q + 3
	}
	if format {
		var fields []code.Span
		end := l.scanFString(i, quote, triple, raw, &fields)
		return end, fields
	}
	for i < l.end {
		c := l.src[i]
		switch {
		case c == '\\':
			i = l.skipEscape(i)
		case c == quote && l.closes(i, quote, triple):
			return l.closeEnd(i, triple), nil
		case c == '\n' && !triple:
			return i, nil
		default:
			i++
		}
	}
	return l.end, nil
}

func (l *lexer) closes(i int, quote byte, triple bool) bool {
	if !triple {
		return true
	}
	return i+2 < l.end && l.src[i+1] == quote && l.src[i+2] == quote
}

func (l *lexer) closeEnd(i int, triple bool) int {
	if triple {
		return i + 3
	}
	return i + 1
}

// skipEscape skips a backslash and the character (or line break) after it.
func (l *lexer) skipEscape(i int) int {
	if n := l.continuation(i + 1); n > 0 {
		return i + 1 + n
	}
	return min(i+2, l.end)
}

// scanFString scans the body of an f-string. Since PEP 701, replacement
// fields can nest brackets, format specs and strings with the same quote.
func (l *lexer) scanFString(i int, quote byte, triple, raw bool, fields *[]code.Span) int {
	for i < l.end {
		c := l.src[i]
		switch {
		case c == '\\':
			next := byte(0)
			if i+1 < l.end {
				next = l.src[i+1]
			}
			switch {
			case next == '{' || next == '}':
				i++
			case !raw && next == 'N' && i+2 < l.end && l.src[i+2] == '{':
				j := i + 3
				for j < l.end && l.src[j] != '}' && l.src[j] != '\n' && l.src[j] != quote {
					j++
				}
				if j < l.end && l.src[j] == '}' {
					j++
				}
				i = j
			default:
				i = l.skipEscape(i)
			}
		case c == quote && l.closes(i, quote, triple):
			return l.closeEnd(i, triple)
		case c == '\n' && !triple:
			return i
		case c == '{':
			if i+1 < l.end && l.src[i+1] == '{' {
				i += 2
				continue
			}
			i = l.scanField(i+1, quote, triple, fields)
		default:
			i++
		}
	}
	return l.end
}

// scanField scans a replacement field from just after its `{` and returns
// the offset after its `}`. We record the expression part in fields. We
// skip conversions and format specs, but still record any fields nested in
// the format spec.
func (l *lexer) scanField(i int, quote byte, triple bool, fields *[]code.Span) int {
	start, depth := i, 0
	record := func(end int) { *fields = append(*fields, code.Span{Start: start, End: end}) }
	for i < l.end {
		c := l.src[i]
		switch {
		case c == '\n' && !triple && depth == 0:
			record(i)
			return i
		case c == '(' || c == '[' || c == '{':
			depth++
			i++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
			i++
		case c == '}':
			if depth > 0 {
				depth--
				i++
				continue
			}
			record(i)
			return i + 1
		case c == '#':
			for i < l.end && l.src[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			i, _ = l.scanString(i, i)
		case depth == 0 && c == '!' && (i+1 >= l.end || l.src[i+1] != '='):
			record(i)
			for i < l.end && l.src[i] != ':' && l.src[i] != '}' && l.src[i] != quote && (l.src[i] != '\n' || triple) {
				i++
			}
			if i < l.end && l.src[i] == ':' {
				return l.scanSpec(i+1, quote, triple, fields)
			}
			if i < l.end && l.src[i] == '}' {
				return i + 1
			}
			return i
		case depth == 0 && c == ':':
			record(i)
			return l.scanSpec(i+1, quote, triple, fields)
		case l.identStart(i):
			j := l.identEnd(i)
			if j < l.end && (l.src[j] == '"' || l.src[j] == '\'') && isStringPrefix(l.src[i:j]) {
				i, _ = l.scanString(i, j)
				continue
			}
			i = j
		default:
			i++
		}
	}
	record(l.end)
	return l.end
}

// scanSpec scans a format spec up to and including the `}` that closes the
// field.
func (l *lexer) scanSpec(i int, quote byte, triple bool, fields *[]code.Span) int {
	for i < l.end {
		c := l.src[i]
		switch {
		case c == '{':
			i = l.scanField(i+1, quote, triple, fields)
		case c == '}':
			return i + 1
		case c == '\n' && !triple:
			return i
		case c == quote && l.closes(i, quote, triple):
			return i
		default:
			i++
		}
	}
	return l.end
}
