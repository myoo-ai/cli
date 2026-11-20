// Package synonyms implements the word_tree and synonym_rewrite commands,
// which help agents replace words with synonyms commonly used by humans.
package synonyms

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/thesaurus"
)

var langFlag = command.Flag{
	Long: "lang", Short: "l", Kind: command.String, Value: "code", Default: "en",
	Usage: "language of the text: " + strings.Join(thesaurus.Codes(), ", "),
}

const dbHelp = `The synonym database of a language is downloaded (LibreOffice thesaurus and
OpenSubtitles word frequencies) and built on first use, then cached in
$MYOO_CACHE_DIR or the user cache directory.`

func language(inv *command.Invocation) (string, error) {
	code := strings.ToLower(inv.String("lang"))
	if _, ok := thesaurus.Lookup(code); !ok {
		return "", command.Usagef("unsupported language `%s`, supported: %s", inv.String("lang"), strings.Join(thesaurus.Codes(), ", "))
	}
	return code, nil
}

func readInput(env *command.Env) (string, error) {
	b, err := io.ReadAll(env.Stdin)
	if err != nil {
		return "", fmt.Errorf("unable to read stdin: %w", err)
	}
	return string(b), nil
}

// word is a token of the input, resolved to its thesaurus headword.
type word struct {
	token
	lemma string
	// guessed from the neighbouring words
	pos thesaurus.POS
}

// resolve keeps the tokens of text whose base form is a thesaurus headword
// and isn't a stopword.
func resolve(db *thesaurus.DB, text string, tokens []token) []word {
	lang := db.Language()
	keys := make([]string, len(tokens))
	for i, t := range tokens {
		keys[i] = thesaurus.Key(t.text)
	}
	adjacent := func(i, j int) bool {
		return strings.TrimSpace(text[tokens[i].end:tokens[j].start]) == ""
	}
	var out []word
	for i, t := range tokens {
		surface := keys[i]
		if lang.IsStopword(surface) {
			continue
		}
		var prev2, prev, next string
		if i > 0 && adjacent(i-1, i) {
			prev = keys[i-1]
			if i > 1 && adjacent(i-2, i-1) {
				prev2 = keys[i-2]
			}
		}
		if i+1 < len(tokens) && adjacent(i, i+1) {
			next = keys[i+1]
		}
		pos := db.POSHint(prev2, prev, surface, next)
		lemma, ok := db.Lemma(surface, pos)
		if !ok || lang.IsStopword(lemma) {
			continue
		}
		out = append(out, word{token: t, lemma: lemma, pos: pos})
	}
	return out
}

// minCloseness is the closeness a synonym needs before it can replace a
// word. It keeps out generic and related terms that the headword doesn't
// list in its main senses (a one-sided generic term gets 0.24).
const minCloseness = 0.3

// candidate is a synonym scored for a single occurrence of a word.
type candidate struct {
	thesaurus.Synonym
	score float64
}

// ranked returns the synonyms of e that can replace a word written as
// surface (case-folded) and used as part of speech pos, best first by
//
//	score = closeness^2 * (0.1 + 0.9*frequency), halved for phrases
//
// If synonyms are about as close, the common word should win, but squaring
// the closeness keeps frequent loosely related words ("go" for "send")
// behind close ones. The 0.1 floor lets closeness decide between words that
// aren't in the frequency list. We use the closeness for pos if the headword
// has senses of that part of speech. We never propose the headword, the
// written word or stopwords, and if there are single-word candidates, the
// best of them goes first.
func ranked(lang *thesaurus.Language, e thesaurus.Entry, surface string, pos thesaurus.POS) []candidate {
	if !e.Has(pos) {
		pos = thesaurus.Unknown
	}
	var out []candidate
	for _, s := range e.Synonyms {
		k := thesaurus.Key(s.Word)
		c := s.ClosenessAs(pos)
		if k == e.Key || k == surface || lang.IsStopword(k) || c < minCloseness {
			continue
		}
		score := c * c * (0.1 + 0.9*s.Frequency)
		if s.Phrase() {
			score /= 2
		}
		out = append(out, candidate{Synonym: s, score: score})
	}
	slices.SortStableFunc(out, func(a, b candidate) int { return cmp.Compare(b.score, a.score) })
	if i := slices.IndexFunc(out, func(c candidate) bool { return !c.Phrase() }); i > 0 {
		best := out[i]
		copy(out[1:i+1], out[:i])
		out[0] = best
	}
	return out
}
