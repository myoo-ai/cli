package prose

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// language holds what detectLang and the word-based rules need to know
// about a language. English has longer, hand-tuned phrase lists in rules.go,
// which its entry points to. The other languages fill the templates below
// with their own words, so adding a language means adding an entry here.
type language struct {
	name string
	// Frequent function words in lower case, such as articles, pronouns and
	// forms of "be". detectLang counts them, and rule-of-three doesn't take
	// one for a list item.
	common string
	// articles can open a list item ("the logs") without being one.
	articles string
	// The words that join the last item of a list ("A, B and C").
	and string
	// Negative parallelism, "not only A but also B" (notOnly), "it's not A,
	// it's B", and "A, not B" (notCloser) where the language marks it
	// clearly. "Not A, but B" ("nicht A, sondern B") is plain grammar in most
	// of them, so we leave it, as we do in English.
	negation []phrase
	// Assistant phrases ("I hope this helps").
	chatbot []phrase
}

var languages = map[string]*language{
	"en": {
		name:     "English",
		common:   "a about after all also an and any are as at be because been but by can could do does for from had has have he her his how i if in into is it its just more my no not of on one only or our out so some than that the their them then there these they this to up us was we were what when which who will with would you your",
		negation: negativeParallelism,
		chatbot:  chatbotPhrases,
	},
	"cs": {
		name:   "Czech",
		common: "a aby ale ani až bez by byl byla bylo být co což další do i jak jako je jeho její jen jiné jsem jsme jsou jste k kde když která které který kteří mezi může na nad nebo není o od po pod podobně pokud pro proto při s se si tak také tedy to tu už v ve však z za ze či že",
		and:    "a i nebo či",
		negation: slices.Concat(
			uwords(notOnly(`nejen`, `ale (?:i|také)`), `není to [^.;:!?\n]{1,60}?[,;] (?:je to|to je)`),
			notCloser(`a`, `ne`, csClause),
		),
		chatbot: uwords(`doufám, že (?:vám|ti) to pomůže`, `je důležité poznamenat`),
	},
	"de": {
		name:     "German",
		common:   "aber alle als also am an andere auch auf aus bei bin bis da damit dann das dass dem den denn der des die dies diese diesem diesen dieser doch du durch ein eine einem einen einer es für hat hatte ich ihr ihre im in ist ja jetzt kann kein keine man mit muss nach nicht noch nur ob oder schon sein sich sie sind so um und uns unter vom von vor war was weil weitere wenn werden wie wir wird wo zu zum zur zwischen über",
		articles: "der die das den dem des ein eine einen einem einer eines",
		and:      "und oder sowie",
		negation: uwords(notOnly(`nicht nur`, `sondern(?: auch)?`), `(?:es|das|dies) ist (?:nicht|kein\p{L}*) [^.;:!?\n]{1,60}?[,;] (?:es|das|dies) ist`),
		chatbot:  uwords(`ich hoffe,? das hilft`, `es ist wichtig zu beachten`, `zusammenfassend lässt sich sagen`),
	},
	"es": {
		name:     "Spanish",
		common:   "a al algo como con cuando de del desde donde el ella ellos en entre era es esa ese eso esta este esto está están fue ha han hasta hay la las le les lo los me mi muy más nada ni no nos o otra otras otro otros para pero poco por porque que qué se ser si sin sobre son su sus también tiene todo tu un una uno unos y ya",
		articles: "el la los las un una unos unas",
		and:      "y e o u",
		negation: slices.Concat(
			uwords(notOnly(`no (?:solo|sólo|solamente)`, `sino(?: también)?`), `no es [^.;:!?\n]{1,60}?[,;] es`),
			notCloser(`y`, `no (?:el|la|los|las|un|una|unos|unas|en|de|por|para|con|como|a|al|del|mi|tu|su|sus|este|esta|ese|esa)`, ""),
		),
		chatbot: uwords(`espero que esto (?:te|le) ayude`, `es importante (?:destacar|tener en cuenta)`),
	},
	"fr": {
		name:     "French",
		common:   "afin ai alors au autres aux avec avoir ce cela ces cet cette comme dans de des donc du elle elles en encore est et il ils je la le les leur leurs lui mais me même ne nous on ont ou où par pas peut plus pour qu quand que qui sa sans se ses si son sont sur tous tout très un une vous y à été être",
		articles: "le la les un une des du",
		and:      "et ou",
		negation: slices.Concat(
			uwords(notOnly(`non seulement`, `mais (?:aussi|également)`), `(?:ce|cela|ça) n['’](?:est|était) pas [^.;:!?\n]{1,60}?[,;] c['’](?:est|était)`),
			notCloser(``, `(?:pas|non pas)`, frClause),
		),
		chatbot: uwords(`j['’]espère que cela (?:vous |t['’])aide`, `il est important de noter`),
	},
	"it": {
		name:     "Italian",
		common:   "a ai al alla alle altre altri anche che chi ci come con cosa da dal dalla degli dei del della delle di dove e ed gli ha hanno ho i il in io la le lo loro ma mi molto ne nei nel nella non o per perché più quando quello questa questo se si sia sono su sua sue sui suo tra un una uno è",
		articles: "il lo la i gli le un uno una",
		and:      "e ed o od",
		negation: slices.Concat(
			uwords(notOnly(`non solo`, `ma anche`), `non è [^.;:!?\n]{1,60}?[,;] è`),
			notCloser(`e`, `non (?:il|lo|la|i|gli|le|un|uno|una|in|di|per|con|come|a|al|del|della|da)`, ""),
		),
		chatbot: uwords(`spero che questo (?:ti|le) sia utile`, `è importante notare`),
	},
	"nl": {
		name:     "Dutch",
		common:   "aan al als andere bij dan dat de deze die dit door een en er geen had heb heeft het hier hij hoe ik in is je kan maar me meer met mij naar niet nog nu of om omdat ons ook op over te tot u uit van veel voor was wat we wel werd wie wij worden wordt zal ze zich zijn zo zou",
		articles: "de het een",
		and:      "en of",
		negation: uwords(notOnly(`niet alleen`, `maar ook`), `(?:het|dit|dat) is (?:niet|geen) [^.;:!?\n]{1,60}?[,;] (?:het|dit|dat) is`),
	},
	"pl": {
		name:   "Polish",
		common: "a aby ale bardzo bez by być był była było co czy dla do gdy go i ich inne ja jak jako jego jej jest już która które który lub ma mi może na nad nie niż o od on ona oraz po pod przez przy się są ta tak także te ten też to tu tylko w we więc z za ze że",
		and:    "i oraz lub albo czy",
		negation: slices.Concat(
			uwords(notOnly(`nie tylko`, `ale (?:też|także|również)`), `to nie jest [^.;:!?\n]{1,60}?[,;] to`),
			notCloser(`a`, `nie (?:w|na|z|do|dla|przez|jako|o|od|po|przy|za)`, ""),
		),
		chatbot: uwords(`mam nadzieję, że to pomoże`, `warto zauważyć`, `należy zauważyć`),
	},
	"pt": {
		name:     "Portuguese",
		common:   "a ao aos as até com como da das de dela dele do dos e ela ele eles em entre era essa esse está estão eu foi isso isto já mais mas me muito na nas nem no nos não o os ou outras outros para pela pelo por porque quando que se sem ser seu seus sua suas também tem um uma você é",
		articles: "o a os as um uma uns umas",
		and:      "e ou",
		negation: slices.Concat(
			uwords(notOnly(`não (?:só|apenas|somente)`, `mas também`), `não é [^.;:!?\n]{1,60}?[,;] é`),
			notCloser(`e`, `não (?:o|a|os|as|um|uma|em|de|por|para|com|como|ao|à|do|da|no|na)`, ""),
		),
		chatbot: uwords(`espero que isso ajude`, `é importante notar`),
	},
	"ru": {
		name:   "Russian",
		common: "а без бы был была были было быть в вам во вот все всё вы где да для до другие его ее если есть еще ещё её же за и из или им их к как когда кто ли мы на над не нет но о об он она они от по при с со так также то только ты у уже что это я",
		and:    "и или",
		negation: slices.Concat(
			uwords(notOnly(`не только`, `но и`), `это не [^.;:!?\n]{1,60}?[,;] это`),
			notCloser(``, `а не`, ""),
		),
	},
	"sk": {
		name:   "Slovak",
		common: "a aby aj ako ale alebo ani až bez bol bola boli bolo by byť do i ich iné ja je jeho jej k kde keď ktorá ktoré ktorí ktorý len medzi môže na nad nie o od po pod podobne pokiaľ pre preto pri s sa si sme so som ste sú tak tiež to tu už v vo však z za zo čo ďalšie že",
		and:    "a i aj alebo či",
		negation: slices.Concat(
			uwords(notOnly(`nielen`, `ale (?:aj|tiež)`), `nie je to [^.;:!?\n]{1,60}?[,;] (?:je to|to je)`),
			notCloser(`a`, `nie`, skClause),
		),
		chatbot: uwords(`dúfam, že (?:vám|ti) to pomôže`, `je dôležité poznamenať`),
	},
}

// The clause lists match the word after a negation that negates a verb, a
// quantity or a degree ("is not", "not all", "not yet"), where it doesn't
// name an alternative.
const (
	csClause = `vždy|všechn\p{L}*|každ\p{L}*|nutně|moc|příliš|úplně|zcela|tak|ještě|jen|až|že|však|zas|zase|jednou|jeden|jedna|jedno`
	frClause = `encore|toujours|tous|tout|toutes|forcément|nécessairement|seulement|du|très|trop|assez|plus|vraiment|mal|loin|question|moins|si|sans|que|même`
	skClause = `je|sú|som|si|sme|ste|bol|bola|bolo|boli|bude|budú|všetk\p{L}*|každ\p{L}*|vždy|úplne|celkom|veľmi|príliš|ešte|až|tak|ani|nikdy|že|nutne|moc|veľa|dosť|práve|hneď|však|zas|zase|raz|jeden|jedna|jedno`
)

// commonSets holds the common words of each language as a set.
var commonSets = func() map[string]map[string]bool {
	m := map[string]map[string]bool{}
	for code, l := range languages {
		m[code] = setOf(strings.Fields(l.common))
	}
	return m
}()

func setOf(ws []string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

// detectLang returns the language whose common words make up the largest
// share of ws, the lower-case words of the text. Function words make up a
// third or more of running prose in all these languages, and still 5 to 10
// % of a reference full of metric names and option tables. Text in a
// language we don't know stays under 3 % (Chinese), or matches the short
// words of several languages equally (Hungarian "a" is a word in 6 of
// them). Then we return "" and only the language-independent rules run.
// Text of under 20 words is too short to tell, so it counts as English, the
// language most input is in.
func detectLang(ws []string) string {
	best, bestN, secondN := "", 0, 0
	for _, code := range slices.Sorted(maps.Keys(languages)) {
		n := 0
		for _, w := range ws {
			if commonSets[code][w] {
				n++
			}
		}
		switch {
		case n > bestN:
			best, bestN, secondN = code, n, bestN
		case n > secondN:
			secondN = n
		}
	}
	switch {
	case bestN >= 2 && bestN > secondN && bestN*25 >= len(ws):
		return best
	case len(ws) < 20:
		return "en"
	}
	return ""
}

// DetectLanguage returns the code of the language ws is written in, or "" if
// it can't tell (see detectLang). ws are lower-case words as returned by
// likelihood.Words.
func DetectLanguage(ws []string) string { return detectLang(ws) }

// RunningProse reports whether ws read as running prose rather than a list
// of names or CSS classes. The common words of one language have to make up
// a sixth of them or more. Function words alone make up a third of prose in
// all the languages we know.
func RunningProse(ws []string) bool {
	for _, common := range commonSets {
		n := 0
		for _, w := range ws {
			if common[w] {
				n++
			}
		}
		if n*6 >= len(ws) && n > 0 {
			return true
		}
	}
	return false
}

// langsWhere returns the sorted codes of the languages that have what has
// asks for, for Rule.Langs.
func langsWhere(has func(l *language) bool) []string {
	var out []string
	for code, l := range languages {
		if has(l) {
			out = append(out, code)
		}
	}
	slices.Sort(out)
	return out
}

// langDetector reports the phrases that pick returns for the language of
// the text.
func langDetector(pick func(l *language) []phrase) func(t *text) []Violation {
	detectors := map[string]func(t *text) []Violation{}
	for code, l := range languages {
		detectors[code] = phraseDetector(pick(l))
	}
	return func(t *text) []Violation {
		if d := detectors[t.lang]; d != nil {
			return d(t)
		}
		return nil
	}
}

// uwords is like words, for phrases that start or end with letters outside
// ASCII. RE2's \b only knows ASCII word characters, so it won't match next
// to "é" or a Cyrillic letter. We match the boundary characters outside
// group 1, which is what phraseDetector reports. A combined expression for
// all phrases would consume the separator after a match and miss a phrase
// that follows right after it.
func uwords(alternatives ...string) []phrase {
	out := make([]phrase, len(alternatives))
	for i, a := range alternatives {
		re := regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_])(` + a + `)(?:[^\p{L}\p{N}_]|$)`)
		out[i] = phrase{re: re, anchors: anchorsOf([]string{a})}
	}
	return out
}

// notOnly builds the "not only A ... but also B" template for a language.
// The gap has to stay inside a sentence and under 100 characters, so we
// don't pair up two unrelated clauses that are far apart.
func notOnly(first, second string) string {
	return first + `[^\p{L}\p{N}_][^.!?\n]{1,100}?[^\p{L}\p{N}_]` + second
}

// closerWords matches the alternative that an "A, not B" closer names, up
// to 6 words.
const closerWords = `[\p{L}\p{N}][\p{L}\p{N}'’/-]*(?:[ \t]+[\p{L}\p{N}][\p{L}\p{N}'’/-]*){0,5}`

// notCloser builds the "A, not B" closer. It matches a negation after a
// comma, a dash, an opening parenthesis or the word and (if given), followed
// by up to 6 words that end the clause ("VIP je nadstavba, nie
// prerušenie."). We report it from the negation on. A closer that ends in a comma consumes the comma
// that the next one starts with ("nie čakaním, nie zbieraním mincí"), so
// those that end in a comma get a phrase of their own, which phraseDetector
// scans separately.
func notCloser(and, neg, clause string) []phrase {
	sep := `,[ \t]*|[ \t][\x{2013}\x{2014}][ \t]*|\(`
	if and != "" {
		sep = `,[ \t]*(?:(?:` + and + `)[ \t]+)?|[ \t][\x{2013}\x{2014}][ \t]*|\(|[ \t](?:` + and + `)[ \t]+`
	}
	var out []phrase
	for _, end := range []string{`[.!?;:)]|[ \t]*(?:\r?\n|$)`, `,[ \t]`} {
		re := regexp.MustCompile(`(?i)(?:` + sep + `)(` + neg + `[ \t]+` + closerWords + `)[*_]*(?:` + end + `)`)
		out = append(out, phrase{re: re, anchors: anchorsOf([]string{neg}), except: clauseRe(neg, clause)})
	}
	return out
}

// clauseRe matches a negation followed by a word of clause.
func clauseRe(neg, clause string) *regexp.Regexp {
	if clause == "" {
		return nil
	}
	return regexp.MustCompile(`(?i)^` + neg + `[ \t]+(?:` + clause + `)(?:[^\p{L}\p{N}_]|$)`)
}

// listWords is what detectRuleOfThree needs to know about a language.
type listWords struct {
	// three matches a list of 3 items in groups 1 to 3.
	three *regexp.Regexp
	// stop reports function words, which don't start or fill a list item,
	// and article the ones that may open an item.
	stop, article func(w string) bool
	// intro matches a preposition right before the first item, nil if we
	// don't know the language's prepositions.
	intro *regexp.Regexp
}

// listItem is a list item of 1 or 2 words.
const listItem = `\p{L}[\p{L}'’-]*(?:` + sp + `\p{L}[\p{L}'’-]*)?`

var lists = func() map[string]listWords {
	m := map[string]listWords{
		"en": {three: threeItemRe, stop: listWordRe.MatchString, article: articleRe.MatchString, intro: introRe},
	}
	for code, l := range languages {
		if code == "en" {
			continue
		}
		and := strings.Join(strings.Fields(l.and), "|")
		stop := setOf(append(strings.Fields(l.common), strings.Fields(l.and)...))
		articles := setOf(strings.Fields(l.articles))
		m[code] = listWords{
			three: regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_])(` + listItem + `),` + sp + `(` + listItem + `),?` + sp +
				`(?:` + and + `)` + sp + `(` + listItem + `)(?:[^\p{L}\p{N}_]|$)`),
			stop:    func(w string) bool { return stop[strings.ToLower(w)] },
			article: func(w string) bool { return articles[strings.ToLower(w)] },
		}
	}
	return m
}()
