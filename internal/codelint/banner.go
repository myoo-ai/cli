package codelint

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

var printNames = setOf("print", "println", "printf", "eprint", "eprintln", "puts", "echo", "log", "info", "debug",
	"warn", "warning", "error", "trace", "write", "writeln", "writeline", "fprintln", "fprintf", "fprint", "printline")

// fstringRepeatRe matches a repeated separator inside an f-string or
// template: f"{'=' * 60}".
var fstringRepeatRe = regexp.MustCompile(`\{\s*['"][=\-*#~_+]{1,3}['"]\s*\*\s*\d+\s*\}`)

// isBannerText reports whether c is a separator line of at least 8 repeated
// characters, or a titled banner ("===== Results =====").
func isBannerText(c string) bool {
	c = strings.TrimSpace(c)
	n := utf8.RuneCountInString(c)
	left, right := decoRun(c), decoRun(reverse(c))
	if left == n {
		return n >= 8
	}
	if left < 3 || right < 3 {
		return false
	}
	return strings.ContainsFunc(string([]rune(c)[left:n-right]), unicode.IsLetter)
}

// isSeparatorUnit reports whether c is one to three decoration characters
// that code would repeat into a separator ("=", "-=").
func isSeparatorUnit(c string) bool {
	n := utf8.RuneCountInString(c)
	return n >= 1 && n <= 3 && !strings.ContainsFunc(c, func(r rune) bool { return !isDecoRune(r) })
}

func checkBannerOutput(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokString {
			continue
		}
		text := s.text(t)
		content := stringContent(text)
		start, end := i, i
		switch {
		case isBannerText(content), fstringRepeatRe.MatchString(text):
		case isSeparatorUnit(content):
			var ok bool
			if start, end, ok = s.repetition(i); !ok || content == "#" && s.headingMarker(end) {
				continue
			}
		default:
			continue
		}
		if s.printed(start) {
			out = append(out, finding{span: code.Span{Start: s.code[start].Span.Start, End: s.code[end].Span.End}})
		}
	}
	return out
}

// headingMarker reports whether the "#" repetition ending at s.code[end] is
// followed by + and a string that starts with a space. That's the "## " that
// opens a markdown heading, so we don't want to flag it.
func (s *source) headingMarker(end int) bool {
	if end+2 >= len(s.code) || s.text(s.code[end+1]) != "+" || s.code[end+2].Kind != code.TokString {
		return false
	}
	return strings.HasPrefix(stringContent(s.text(s.code[end+2])), " ")
}

// repetition recognizes an expression that repeats the string at s.code[i]
// into a separator line. It returns the indices of its first and last token.
func (s *source) repetition(i int) (start, end int, ok bool) {
	longCount := func(k int) bool {
		if k >= len(s.code) {
			return false
		}
		if s.code[k].Kind == code.TokIdent {
			return true
		}
		n, err := strconv.Atoi(s.tok(k))
		return err == nil && n >= 8
	}
	switch {
	case s.tok(i+1) == "*" && longCount(i+2):
		return i, i + 2, true
	case s.tok(i+1) == "." && s.tok(i+2) == "repeat" && s.tok(i+3) == "(" && longCount(i+4):
		return i, min(s.matchClose(s.code, i+3), len(s.code)-1), true
	case s.tok(i-1) == "(" && s.tok(i-2) == "Repeat" && longCount(i+2):
		start := i - 2
		if s.tok(i-3) == "." {
			start = i - 4
		}
		return max(start, 0), min(s.matchClose(s.code, i-1), len(s.code)-1), true
	case s.tok(i-1) == "(" && s.tok(i-2) == "string" && s.tok(i-3) == "new" && longCount(i+2):
		return i - 3, min(s.matchClose(s.code, i-1), len(s.code)-1), true
	case s.tok(i-1) == "," && longCount(i-2) && s.tok(i-3) == "(" && s.tok(i-4) == "string":
		return i - 4, min(s.matchClose(s.code, i-3), len(s.code)-1), true
	case s.tok(i-1) == "(" && s.tok(i-2) == "str_repeat" && s.tok(i+1) == "," && longCount(i+2):
		return i - 2, min(s.matchClose(s.code, i-1), len(s.code)-1), true
	}
	return 0, 0, false
}

// printed reports whether the expression at s.code[i] is passed to a print
// or log call, streamed to std::cout/cerr, or echoed by PHP.
func (s *source) printed(i int) bool {
	depth, levels := 0, 0
	for j := i - 1; j >= 0; j-- {
		t := s.code[j]
		switch text := s.text(t); text {
		case ")", "]":
			depth++
		case "(", "[", "{":
			if depth > 0 {
				depth--
				break
			}
			if text != "(" || j == 0 {
				return false
			}
			if callee := s.code[j-1]; callee.Kind == code.TokIdent && printNames[strings.ToLower(s.text(callee))] {
				return true
			}
			if s.f.Lang == code.Rust && j > 1 && s.text(s.code[j-1]) == "!" && printNames[s.text(s.code[j-2])] {
				return true
			}
			if levels++; levels > 3 {
				return false
			}
		case "}", ";":
			if depth == 0 {
				return false
			}
		case "cout", "cerr", "clog":
			if depth == 0 {
				return true
			}
		case "echo", "print", "<?=":
			if depth == 0 && s.f.Lang == code.PHP {
				return true
			}
		}
		if depth == 0 && s.firstOnLine(j) {
			return false
		}
	}
	return false
}
