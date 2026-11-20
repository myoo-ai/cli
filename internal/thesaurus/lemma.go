package thesaurus

import (
	"strings"
	"unicode/utf8"
)

// Lemma maps a word to the thesaurus headword that's its base form. We try,
// in this order:
//
//  1. an irregular form from the language table (went: go),
//  2. the word itself if it's a headword (this keeps nouns like "news" and
//     participial adjectives like "finished"),
//  3. otherwise the most frequent headword among the suffix-stripping
//     candidates, where the earliest candidate wins ties.
//
// If pos is known, we try headwords with senses of that part of speech first
// ("have finished" maps to the verb "finish", not the adjective "finished").
// For a verb in a thesaurus without part-of-speech labels, the
// suffix-stripping candidates come before the word itself, which turns
// participles into infinitives ("hemos terminado": terminar). ok is false if
// no candidate is a headword.
func (db *DB) Lemma(word string, pos POS) (lemma string, ok bool) {
	k := Key(word)
	if k == "" {
		return "", false
	}
	if pos != Unknown {
		if lemma, ok := db.lemma(k, pos, true); ok {
			return lemma, true
		}
	}
	if pos == Verb {
		if lemma, ok := db.lemma(k, Unknown, false); ok {
			return lemma, true
		}
	}
	return db.lemma(k, Unknown, true)
}

// lemma picks the base form of the case-folded word k from the headwords
// with senses of part of speech pos (any of them for Unknown). If surface is
// set, we can also return k itself.
func (db *DB) lemma(k string, pos POS, surface bool) (string, bool) {
	has := func(key string) bool {
		i, ok := db.find(key)
		return ok && (pos == Unknown || db.pos[i]&pos.bit() != 0)
	}
	if db.lang.Code == "en" {
		if base, irregular := englishIrregular[k]; irregular && has(base) {
			return base, true
		}
	}
	if surface && has(k) {
		return k, true
	}
	best, bestFreq := "", -1.0
	for _, c := range db.lang.candidates(k) {
		if f, ok := db.Frequency(c); ok && f > bestFreq && has(c) {
			best, bestFreq = c, f
		}
	}
	return best, best != ""
}

// minStem is the shortest stem, in runes, we let a suffix leave behind.
const minStem = 2

// candidates lists possible base forms of the case-folded word w, not
// including w.
func (l *Language) candidates(w string) []string {
	if l.Code == "en" {
		return englishCandidates(w)
	}
	var out []string
	stems := []string{w}
	for _, p := range l.prefixes {
		if s, ok := strings.CutPrefix(w, p); ok && utf8.RuneCountInString(s) > minStem {
			stems = append(stems, s)
		}
	}
	for _, stem := range stems {
		if stem != w {
			out = append(out, stem)
		}
		for _, suf := range l.suffixes {
			base, ok := strings.CutSuffix(stem, suf)
			if !ok || utf8.RuneCountInString(base) < minStem {
				continue
			}
			out = append(out, base)
			for _, e := range l.endings {
				out = append(out, base+e)
			}
		}
	}
	return out
}

// englishCandidates undoes regular English inflection: plurals and third
// person (-s, -es, -ies), past forms (-ed, -ied), participles (-ing),
// comparatives (-er, -est, -ier, -iest), doubled final consonants (stopped,
// bigger) and the possessive 's.
func englishCandidates(w string) []string {
	var out []string
	add := func(s string) {
		if utf8.RuneCountInString(s) >= minStem {
			out = append(out, s)
		}
	}
	for _, poss := range []string{"'s", "s'"} {
		if base, ok := strings.CutSuffix(w, poss); ok {
			add(base)
			if poss == "s'" {
				add(base + "s")
			}
			w = base
		}
	}
	type rule struct{ suffix, repl string }
	for _, r := range []rule{
		{"ies", "y"}, {"ied", "y"}, {"ier", "y"}, {"iest", "y"},
		{"es", ""}, {"s", ""},
		{"ed", ""}, {"ed", "e"}, {"ing", ""}, {"ing", "e"},
		{"er", ""}, {"er", "e"}, {"est", ""}, {"est", "e"},
	} {
		base, ok := strings.CutSuffix(w, r.suffix)
		if !ok || (r.suffix == "s" && strings.HasSuffix(base, "s")) {
			continue
		}
		add(base + r.repl)
		if r.repl == "" && doubled(base) {
			add(base[:len(base)-1])
		}
	}
	return out
}

// doubled reports whether s ends in a doubled consonant (stopp, bigg).
func doubled(s string) bool {
	n := len(s)
	if n < 3 || s[n-1] != s[n-2] {
		return false
	}
	return strings.IndexByte("bdfgklmnprtvz", s[n-1]) >= 0
}

// englishIrregular maps irregular inflected forms to their base forms. We
// left out forms that are common words in their own right (left, felt, saw,
// found, lost) on purpose.
var englishIrregular = map[string]string{
	"am": "be", "is": "be", "are": "be", "was": "be", "were": "be", "been": "be", "being": "be",
	"has": "have", "had": "have", "having": "have",
	"does": "do", "did": "do", "done": "do",
	"went": "go", "gone": "go", "goes": "go",
	"made": "make", "said": "say", "got": "get", "gotten": "get",
	"took": "take", "taken": "take", "came": "come", "seen": "see",
	"knew": "know", "known": "know", "thought": "think", "told": "tell",
	"gave": "give", "given": "give", "ran": "run", "wrote": "write", "written": "write",
	"sent": "send", "built": "build", "kept": "keep", "brought": "bring", "bought": "buy",
	"began": "begin", "begun": "begin", "held": "hold", "stood": "stand",
	"understood": "understand", "meant": "mean", "met": "meet", "paid": "pay",
	"sold": "sell", "spoke": "speak", "spoken": "speak", "chose": "choose", "chosen": "choose",
	"broke": "break", "broken": "break", "drove": "drive", "driven": "drive",
	"ate": "eat", "eaten": "eat", "grew": "grow", "grown": "grow", "drew": "draw", "drawn": "draw",
	"threw": "throw", "thrown": "throw", "flew": "fly", "flown": "fly", "wore": "wear", "worn": "wear",
	"won": "win", "became": "become", "forgot": "forget", "forgotten": "forget",
	"taught": "teach", "caught": "catch", "fought": "fight", "sought": "seek",
	"slept": "sleep", "spent": "spend", "lent": "lend", "dealt": "deal", "sat": "sit",
	"rang": "ring", "rung": "ring", "sang": "sing", "sung": "sing", "swam": "swim",
	"hid": "hide", "hidden": "hide", "rode": "ride", "ridden": "ride", "risen": "rise",
	"shook": "shake", "shaken": "shake", "woke": "wake", "woken": "wake", "froze": "freeze", "frozen": "freeze",
	"stole": "steal", "stolen": "steal", "tore": "tear", "torn": "tear",
	"children": "child", "men": "man", "women": "woman", "feet": "foot", "teeth": "tooth",
	"mice": "mouse", "geese": "goose", "wives": "wife", "knives": "knife",
	"halves": "half", "wolves": "wolf", "shelves": "shelf",
}
