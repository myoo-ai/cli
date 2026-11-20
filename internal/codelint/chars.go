package codelint

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// textSpans returns all comments and docstrings, plus string literals outside
// test files, since test strings are usually input data.
func (s *source) textSpans() []code.Span {
	var out []code.Span
	for _, c := range s.comments {
		if !c.directive {
			out = append(out, c.Span)
		}
	}
	if !s.test {
		for _, t := range s.code {
			if t.Kind == code.TokString {
				out = append(out, t.Span)
			}
		}
	}
	return out
}

// scanRunes calls match for each rune in the checked spans. match returns
// the byte length of a hit that starts at i, or 0 if there's none.
func (s *source) scanRunes(match func(src []byte, i, end int) int) []finding {
	var out []finding
	for _, sp := range s.textSpans() {
		for i := sp.Start; i < sp.End; {
			if n := match(s.f.Src, i, sp.End); n > 0 {
				out = append(out, finding{span: code.Span{Start: i, End: i + n}})
				i += n
				continue
			}
			_, size := utf8.DecodeRune(s.f.Src[i:sp.End])
			i += size
		}
	}
	return out
}

// iconRunes are arrows, check marks, ballot boxes, stars and bullets that
// people use as icons.
var iconRunes = map[rune]bool{
	'\u2713': true, '\u2714': true, '\u2717': true, '\u2718': true, '\u2610': true, '\u2611': true, '\u2612': true,
	'\u2605': true, '\u2606': true, '\u2726': true, '\u2727': true, '\u2731': true, '\u276F': true, '\u276E': true,
	'\u2794': true, '\u279C': true, '\u27A1': true, '\u27A4': true, '\u2B95': true, '\u2B05': true, '\u2B06': true, '\u2B07': true,
	'\u2022': true, '\u25AA': true, '\u25AB': true, '\u25B8': true, '\u25B9': true, '\u25BA': true, '\u25B6': true,
	'\u25C0': true, '\u25C6': true, '\u25C7': true, '\u25E6': true, '\u25CF': true, '\u25CB': true, '\u25A0': true,
	'\u25A1': true, '\u2023': true, '\u2043': true, '\u25B2': true, '\u25BC': true,
}

// isIcon also counts the arrow and box-drawing blocks, and the symbols and
// dingbats at U+2600-U+27BF that aren't emoji.
func isIcon(r rune) bool {
	return iconRunes[r] || r >= 0x2190 && r <= 0x21FF || r >= 0x27F0 && r <= 0x27FF || r >= 0x2900 && r <= 0x297F || r >= 0x2500 && r <= 0x257F ||
		r >= 0x2600 && r <= 0x27BF && !unicode.Is(emojiBase, r)
}

// emojiBase lists the pictographic code points that can start an emoji.
// These are the Emoji=Yes code points of Unicode's emoji-data.txt outside
// ASCII. In the symbol and dingbat blocks only some code points are emoji. A
// fleuron or a heavy asterisk is a typographic glyph, so we leave those to
// isIcon.
var emojiBase = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x203C, Hi: 0x203C, Stride: 1}, {Lo: 0x2049, Hi: 0x2049, Stride: 1}, {Lo: 0x2139, Hi: 0x2139, Stride: 1},
		{Lo: 0x231A, Hi: 0x231B, Stride: 1}, {Lo: 0x23F0, Hi: 0x23F3, Stride: 3},
		{Lo: 0x2600, Hi: 0x2604, Stride: 1}, {Lo: 0x260E, Hi: 0x2611, Stride: 3}, {Lo: 0x2614, Hi: 0x2615, Stride: 1},
		{Lo: 0x2618, Hi: 0x261D, Stride: 5}, {Lo: 0x2620, Hi: 0x2620, Stride: 1}, {Lo: 0x2622, Hi: 0x2623, Stride: 1},
		{Lo: 0x2626, Hi: 0x262A, Stride: 4}, {Lo: 0x262E, Hi: 0x262F, Stride: 1}, {Lo: 0x2638, Hi: 0x263A, Stride: 1},
		{Lo: 0x2640, Hi: 0x2642, Stride: 2}, {Lo: 0x2648, Hi: 0x2653, Stride: 1}, {Lo: 0x265F, Hi: 0x2660, Stride: 1},
		{Lo: 0x2663, Hi: 0x2663, Stride: 1}, {Lo: 0x2665, Hi: 0x2666, Stride: 1}, {Lo: 0x2668, Hi: 0x2668, Stride: 1},
		{Lo: 0x267B, Hi: 0x267B, Stride: 1}, {Lo: 0x267E, Hi: 0x267F, Stride: 1}, {Lo: 0x2692, Hi: 0x2697, Stride: 1},
		{Lo: 0x2699, Hi: 0x2699, Stride: 1}, {Lo: 0x269B, Hi: 0x269C, Stride: 1}, {Lo: 0x26A0, Hi: 0x26A1, Stride: 1},
		{Lo: 0x26A7, Hi: 0x26A7, Stride: 1}, {Lo: 0x26AA, Hi: 0x26AB, Stride: 1}, {Lo: 0x26B0, Hi: 0x26B1, Stride: 1},
		{Lo: 0x26BD, Hi: 0x26BE, Stride: 1}, {Lo: 0x26C4, Hi: 0x26C5, Stride: 1}, {Lo: 0x26C8, Hi: 0x26C8, Stride: 1},
		{Lo: 0x26CE, Hi: 0x26CF, Stride: 1}, {Lo: 0x26D1, Hi: 0x26D1, Stride: 1}, {Lo: 0x26D3, Hi: 0x26D4, Stride: 1},
		{Lo: 0x26E9, Hi: 0x26EA, Stride: 1}, {Lo: 0x26F0, Hi: 0x26F5, Stride: 1}, {Lo: 0x26F7, Hi: 0x26FA, Stride: 1},
		{Lo: 0x26FD, Hi: 0x26FD, Stride: 1}, {Lo: 0x2702, Hi: 0x2705, Stride: 3}, {Lo: 0x2708, Hi: 0x270D, Stride: 1},
		{Lo: 0x270F, Hi: 0x270F, Stride: 1}, {Lo: 0x2712, Hi: 0x2716, Stride: 2}, {Lo: 0x271D, Hi: 0x2721, Stride: 4},
		{Lo: 0x2728, Hi: 0x2728, Stride: 1}, {Lo: 0x2733, Hi: 0x2734, Stride: 1}, {Lo: 0x2744, Hi: 0x2747, Stride: 3},
		{Lo: 0x274C, Hi: 0x274E, Stride: 2}, {Lo: 0x2753, Hi: 0x2755, Stride: 1}, {Lo: 0x2757, Hi: 0x2757, Stride: 1},
		{Lo: 0x2763, Hi: 0x2764, Stride: 1}, {Lo: 0x2795, Hi: 0x2797, Stride: 1}, {Lo: 0x27A1, Hi: 0x27A1, Stride: 1},
		{Lo: 0x27B0, Hi: 0x27BF, Stride: 15}, {Lo: 0x2B1B, Hi: 0x2B1C, Stride: 1}, {Lo: 0x2B50, Hi: 0x2B55, Stride: 5},
	},
	R32: []unicode.Range32{{Lo: 0x1F000, Hi: 0x1FAFF, Stride: 1}},
}

// emojiModifiers are the zero-width joiner, variation selector, skin tones,
// keycap and tag characters that continue an emoji sequence.
var emojiModifiers = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0x200D, Hi: 0x200D, Stride: 1}, {Lo: 0x20E3, Hi: 0x20E3, Stride: 1}, {Lo: 0xFE0F, Hi: 0xFE0F, Stride: 1}},
	R32: []unicode.Range32{{Lo: 0x1F3FB, Hi: 0x1F3FF, Stride: 1}, {Lo: 0xE0020, Hi: 0xE007F, Stride: 1}},
}

func isEmojiBase(r rune) bool { return unicode.Is(emojiBase, r) && !iconRunes[r] }

func isEmojiPart(r rune) bool { return isEmojiBase(r) || unicode.Is(emojiModifiers, r) }

func checkEmoji(s *source) []finding {
	return s.scanRunes(func(src []byte, i, end int) int {
		r, size := utf8.DecodeRune(src[i:end])
		if !isEmojiBase(r) || s.quotedAt(i) {
			return 0
		}
		n := size
		for i+n < end {
			r2, s2 := utf8.DecodeRune(src[i+n : end])
			if !isEmojiPart(r2) {
				break
			}
			n += s2
		}
		return n
	})
}

func checkIcons(s *source) []finding {
	return s.scanRunes(func(src []byte, i, end int) int {
		r, size := utf8.DecodeRune(src[i:end])
		switch {
		case r == '\u00B7':
			// A middle dot between spaces is a separator icon. Anywhere else
			// it's punctuation (Catalan l\u00B7l, unit products), and in quoted
			// code it's a product operator.
			if i > 0 && src[i-1] == ' ' && i+size < end && src[i+size] == ' ' && !s.quotedAt(i) {
				return size
			}
			return 0
		case !isIcon(r):
			return 0
		case s.keyboardLabelAt(i, end), s.glyphLiteralAt(i, end), s.infixArrowAt(i, size), s.quotedAt(i):
			return 0
		}
		n := size
		if r >= 0x2500 && r <= 0x257F {
			for i+n < end {
				r2, s2 := utf8.DecodeRune(src[i+n : end])
				if r2 < 0x2500 || r2 > 0x257F {
					break
				}
				n += s2
			}
		}
		return n
	})
}

// quotedAt reports whether off is in text that quotes data instead of
// decorating prose. That can be a regex literal, which has to match the
// character, or a comment's "..." or `...` span, {@code ...} span, <pre> or
// <code> element or tab-indented Go doc code block. Quotes can span the
// lines of a group of line comments.
func (s *source) quotedAt(off int) bool {
	ci := slices.IndexFunc(s.comments, func(c *comment) bool { return c.Span.Contains(off) })
	if ci < 0 {
		at := s.codeIndex(off) - 1
		return at >= 0 && (s.f.Lang == code.JavaScript || s.f.Lang == code.TypeScript) &&
			s.code[at].Kind == code.TokString && s.code[at].Span.Contains(off) && s.text(s.code[at])[0] == '/'
	}
	if s.goCodeBlockLine(s.line(off)) {
		return true
	}
	first := ci
	for first > 0 && s.comments[first].standalone && s.comments[first-1].standalone && s.comments[first-1].end == s.comments[first].line-1 {
		first--
	}
	before := s.f.Src[s.comments[first].Span.Start:off]
	if bytes.Count(before, []byte("`"))%2 == 1 || bytes.Count(before, []byte(`"`))%2 == 1 || insideCodeElement(before) {
		return true
	}
	open := bytes.LastIndex(before, []byte("{@code"))
	if open < 0 {
		return false
	}
	depth := 0
	for _, b := range before[open:] {
		switch b {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth > 0
}

var (
	codeOpenRe  = regexp.MustCompile(`(?i)<(?:pre|code)(?:\s[^>]*)?>`)
	codeCloseRe = regexp.MustCompile(`(?i)</(?:pre|code)>`)
)

// insideCodeElement reports whether the comment text before ends inside
// a <pre> or <code> element of a Javadoc, JSDoc or XML doc comment.
func insideCodeElement(before []byte) bool {
	opens := codeOpenRe.FindAllIndex(before, -1)
	if len(opens) == 0 {
		return false
	}
	closes := codeCloseRe.FindAllIndex(before, -1)
	return len(closes) == 0 || opens[len(opens)-1][0] > closes[len(closes)-1][0]
}

// goCodeBlockLine reports whether line is a "//\t" line of a Go comment.
// gofmt keeps those as a preformatted code block in the docs.
func (s *source) goCodeBlockLine(line int) bool {
	if s.f.Lang != code.Go {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(s.f.Text(s.f.LineSpan(line)), " \t"), "//\t")
}

// literalAt returns the content of the string literal that contains off and
// ends at end.
func (s *source) literalAt(off, end int) (string, bool) {
	at := s.codeIndex(off) - 1
	if at < 0 {
		return "", false
	}
	t := s.code[at]
	if t.Kind != code.TokString || t.Span.End != end || !t.Span.Contains(off) {
		return "", false
	}
	text := s.text(t)
	if len(text) < 2 || text[0] != text[len(text)-1] {
		return "", false
	}
	return text[1 : len(text)-1], true
}

func (s *source) keyboardLabelAt(off, end int) bool {
	label, ok := s.literalAt(off, end)
	return ok && keyboardLabel(label)
}

// glyphLiteralAt reports whether off is in a string literal made only of
// shapes, stars, arrows, bullets or dingbats. Such a literal is a glyph the
// program draws or maps to, like a play button, not an icon decorating text.
// Check marks and ballot boxes mean a status even on their own, so they don't count.
func (s *source) glyphLiteralAt(off, end int) bool {
	text, ok := s.literalAt(off, end)
	if !ok {
		return false
	}
	glyph := false
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
		case statusMarks[r]:
			return false
		case r >= 0x25A0 && r <= 0x25FF, isIcon(r):
			glyph = true
		default:
			return false
		}
	}
	return glyph
}

var statusMarks = map[rune]bool{'\u2713': true, '\u2714': true, '\u2717': true, '\u2718': true, '\u2610': true, '\u2611': true, '\u2612': true}

func isArrow(r rune) bool {
	return r >= 0x2190 && r <= 0x21FF || r >= 0x27F0 && r <= 0x27FF || r >= 0x2900 && r <= 0x297F ||
		r == '\u2794' || r == '\u279C' || r == '\u27A1' || r == '\u27A4' || r == '\u2B95' || r >= 0x2B05 && r <= 0x2B07
}

var (
	placeholderEndRe   = regexp.MustCompile(`(?:\}|%[-+ #0-9.]*[a-zA-Z])$`)
	placeholderStartRe = regexp.MustCompile(`^(?:\{|\$\{|%)`)
)

// infixArrowAt reports whether off is an arrow between two placeholders of
// a string literal, as in "{from} → {to}", `${a} ↔ ${b}` or "%s → %s".
// There the arrow shows the user how two values relate. Between words
// ("ready → ship") it stands in for prose, so it's still an icon.
func (s *source) infixArrowAt(off, size int) bool {
	if r, _ := utf8.DecodeRune(s.f.Src[off:]); !isArrow(r) {
		return false
	}
	at := s.codeIndex(off) - 1
	if at < 0 || s.code[at].Kind != code.TokString || !s.code[at].Span.Contains(off) {
		return false
	}
	sp := s.code[at].Span
	before := strings.TrimSuffix(string(s.f.Src[sp.Start:off]), " ")
	after := strings.TrimPrefix(string(s.f.Src[off+size:sp.End]), " ")
	return placeholderEndRe.MatchString(before) && placeholderStartRe.MatchString(after)
}

// keyboardLabel reports whether label is a keyboard legend. Those are
// functional UI labels, so their arrows aren't decorative icons.
func keyboardLabel(label string) bool {
	label = strings.TrimSpace(label)
	hasGlyph := false
	for len(label) > 0 {
		r, size := utf8.DecodeRuneInString(label)
		if !strings.ContainsRune("\u2318\u2325\u2303\u21E7\u21EA\u21E5\u21E4\u238B\u232B\u2326\u21A9\u21B5\u2191\u2193\u2190\u2192", r) {
			break
		}
		hasGlyph = true
		label = strings.TrimLeft(label[size:], " +")
	}
	if !hasGlyph {
		return false
	}
	if label == "" {
		return true
	}
	if r, size := utf8.DecodeRuneInString(label); size == len(label) && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
		return true
	}
	switch label {
	case "Tab", "Enter", "Return", "Escape", "Esc", "Delete", "Backspace", "Home", "End", "Page Up", "Page Down":
		return true
	}
	if len(label) < 2 || len(label) > 3 || label[0] != 'F' {
		return false
	}
	n := 0
	for _, r := range label[1:] {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return n >= 1 && n <= 24
}

func checkEmdash(s *source) []finding {
	return s.scanRunes(func(src []byte, i, end int) int {
		r, size := utf8.DecodeRune(src[i:end])
		switch {
		case r == '\u2014' || r == '\u2015':
			return size
		case r == '\u2013' && i > 0 && src[i-1] == ' ' && i+size < end && src[i+size] == ' ':
			return size
		}
		return 0
	})
}
