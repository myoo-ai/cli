package prose

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/textpos"
)

// text is the linted input with a line table and the byte ranges of code,
// URLs, e-mail addresses and file paths, which rules shouldn't report on.
type text struct {
	src   string
	lines []int // byte offset where each line starts
	masks []span
	lang  string // --lang code, Lint sets it for the language-specific detectors
	lower string // src in lower case, see lowered
	// comment marks a code comment from LintComment, which gets the Comment
	// rules.
	comment bool
}

// lowered returns the text in lower case for literal pre-checks. The offsets
// can differ from src, so don't use them there.
func (t *text) lowered() string {
	if t.lower == "" {
		t.lower = strings.ToLower(t.src)
	}
	return t.lower
}

type span struct{ start, end int }

func newText(src string) *text {
	t := &text{src: src, lines: []int{0}}
	// myoo-ignore: go-range-over-int
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			t.lines = append(t.lines, i+1)
		}
	}
	t.masks = findMasks(src)
	return t
}

func (t *text) lineCount() int { return len(t.lines) }

// line returns the byte span of 1-based line n, without the "\r\n" or "\n".
func (t *text) line(n int) span {
	start, end := textpos.LineBounds(t.src, t.lines, n)
	return span{start, end}
}

// pos converts a byte offset to a 1-based line and a column in code points.
func (t *text) pos(off int) (line, col int) {
	line = sort.Search(len(t.lines), func(i int) bool { return t.lines[i] > off })
	return line, utf8.RuneCountInString(t.src[t.lines[line-1]:off]) + 1
}

func (t *text) masked(s span) bool {
	for _, m := range t.masks {
		if s.start < m.end && m.start < s.end {
			return true
		}
	}
	return false
}

var (
	urlRe   = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s<>"'` + "`" + `]+|\bwww\.[^\s<>"'` + "`" + `]+`)
	emailRe = regexp.MustCompile(`[\w.+-]+@[\w-]+(?:\.[\w-]+)+`)
	pathRe  = regexp.MustCompile(`(?:~|\.{1,2})?/[\w.*/-]+|\b[\w.-]+/[\w.*/-]*|\b[A-Za-z]:\\\S+`)
	// htmlCodeRe matches the <pre> and <code> elements that markdown
	// documents embed for code which needs attributes or a summary.
	htmlCodeRe = regexp.MustCompile(`(?is)<pre\b[^>]*>.*?</pre>|<code\b[^>]*>.*?</code>`)
)

// findMasks returns fenced code blocks, inline code spans, HTML <pre> and
// <code> elements, URLs, e-mail addresses and file paths. We leave chatbot
// citation artefacts inside URLs unmasked so chatbot-artifact can report
// them.
func findMasks(src string) []span {
	var masks []span
	inFence := false
	fenceStart := 0
	off := 0
	for line := range strings.SplitAfterSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if inFence {
				masks = append(masks, span{fenceStart, off + len(line)})
			} else {
				fenceStart = off
			}
			inFence = !inFence
		} else if !inFence {
			masks = append(masks, inlineCode(line, off)...)
		}
		off += len(line)
	}
	if inFence {
		masks = append(masks, span{fenceStart, len(src)})
	}
	for _, re := range []*regexp.Regexp{urlRe, emailRe, pathRe, htmlCodeRe} {
		for _, m := range re.FindAllStringIndex(src, -1) {
			masks = append(masks, withoutArtifacts(src, span{m[0], m[1]})...)
		}
	}
	return masks
}

// inlineCode finds `code` spans on a line. An unmatched backtick doesn't
// mask anything.
func inlineCode(line string, base int) []span {
	var out []span
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		fence := strings.Repeat("`", n)
		end := strings.Index(line[i+n:], fence)
		if end < 0 {
			break
		}
		closeAt := i + n + end + n
		out = append(out, span{base + i, base + closeAt})
		i = closeAt
	}
	return out
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '\u2019' || r == '-'
}

func countWords(s string) int {
	n := 0
	in := false
	for _, r := range s {
		w := unicode.IsLetter(r) || unicode.IsDigit(r)
		if w && !in {
			n++
		}
		in = w || (in && isWordRune(r))
	}
	return n
}

// sentenceBefore returns the part of the line before off that's in the same
// sentence.
func (t *text) sentenceBefore(off int) string {
	l, _ := t.pos(off)
	s := t.src[t.line(l).start:off]
	for i := len(s) - 1; i > 0; i-- {
		if (s[i-1] == '.' || s[i-1] == '!' || s[i-1] == '?') && (s[i] == ' ' || s[i] == '\t') {
			return s[i:]
		}
	}
	return s
}

// sentenceAfter returns the rest of the sentence after off, within the line.
func (t *text) sentenceAfter(off int) string {
	l, _ := t.pos(off)
	s := t.src[off:t.line(l).end]
	for i := 0; i < len(s); i++ {
		if (s[i] == '.' || s[i] == '!' || s[i] == '?') && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
			return s[:i]
		}
	}
	return s
}

// capitalizeAt returns the upper-cased first letter of the word at off, and
// the byte length of the letter it replaces.
func (t *text) capitalizeAt(off int) (string, int) {
	r, size := utf8.DecodeRuneInString(t.src[off:])
	return string(unicode.ToUpper(r)), size
}
