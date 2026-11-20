package likelihood

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Evidence is how much a single signal contributes.
type Evidence struct {
	Signal string
	// LogOdds is positive if the input reads as AI written.
	LogOdds float64
	// Basis says what we counted.
	Basis string
}

// Bounds cap a contribution at MaxAI towards AI and at MaxHuman towards
// human authorship.
type Bounds struct {
	MaxAI, MaxHuman float64
}

// bound squashes x smoothly into (-MaxHuman, MaxAI) with tanh. If a bound is
// 0, evidence in that direction is dropped.
func (b Bounds) bound(x float64) float64 {
	switch {
	case x > 0 && b.MaxAI > 0:
		return b.MaxAI * math.Tanh(x/b.MaxAI)
	case x < 0 && b.MaxHuman > 0:
		return b.MaxHuman * math.Tanh(x/b.MaxHuman)
	}
	return 0
}

// llr is the log-likelihood ratio of x under two normal distributions with
// variance v, centered on ai and on human.
func llr(x, human, ai, v float64) float64 {
	return ((x-human)*(x-human) - (x-ai)*(x-ai)) / (2 * v)
}

// overdispersion scales up the sampling variance of a count. Words and
// lines come in clusters (a long comment, a templated section), so a count
// varies more than it would with independent draws.
const overdispersion = 2

// Rate is a signal counted against some exposure, like per 1000 words or
// lines. Human and AI are the median rates of the two groups, and Spread is
// the standard deviation of the log of the rate within a group.
type Rate struct {
	Signal            string
	Human, AI, Spread float64
	Bounds
}

// Evidence returns the contribution of n occurrences in exposure units. We
// add half an occurrence at the midpoint rate to the observation, so a zero
// count doesn't blow up the log and small samples get pulled to the middle.
func (r Rate) Evidence(n int, exposure float64, basis string) Evidence {
	mid := math.Sqrt(r.Human * r.AI)
	x := math.Log((float64(n) + 0.5) / (exposure + 0.5/mid))
	v := r.Spread*r.Spread + overdispersion/(float64(n)+0.5)
	return Evidence{Signal: r.Signal, LogOdds: r.bound(llr(x, math.Log(r.Human), math.Log(r.AI), v)), Basis: basis}
}

// Share is a signal that splits items into two kinds. Human and AI are the
// median shares of the first kind in each group, and Spread is the standard
// deviation of the logit of the share within a group.
type Share struct {
	Signal            string
	Human, AI, Spread float64
	Bounds
}

// Evidence returns the contribution of k items of the first kind out of n.
func (s Share) Evidence(k, n int, basis string) Evidence {
	mid := (s.Human + s.AI) / 2
	p := (float64(k) + mid) / (float64(n) + 1)
	v := s.Spread*s.Spread + overdispersion*(1/(float64(k)+0.5)+1/(float64(n-k)+0.5))
	return Evidence{Signal: s.Signal, LogOdds: s.bound(llr(logit(p), logit(s.Human), logit(s.AI), v)), Basis: basis}
}

// Logit is a signal that already is a log-odds of AI authorship, such as a
// logistic regression that averages over the words of a text. A short text
// averages over few words, so its log-odds shrinks towards zero, to half
// of it at Half words.
type Logit struct {
	Signal string
	Half   float64
	Bounds
}

// Evidence returns the contribution of log-odds x measured on n words.
func (l Logit) Evidence(x float64, n int, basis string) Evidence {
	return Evidence{Signal: l.Signal, LogOdds: l.bound(x * float64(n) / (float64(n) + l.Half)), Basis: basis}
}

func logit(p float64) float64 { return math.Log(p / (1 - p)) }

// Percent combines the prior log-odds with the evidence into a percentage.
func Percent(prior float64, ev []Evidence) float64 {
	z := prior
	for _, e := range ev {
		z += e.LogOdds
	}
	return 100 / (1 + math.Exp(-z))
}

// Write prints the last line of a lint report, "(likely) AI patterns: <n> %",
// with at most a single decimal.
func Write(w io.Writer, pct float64) error {
	pct = math.Round(pct*10) / 10
	_, err := fmt.Fprintf(w, "(likely) AI patterns: %s %%\n", strconv.FormatFloat(pct, 'f', -1, 64))
	return err
}

// EvidenceFormat describes the lines that WriteEvidence prints.
const EvidenceFormat = "`<log-odds, positive reads as AI> <signal>: <what was counted>`"

// WriteEvidence prints the prior and then the evidence, strongest first.
func WriteEvidence(w io.Writer, prior float64, ev []Evidence) {
	ev = slices.Clone(ev)
	slices.SortStableFunc(ev, func(a, b Evidence) int {
		return cmp.Compare(math.Abs(b.LogOdds), math.Abs(a.LogOdds))
	})
	fmt.Fprintf(w, "%+.2f prior: before any evidence\n", prior)
	for _, e := range ev {
		if math.Abs(e.LogOdds) >= 0.005 {
			fmt.Fprintf(w, "%+.2f %s: %s\n", e.LogOdds, e.Signal, e.Basis)
		}
	}
}

// These words make up the voice signal of English text. People write the
// way they talk, with modal and auxiliary verbs, conditionals, pronouns and
// hedges ("we need this if the cache is cold"). AI agents tend to write in a
// dense declarative voice, with quantifiers, possessives and phrasal
// coordination ("keeps every entry once, without locking"). We picked the
// words on the comments of 20 human and 12 AI codebases, then checked them
// on 8 human and 3 AI codebases we'd held out.
var (
	humanVoice = setOf("will", "to", "this", "if", "be", "we", "do", "but", "get", "should", "can", "you",
		"some", "need", "have", "want", "just", "make", "because", "since", "might", "maybe", "probably",
		"actually", "really", "all", "also", "there", "here", "our", "us", "your")
	aiVoice = setOf("every", "one", "never", "its", "per", "without", "whose", "once", "and", "when",
		"both", "rather", "unless", "what", "through", "while", "itself")
)

// CountVoice counts the human-style and the AI-style function words in a
// list of lower-case English words.
func CountVoice(words []string) (human, ai int) {
	for _, w := range words {
		if humanVoice[w] {
			human++
		} else if aiVoice[w] {
			ai++
		}
	}
	return human, ai
}

// Words splits text into lower-case words of letters and digits, which is
// what CountVoice expects. We keep apostrophes inside a word ("let's").
func Words(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, strings.Trim(b.String(), "'"))
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case (r == '\'' || r == '\u2019') && b.Len() > 0:
			b.WriteByte('\'')
		default:
			flush()
		}
	}
	flush()
	return out
}

func setOf(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}
