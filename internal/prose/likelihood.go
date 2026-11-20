package prose

import (
	"fmt"
	"strings"

	"github.com/myoo-ai/cli/internal/likelihood"
)

// We fit the rates below on the markdown docs of 25 human projects and 14
// projects written by AI agents (about 1000 and 800 documents), with no
// held-out set. Pooled per project, the signals put 24 of the human
// projects below 10 % and most AI projects above 90 %. A single short
// document is a much smaller sample, so you should expect the estimate to
// stay closer to the prior.

// Log-odds of AI authorship before we see any evidence, 10 %.
const aiPrior = -2.2

var (
	// wordChoice is the English word-choice model of wording_en.go. It's a
	// logistic regression on the rates of about 1300 common words and of
	// question and exclamation marks, so its value already is a log-odds.
	// On held-out same-topic pairs of human and AI text it ranks a random AI
	// text above a random human one 92 % of the time, and 91 % on the
	// documentation of the reference projects. It replaced a share of 49
	// hand-picked function words, which missed AI text that has none of the
	// patterns.
	wordChoice = likelihood.Logit{
		Signal: "wording", Half: 100,
		Bounds: likelihood.Bounds{MaxAI: 3, MaxHuman: 3},
	}
	// patterns counts violations of the scored rules per 1000 words. Text in
	// other languages has no word-choice signal, so its estimate comes from
	// this one and bold.
	patterns = likelihood.Rate{
		Signal: "prose patterns", Human: 2.4, AI: 24, Spread: 0.74,
		MaxAI: 3, MaxHuman: 2,
	}
	// emphasis counts bold spans per 1000 words of a markdown document,
	// labels and table cells included. Human docs bold a term or a warning
	// here and there, AI agents bold a phrase in most paragraphs and label
	// list items with it, in every language. We fit it on the same documents
	// as the other two and checked it on Wikipedia articles and AI documents
	// in Slovak, Czech, Polish, German, French and Spanish (see the README).
	// Plain text has no way to write bold, so we leave it out there.
	emphasis = likelihood.Rate{
		Signal: "bold", Human: 1.2, AI: 9.2, Spread: 1.3,
		MaxAI: 2, MaxHuman: 1,
	}
)

// unscored lists rules that human docs break about as often as AI output.
// We still report them, but they don't count towards the likelihood. The
// rules from copula-avoidance to words-instead-of-numbers were added after
// the fit, so they stay unscored until we refit with them. numbered-headings
// came later too, but scoring it moves the medians of the fit by about 5 %
// (3.0 to 3.1 violations per 1000 words in human docs, 23 to 25 in AI docs),
// so it counts. Unneeded backslash escapes come from converters such as a
// Google Docs export, which people use as often as agents do.
var unscored = map[string]bool{
	"rule-of-three": true, "transition-openers": true, "capitals-as-heading": true, "title-case-heading": true,
	"curly-quotes": true, "left-padded-numbers": true, "invisible-characters": true,
	"copula-avoidance": true, "nominalization": true, "meta-preamble": true, "hedging-disclaimer": true,
	"procedural-reassurance": true, "excessive-bold": true,
	"empty-intensifier": true, "credibility-adverb": true, "worth-construction": true, "false-ease": true,
	"stacked-hedge": true, "participial-closer": true, "rather-than-gerund": true, "hollow-adjective": true,
	"personified-object": true, "journey-imagery": true, "shop-talk": true, "thing-as-subject": true,
	"chatty-verdict": true, "slogan-compression": true, "clause-for-adjective": true, "regard-instruction": true,
	"corporate-jargon": true, "heading-commentary": true, "words-instead-of-numbers": true,
	"backslash-escapes-in-prose": true,
}

// aiEvidence returns the signals of AI authorship in t and vs, and the
// number of words they're based on.
func aiEvidence(t *text, lang string, vs []Violation) ([]likelihood.Evidence, int) {
	ws := t.words()
	n := 0
	for _, v := range vs {
		if !unscored[v.Rule.ID] {
			n++
		}
	}
	words := fmt.Sprintf("%d words", len(ws))
	if l := languages[lang]; l != nil {
		words = fmt.Sprintf("%d %s words", len(ws), l.name)
	}
	ev := []likelihood.Evidence{
		patterns.Evidence(n, float64(len(ws))/1000, fmt.Sprintf("%d scored violations in %s", n, words)),
	}
	if b := t.boldCount(); b > 0 || t.markdown() {
		ev = append(ev, emphasis.Evidence(b, float64(len(ws))/1000, fmt.Sprintf("%d bold spans in %s of markdown, human docs have ~1 per 1000 words, AI docs ~10", b, words)))
	}
	if lang != "en" {
		return ev, len(ws)
	}
	toks, n := t.wordingTokens()
	if x, ai, human, ok := wording(toks, n); ok {
		ev = append(ev, wordChoice.Evidence(x, n, fmt.Sprintf("%s, used more than people use them: %s (AI-like), %s (human-like)",
			words, orNone(ai), orNone(human))))
	}
	return ev, len(ws)
}

// orNone joins words for an evidence line.
func orNone(ws []string) string {
	if len(ws) == 0 {
		return "none"
	}
	return strings.Join(ws, ", ")
}

// words returns the lower-case words of t, skipping masked code, URLs and
// paths.
func (t *text) words() []string {
	b := []byte(t.src)
	for _, m := range t.masks {
		for i := m.start; i < m.end; i++ {
			b[i] = ' '
		}
	}
	return likelihood.Words(string(b))
}
