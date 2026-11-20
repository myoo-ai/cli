package php

import (
	"bytes"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// tok is a lexical token. Open and close tags are punctuation tokens, so a
// `?>` can end a statement the same way `;` does.
type tok struct {
	kind code.TokenKind
	s, e int
	// fields are the interpolated parts of a double-quoted string, heredoc
	// or backtick literal.
	fields []field
}

func (t tok) token() code.Token {
	return code.Token{Kind: t.kind, Span: code.Span{Start: t.s, End: t.e}}
}

// field is an interpolated part of a string. If varName is set, the field
// is the bare name of the `${name}` syntax. Otherwise it holds PHP code,
// like `$a->b` or the inside of `{$a['k']}`.
type field struct {
	s, e    int
	varName bool
}

type lexer struct {
	src      []byte
	end      int
	toks     []tok
	comments []code.Comment
	halted   bool
}

// keywords holds the reserved words, which we match case-insensitively. We
// list self and parent too because they never name a class, and the magic
// constants because nothing can redefine them.
var keywords = code.StringSet("abstract", "and", "array", "as", "break", "callable", "case", "catch", "class", "clone",
	"const", "continue", "declare", "default", "do", "echo", "else", "elseif", "empty", "enddeclare", "endfor",
	"endforeach", "endif", "endswitch", "endwhile", "eval", "exit", "die", "extends", "final", "finally", "fn",
	"for", "foreach", "function", "global", "goto", "if", "implements", "include", "include_once", "instanceof",
	"insteadof", "interface", "isset", "list", "match", "namespace", "new", "or", "print", "private",
	"protected", "public", "readonly", "require", "require_once", "return", "static", "switch", "throw",
	"trait", "try", "unset", "use", "var", "while", "xor", "yield", "self", "parent", "true", "false", "null",
	"__class__", "__dir__", "__file__", "__function__", "__line__", "__method__", "__namespace__", "__trait__",
	"__property__", "__halt_compiler")

// casts are the type names of the cast operators `(int)`, `(string)` and
// so on. PHP lexes a cast as a single token.
var casts = code.StringSet("int", "integer", "bool", "boolean", "float", "double", "real", "string", "binary", "array", "object", "unset")

// operators lists the multi-character operators, longest first.
var operators = []string{"<=>", "**=", "...", "<<=", ">>=", "===", "!==", "??=", "?->",
	"**", "++", "--", "->", "=>", "::", "==", "!=", "<>", "<=", ">=", "&&", "||", "??",
	"+=", "-=", "*=", "/=", ".=", "%=", "&=", "|=", "^=", "<<", ">>"}

// lex tokenizes src[start:end]. If inCode is set we start inside PHP code,
// otherwise in inline HTML before the first open tag.
func lex(src []byte, start, end int, inCode bool) *lexer {
	l := &lexer{src: src, end: end}
	i := start
	for i < end && !l.halted {
		if !inCode {
			i = l.html(i)
			inCode = true
			continue
		}
		var closed bool
		i, closed = l.next(i)
		if closed {
			inCode = false
		}
	}
	return l
}

// startsInCode reports whether a file is PHP code from the first byte on,
// which we assume if it has no open tag or markup at all. Such files are
// snippets, like the identifier a rename validates, and PHP itself would
// print them as text.
func startsInCode(src []byte) bool {
	return bytes.IndexByte(src, '<') < 0
}

// html skips inline HTML from i and emits the open tag that ends it. It
// returns the offset after the tag, or end if there is none.
func (l *lexer) html(i int) int {
	for {
		k := bytes.Index(l.src[i:l.end], []byte("<?"))
		if k < 0 {
			return l.end
		}
		i += k
		if n := openTag(l.src[:l.end], i); n > 0 {
			l.emit(code.TokPunct, i, i+n)
			return i + n
		}
		i += 2
	}
}

// openTag returns the length of the open tag at i, or 0 if there is none.
// We accept `<?php` followed by white space or the end, `<?=`, and a short
// `<?` followed by white space, so `<?xml` declarations stay in the HTML.
func openTag(src []byte, i int) int {
	rest := src[i:]
	switch {
	case len(rest) >= 5 && strings.EqualFold(string(rest[:5]), "<?php") && (len(rest) == 5 || isSpace(rest[5])):
		return 5
	case bytes.HasPrefix(rest, []byte("<?=")):
		return 3
	case len(rest) == 2 || len(rest) > 2 && isSpace(rest[2]):
		return 2
	}
	return 0
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentChar(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (l *lexer) at(i int) byte {
	if i < l.end {
		return l.src[i]
	}
	return 0
}

func (l *lexer) emit(kind code.TokenKind, s, e int) {
	l.toks = append(l.toks, tok{kind: kind, s: s, e: e})
}

func (l *lexer) comment(kind code.CommentKind, s, e int) {
	l.emit(code.TokComment, s, e)
	l.comments = append(l.comments, code.Comment{Kind: kind, Span: code.Span{Start: s, End: e}})
}

// prev returns the text of the code token back tokens before the last one,
// skipping comments. It returns "" if there aren't enough tokens.
func (l *lexer) prev(back int) string {
	for _, t := range slices.Backward(l.toks) {
		if t.kind == code.TokComment {
			continue
		}
		if back == 0 {
			return string(l.src[t.s:t.e])
		}
		back--
	}
	return ""
}

// next lexes a token at i. closed reports a close tag, after which we're
// back in inline HTML.
func (l *lexer) next(i int) (int, bool) {
	c := l.src[i]
	switch {
	case isSpace(c):
		return i + 1, false
	case c == '?' && l.at(i+1) == '>':
		l.emit(code.TokPunct, i, i+2)
		l.haltAfter(i)
		i += 2
		if l.at(i) == '\r' {
			i++
		}
		if l.at(i) == '\n' {
			i++
		}
		return i, true
	case c == '#' && l.at(i+1) == '[':
		l.emit(code.TokPunct, i, i+1)
		return i + 1, false
	case c == '#' || c == '/' && l.at(i+1) == '/':
		return l.lineComment(i), false
	case c == '/' && l.at(i+1) == '*':
		return l.blockComment(i), false
	case c == '$' && isIdentStart(l.at(i+1)):
		e := l.identEnd(i + 1)
		l.emit(code.TokIdent, i, e)
		return e, false
	case (c == 'b' || c == 'B') && (l.at(i+1) == '\'' || l.at(i+1) == '"'):
		return l.quoted(i, i+1), false
	case isIdentStart(c):
		return l.word(i), false
	case isDigit(c) || c == '.' && isDigit(l.at(i+1)):
		return l.number(i), false
	case c == '\'' || c == '"' || c == '`':
		return l.quoted(i, i), false
	case c == '<' && l.at(i+1) == '<' && l.at(i+2) == '<':
		if e, ok := l.heredoc(i); ok {
			return e, false
		}
	case c == '(':
		if e, ok := l.cast(i); ok {
			l.emit(code.TokKeyword, i, e)
			return e, false
		}
	}
	for _, op := range operators {
		if i+len(op) <= l.end && string(l.src[i:i+len(op)]) == op {
			l.emit(code.TokPunct, i, i+len(op))
			l.haltAfter(i)
			return i + len(op), false
		}
	}
	_, size := utf8.DecodeRune(l.src[i:l.end])
	l.emit(code.TokPunct, i, i+size)
	l.haltAfter(i)
	return i + size, false
}

// haltAfter stops lexing at the `;` or close tag that ends a
// `__halt_compiler();` statement, because the bytes after it are data.
func (l *lexer) haltAfter(i int) {
	if c := l.src[i]; c != ';' && c != '?' {
		return
	}
	for k, back := len(l.toks)-2, 0; k >= 0 && back < 4; k-- {
		if l.toks[k].kind == code.TokComment {
			continue
		}
		if strings.EqualFold(string(l.src[l.toks[k].s:l.toks[k].e]), "__halt_compiler") {
			l.halted = true
			return
		}
		back++
	}
}

func (l *lexer) identEnd(i int) int {
	for i < l.end && isIdentChar(l.src[i]) {
		i++
	}
	return i
}

// lineComment lexes a `#` or `//` comment. It ends at the end of the line
// or before a close tag.
func (l *lexer) lineComment(i int) int {
	e := i
	for e < l.end && l.src[e] != '\n' && l.src[e] != '\r' && (l.src[e] != '?' || l.at(e+1) != '>') {
		e++
	}
	end := e
	for end > i && (l.src[end-1] == ' ' || l.src[end-1] == '\t') {
		end--
	}
	l.comment(code.LineComment, i, end)
	return e
}

// blockComment lexes `/* */` and `/** */` doc comments. A doc comment needs
// white space after `/**`, so `/**/` stays a plain block comment.
func (l *lexer) blockComment(i int) int {
	e := l.end
	if k := bytes.Index(l.src[i+2:l.end], []byte("*/")); k >= 0 {
		e = i + 2 + k + 2
	}
	kind := code.BlockComment
	if l.at(i+2) == '*' && isSpace(l.at(i+3)) {
		kind = code.DocComment
	}
	l.comment(kind, i, e)
	return e
}

// word lexes an identifier or keyword. Reserved words are plain names after
// `->`, `?->`, `::` (except `class`), `function` and `const`. `enum` is
// only a keyword if it declares an enum.
func (l *lexer) word(i int) int {
	e := l.identEnd(i)
	lower := strings.ToLower(string(l.src[i:e]))
	kind := code.TokIdent
	if keywords[lower] {
		kind = code.TokKeyword
	}
	switch p := l.prev(0); {
	case p == "->" || p == "?->":
		kind = code.TokIdent
	case p == "::" && lower != "class":
		kind = code.TokIdent
	case strings.EqualFold(p, "function") || strings.EqualFold(p, "const") || p == "&" && strings.EqualFold(l.prev(1), "function"):
		kind = code.TokIdent
	case lower == "enum" && l.enumDecl(e):
		kind = code.TokKeyword
	}
	if lower == "__halt_compiler" {
		kind = code.TokKeyword
	}
	l.emit(kind, i, e)
	return e
}

// enumDecl reports whether the `enum` that ends at e is followed by the
// name of the enum it declares.
func (l *lexer) enumDecl(e int) bool {
	k := e
	for k < l.end && isSpace(l.src[k]) {
		k++
	}
	if k == e || !isIdentStart(l.at(k)) {
		return false
	}
	w := strings.ToLower(string(l.src[k:l.identEnd(k)]))
	return w != "extends" && w != "implements"
}

func (l *lexer) number(i int) int {
	e := i
	if l.src[e] == '0' && e+1 < l.end && strings.IndexByte("xXbBoO", l.src[e+1]) >= 0 {
		e += 2
		for e < l.end && (isIdentChar(l.src[e])) {
			e++
		}
		l.emit(code.TokNumber, i, e)
		return e
	}
	for e < l.end && (isDigit(l.src[e]) || l.src[e] == '_') {
		e++
	}
	if l.at(e) == '.' && isDigit(l.at(e+1)) {
		e++
		for e < l.end && (isDigit(l.src[e]) || l.src[e] == '_') {
			e++
		}
	}
	if c := l.at(e); (c == 'e' || c == 'E') && (isDigit(l.at(e+1)) || (l.at(e+1) == '+' || l.at(e+1) == '-') && isDigit(l.at(e+2))) {
		e += 2
		for e < l.end && isDigit(l.src[e]) {
			e++
		}
	}
	l.emit(code.TokNumber, i, e)
	return e
}

// cast returns the end of a cast operator like `( int )` at i.
func (l *lexer) cast(i int) (int, bool) {
	k := i + 1
	for k < l.end && (l.src[k] == ' ' || l.src[k] == '\t') {
		k++
	}
	if !isIdentStart(l.at(k)) {
		return 0, false
	}
	e := l.identEnd(k)
	if !casts[strings.ToLower(string(l.src[k:e]))] {
		return 0, false
	}
	for e < l.end && (l.src[e] == ' ' || l.src[e] == '\t') {
		e++
	}
	if l.at(e) != ')' {
		return 0, false
	}
	return e + 1, true
}

// quoted lexes a string literal with its quote at q. The span starts at
// start, so it can include a `b` prefix. Single-quoted strings have no
// interpolation.
func (l *lexer) quoted(start, q int) int {
	quote := l.src[q]
	if quote == '\'' {
		e := q + 1
		for e < l.end && l.src[e] != '\'' {
			if l.src[e] == '\\' {
				e++
			}
			e++
		}
		e = min(e+1, l.end)
		l.emit(code.TokString, start, e)
		return e
	}
	fields, e := interpolation(l.src, q+1, l.end, quote)
	l.toks = append(l.toks, tok{kind: code.TokString, s: start, e: e, fields: fields})
	return e
}

// heredoc lexes `<<<ID`, `<<<"ID"` and the nowdoc `<<<'ID'` up to the
// closing identifier. Since PHP 7.3 that can be indented and followed by
// any character that can't continue the identifier.
func (l *lexer) heredoc(i int) (int, bool) {
	k := i + 3
	for k < l.end && (l.src[k] == ' ' || l.src[k] == '\t') {
		k++
	}
	var quote byte
	if c := l.at(k); c == '\'' || c == '"' {
		quote = c
		k++
	}
	if !isIdentStart(l.at(k)) {
		return 0, false
	}
	idEnd := l.identEnd(k)
	id := l.src[k:idEnd]
	k = idEnd
	if quote != 0 {
		if l.at(k) != quote {
			return 0, false
		}
		k++
	}
	if l.at(k) == '\r' {
		k++
	}
	if l.at(k) != '\n' {
		return 0, false
	}
	bodyStart := k + 1
	end, bodyEnd := l.end, l.end
	for line := bodyStart; line < l.end; {
		c := line
		for c < l.end && (l.src[c] == ' ' || l.src[c] == '\t') {
			c++
		}
		if bytes.HasPrefix(l.src[c:l.end], id) && !isIdentChar(l.at(c+len(id))) {
			end, bodyEnd = c+len(id), line
			break
		}
		nl := bytes.IndexByte(l.src[line:l.end], '\n')
		if nl < 0 {
			break
		}
		line += nl + 1
	}
	var fields []field
	if quote != '\'' {
		fields, _ = interpolation(l.src, bodyStart, bodyEnd, 0)
	}
	l.toks = append(l.toks, tok{kind: code.TokString, s: i, e: end, fields: fields})
	return end, true
}

// interpolation scans the body of an interpolating literal from i. It
// returns the fields and the offset after the closing quote. If quote is 0
// we scan to end, which is what a heredoc body needs.
func interpolation(src []byte, i, end int, quote byte) ([]field, int) {
	var fields []field
	at := func(k int) byte {
		if k < end {
			return src[k]
		}
		return 0
	}
	identEnd := func(k int) int {
		for k < end && isIdentChar(src[k]) {
			k++
		}
		return k
	}
	for i < end {
		c := src[i]
		switch {
		case c == '\\':
			i += 2
			continue
		case quote != 0 && c == quote:
			return fields, i + 1
		case c == '$' && isIdentStart(at(i+1)):
			e := identEnd(i + 1)
			switch {
			case at(e) == '[':
				fields = append(fields, field{s: i, e: e})
				k := e + 1
				if at(k) == '$' && isIdentStart(at(k+1)) {
					fields = append(fields, field{s: k, e: identEnd(k + 1)})
				}
				for k < end && src[k] != ']' && src[k] != quote && src[k] != '\n' {
					k++
				}
				if at(k) == ']' {
					k++
				}
				i = k
				continue
			case at(e) == '-' && at(e+1) == '>' && isIdentStart(at(e+2)):
				e = identEnd(e + 2)
			case at(e) == '?' && at(e+1) == '-' && at(e+2) == '>' && isIdentStart(at(e+3)):
				e = identEnd(e + 3)
			}
			fields = append(fields, field{s: i, e: e})
			i = e
			continue
		case c == '{' && at(i+1) == '$':
			k := closeBrace(src, i, end)
			fields = append(fields, field{s: i + 1, e: k})
			i = min(k+1, end)
			continue
		case c == '$' && at(i+1) == '{':
			k := closeBrace(src, i+1, end)
			inner := field{s: i + 2, e: k}
			if e := identEnd(i + 2); e == k && k > i+2 {
				inner.varName = true
			}
			fields = append(fields, inner)
			i = min(k+1, end)
			continue
		}
		i++
	}
	return fields, end
}

// closeBrace returns the offset of the `}` that matches the `{` at open, or
// end if there is none. We skip over quoted strings.
func closeBrace(src []byte, open, end int) int {
	depth := 0
	for k := open; k < end; k++ {
		switch src[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return k
			}
		case '\'', '"':
			q := src[k]
			for k++; k < end && src[k] != q; k++ {
				if src[k] == '\\' {
					k++
				}
			}
		}
	}
	return end
}
