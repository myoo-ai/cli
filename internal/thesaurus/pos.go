package thesaurus

import "strings"

// POS is a coarse part of speech.
type POS uint8

// Parts of speech the database knows about. Unknown means the thesaurus or
// the context can't tell us.
const (
	Unknown POS = iota
	Noun
	Verb
	Adjective
	Adverb
	numPOS
)

func (p POS) bit() uint8 { return 1 << (p - 1) }

// posLabels maps lower-case part-of-speech labels from the supported
// thesauri to parts of speech. Labels that aren't listed (or ambiguous ones
// like "adjectif nom") count as Unknown.
var posLabels = map[string]POS{
	"noun": Noun, "n": Noun, "nom": Noun, "s.": Noun, "s.m.": Noun, "s.f.": Noun,
	"s.m.f.": Noun, "s.m.inv.": Noun, "s.f.inv.": Noun, "subst": Noun, "същ.": Noun,
	"sam.": Noun, "podst. jm.": Noun, "сущ.": Noun, "اسم": Noun, "مصدر": Noun,
	"substantivo": Noun, "substantivo masculino": Noun, "substantivo feminino": Noun,

	"verb": Verb, "v": Verb, "v.": Verb, "verbe": Verb, "vb.": Verb, "гл.": Verb,
	"gl.": Verb, "slov.": Verb, "فعل": Verb, "verbo transitivo": Verb,
	"verbo intransitivo": Verb,

	"adj": Adjective, "adj.": Adjective, "adjectif": Adjective, "agg.": Adjective,
	"прил.": Adjective, "prid.": Adjective, "příd. jm.": Adjective, "a": Adjective,
	"adxectivo": Adjective,

	"adv": Adverb, "adv.": Adverb, "adverbe": Adverb, "avv.": Adverb, "нар.": Adverb,
	"prisl.": Adverb, "přísl.": Adverb, "adverbio": Adverb,
}

// senseLabel normalizes the first field of a sense line: "(noun)", "[n]",
// "   (-) " become "noun", "n" and "-".
func senseLabel(field string) string {
	s := strings.TrimSpace(field)
	if len(s) >= 2 && (s[0] == '(' && s[len(s)-1] == ')' || s[0] == '[' && s[len(s)-1] == ']') {
		s = s[1 : len(s)-1]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// englishVerbCues come before a verb: "to", modals, auxiliaries and subject
// pronouns.
var englishVerbCues = wordSet(`to can could will would shall should may might must do does did don't doesn't didn't
	won't wouldn't can't cannot couldn't shouldn't mustn't hasn't haven't hadn't i we you they he she it let's please
	i'll we'll you'll they'll he'll she'll it'll i'd we'd you'd they'd i've we've you've they've`)

// englishSubjectCues follow the subject of a clause, so the word before them
// has to be a noun: "the guide will".
var englishSubjectCues = wordSet(`will would can could should may might must shall is are was were has have had
	does did isn't aren't wasn't weren't hasn't haven't doesn't didn't won't can't`)

// perfectAuxiliaries lists the auxiliary forms of each language that put a
// past participle right after them ("have finished", "avons terminé",
// "hemos terminado"). We leave out German and the Slavic languages, because
// their participles rarely follow the auxiliary directly.
var perfectAuxiliaries = map[string]string{
	"ca": `he has ha hem heu han havia havies havíem havíeu havien haver`,
	"en": `have has had having`,
	"es": `he has ha hemos habéis han había habías habíamos habíais habían habré habrá habría haber habiendo`,
	"fr": `ai as a avons avez ont avais avait avions aviez avaient aurai auras aura aurons aurez auront aurait avoir ayant`,
	"it": `ho hai ha abbiamo avete hanno avevo avevi aveva avevamo avevate avevano avrò avrà avrebbe avere avendo`,
	"pt": `tenho tens tem temos tendes têm tinha tinhas tínhamos tinham terei terá teria ter tendo`,
	"ro": `am ai a ați au`,
}

// englishNounCues come before a noun or an adjective: articles, determiners,
// numerals, possessives and prepositions.
var englishNounCues = wordSet(`the a an this these those my your our their his its every each some any no
	several many few both one two three four five six seven eight nine ten
	of in on at for with from by about into through over under between during without`)

// POSHint guesses the part of speech of word from its neighbours (all
// case-folded). prev2, prev and next are the adjacent words if there's only
// whitespace between them, and "" otherwise. A word right after a perfect
// auxiliary is a verb in all languages (in English only if it looks like a
// participle). English has more rules, so there a word is
//
//   - a verb after "to", a modal or a subject pronoun ("can write"),
//   - after a determiner or preposition, an adjective if a content word
//     follows ("the annual review") and a noun otherwise ("the release with"),
//   - a noun before an auxiliary or after a word that's only an adjective
//     ("the guide will", "powerful tools"),
//   - a verb after an adverb in -ly ("carefully designed"),
//   - a noun after a determiner and a content word, at the end of a phrase
//     ("a seamless experience."),
//   - an adverb if it ends in -ly.
func (db *DB) POSHint(prev2, prev, word, next string) POS {
	l := db.lang
	if l.Code != "en" {
		if l.auxiliaries[prev] {
			return Verb
		}
		return Unknown
	}
	_, irregular := englishIrregular[word]
	content := func(w string) bool { return w != "" && !l.IsStopword(w) }
	switch {
	case l.auxiliaries[prev] && (strings.HasSuffix(word, "ed") || strings.HasSuffix(word, "en") || irregular):
		return Verb
	case englishVerbCues[prev]:
		return Verb
	case englishNounCues[prev]:
		if content(next) {
			return Adjective
		}
		return Noun
	case englishSubjectCues[next], content(prev) && db.onlyAdjective(prev):
		return Noun
	case strings.HasSuffix(prev, "ly") && !englishNounCues[prev2]:
		return Verb
	case englishNounCues[prev2] && content(prev) && !content(next):
		return Noun
	case strings.HasSuffix(word, "ly"):
		return Adverb
	}
	return Unknown
}

// onlyAdjective reports whether the base form of w is an adjective, and not
// a noun or a verb.
func (db *DB) onlyAdjective(w string) bool {
	lemma, ok := db.lemma(w, Unknown, true)
	if !ok {
		return false
	}
	i, _ := db.find(lemma)
	return db.pos[i]&(Adjective.bit()|Noun.bit()|Verb.bit()) == Adjective.bit()
}
