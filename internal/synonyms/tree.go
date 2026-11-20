package synonyms

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/thesaurus"
)

const treeFormat = "(roots are input words in base form, most frequent first. children are synonyms ordered by closeness) " +
	"`<indent>- <word>:L<first line>:<first col>:<count in input>[:<closeness to parent>]:<frequency in the language>`"

// maxChildren caps the synonyms we list for a root, and a "..." line stands
// for the rest.
const maxChildren = 12

// WordTreeCommand returns the word_tree command. It prints the words of the
// input in base form with their synonyms and scores.
func WordTreeCommand() *command.Command {
	return &command.Command{
		Name:    "word_tree",
		Summary: "tree of synonyms with closeness and frequency scores for every word of a text",
		Usage:   "word_tree [--lang=<code>] [--word=<word1,word2,...>] [--download-db]",
		Help: `Reads text from stdin unless --word is given. Every word with synonyms becomes a
root in its base form, the most frequent words of the input first. Children are
its synonyms, closest first, at most 12 followed by "..." when there are more.
Scores are in [0,1]: closeness says how close the synonym is to the root,
frequency how common the word is in the language (1 is the most common word).
Words absent from the input are shown at L0:0 with count 0.

` + dbHelp + `
--download-db (re)builds it and prints its path.`,
		Flags: []command.Flag{
			langFlag,
			{Long: "word", Short: "w", Kind: command.String, Value: "words", Usage: "comma separated words to show instead of reading stdin"},
			{Long: "download-db", Kind: command.Bool, Usage: "download and build the synonym database for --lang, then print its path"},
		},
		Run: runTree,
	}
}

func runTree(env *command.Env, inv *command.Invocation) error {
	lang, err := language(inv)
	if err != nil {
		return err
	}
	if inv.Bool("download-db") {
		path, err := thesaurus.Download(context.Background(), lang)
		if err != nil {
			return err
		}
		env.Format("`<path to database>`")
		fmt.Fprintln(env.Stdout, path)
		return nil
	}
	var text string
	if !inv.Has("word") {
		if text, err = readInput(env); err != nil {
			return err
		}
	}
	db, err := thesaurus.Open(context.Background(), lang)
	if err != nil {
		return err
	}
	defer db.Close()

	var roots []root
	var missing []string
	if inv.Has("word") {
		roots, missing, err = wordRoots(db, inv.String("word"))
	} else {
		roots, err = textRoots(db, text)
	}
	if err != nil {
		return err
	}
	env.Format(treeFormat)
	inInput := map[string]*root{}
	for i := range roots {
		if roots[i].count > 0 {
			inInput[roots[i].entry.Key] = &roots[i]
		}
	}
	var b strings.Builder
	for _, r := range roots {
		fmt.Fprintf(&b, "- %s:%s:%.4f\n", r.entry.Word, r.position(), r.entry.Frequency)
		for i, s := range r.entry.Synonyms {
			if i == maxChildren {
				b.WriteString("  ...\n")
				break
			}
			pos := "L0:0:0"
			if in := inInput[thesaurus.Key(s.Word)]; in != nil {
				pos = in.position()
			}
			fmt.Fprintf(&b, "  - %s:%s:%.4f:%.4f\n", s.Word, pos, s.Closeness, s.Frequency)
		}
	}
	fmt.Fprint(env.Stdout, b.String())
	if len(missing) > 0 {
		return fmt.Errorf("unable to find synonyms for %s in the `%s` thesaurus, check the spelling or try another form of the word", quoteList(missing), lang)
	}
	return nil
}

type root struct {
	entry     thesaurus.Entry
	line, col int
	count     int
	offset    int
}

func (r *root) position() string { return fmt.Sprintf("L%d:%d:%d", r.line, r.col, r.count) }

// textRoots groups the words of text that have synonyms by base form, most
// frequent first, then by first occurrence.
func textRoots(db *thesaurus.DB, text string) ([]root, error) {
	byLemma := map[string]*root{}
	var roots []*root
	for _, w := range resolve(db, text, tokenize(text)) {
		if r := byLemma[w.lemma]; r != nil {
			r.count++
			continue
		}
		e, ok, err := db.Lookup(w.lemma)
		if err != nil {
			return nil, err
		}
		if !ok || len(e.Synonyms) == 0 {
			continue
		}
		r := &root{entry: e, line: w.line, col: w.col, count: 1, offset: w.start}
		byLemma[w.lemma] = r
		roots = append(roots, r)
	}
	slices.SortStableFunc(roots, func(a, b *root) int {
		return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.offset, b.offset))
	})
	out := make([]root, len(roots))
	for i, r := range roots {
		out[i] = *r
	}
	return out, nil
}

// wordRoots looks up the words of --word in the given order. Words we can't
// resolve to a base form come back as missing, and we skip base forms that
// have no thesaurus entry.
func wordRoots(db *thesaurus.DB, list string) (roots []root, missing []string, err error) {
	seen := map[string]bool{}
	for w := range strings.SplitSeq(list, ",") {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		key := thesaurus.Key(w)
		lemma, ok := key, false
		if _, ok = db.Frequency(key); !ok {
			lemma, ok = db.Lemma(key, thesaurus.Unknown)
		}
		if !ok {
			missing = append(missing, w)
			continue
		}
		if seen[lemma] {
			continue
		}
		seen[lemma] = true
		e, ok, err := db.Lookup(lemma)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			roots = append(roots, root{entry: e})
		}
	}
	if len(roots) == 0 && len(missing) == 0 {
		return nil, nil, command.Usagef("--word needs at least one word")
	}
	return roots, missing, nil
}

func quoteList(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = "`" + w + "`"
	}
	return strings.Join(q, ", ")
}
