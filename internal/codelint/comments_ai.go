package codelint

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/myoo-ai/cli/internal/code"
)

// commentPhrase is a phrase pattern of a comment rule. If after is set, it
// rejects a match by the text that follows, since Go's regexp can't look
// ahead. sentence limits the pattern to the start of a sentence. userFacing
// marks phrases that API docs and example programs can rightly address to
// their reader, so we leave those alone there.
type commentPhrase struct {
	re         *regexp.Regexp
	after      *regexp.Regexp
	sentence   bool
	userFacing bool
}

var changeNarrationPhrases = []commentPhrase{
	{re: regexp.MustCompile(`(?i)\bnow\s+uses\b`)},
	// "the file was renamed from a.txt" and "the solution that held until
	// now" describe runtime state, so we only count these two as an opener.
	{re: regexp.MustCompile(`(?i)\brenamed\s+from\b`), sentence: true},
	{re: regexp.MustCompile(`(?i)\buntil\s+now\b`), sentence: true},
	// "allows Used to be called directly" names the method Used.
	{re: regexp.MustCompile(`\bused\s+to\s+be\s+(?:called|named)\b`)},
	{re: regexp.MustCompile(`\bUsed\s+to\s+be\s+(?:called|named)\b`), sentence: true},
	// "formerly known as" usually gives the old name of a protocol or a
	// product, which readers search for.
	{re: regexp.MustCompile(`(?i)\b(?:previously|formerly)\s+(?:called|named)\b`)},
	{re: regexp.MustCompile(`(?i)\binstead\s+of\s+the\s+old\b`)},
	{re: regexp.MustCompile(`(?i)\bthe\s+old\s+(?:implementation|approach|logic|behaviou?r)\b`)},
	{re: regexp.MustCompile(`(?i)\b(?:fixed|changed|updated|refactored|added)\s+(?:per|as\s+per|after)\s+(?:code\s+)?review\b`)},
	{re: regexp.MustCompile(`(?i)\bsame\s+(?:behaviou?r|logic|semantics)\s+as\s+before\b`)},
	{re: regexp.MustCompile(`(?i)\bthe\s+former\s+\w+\s+(?:module|class|function|component)\b`)},
	{re: regexp.MustCompile(`(?i)\bno\s+longer\s+(?:uses|calls)\b`)},
	// "the key no longer needs to be set" is about the caller.
	{re: regexp.MustCompile(`(?i)\bno\s+longer\s+needs\s+to\b`), after: regexp.MustCompile(`(?i)^\s+be\b`)},
	// We only count changelog markers in upper case at the start of a comment.
	{re: regexp.MustCompile(`^\s*((?:NEW|FIXED|CHANGED|UPDATED):)`)},
}

// clauseEnd rejects a phrase that the clause goes on to qualify: "This is
// a stub version which simply returns v when unsafe is unavailable", "a
// naive implementation with a count member" and "This is just a
// placeholder node" state the limitation or name a concept.
var clauseEnd = regexp.MustCompile(`^\s*\w`)

// demoGradePhrases leaves out "for simplicity". In human code it introduces
// a stated trade-off ("for simplicity, we don't permit sending undersized
// datagrams"), which is what the rule asks for.
var demoGradePhrases = []commentPhrase{
	{re: regexp.MustCompile(`(?i)\bin\s+a\s+real\s+(?:implementation|app|application|system|project|world)\b`)},
	{re: regexp.MustCompile(`(?i)\bin\s+production,?\s+(?:(?:you|we|one)\s+(?:would|should|might|could)|use|replace|switch\s+to|swap)\b`)},
	{re: regexp.MustCompile(`(?i)\bthis\s+is\s+(?:just\s+|only\s+)?an?\s+(?:stub|mock|simplified|placeholder|naive|dummy)(?:\s+(?:version|implementation))?(?:\s+for\s+now)?\b`),
		after: clauseEnd, sentence: true},
	{re: regexp.MustCompile(`(?i)\b(?:an?\s+)?(?:simplified|naive|placeholder|dummy)\s+(?:version|implementation)(?:\s+for\s+now)?\b`),
		after: clauseEnd, sentence: true},
}

var reviewerPhrases = []commentPhrase{
	// "Redirect stdout to a file as requested" means the configuration, so
	// we only count a bare "as requested" as an opener: "As requested, ...".
	{re: regexp.MustCompile(`(?i)\bas\s+requested\b`), after: regexp.MustCompile(`^\s*[^,\s]`), sentence: true},
	{re: regexp.MustCompile(`(?i)\bas\s+you\s+(?:requested|asked|suggested)\b`)},
	{re: regexp.MustCompile(`(?i)\bper\s+your\s+(?:request|feedback)\b`)},
	{re: regexp.MustCompile(`(?i)\bnote\s+(?:for|to)\s+(?:the\s+)?reviewers?\b`)},
	{re: regexp.MustCompile(`(?i)\badjust\s+as\s+needed\b`)},
	{re: regexp.MustCompile(`(?i)\b(?:modify|change|update|tweak)\s+(?:this|these|it)\s+as\s+needed\b`)},
	{re: regexp.MustCompile(`(?i)\bfeel\s+free\s+to\b`), userFacing: true},
	{re: regexp.MustCompile(`(?i)\byou\s+(?:may|might|can)\s+want\s+to\s+(?:adjust|change|modify|tweak|tune|customi[sz]e|replace)\b`), userFacing: true},
}

// deprecationRe matches deprecation notices, which say what replaced the
// old API and when.
var deprecationRe = regexp.MustCompile(`(?i)@deprecated|\bdeprecated:`)

func checkChangeNarration(s *source) []finding {
	return s.commentPhrases(changeNarrationPhrases, deprecationRe)
}

func checkReviewerAddressed(s *source) []finding { return s.commentPhrases(reviewerPhrases, nil) }

// checkDemoGrade skips tests and example programs, since a mock or a
// simplified setup is the point there.
func checkDemoGrade(s *source) []finding {
	if s.test || examplePathRe.MatchString(s.f.Path) {
		return nil
	}
	return s.commentPhrases(demoGradePhrases, nil)
}

// examplePathRe matches the paths of example programs and tutorials.
var examplePathRe = regexp.MustCompile(`(?i)example|demo|sample|tutorial`)

// commentPhrases reports the phrases in comments and docstrings. It reads
// the same text comment-prose reads, so it skips quoted text, Go doc code
// blocks, commented-out code, license notices and comments that match skip.
// If a pattern has a capturing group, we report the group, otherwise the
// whole match. A phrase that overlaps an earlier one is the same finding.
// The detail quotes the phrase.
func (s *source) commentPhrases(phrases []commentPhrase, skip *regexp.Regexp) []finding {
	example := examplePathRe.MatchString(s.f.Path)
	var out []finding
	for ci, c := range s.comments {
		if c.directive || c.license || len(c.lines) == 0 || commentedOutCode(c) || skip != nil && skip.MatchString(c.text()) {
			continue
		}
		text := s.proseText(c)
		var found []code.Span
		for _, p := range phrases {
			ms := p.re.FindAllStringSubmatchIndex(text, -1)
			if len(ms) == 0 || p.userFacing && (example || s.documents(ci)) {
				continue
			}
			for _, m := range ms {
				start, end := m[0], m[1]
				if len(m) > 2 && m[2] >= 0 {
					start, end = m[2], m[3]
				}
				if p.after != nil && p.after.MatchString(s.continuation(ci, text[end:])) || p.sentence && !s.opensSentence(ci, text[:start]) {
					continue
				}
				sp := code.Span{Start: c.Span.Start + start, End: c.Span.Start + end}
				if s.quotedAt(sp.Start) || slices.ContainsFunc(found, func(f code.Span) bool { return f.Start < sp.End && sp.Start < f.End }) {
					continue
				}
				found = append(found, sp)
				out = append(out, finding{span: sp, detail: "`" + strings.Join(strings.Fields(text[start:end]), " ") + "`"})
			}
		}
	}
	return out
}

// documents reports whether comment ci documents a declaration. That's a doc
// comment or docstring, or a comment group that ends on the line above a
// declaration outside a function body, which is how Go writes its docs.
func (s *source) documents(ci int) bool {
	if c := s.comments[ci]; c.docstring || c.Kind == code.DocComment {
		return true
	}
	for ci+1 < len(s.comments) && s.continues(ci, ci+1) {
		ci++
	}
	line := s.comments[ci].end + 1
	for _, d := range s.f.Decls {
		if s.line(d.Span.Start) == line && d.Kind != code.KindParameter && d.Kind != code.KindTypeParameter &&
			(d.Parent == nil || !d.Parent.Callable()) {
			return true
		}
	}
	return false
}

// opensSentence reports whether a phrase that follows before in the prose
// text of comment ci starts a sentence. If a comment continues the line
// comment above it and that one doesn't end with a full stop, we're in the
// middle of a sentence.
func (s *source) opensSentence(ci int, before string) bool {
	before = strings.TrimRightFunc(before, unicode.IsSpace)
	if before != "" {
		return strings.ContainsAny(before[len(before)-1:], ".!?:")
	}
	if ci == 0 || !s.continues(ci-1, ci) {
		return true
	}
	prev := s.comments[ci-1]
	last := prev.lines[len(prev.lines)-1]
	return last == "" || strings.ContainsAny(last[len(last)-1:], ".!?:")
}

// continuation returns rest, the prose text after a phrase in comment ci. If
// rest is blank and the next line comment continues ci, we append the first
// line of that comment: "no longer needs to\n// be scanned".
func (s *source) continuation(ci int, rest string) string {
	if strings.TrimSpace(rest) != "" || ci+1 >= len(s.comments) || !s.continues(ci, ci+1) {
		return rest
	}
	return rest + " " + s.comments[ci+1].lines[0]
}

// continues reports whether comment j is a line comment on the line after
// comment i, so the reader sees both as a single text.
func (s *source) continues(i, j int) bool {
	a, b := s.comments[i], s.comments[j]
	return a.standalone && b.standalone && a.Kind == b.Kind && b.line == a.end+1 && len(a.lines) > 0 && len(b.lines) > 0
}
