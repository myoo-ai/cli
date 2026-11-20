package prose

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// wordStat is what the word-choice model knows about a word or mark: how far
// each use of it per word of text moves the log-odds of AI authorship, and
// how often human text uses it per word.
type wordStat struct {
	weight, human float64
}

// wordingTokenRe matches a word, or a question or exclamation mark, which
// people write more often than agents do (see train_wording.py).
var wordingTokenRe = regexp.MustCompile(`[\pL\pN]+(?:['’][\pL\pN]+)*|[?!]`)

// markNames names the marks in evidence lines.
var markNames = map[string]string{"?": "questions", "!": "exclamations"}

// wordingTokens returns the lower-case words and the question and
// exclamation marks of t outside code, URLs and paths, and the number of
// words among them.
func (t *text) wordingTokens() (tokens []string, words int) {
	b := []byte(t.src)
	for _, m := range t.masks {
		for i := m.start; i < m.end; i++ {
			b[i] = ' '
		}
	}
	for _, m := range wordingTokenRe.FindAll(b, -1) {
		tok := strings.ToLower(strings.ReplaceAll(string(m), "’", "'"))
		if _, mark := markNames[tok]; !mark {
			words++
		}
		tokens = append(tokens, tok)
	}
	return tokens, words
}

// wording scores tokens from wordingTokens with the model in wording_en.go
// and returns the log-odds of AI authorship at even priors. It also names
// the words and marks the text uses more often than people do that pushed
// the score furthest towards AI authorship, and towards human authorship,
// at most 5 of each with their counts ("people 10"). ok is false when no
// token is in the model, so the text tells nothing by its wording.
func wording(tokens []string, words int) (logOdds float64, ai, human []string, ok bool) {
	count := map[string]int{}
	sum := 0.0
	for _, w := range tokens {
		if s, ok := wordingModel[w]; ok {
			sum += s.weight
			count[w]++
		}
	}
	if len(count) == 0 || words == 0 {
		return 0, nil, nil, false
	}
	n := float64(words)
	// excess is how far the uses beyond the human rate moved the score.
	excess := func(w string) float64 {
		s := wordingModel[w]
		return s.weight * (float64(count[w])/n - s.human)
	}
	overused := slices.DeleteFunc(slices.Collect(maps.Keys(count)), func(w string) bool {
		return float64(count[w])/n <= wordingModel[w].human
	})
	slices.SortFunc(overused, func(a, b string) int {
		return cmp.Or(cmp.Compare(excess(b), excess(a)), strings.Compare(a, b))
	})
	label := func(w string) string {
		name, ok := markNames[w]
		if !ok {
			name = w
		}
		return fmt.Sprintf("%s %d", name, count[w])
	}
	for _, w := range overused {
		if excess(w) > 0 && len(ai) < 5 {
			ai = append(ai, label(w))
		}
	}
	for i := len(overused) - 1; i >= 0 && excess(overused[i]) < 0 && len(human) < 5; i-- {
		human = append(human, label(overused[i]))
	}
	return wordingBias + sum/n, ai, human, true
}
