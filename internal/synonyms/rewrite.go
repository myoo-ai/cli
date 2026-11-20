package synonyms

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/thesaurus"
)

const rewriteFormat = "(for every word with a synonym, words without a matching synonym are left as-is) " +
	"`[<word with matching highest-score synonym>] (<other high-scoring alternative synonyms for the word>)`"

// maxAlternatives caps the alternatives we show after the main pick, and
// ", ..." stands for the rest.
const maxAlternatives = 3

// RewriteCommand returns the synonym_rewrite command. It replaces the words
// of the input with their best synonym and lists the alternatives.
func RewriteCommand() *command.Command {
	return &command.Command{
		Name:    "synonym_rewrite",
		Summary: "rewrite prose from stdin with commonly used synonyms",
		Usage:   "synonym_rewrite [--lang=<code>]",
		Help: `Reads text from stdin and prints it with every word that has a synonym replaced
by "[best] (alternative, ...)". The best synonym combines closeness to the word
with how common it is in the language. Synonyms are base forms, so the result
needs grammatical fixes afterwards. Stopwords, code spans, URLs and paths are
left unchanged, as is everything between the words.

` + dbHelp + `
Run "myoo word_tree --download-db" to build it ahead of time.`,
		Flags: []command.Flag{langFlag},
		Run:   runRewrite,
	}
}

func runRewrite(env *command.Env, inv *command.Invocation) error {
	lang, err := language(inv)
	if err != nil {
		return err
	}
	text, err := readInput(env)
	if err != nil {
		return err
	}
	db, err := thesaurus.Open(context.Background(), lang)
	if err != nil {
		return err
	}
	defer db.Close()
	out, err := rewrite(db, text)
	if err != nil {
		return err
	}
	env.Format(rewriteFormat)
	fmt.Fprint(env.Stdout, out)
	return nil
}

func rewrite(db *thesaurus.DB, text string) (string, error) {
	var b strings.Builder
	b.Grow(len(text) * 2)
	last := 0
	type cacheKey struct {
		lemma, surface string
		pos            thesaurus.POS
	}
	cache := map[cacheKey][]candidate{}
	for _, w := range resolve(db, text, tokenize(text)) {
		surface := thesaurus.Key(w.text)
		ck := cacheKey{w.lemma, surface, w.pos}
		syns, ok := cache[ck]
		if !ok {
			e, found, err := db.Lookup(w.lemma)
			if err != nil {
				return "", err
			}
			if found {
				syns = ranked(db.Language(), e, surface, w.pos)
			}
			cache[ck] = syns
		}
		if len(syns) == 0 {
			continue
		}
		b.WriteString(text[last:w.start])
		b.WriteString("[" + matchCase(w.text, syns[0].Word) + "]")
		if alts := syns[1:]; len(alts) > 0 {
			b.WriteString(" (")
			for i, s := range alts[:min(len(alts), maxAlternatives)] {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(s.Word)
			}
			if len(alts) > maxAlternatives {
				b.WriteString(", ...")
			}
			b.WriteString(")")
		}
		last = w.end
	}
	b.WriteString(text[last:])
	return b.String(), nil
}

// matchCase gives repl the capitalization of orig. If orig is all upper case
// (two letters or more) we upper-case repl, if it's capitalized we
// capitalize repl, and otherwise we keep the thesaurus spelling.
func matchCase(orig, repl string) string {
	letters, upper := 0, 0
	for _, r := range orig {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters > 1 && upper == letters {
		return strings.ToUpper(repl)
	}
	first, _ := utf8.DecodeRuneInString(orig)
	if !unicode.IsUpper(first) {
		return repl
	}
	r, size := utf8.DecodeRuneInString(repl)
	return string(unicode.ToTitle(r)) + repl[size:]
}
