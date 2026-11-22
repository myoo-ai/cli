package prose

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// exceptProperNouns drops findings of detect that start with a capitalized
// word in the middle of a sentence. That word is the name of a product or a
// place ("the Minecraft Realms client"), not the AI word it spells. A table
// cell starts a sentence of its own.
func exceptProperNouns(detect func(t *text) []Violation) func(t *text) []Violation {
	return func(t *text) []Violation {
		return slices.DeleteFunc(detect(t), func(v Violation) bool {
			first, size := utf8.DecodeRuneInString(t.src[v.Start:v.End])
			second, _ := utf8.DecodeRuneInString(t.src[v.Start+size : v.End])
			before := t.sentenceBefore(v.Start)
			if i := strings.LastIndexByte(before, '|'); i >= 0 {
				before = before[i+1:]
			}
			return unicode.IsUpper(first) && unicode.IsLower(second) && countWords(before) > 0
		})
	}
}

// We keep the rules for markdown layout tics, chatbot residue, invisible
// characters, locale typography and stock English sentence patterns out of
// rules.go so it stays readable. The words of other languages live in
// lang.go.

// langsOf returns the sorted keys of a per-language table, for Rule.Langs.
func langsOf[V any](byLang map[string]V) []string {
	return slices.Sorted(maps.Keys(byLang))
}

// withoutOverlap drops findings of detect that overlap a match of other, so
// a phrase like "rich tapestry" only gets reported by a single rule.
func withoutOverlap(detect func(t *text) []Violation, other []phrase) func(t *text) []Violation {
	taken := phraseDetector(other)
	return func(t *text) []Violation {
		ts := taken(t)
		return slices.DeleteFunc(detect(t), func(v Violation) bool {
			for _, o := range ts {
				if v.Start < o.End && o.Start < v.End {
					return true
				}
			}
			return false
		})
	}
}

// exampleIntroRe matches "Here is an example ...:" and similar intros to a
// sample. Human docs use those as often as chatbots do.
var exampleIntroRe = regexp.MustCompile(`(?i)^here\b.*\b(?:example|sample|snippet|excerpt|demo|screenshot|output)s?\b`)

func quietWhen(detect func(t *text) []Violation, re *regexp.Regexp) func(t *text) []Violation {
	return func(t *text) []Violation {
		return slices.DeleteFunc(detect(t), func(v Violation) bool { return re.MatchString(v.Detail) })
	}
}

var significancePuffery = words(
	`(?:stands?|stood|standing|serves?|served|serving) as (?:a |an )?(?:lasting |enduring |powerful |poignant |stark )?(?:testament|reminder|symbol)(?: to| of)?`,
	`(?:stands?|stood|standing) as (?:a|an)`, `a testament to`,
	`(?:plays?|played|playing) (?:a|an) (?:crucial|pivotal|vital|key|significant) role`,
	`(?:enduring|lasting) legacy`, `indelible mark`, `deeply rooted`, `(?:ever-)?evolving landscape`,
	`(?:set|sets|setting) the stage for`, `reflects? (?:a )?broader`, `focal point`,
	`(?:this|that|it) matters because`, `(?:here['\x{2019}]?s )?why (?:this|that|it) matters`, `(?:this|that) (?:really |also )?matters`,
	`what makes (?:this|that|it) (?:so )?(?:significant|important|notable|remarkable)`, `the important (?:part|thing|bit) (?:is|here)`,
	`without (?:sacrificing|compromising)`,
	`rich (?:cultural )?(?:heritage|history|tapestry)`, `in the heart of`, `nestled`,
)

// artifactRe matches leaked chatbot citation markers and unfilled template
// placeholders. They don't depend on the language and often sit inside
// URLs, so findMasks has to leave them visible.
var artifactRe = regexp.MustCompile(`:?contentReference\[oaicite:\d+\](?:\{index=\d+\})?|oaicite(?::\d+)?|\bturn\d+(?:search|news|view)\d+\b|utm_source=(?:chatgpt\.com|openai)\b|\bINSERT_[A-Z][A-Z_]*\b|\b20\d\d-[xX]{2}-[xX]{2}\b`)

// artifactAnchors holds a lower-case literal for each artifact, so we can
// skip the expression on text that has none.
var artifactAnchors = phrase{anchors: []string{"oaicite", "turn", "utm_source=", "insert_", "-xx-"}}

func detectArtifacts(t *text) []Violation {
	if !artifactAnchors.scans(t) {
		return nil
	}
	var out []Violation
	for _, m := range artifactRe.FindAllStringIndex(t.src, -1) {
		s, e := m[0], m[1]
		v := Violation{Start: s, End: e, Detail: t.src[s:e]}
		v.Fix = artifactFix(t.src, s, e)
		out = append(out, v)
	}
	return out
}

// artifactFix deletes a citation marker together with the URL query
// separator it would leave dangling. Placeholders get no fix because we
// can't know the right value.
func artifactFix(src string, s, e int) []Edit {
	tok := src[s:e]
	switch {
	case strings.HasPrefix(tok, "INSERT_") || strings.HasPrefix(tok, "20"):
		return nil
	case strings.HasPrefix(tok, "utm_source="):
		if s > 0 && src[s-1] == '&' {
			return []Edit{{s - 1, e, ""}}
		}
		if e < len(src) && src[e] == '&' {
			return []Edit{{s, e + 1, ""}}
		}
		if s > 0 && src[s-1] == '?' {
			return []Edit{{s - 1, e, ""}}
		}
		return nil
	}
	if s > 0 && src[s-1] == ' ' {
		s--
	}
	return []Edit{{s, e, ""}}
}

// withoutArtifacts splits a URL or path mask around artifactRe matches.
func withoutArtifacts(src string, s span) []span {
	var out []span
	start := s.start
	for _, m := range artifactRe.FindAllStringIndex(src[s.start:s.end], -1) {
		if s.start+m[0] > start {
			out = append(out, span{start, s.start + m[0]})
		}
		start = s.start + m[1]
	}
	if start < s.end {
		out = append(out, span{start, s.end})
	}
	return out
}

// invisibleNames names the characters invisible-characters reports. We put
// the name in the detail because the character itself doesn't show.
var invisibleNames = map[rune]string{
	'\u200B': "U+200B zero-width space", '\u200C': "U+200C zero-width non-joiner",
	'\u200D': "U+200D zero-width joiner", '\u2060': "U+2060 word joiner",
	'\uFEFF': "U+FEFF byte order mark", '\u00AD': "U+00AD soft hyphen",
	'\u202F': "U+202F narrow no-break space",
}

func detectInvisible(t *text) []Violation {
	var out []Violation
	for i, r := range t.src {
		name, ok := invisibleNames[r]
		if !ok {
			continue
		}
		size := utf8.RuneLen(r)
		rep := ""
		switch r {
		case '\uFEFF':
			if i == 0 {
				continue // a leading byte order mark marks the encoding, it's fine
			}
		case '\u202F':
			if t.lang == "fr" {
				continue // French typography puts it before ; : ! ? and inside « »
			}
			rep = " "
		case '\u200C', '\u200D':
			if joinsScript(t.src, i, i+size) {
				continue
			}
		}
		out = append(out, Violation{Start: i, End: i + size, Detail: name, Fix: []Edit{{i, i + size, rep}}})
	}
	return out
}

// joinsScript reports whether the joiner between s and e belongs there: as
// part of an emoji sequence, or to shape a script like Persian or Devanagari
// that needs it.
func joinsScript(src string, s, e int) bool {
	prev, _ := utf8.DecodeLastRuneInString(src[:s])
	next, _ := utf8.DecodeRuneInString(src[e:])
	for _, r := range []rune{prev, next} {
		if isEmojiPart(r) || unicode.IsLetter(r) && !unicode.In(r, unicode.Latin, unicode.Greek, unicode.Cyrillic) {
			return true
		}
	}
	return false
}

// localeQuotePairs maps languages whose primary double quotes are `„…“`,
// `„…”` or `«…»` to their opening and closing quote.
var localeQuotePairs = map[string][2]string{
	"de": {"„", "“"}, "cs": {"„", "“"}, "sk": {"„", "“"}, "bg": {"„", "“"}, "lt": {"„", "“"},
	"lv": {"„", "“"}, "et": {"„", "“"}, "sl": {"„", "“"}, "sr": {"„", "“"},
	"pl": {"„", "”"}, "hu": {"„", "”"}, "ro": {"„", "”"}, "hr": {"„", "”"},
	"ru": {"«", "»"}, "uk": {"«", "»"},
}

// detectLocaleQuotes flags `“` used as an opening quote. In German or Czech
// the same character closes `„…“`, so we only count an opener (after a
// space or line start, before a non-space).
func detectLocaleQuotes(t *text) []Violation {
	pair := localeQuotePairs[t.lang]
	var out []Violation
	for i := 0; i < len(t.src); {
		r, size := utf8.DecodeRuneInString(t.src[i:])
		if r != '\u201C' || !opensQuote(t.src, i, i+size) {
			i += size
			continue
		}
		v := Violation{Start: i, End: i + size}
		if c := closingQuote(t.src, i+size); c >= 0 {
			v.Fix = []Edit{{i, i + size, pair[0]}, {c, c + len("\u201D"), pair[1]}}
		}
		out = append(out, v)
		i += size
	}
	return out
}

func opensQuote(src string, s, e int) bool {
	prev, _ := utf8.DecodeLastRuneInString(src[:s])
	next, size := utf8.DecodeRuneInString(src[e:])
	if size == 0 || unicode.IsSpace(next) {
		return false
	}
	return s == 0 || unicode.IsSpace(prev) || strings.ContainsRune("([{/-\u2013\u2014", prev)
}

// closingQuote returns the offset of the `”` that closes a quote opened
// before from. It returns -1 if the paragraph ends or another quote opens
// first.
func closingQuote(src string, from int) int {
	for i, r := range src[from:] {
		switch {
		case r == '\u201D':
			return from + i
		case r == '\u201C' || strings.HasPrefix(src[from+i:], "\n\n") || strings.HasPrefix(src[from+i:], "\n\r\n"):
			return -1
		}
	}
	return -1
}

// inlineHeaderRe matches a line that opens with a bold label, optionally
// after a list marker: "- **Speed:** ...", "1. __Speed__ - ...".
var inlineHeaderRe = regexp.MustCompile(`^[ \t]*(?:(?:[-*+]|\d{1,3}[.)])[ \t]+)?(\*\*[^*\s][^*\n]{0,78}?\*\*|__[^_\s][^_\n]{0,78}?__)([ \t]*[:\x{2013}\x{2014}-]?)`)

// admonitionRe matches labels like "**Note:**" that human docs use to flag
// a single remark, not to structure a list.
var admonitionRe = regexp.MustCompile(`(?i)^(?:note|notes|warning|important|tip|caution|hint|info|deprecated|update|edit|tl;dr|example|nb|n\.b\.)\s*:?$`)

// lineLabel returns the byte span of the bold label that opens the line ls
// if a colon or a dash follows it. Otherwise ok is false.
func (t *text) lineLabel(ls span) (s span, ok bool) {
	line := t.src[ls.start:ls.end]
	m := inlineHeaderRe.FindStringSubmatchIndex(line)
	if m == nil {
		return span{}, false
	}
	bold := line[m[2]:m[3]]
	sep := strings.TrimSpace(line[m[4]:m[5]])
	labelled := strings.HasSuffix(strings.TrimSpace(bold[2:len(bold)-2]), ":")
	if sep == ":" {
		labelled = true
	} else if sep != "" {
		// A dash has to be followed by a space, so "**a**-b" isn't a label.
		labelled = m[5] < len(line) && line[m[5]] == ' '
	}
	s = span{ls.start + m[2], ls.start + m[3]}
	return s, labelled && !t.masked(s)
}

// inlineHeader is lineLabel limited to the kind of label AI prose writes: a
// capitalized phrase of three letters or more, followed by prose or by
// nothing. We skip admonitions ("**Note:**"), changelog scopes
// ("**build**: [Fix x](url)"), Q/A markers, and labels followed by one or
// two words, since those are a key/value table written as a list
// ("- **A**: X").
func (t *text) inlineHeader(ls span) (span, bool) {
	s, ok := t.lineLabel(ls)
	if !ok {
		return s, false
	}
	inner := strings.TrimSuffix(strings.TrimSpace(t.src[s.start+2:s.end-2]), ":")
	rest := strings.TrimLeft(t.src[s.end:ls.end], " \t:-\u2013\u2014")
	first, _ := utf8.DecodeRuneInString(inner)
	switch {
	case admonitionRe.MatchString(inner), utf8.RuneCountInString(inner) < 3, strings.Contains(inner, ":"):
		return s, false
	case unicode.IsLower(first) || !unicode.IsLetter(first) && !unicode.IsDigit(first):
		return s, false
	case strings.HasPrefix(rest, "["), countWords(rest) == 1, countWords(rest) == 2:
		return s, false
	}
	return s, true
}

// detectInlineHeaderList flags lists whose items open with a bold label. A
// single bold label is normal, so we need at least two such lines. They also
// have to make up 3 % or more of the non-blank lines, because long human
// references like option lists have a few labelled lists in thousands of
// lines.
func detectInlineHeaderList(t *text) []Violation {
	var out []Violation
	nonBlank := 0
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		if strings.TrimSpace(t.src[ls.start:ls.end]) != "" {
			nonBlank++
		}
		if s, ok := t.inlineHeader(ls); ok {
			out = append(out, Violation{Start: s.start, End: s.end})
		}
	}
	if len(out) < 2 || len(out)*100 < nonBlank*3 {
		return nil
	}
	return out
}

var (
	atxHeadingRe = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	setextLineRe = regexp.MustCompile(`^ {0,3}(?:=+|-+)[ \t]*$`)
	listStartRe  = regexp.MustCompile(`^[ \t]*(?:[-*+>|]|\d{1,3}[.)])(?:[ \t]|$)`)
	headWordRe   = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}'\x{2019}.-]*`)
)

// headings returns the text span of all ATX headings, and of setext headings
// that are a single line after a blank line.
func (t *text) headings() []span {
	var out []span
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		line := t.src[ls.start:ls.end]
		if t.masked(span{ls.start, ls.start + 1}) {
			continue
		}
		if m := atxHeadingRe.FindStringSubmatchIndex(line); m != nil {
			out = append(out, span{ls.start + m[2], ls.start + m[3]})
			continue
		}
		if n == t.lineCount() || strings.TrimSpace(line) == "" || setextLineRe.MatchString(line) || listStartRe.MatchString(line) {
			continue
		}
		next := t.line(n + 1)
		if !setextLineRe.MatchString(t.src[next.start:next.end]) {
			continue
		}
		if prev := t.line(max(1, n-1)); n > 1 && strings.TrimSpace(t.src[prev.start:prev.end]) != "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		s := ls.start + strings.Index(line, trimmed)
		out = append(out, span{s, s + len(trimmed)})
	}
	return out
}

// minorWords stay lowercase in Title Case, so a lowercase minor word doesn't
// tell us the heading is in sentence case. Words of up to three letters
// count as minor in all languages.
var minorWords = map[string]bool{
	"with": true, "from": true, "into": true, "onto": true, "over": true, "than": true, "that": true,
	"this": true, "upon": true, "like": true, "near": true, "past": true, "amid": true, "also": true,
	"your": true, "their": true, "versus": true, "pour": true, "avec": true, "dans": true, "sans": true,
	"sous": true, "para": true, "sobre": true, "entre": true, "como": true, "desde": true, "hacia": true,
	"della": true, "dello": true, "degli": true, "delle": true, "pelo": true, "pela": true, "voor": true,
	"naar": true, "door": true,
}

// detectTitleCaseHeading flags headings in Title Case ("Getting Started
// With The Tool"). Words the body capitalizes mid-sentence are proper nouns
// and don't count, and neither do acronyms or the word after a colon. German
// capitalizes all nouns, so we skip it.
func detectTitleCaseHeading(t *text) []Violation {
	if t.lang == "de" {
		return nil
	}
	heads := t.headings()
	proper := t.properNouns(heads)
	var out []Violation
	for _, h := range heads {
		if v, ok := t.titleCase(h, proper); ok {
			out = append(out, v)
		}
	}
	return out
}

// titleCase checks a heading and returns its violation, with a fix that
// lowercases the Title Case words. Numbers ("4. Permanent Ban") aren't words
// here, so section numbering can't make a heading long enough to count.
func (t *text) titleCase(h span, proper map[string]bool) (Violation, bool) {
	var fix []Edit
	total, lowerContent := 0, 0
	prevEnd := h.start
	for _, m := range headWordRe.FindAllStringIndex(t.src[h.start:h.end], -1) {
		s, e := h.start+m[0], h.start+m[1]
		w := t.src[s:e]
		first, size := utf8.DecodeRuneInString(w)
		if t.masked(span{s, e}) || !unicode.IsLetter(first) {
			continue
		}
		fresh := total == 0 || strings.ContainsAny(t.src[prevEnd:s], ":.!?")
		total++
		prevEnd = e
		switch {
		case fresh:
		case unicode.IsUpper(first):
			if strings.IndexFunc(w[size:], unicode.IsUpper) < 0 && !proper[nounKey(w)] {
				fix = append(fix, Edit{s, s + size, string(unicode.ToLower(first))})
			}
		case utf8.RuneCountInString(w) > 3 && !minorWords[strings.ToLower(w)]:
			lowerContent++
		}
	}
	if total < 3 || len(fix) < 2 || lowerContent > 0 {
		return Violation{}, false
	}
	return Violation{Start: h.start, End: h.end, Fix: fix}, true
}

// nounKey strips a possessive so "Docker's" and "Docker" match.
func nounKey(w string) string {
	w = strings.TrimRight(w, ".-")
	for _, suf := range []string{"'s", "\u2019s"} {
		w = strings.TrimSuffix(w, suf)
	}
	return w
}

// properNouns collects the words the body capitalizes in the middle of a
// sentence outside headings and masked code.
func (t *text) properNouns(heads []span) map[string]bool {
	isHead := map[int]bool{}
	for _, h := range heads {
		l, _ := t.pos(h.start)
		isHead[l] = true
	}
	out := map[string]bool{}
	for n := 1; n <= t.lineCount(); n++ {
		if isHead[n] {
			continue
		}
		ls := t.line(n)
		line := t.src[ls.start:ls.end]
		for _, m := range headWordRe.FindAllStringIndex(line, -1) {
			first, _ := utf8.DecodeRuneInString(line[m[0]:])
			if !unicode.IsUpper(first) || t.masked(span{ls.start + m[0], ls.start + m[1]}) {
				continue
			}
			before := strings.TrimRight(line[:m[0]], " \t*_[")
			prev, _ := utf8.DecodeLastRuneInString(before)
			if before != "" && (unicode.IsLetter(prev) || unicode.IsDigit(prev) || strings.ContainsRune(",;('\"/&\u201C\u201E", prev)) {
				out[nounKey(line[m[0]:m[1]])] = true
			}
		}
	}
	return out
}

// sectionNumberRe matches a section number that opens a heading: "1.",
// "2)", "7.1", "7.1.", also with the backslash a converter puts before the
// dot ("1\."). A title word has to follow, so a changelog heading such as
// "## 1.2.3" or "## 1.2.3 (2024-05-01)" doesn't count.
var sectionNumberRe = regexp.MustCompile(`^(\d{1,3}(?:\\?\.\d{1,3})*\\?[.)]|\d{1,3}(?:\\?\.\d{1,3})+)[ \t]+\pL`)

// detectNumberedHeadings flags headings that open with a section number.
// We don't fix them, because the text may refer to the sections by number.
func detectNumberedHeadings(t *text) []Violation {
	var out []Violation
	for _, h := range t.headings() {
		if m := sectionNumberRe.FindStringSubmatchIndex(t.src[h.start:h.end]); m != nil {
			s, e := h.start+m[2], h.start+m[3]
			out = append(out, Violation{Start: s, End: e, Detail: t.src[s:e]})
		}
	}
	return out
}

var (
	headingMarkRe = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+`)
	listNumberRe  = regexp.MustCompile(`^[ \t>]*\d{1,9}$`)
	entityRestRe  = regexp.MustCompile(`^&#?\w+;`)
)

// detectBackslashEscapes flags backslashes before punctuation that markdown
// would print as is: "1\." in a heading, "A \= B", "\~50 %". It leaves the
// escapes that change how a line renders, such as a "-", "+" or "1." that
// would open a list item, "\!" before a link, "\&" before an entity, a dot
// between letters that would turn into a link ("rsync\.net"), and the
// characters that format text anywhere (`*`, `_`, backticks, brackets, `<`,
// `|`, `$`).
func detectBackslashEscapes(t *text) []Violation {
	if !strings.Contains(t.src, `\`) {
		return nil
	}
	var out []Violation
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		line := t.src[ls.start:ls.end]
		from := 0
		if m := headingMarkRe.FindStringIndex(line); m != nil {
			from = m[1]
		}
		for i := from; i+1 < len(line); i++ {
			if line[i] != '\\' {
				continue
			}
			if line[i+1] == '\\' {
				i++ // an escaped backslash
				continue
			}
			if !strings.ContainsRune(`=+-.!~:;,?@'"/%&^`, rune(line[i+1])) || neededEscape(line, from, i) {
				continue
			}
			s := ls.start + i
			out = append(out, Violation{Start: s, End: s + 2, Detail: line[i : i+2], Fix: []Edit{{s, s + 1, ""}}})
		}
	}
	return out
}

// neededEscape reports whether the backslash at line[i] keeps markdown from
// formatting the character after it. A heading's text starts at from, and
// it can't open a list item.
func neededEscape(line string, from, i int) bool {
	before := line[from:i]
	opens := from == 0 && strings.TrimLeft(before, " \t>") == ""
	rest := line[i+1:]
	switch rest[0] {
	case '-', '+':
		return opens
	case '.':
		prev, _ := utf8.DecodeLastRuneInString(before)
		next, _ := utf8.DecodeRuneInString(rest[1:])
		return from == 0 && listNumberRe.MatchString(before) || isWordRune(prev) && unicode.IsLetter(next)
	case '!':
		return strings.HasPrefix(rest[1:], "[")
	case '&':
		return entityRestRe.MatchString(rest)
	case '=':
		return opens && strings.Trim(rest, "= \t") == ""
	}
	return false
}

// sentenceStart matches where a sentence starts. That's the start of the
// text or of a line, or the space after a full stop or a colon, followed by
// indentation and list, quote or heading markers. We match the phrases
// after it case-sensitively, so a capital letter confirms the sentence
// really starts there.
const sentenceStart = `(?:^|[.!?:]["')\]]?\s+|\n)[ \t>*+#-]*`

// copulaAvoidance lists the verbs AI prose uses instead of "is" or "has".
// We leave out "acts as", because a proxy or a cache does act as something,
// and "served as", which docs use for files a server sends ("a binary
// served as a static file"). The base form after a modal ("will also serve
// as a cache") reads naturally, so it's out too. "functions as" only counts
// after a subject or an adverb, since "passes functions as arguments" uses
// the noun.
var copulaAvoidance = []phrase{
	{re: regexp.MustCompile(`(?i)\b((?:serves|serving) as (?:a|an|the|its|their|our|your|both|one)\b)`), anchors: []string{" as "}},
	{re: regexp.MustCompile(`(?i)\b(?:it|this|that|which|also|effectively|essentially|now|still|thus|primarily|mainly) (functions as (?:a|an|the)\b)`), anchors: []string{"functions as"}},
	{re: regexp.MustCompile(`(?i)\b(boast(?:s|ed|ing)? (?:a|an|the|its|their|over|more|nearly|around|several|many|some|dozens|hundreds|thousands|millions|one|two|three|four|five|six|seven|eight|nine|ten|\d+)\b)`), anchors: []string{"boast"}},
	{re: regexp.MustCompile(`(?i)\b((?:stands?|stood|standing) as (?:the|one)\b)`), anchors: []string{" as "}},
}

// nominalization only matches light verbs that have no other job in front
// of an action noun and "of". "provides an implementation of" and "makes a
// copy of" are common and fine in API docs.
var nominalization = []phrase{
	{re: regexp.MustCompile(`(?i)\b((?:perform|performs|performed|performing|carry out|carries out|carried out|carrying out|conduct|conducts|conducted|conducting) (?:an? |the |some )?[a-z]+(?:tion|sion|ment|ance|ence|sis) of)\b`), anchors: []string{"perform", "carr", "conduct"}},
}

// passiveBeforeRe matches a form of "be" or "get" right before a phrase.
// "is served as a download" means serving files, so it's no copula.
var passiveBeforeRe = regexp.MustCompile(`(?i)\b(?:is|are|was|were|be|been|being|gets?|got|getting)(?:\s+\w+ly)?\s+$`)

// detectCopula reports copulaAvoidance, except inside the
// significance-puffery phrases ("serves as a testament") and in the passive
// voice.
func detectCopula(t *text) []Violation {
	detect := withoutOverlap(phraseDetector(copulaAvoidance), significancePuffery)
	return slices.DeleteFunc(detect(t), func(v Violation) bool {
		return passiveBeforeRe.MatchString(t.src[max(0, v.Start-40):v.Start])
	})
}

// metaPreamble leaves out "This document describes", which specs use to
// scope themselves. "provides" only counts before "an overview" and the
// like, because "This section provides position information" in a file
// format describes a section of the file.
var metaPreamble = []phrase{
	{re: regexp.MustCompile(sentenceStart + `(This (?:document|guide|section|page|article|tutorial|README|chapter|post) (?:explains|covers|outlines|provides (?:an? )?(?:overview|introduction|summary|walkthrough|guide)|walks you through|shows you|introduces|summari[sz]es|will (?:explain|describe|cover|show|walk|guide|outline|teach|help|introduce|go))\b)`), anchors: []string{"this "}},
	{re: regexp.MustCompile(sentenceStart + `(In this (?:section|guide|document|article|tutorial|README|chapter|post),? (?:we|you)(?:['\x{2019}]ll| will| are going to)\b)`), anchors: []string{"in this "}},
	{re: regexp.MustCompile(sentenceStart + `(Below,? you(?:['\x{2019}]ll| will) find\b)`), anchors: []string{"below"}},
	{re: regexp.MustCompile(sentenceStart + `(The following (?:section|sections|chapter|chapters) (?:explains?|covers?|outlines?)\b)`), anchors: []string{"the following "}},
	{re: regexp.MustCompile(sentenceStart + `((?:Details|More details|More on (?:that|this)|Answers|Explanation|Specifics|Breakdown|Context|Summary)(?:,? [a-z ]{1,20})? below|Short version first)[.:,]`), anchors: []string{"below", "short version"}},
}

// hedgingDisclaimer only matches "consult the documentation" if there's no
// target, since "Consult the documentation for go/token" points somewhere.
var hedgingDisclaimer = words(
	`(?:make|be) sure (?:to|you) (?:test|verify|check|double-check|review)(?: \w+){0,2}? (?:thoroughly|carefully)`,
	`(?:make|be) sure (?:to|you) (?:test|verify|double-check) (?:everything|your (?:changes|work|code|setup|configuration|implementation))`,
	`(?:always )?test(?: \w+){0,2}? thoroughly before`, `thoroughly test(?: \w+){0,3}? before`,
	`results may vary`, `consult (?:the|your) (?:official|relevant) documentation`,
	`consult the documentation for (?:more|further) (?:details|information)`,
)

// proceduralReassurance leaves out "no breaking changes" and "no functional
// changes". Release notes and commit messages use them to tell upgraders a
// release is safe, and that's information. Backward compatibility counts in
// the trailing "ensuring ..." of a change summary and in "while maintaining
// full ...". It doesn't count in "This ensures backward compatibility" or
// "while maintaining backward compatibility", because those explain why
// code or a protocol works the way it does.
var proceduralReassurance = words(
	`while (?:preserving|maintaining|keeping|retaining) (?:the |all |full )?(?:existing|current|original) (?:behaviou?r|functionality|API|APIs|semantics|interface|logic)`,
	`without (?:breaking|changing|affecting) (?:the |any )?existing (?:behaviou?r|functionality|logic)`,
	`without breaking any(?:thing| behaviou?r| functionality)`,
	`(?:(?:while|and) )?ensuring (?:full |complete )?backwards?[- ]compatibility`,
	`while (?:maintaining|preserving) (?:full|complete) backwards?[- ]compatibility`,
)

// boldRe matches a bold span on a single line. "__x__" needs a space inside
// so we don't take Python names like __init__ for bold.
var boldRe = regexp.MustCompile(`\*\*[^*\s](?:[^*\n]*[^*\s])?\*\*|__[^_\s][^_\n]*\s[^_\n]*[^_\s]__`)

// boldSpans returns the bold spans of a line. A span that touches a word
// outside it ("x**2 + y**2") isn't markdown. Unless labels is set, we leave
// out the label that opens a line or list item ("- **Speed:** ..."), which
// inline-header-list judges.
func (t *text) boldSpans(ls span, labels bool) []span {
	line := t.src[ls.start:ls.end]
	label, isLabel := span{}, false
	if !labels {
		label, isLabel = t.lineLabel(ls)
	}
	var out []span
	for _, m := range boldRe.FindAllStringIndex(line, -1) {
		before, _ := utf8.DecodeLastRuneInString(line[:m[0]])
		after, _ := utf8.DecodeRuneInString(line[m[1]:])
		if m[0] > 0 && isWordRune(before) && before != '-' || m[1] < len(line) && isWordRune(after) && after != '-' && after != '\'' {
			continue
		}
		s := span{ls.start + m[0], ls.start + m[1]}
		if !t.masked(s) && !(isLabel && s == label) {
			out = append(out, s)
		}
	}
	return out
}

// boldCount counts the bold spans of the text, labels included.
func (t *text) boldCount() int {
	n := 0
	for l := 1; l <= t.lineCount(); l++ {
		n += len(t.boldSpans(t.line(l), true))
	}
	return n
}

// markdownRe matches a heading, a table delimiter row, a link or a code
// fence, which plain text doesn't have.
var markdownRe = regexp.MustCompile("(?m)^ {0,3}#{1,6}[ \\t]+\\S|^[ \\t]*\\|?[ \\t]*:?-{3,}:?[ \\t]*\\||\\]\\([^)\\s]+\\)|^[ \\t]*(?:```|~~~)")

// markdown reports whether the text is written in markdown.
func (t *text) markdown() bool { return markdownRe.MatchString(t.src) }

var listItemRe = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d{1,3}[.)])[ \t]`)

// detectExcessiveBold flags bold used for emphasis all over the text: three
// or more bold spans in a paragraph or list item, or more than one per 40
// words of prose with at least four in the text. A bold sentence that opens
// a paragraph counts, a label doesn't. A skimmer might need the first span
// of a paragraph or of a dense text, so we don't report it. We skip headings
// and tables.
func detectExcessiveBold(t *text) []Violation {
	type bold struct {
		s    span
		para int
	}
	var spans []bold
	perPara := map[int]int{}
	para, words := 0, 0
	fenced := false
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		line := t.src[ls.start:ls.end]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
		}
		if trimmed == "" || fenced {
			para++
			continue
		}
		if listItemRe.MatchString(line) {
			para++
		}
		if strings.HasPrefix(trimmed, "|") || atxHeadingRe.MatchString(line) || setextLineRe.MatchString(line) {
			continue
		}
		words += countWords(line)
		for _, s := range t.boldSpans(ls, false) {
			spans = append(spans, bold{s, para})
			perPara[para]++
		}
	}
	dense := len(spans) >= 4 && len(spans)*40 > words
	var out []Violation
	seen := map[int]bool{}
	for i, b := range spans {
		first := !seen[b.para]
		seen[b.para] = true
		if dense && i > 0 || perPara[b.para] >= 3 && !first {
			out = append(out, Violation{Start: b.s.start, End: b.s.end, Detail: t.src[b.s.start:b.s.end]})
		}
	}
	return out
}
