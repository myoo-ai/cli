package synonyms

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type token struct {
	text       string
	start, end int // byte offsets into the text
	line, col  int // 1-based, col counts code points
}

// tokenize returns the words of text we can look up. A word is a run of
// letters and combining marks, and it keeps apostrophes and hyphens between
// letters. We skip words inside backtick code spans, words touching digits
// or underscores, camelCase words and words in code-like chunks (URLs,
// paths, flags, assignments, file names), so a rewrite can't change code.
func tokenize(text string) []token {
	code := codeSpans(text)
	var tokens []token
	line, col := 1, 1
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) {
			if r == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
			i += size
			continue
		}
		start, startCol := i, col
		end, runes := wordEnd(text, i)
		i, col = end, col+runes
		if !inSpans(code, start) && !codeLike(text, start, end) {
			tokens = append(tokens, token{text: text[start:end], start: start, end: end, line: line, col: startCol})
		}
	}
	return tokens
}

// wordEnd returns the end offset and the rune count of the word that starts
// at i.
func wordEnd(text string, i int) (end, runes int) {
	for i < len(text) {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case unicode.IsLetter(r), unicode.IsMark(r):
		case isJoiner(r):
			next, _ := utf8.DecodeRuneInString(text[i+size:])
			if !unicode.IsLetter(next) {
				return i, runes
			}
		default:
			return i, runes
		}
		i += size
		runes++
	}
	return i, runes
}

func isJoiner(r rune) bool {
	return r == '\'' || r == '\u2019' || r == '-' || r == '\u2010'
}

func codeLike(text string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	if unicode.IsDigit(before) || unicode.IsDigit(after) || before == '_' || after == '_' {
		return true
	}
	prevLower := false
	for _, r := range text[start:end] {
		if unicode.IsUpper(r) && prevLower {
			return true
		}
		prevLower = unicode.IsLower(r)
	}
	chunkStart := 0
	if n := strings.LastIndexFunc(text[:start], unicode.IsSpace); n >= 0 {
		_, size := utf8.DecodeRuneInString(text[n:])
		chunkStart = n + size
	}
	chunkEnd := len(text)
	if n := strings.IndexFunc(text[end:], unicode.IsSpace); n >= 0 {
		chunkEnd = end + n
	}
	chunk := text[chunkStart:chunkEnd]
	if strings.Contains(chunk, "://") || strings.ContainsAny(chunk, "@_$=\\/<>{}|~#") || len(chunk) > 1 && chunk[0] == '-' {
		return true
	}
	prevDot := false
	for _, r := range chunk {
		if prevDot && unicode.IsLetter(r) {
			return true
		}
		prevDot = r == '.'
	}
	return false
}

type span struct{ start, end int }

func inSpans(spans []span, off int) bool {
	for _, s := range spans {
		if off >= s.start && off < s.end {
			return true
		}
	}
	return false
}

// codeSpans finds Markdown code. A run of n backticks opens a span that the
// next run of exactly n backticks closes (this covers fenced blocks too),
// and a line starting with ~~~ opens a block up to the next such line. We
// treat unmatched runs and unclosed ~~~ blocks as plain text.
func codeSpans(text string) []span {
	type run struct{ start, n int }
	var runs []run
	for i := 0; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(text) && text[j] == '`' {
			j++
		}
		runs = append(runs, run{i, j - i})
		i = j
	}
	var spans []span
	for i := 0; i < len(runs); i++ {
		for j := i + 1; j < len(runs); j++ {
			if runs[j].n == runs[i].n {
				spans = append(spans, span{runs[i].start, runs[j].start + runs[j].n})
				i = j
				break
			}
		}
	}
	return append(spans, tildeFences(text)...)
}

// tildeFences finds ~~~ fenced blocks, including the fence lines.
func tildeFences(text string) []span {
	var spans []span
	open := -1
	for off := 0; off < len(text); {
		end := strings.IndexByte(text[off:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += off + 1
		}
		if strings.HasPrefix(strings.TrimLeft(text[off:end], " "), "~~~") {
			if open < 0 {
				open = off
			} else {
				spans = append(spans, span{open, end})
				open = -1
			}
		}
		off = end
	}
	return spans
}
