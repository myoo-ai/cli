package codelint

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// People write short, lower-case, one-line comments that a scored rule
// would still match ("// create temp dirs" above os.MkdirTemp). The score
// says how much a comment reads like generated prose, and scored rules only
// report a comment if its score is above aiScoreThreshold.
const (
	scoreUppercase = 1 // the first letter is upper-case ("1. Parse", "Step 1")
	scoreMultiline = 3 // spans several lines
	scoreLong      = 3 // has at least longComment characters
	scoreProse     = 3 // for each prose_lint violation in the comment
	longComment    = 30
	// No single signal gets past aiScoreThreshold on its own, so a comment
	// needs two of them, or two prose_lint violations, to be reported.
	aiScoreThreshold = 3
)

// aiScore scores the comment group around sp, with tags like C#'s <summary>
// stripped. Findings of several rules can share a group, so we cache the
// score.
func (s *source) aiScore(sp code.Span) int {
	first, last := s.commentGroup(sp)
	if first < 0 {
		return 0
	}
	key := [2]int{first, last}
	if score, ok := s.groupScores[key]; ok {
		return score
	}
	var lines []string
	for _, c := range s.comments[first : last+1] {
		for _, l := range c.lines {
			lines = append(lines, strings.TrimSpace(xmlTagRe.ReplaceAllString(l, "")))
		}
	}
	text := strings.TrimSpace(strings.Join(lines, " "))
	score := 0
	if i := strings.IndexFunc(text, unicode.IsLetter); i >= 0 {
		if r, _ := utf8.DecodeRuneInString(text[i:]); unicode.IsUpper(r) {
			score += scoreUppercase
		}
	}
	n := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	if n > 1 {
		score += scoreMultiline
	}
	if utf8.RuneCountInString(text) >= longComment {
		score += scoreLong
	}
	score += scoreProse * len(s.lintCommentText(strings.Join(lines, "\n"), s.comments[first].docstring))
	if s.groupScores == nil {
		s.groupScores = map[[2]int]int{}
	}
	s.groupScores[key] = score
	return score
}

// commentGroup returns the indices of the first and last comment in the
// group around sp, or -1, -1 if sp doesn't overlap a comment. The group is
// the comments sp overlaps, plus standalone comments of the same kind on
// the lines right next to them, because to a reader a block of "//" lines
// is a single comment.
func (s *source) commentGroup(sp code.Span) (int, int) {
	first := sort.Search(len(s.comments), func(i int) bool { return s.comments[i].Span.End > sp.Start })
	if first == len(s.comments) || s.comments[first].Span.Start >= sp.End {
		return -1, -1
	}
	last := first
	for last+1 < len(s.comments) && s.comments[last+1].Span.Start < sp.End {
		last++
	}
	adjacent := func(a, b *comment) bool {
		return a.Kind == b.Kind && a.standalone && b.standalone && !a.docstring && !b.docstring &&
			!a.directive && !b.directive && b.line == a.end+1
	}
	for first > 0 && adjacent(s.comments[first-1], s.comments[first]) {
		first--
	}
	for last+1 < len(s.comments) && adjacent(s.comments[last], s.comments[last+1]) {
		last++
	}
	return first, last
}
