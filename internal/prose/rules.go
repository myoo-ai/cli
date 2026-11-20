package prose

import (
	"cmp"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rule is a prose_lint check. We can't detect Manual rules mechanically, but
// we list them so agents with a skill can review them.
type Rule struct {
	ID             string
	Description    string
	Recommendation string
	Manual         bool
	// Langs limits the rule to these --lang codes. nil means it applies to
	// all languages.
	Langs []string
	// ProseOnly rules target written deliverables. LintString, which
	// code_lint runs on comments, leaves them out.
	ProseOnly bool
	// Comment rules only apply to code comments, which code_lint lints
	// through LintComment.
	Comment bool
	// Unmasked rules also check code, URLs, e-mail addresses and paths,
	// which Lint otherwise skips. Only a pattern that is never right there
	// either belongs here.
	Unmasked bool
	detect   func(t *text) []Violation
}

// Violation is a single finding. If Detail is set, it names the offending
// text and gets appended to the description.
type Violation struct {
	Rule   *Rule
	Start  int // byte offsets into the input
	End    int
	Detail string
	Fix    []Edit // nil if there's no automatic fix
}

// Edit replaces the input bytes [Start, End) with Text.
type Edit struct {
	Start, End int
	Text       string
}

// joinAdvice puts joining with a conjunction first. Agents write fewer
// compound sentences and fewer "however" and "therefore" than people, so
// splitting every joint into short sentences makes text read more like AI.
const joinAdvice = "try to use a different way to join the sentences (for instance, by replacing the %s with a conjunction or connective such as \", and\", \", but\", \", so\", \", because\", \", which\" or \", therefore\", or by adding a period and starting a new sentence)."

// Rules is the catalogue, in the order --list-rules prints it.
var Rules = []*Rule{
	{
		ID:             "capitals-as-heading",
		Description:    "use of capital letters to signify heading style in non-markdown plaintext prose.",
		Recommendation: "write headings in sentence case, or use markdown headings if the output is markdown.",
		detect:         detectCapitalsHeading,
	},
	{
		ID:             "utf8-chars-as-icons",
		Description:    "use of utf-8 characters as a replacement for images/icons.",
		Recommendation: "in plaintext prose, use ASCII characters instead (for example, `->` for a right arrow). see also skill: no-ai-cliche.",
		detect:         detectIcons,
	},
	{
		ID:             "emoji",
		Description:    "use of emoji in prose.",
		Recommendation: "remove the emoji. say it with words if it had meaning.",
		detect:         detectEmoji,
	},
	{
		ID:             "emdash",
		Description:    "use of emdash.",
		Recommendation: strings.Replace(joinAdvice, "%s", "emdash", 1) + " if the dash must remain, always replace it with a regular dash.",
		detect:         detectEmdash,
	},
	{
		ID:             "semicolon-inbetween-sentences",
		Description:    "use of semicolon as a conjunction in middle of a compound sentence.",
		Recommendation: strings.Replace(joinAdvice, "%s", "semicolon", 1) + " alternatively, replace the semicolon with a regular comma if applicable. when one of the sentences is a single word, it is also fine to remove that sentence entirely, for example `Proposed; requires approval` becomes `Requires approval`.",
		detect:         detectSemicolon,
	},
	{
		ID:             "colon-inbetween-sentences",
		Description:    "use of colon as a conjunction in middle of a compound sentence.",
		Recommendation: strings.Replace(joinAdvice, "%s", "colon", 1),
		detect:         detectColon,
	},
	{
		ID:             "label-colon",
		Description:    "use of a colon in a code comment to attach an explanation to a short label or a code sample (`fast path: nothing to do`, `q.x: member of whatever q resolves to`).",
		Recommendation: "write the explanation as a sentence without the label, or keep only the code sample if it says enough (`// q.x`). tags such as `TODO:`, `NOTE:`, `Deprecated:` and `SAFETY:` are fine.",
		Comment:        true,
		detect:         detectLabelColon,
	},
	{
		ID:             "bullet-as-separator",
		Description:    "use of a bullet point character as a separator between inline items.",
		Recommendation: "separate the items with commas, a sentence, or line breaks.",
		detect:         detectBulletSeparator,
	},
	{
		ID:             "left-padded-numbers",
		Description:    "use of zero-padded numbers (01, 02, ...) for numbering.",
		Recommendation: "number items without padding (1, 2, ...) or drop the numbering when the order is obvious.",
		detect:         detectPaddedNumbers,
	},
	{
		ID:             "curly-quotes",
		Description:    "use of typographic (curly) quotes and apostrophes.",
		Recommendation: "use straight quotes (\" and ') in plaintext and code-adjacent prose.",
		Langs:          []string{"en"},
		detect:         detectCurlyQuotes,
	},
	{
		ID:             "unicode-ellipsis",
		Description:    "use of the single-character ellipsis, including inside code and URLs.",
		Recommendation: "write 3 periods (...) instead, or end the sentence.",
		Unmasked:       true,
		detect:         detectEllipsis,
	},
	{
		ID:             "rule-of-three",
		Description:    "use of a list of exactly 3 items (\"A, B and C\").",
		Recommendation: "AI prose reaches for 3 examples by habit. keep only the items that matter, or name as many as the point needs.",
		Langs:          langsOf(lists),
		detect:         detectRuleOfThree,
	},
	{
		ID:             "negative-parallelism",
		Description:    "use of negative parallelism (`not just X, but Y`, `it's not X, it's Y`, `X is Y, not Z`, `not only ... but also`, `less X, more Y`, `no X, no Y, just Z`, and their counterparts in other languages, such as `X, nie Y` in Slovak).",
		Recommendation: "state the point directly instead of contrasting it with something nobody claimed.",
		Langs:          langsWhere(func(l *language) bool { return l.negation != nil }),
		detect:         langDetector(func(l *language) []phrase { return l.negation }),
	},
	{
		ID:             "ai-vocabulary",
		Description:    "use of a word that is overrepresented in AI generated text",
		Recommendation: "use a plainer, more specific word, or rephrase the sentence.",
		Langs:          []string{"en"},
		detect:         exceptProperNouns(withoutOverlap(phraseDetector(aiVocabulary), significancePuffery)),
	},
	{
		ID:             "filler-phrases",
		Description:    "use of a filler phrase that adds no information",
		Recommendation: "delete the phrase and start with the actual point.",
		Langs:          []string{"en"},
		detect:         phraseDetector(fillerPhrases),
	},
	{
		ID:             "chatbot-phrases",
		Description:    "use of conversational chatbot phrasing or an unfilled template placeholder",
		Recommendation: "remove the phrase. deliverables should not address the reader like a chat assistant.",
		Langs:          langsWhere(func(l *language) bool { return l.chatbot != nil }),
		detect:         quietWhen(langDetector(func(l *language) []phrase { return l.chatbot }), exampleIntroRe),
	},
	{
		ID:             "knowledge-cutoff",
		Description:    "use of a knowledge-cutoff or limited-information disclaimer",
		Recommendation: "remove the disclaimer. verify the fact or state what is unknown.",
		Langs:          []string{"en"},
		detect:         phraseDetector(knowledgeCutoff),
	},
	{
		ID:             "transition-openers",
		Description:    "use of a transition word that AI text opens sentences with more often than people do",
		Recommendation: "drop the transition word. if the sentences need connecting, join them into one sentence with a conjunction (`and`, `but`, `so`, `because`, `which`) or use a connective people use (`however`, `also`, `therefore`).",
		Langs:          []string{"en"},
		detect:         phraseDetector(transitionOpeners),
	},
	{
		ID:             "superficial-analysis",
		Description:    "use of a trailing participle clause that adds vague significance (`, highlighting ...`, `, underscoring ...`).",
		Recommendation: "remove the clause, or replace it with a concrete fact in its own sentence.",
		Langs:          []string{"en"},
		detect:         phraseDetector(superficialAnalysis),
	},
	{
		ID:             "significance-puffery",
		Description:    "use of a stock phrase that inflates significance (`stands as a testament`, `plays a pivotal role`, `why this matters`)",
		Recommendation: "say what the thing does or what happened. if it matters, show the concrete consequence instead of asserting importance.",
		Langs:          []string{"en"},
		detect:         phraseDetector(significancePuffery),
	},
	{
		ID:             "copula-avoidance",
		Description:    "use of a wordier verb in place of `is` or `has` (`serves as a`, `functions as the`, `boasts a`, `stands as the`)",
		Recommendation: "use is, are or has. `The Config struct serves as the central place for settings` becomes `Config holds the settings`.",
		Langs:          []string{"en"},
		detect:         detectCopula,
	},
	{
		ID:             "nominalization",
		Description:    "use of a light verb with an action noun where the verb would do (`performs validation of`, `carries out the initialization of`, `conducts an analysis of`)",
		Recommendation: "use the verb: `performs validation of the input` becomes `validates the input`.",
		Langs:          []string{"en"},
		detect:         phraseDetector(nominalization),
	},
	{
		ID:             "meta-preamble",
		Description:    "use of a sentence that announces the text instead of saying something (`This guide explains`, `In this section, we will`, `Below you will find`)",
		Recommendation: "delete the announcement and start with the content. headings already tell the reader what a section covers.",
		Langs:          []string{"en"},
		detect:         phraseDetector(metaPreamble),
	},
	{
		ID:             "hedging-disclaimer",
		Description:    "use of a generic caveat or advice to test, verify or read the docs (`make sure to test thoroughly`, `results may vary`, `consult the official documentation`)",
		Recommendation: "delete it, or replace it with the concrete risk and what to check.",
		Langs:          []string{"en"},
		detect:         phraseDetector(hedgingDisclaimer),
	},
	{
		ID:             "procedural-reassurance",
		Description:    "use of a reassurance about what a change kept instead of what it changed (`while preserving existing behavior`, `without breaking existing functionality`, `ensuring backward compatibility`)",
		Recommendation: "say what changed. mention a non-change only when a reader would otherwise assume the opposite.",
		Langs:          []string{"en"},
		detect:         phraseDetector(proceduralReassurance),
	},
	{
		ID:             "chatbot-artifact",
		Description:    "leaked chatbot citation marker (`oaicite`, `turn0search0`, `utm_source=chatgpt.com`) or unfilled placeholder (`INSERT_URL`, `2025-xx-xx`)",
		Recommendation: "delete the citation marker and cite the real source. fill in or remove the placeholder.",
		detect:         detectArtifacts,
	},
	{
		ID:             "invisible-characters",
		Description:    "use of an invisible character (zero-width space or joiner, word joiner, soft hyphen, stray byte order mark, narrow no-break space)",
		Recommendation: "delete the character. replace a narrow no-break space with a regular space.",
		detect:         detectInvisible,
	},
	{
		ID:             "locale-quotes",
		Description:    "use of English-style quotes (`\u201C...\u201D`) in a language with its own quotation marks.",
		Recommendation: "use the quotation marks of the language: `\u201E...\u201C` in German and Czech, `\u201E...\u201D` in Polish and `\u00AB...\u00BB` in Russian.",
		Langs:          langsOf(localeQuotePairs),
		detect:         detectLocaleQuotes,
	},
	{
		ID:             "inline-header-list",
		Description:    "use of bold labels to open list items or paragraphs (`- **Speed:** ...`).",
		Recommendation: "write the items as plain sentences, or drop the labels. use a heading or a table if the labels structure the text.",
		detect:         detectInlineHeaderList,
	},
	{
		ID:             "title-case-heading",
		Description:    "use of Title Case in a heading (`Getting Started With The Tool`).",
		Recommendation: "write headings in sentence case. keep capitals only for proper nouns, acronyms and the first word after a colon.",
		detect:         detectTitleCaseHeading,
	},
	{
		ID:             "numbered-headings",
		Description:    "use of section numbers in headings (`## 1. Overview`, `### 7.1 Setup`).",
		Recommendation: "drop the numbers and refer to sections by their names. if the order matters, say so in the text, or write the steps as a numbered list instead of headings.",
		ProseOnly:      true,
		detect:         detectNumberedHeadings,
	},
	{
		ID:             "excessive-bold",
		Description:    "use of bold for emphasis all over the text (3 or more bold spans in a paragraph or list item, or more than one per 40 words).",
		Recommendation: "keep bold for the one thing a skimmer must see, and write the rest as plain text.",
		detect:         detectExcessiveBold,
	},
	{
		ID:             "backslash-escapes-in-prose",
		Description:    "use of a backslash escape that markdown doesn't need (`## 1\\. Goal`, `A \\= B`, `\\~50 %`), as a converter like a Google Docs export leaves them.",
		Recommendation: "remove the backslash. escape only what markdown would format otherwise: `*`, `_`, backticks, brackets, `<`, `|` in tables, and a `#`, `-`, `+` or `1.` that opens a line.",
		ProseOnly:      true,
		detect:         detectBackslashEscapes,
	},
	{
		ID:             "empty-intensifier",
		Description:    "use of an intensifier that adds no information (`very`, `extremely`, `highly`, `significantly`, `truly`)",
		Recommendation: "drop the intensifier, or replace the phrase with a stronger word or the number that shows the degree.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         detectIntensifiers,
	},
	{
		ID:             "credibility-adverb",
		Description:    "use of an adverb that asks the reader to believe a claim (`genuinely`, `actually`, `in fact`, `indeed`)",
		Recommendation: "drop the adverb and give the evidence that shows the claim. `The hotfix is genuinely applied` becomes `The hotfix is applied. All nine hunks are in the deployed code.`",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         withoutOverlap(phraseDetector(credibilityAdverbs), transitionOpeners),
	},
	{
		ID:             "worth-construction",
		Description:    "use of `worth <verb>ing` or a rating adjective that rates a thing instead of saying it (`worth noting`, `worth asking for`, `a notable exception`)",
		Recommendation: "give the instruction (`ask for it`) or state the fact. `It is worth noting that X` is just `X`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         quietWhen(withoutOverlap(phraseDetector(worthConstruction), fillerPhrases), thingRe),
	},
	{
		ID:             "false-ease",
		Description:    "use of a word that promises a step is easy (`simply`, `just run`, `easily`, `effortlessly`)",
		Recommendation: "drop the word and show the step.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         withoutOverlap(phraseDetector(falseEase), transitionOpeners),
	},
	{
		ID:             "stacked-hedge",
		Description:    "use of 2 hedges on a single claim (`could potentially`, `might possibly`, `appears to possibly`)",
		Recommendation: "say what was found, or say what is unknown and why.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(stackedHedges),
	},
	{
		ID:             "participial-closer",
		Description:    "use of a trailing participle clause that tacks a consequence onto a sentence (`, making ...`, `, allowing ...`, `, ensuring ...`)",
		Recommendation: "end the sentence and give the consequence its own sentence with a subject and a verb, or cut it.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         withoutOverlap(withoutOverlap(phraseDetector(participialClosers), superficialAnalysis), proceduralReassurance),
	},
	{
		ID:             "rather-than-gerund",
		Description:    "use of `rather than` with a gerund after a word of another form (`verified rather than assuming`, `checked rather than trusting`)",
		Recommendation: "say what to do and what it gives the reader: `open the PNG and compare it to the source`. a comparison with matching forms is fine: `inferred rather than observed`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         detectRatherThan,
	},
	{
		ID:             "hollow-adjective",
		Description:    "use of an adjective that praises without saying anything (`powerful`, `comprehensive`, `world-class`, `state-of-the-art`)",
		Recommendation: "state the concrete capability.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         exceptProperNouns(phraseDetector(hollowAdjectives)),
	},
	{
		ID:             "personified-object",
		Description:    "use of a verb that makes an object act like a person (`the header carries the ID`, `only nine days of logs survive`)",
		Recommendation: "use the plain verb: `the header contains the ID`, `the logs go back to 2026-09-09`, `all seven are still on disk`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         exceptFollowedBy(phraseDetector(personifiedObjects), phrasalAfterRe),
	},
	{
		ID:             "journey-imagery",
		Description:    "use of journey or gatekeeper imagery for how a product works (`first line of defense`, `at the front door`, `stops them in their tracks`)",
		Recommendation: "name the component, its action and the object: `the WAF blocks malicious HTTP requests to the store`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(journeyImagery),
	},
	{
		ID:             "shop-talk",
		Description:    "use of in-group metaphor for an event (`blast radius`, `dwell time`, `in the wild`, `landed on the server`, `attack surface`)",
		Recommendation: "name the literal event: the attacker uploaded a file, started a process, read the database.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(shopTalk),
	},
	{
		ID:             "thing-as-subject",
		Description:    "use of a thing as the subject of a sentence about what a person needs or can do (`the report lands with your developer`, `three findings need your attention`, `the CSV lets your team track progress`)",
		Recommendation: "make the person the subject: `your developer gets the report`, `your team can track progress in the CSV`. when the person is only implied, write the instruction.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(thingAsSubject),
	},
	{
		ID:             "chatty-verdict",
		Description:    "use of a sales-call verdict (`X is the one`, `the way to go`, `your best bet`, `that's where X comes in`, `has you covered`)",
		Recommendation: "put the condition first and name the choice plainly: `Above 20 million in revenue, you need Enterprise.`",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(chattyVerdicts),
	},
	{
		ID:             "slogan-compression",
		Description:    "use of a tool or a quantity as the subject of something the reader does (`one command turns it off`, `two clicks restore the backup`)",
		Recommendation: "make the reader the subject and put the condition first: `If you need to turn it off, you can do so with a single command.`",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(sloganCompression),
	},
	{
		ID:             "clause-for-adjective",
		Description:    "use of a clause that invents people who failed to act, where an adjective does the job (`logins nobody has accounted for`)",
		Recommendation: "use the adjective: `unverified logins`, `unreviewed files`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(clauseForAdjective),
	},
	{
		ID:             "regard-instruction",
		Description:    "use of an instruction about how to regard something (`treat those stores as affected`, `consider the key compromised`)",
		Recommendation: "state the fact that leads there: `the same server runs those two stores`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(regardInstruction),
	},
	{
		ID:             "corporate-jargon",
		Description:    "use of a corporate word where a plain one exists (`utilize`, `facilitate`, `initiate`, `obtain`, `regarding`, `purchase`, `demonstrates`, `the argument rests on`)",
		Recommendation: "use the plain word: use, help, start, get, about, buy, shows, is based on.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         phraseDetector(corporateJargon),
	},
	{
		ID:             "heading-commentary",
		Description:    "use of a heading that comments on the document instead of naming its subject (`The timestamp problem, stated up front`)",
		Recommendation: "name the subject as a plain noun phrase: `Incomplete timestamps`.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         detectHeadingCommentary,
	},
	{
		ID:             "words-instead-of-numbers",
		Description:    "use of written numbers in sentences instead of numbers, e.g. `three` -> `3`",
		Recommendation: "write the number as digits: `three stores` becomes `3 stores`. hyphenated compounds (`two-factor`) and `one` on its own are left alone.",
		Langs:          []string{"en"},
		ProseOnly:      true,
		detect:         exceptProperNouns(detectNumberWords),
	},
	{
		ID:             "unnecessary-context",
		Description:    "providing context when not necessary (ex. \"What is the capital of France?\" -> \"The capital of France is Paris. Located on the Seine River in the north-central part of the country, it ...\", the second sentence is unnecessary and provides additional information beyond the scope of the original question).",
		Recommendation: "avoid polluting outputs with information that is not important or was not requested.",
		Manual:         true,
	},
	{
		ID:             "unnecessary-specification",
		Description:    "narrowing down assertions with qualifiers nobody asked for (ex. \"Does the API support pagination?\" -> \"Yes, the API supports pagination, specifically for list endpoints, in the current version, when called over HTTPS\", where the qualifiers restate scope instead of answering).",
		Recommendation: "answer at the precision the reader needs and drop qualifiers that do not change the meaning.",
		Manual:         true,
	},
	{
		ID:             "stating-the-obvious",
		Description:    "explaining what readers understand intuitively (ex. \"Click the copy button to copy the text\" next to a copy icon).",
		Recommendation: "remove remarks and instructions that do not tell the reader anything new.",
		Manual:         true,
	},
}

func (r *Rule) appliesTo(lang string) bool {
	if r.Manual {
		return false
	}
	if r.Langs == nil {
		return true
	}
	return slices.Contains(r.Langs, lang)
}

func detectCapitalsHeading(t *text) []Violation {
	var out []Violation
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		line := t.src[ls.start:ls.end]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || utf8.RuneCountInString(trimmed) > 80 {
			continue
		}
		// Only upper-case letters count, so we don't mistake lines in scripts
		// without case (Arabic, Chinese) for capitalized headings.
		letters, hasLower := 0, false
		for _, r := range trimmed {
			if unicode.IsUpper(r) {
				letters++
			}
			hasLower = hasLower || unicode.IsLower(r)
		}
		if hasLower || letters < 4 || countWords(trimmed) < 2 && letters < 6 {
			continue
		}
		start := ls.start + strings.Index(line, trimmed)
		end := start + len(trimmed)
		lower := strings.ToLower(trimmed)
		first, size := utf8.DecodeRuneInString(lower)
		fixed := string(unicode.ToUpper(first)) + lower[size:]
		out = append(out, Violation{Start: start, End: end, Fix: []Edit{{start, end, fixed}}})
	}
	return out
}

// iconReplacements maps icon characters to ASCII; "" means we have no
// automatic fix.
var iconReplacements = map[rune]string{
	'\u2192': "->", '\u2190': "<-", '\u2194': "<->", '\u21D2': "=>", '\u21D0': "<=", '\u21D4': "<=>",
	'\u27F6': "->", '\u27F5': "<-", '\u27F7': "<->", '\u27F9': "=>", '\u2794': "->", '\u279C': "->",
	'\u27A1': "->", '\u27A4': "->", '\u2B95': "->", '\u21A6': "->", '\u21B3': "->", '\u2191': "", '\u2193': "",
	'\u2713': "", '\u2714': "", '\u2717': "", '\u2718': "", '\u2610': "", '\u2611': "", '\u2612': "",
	'\u2605': "", '\u2606': "", '\u2726': "", '\u2727': "", '\u2731': "", '\u276F': ">", '\u276E': "<",
}

// lineBullets are icons that people use as list bullets at the start of a
// line.
var lineBullets = map[rune]bool{
	'\u2022': true, '\u25AA': true, '\u25AB': true, '\u25B8': true, '\u25B9': true, '\u25BA': true,
	'\u25B6': true, '\u25C6': true, '\u25C7': true, '\u25E6': true, '\u25CF': true, '\u25CB': true,
	'\u25A0': true, '\u25A1': true, '\u2023': true, '\u2043': true,
}

func isIcon(r rune) bool {
	if _, ok := iconReplacements[r]; ok {
		return true
	}
	return r >= 0x2190 && r <= 0x21FF || r >= 0x27F0 && r <= 0x27FF || r >= 0x2900 && r <= 0x297F || r >= 0x2500 && r <= 0x257F
}

func detectIcons(t *text) []Violation {
	var out []Violation
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		lineStart := true
		for i := ls.start; i < ls.end; {
			r, size := utf8.DecodeRuneInString(t.src[i:])
			switch {
			case lineStart && lineBullets[r]:
				out = append(out, Violation{Start: i, End: i + size, Fix: []Edit{{i, i + size, "-"}}})
			case isIcon(r):
				end := i + size
				if r >= 0x2500 && r <= 0x257F {
					end = boxDrawingEnd(t.src, end, ls.end)
				}
				v := Violation{Start: i, End: end}
				if rep := iconReplacements[r]; rep != "" {
					v.Fix = []Edit{{i, end, rep}}
				}
				out = append(out, v)
				size = end - i
			}
			if r != ' ' && r != '\t' {
				lineStart = false
			}
			i += size
		}
	}
	return out
}

func boxDrawingEnd(src string, end, limit int) int {
	for end < limit {
		r, size := utf8.DecodeRuneInString(src[end:])
		if r < 0x2500 || r > 0x257F {
			break
		}
		end += size
	}
	return end
}

func isEmojiBase(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		_, icon := iconReplacements[r]
		return !icon
	case r == 0x2B50 || r == 0x2B55 || r == 0x2B1B || r == 0x2B1C || r == 0x203C || r == 0x2049 || r == 0x2139:
		return true
	}
	return false
}

func isEmojiPart(r rune) bool {
	return isEmojiBase(r) || r == 0x200D || r == 0xFE0F || r >= 0x1F3FB && r <= 0x1F3FF || r == 0x20E3 || r >= 0xE0020 && r <= 0xE007F
}

func detectEmoji(t *text) []Violation {
	var out []Violation
	fixedTo := 0 // end of the previous fix, we can't reuse those bytes
	for i := 0; i < len(t.src); {
		r, size := utf8.DecodeRuneInString(t.src[i:])
		if !isEmojiBase(r) {
			i += size
			continue
		}
		end := i + size
		for end < len(t.src) {
			r2, s2 := utf8.DecodeRuneInString(t.src[end:])
			if !isEmojiPart(r2) {
				break
			}
			end += s2
		}
		// Remove a single separating space with the emoji. We prefer the one
		// before it so "a \U0001F600 \U0001F389" collapses to "a".
		fixStart, fixEnd := i, end
		if fixStart > fixedTo && t.src[fixStart-1] == ' ' {
			fixStart--
		} else if fixEnd < len(t.src) && t.src[fixEnd] == ' ' {
			fixEnd++
		}
		out = append(out, Violation{Start: i, End: end, Fix: []Edit{{fixStart, fixEnd, ""}}})
		fixedTo = fixEnd
		i = end
	}
	return out
}

func detectEmdash(t *text) []Violation {
	var out []Violation
	for i := 0; i < len(t.src); {
		r, size := utf8.DecodeRuneInString(t.src[i:])
		dash := r == '\u2014' || r == '\u2015'
		if r == '\u2013' {
			// An en dash between spaces is used as a dash. Between digits or
			// words it marks a range, so we leave it.
			dash = i > 0 && t.src[i-1] == ' ' && i+size < len(t.src) && t.src[i+size] == ' '
		}
		if !dash {
			i += size
			continue
		}
		fs, fe := i, i+size
		for fs > 0 && (t.src[fs-1] == ' ' || t.src[fs-1] == '\t') {
			fs--
		}
		for fe < len(t.src) && (t.src[fe] == ' ' || t.src[fe] == '\t') {
			fe++
		}
		rep := " - "
		if fs == 0 || t.src[fs-1] == '\n' {
			rep = "- "
		} else if fe == len(t.src) || t.src[fe] == '\n' || t.src[fe] == '\r' {
			rep = " -"
		}
		out = append(out, Violation{Start: i, End: i + size, Fix: []Edit{{fs, fe, rep}}})
		i += size
	}
	return out
}

var entityRe = regexp.MustCompile(`&#?\w+$`)

// joiningPunct finds punct used to join two clauses. We need enough words
// before it in the same sentence, a word right after it, and enough words
// after. Inline code counts as part of a clause: "`/srv`; the payloads
// assume it".
func joiningPunct(t *text, punct byte, minBefore, minAfter int, accept func(before, after string, off int) bool) []Violation {
	var out []Violation
	for i := 0; i < len(t.src); i++ {
		if t.src[i] != punct || i+2 >= len(t.src) || t.src[i+1] != ' ' {
			continue
		}
		j := i + 1
		for j < len(t.src) && t.src[j] == ' ' {
			j++
		}
		r, _ := utf8.DecodeRuneInString(t.src[j:])
		if !unicode.IsLetter(r) && r != '`' {
			continue
		}
		prev, _ := utf8.DecodeLastRuneInString(t.src[:i])
		if !(unicode.IsLetter(prev) || unicode.IsDigit(prev) || strings.ContainsRune(")\"'`\u201D\u2019", prev)) {
			continue
		}
		before, after := parenthetical(t.sentenceBefore(i), t.sentenceAfter(i+1))
		if countWords(before) < minBefore || countWords(after) < minAfter || !accept(before, after, i) {
			continue
		}
		up, size := t.capitalizeAt(j)
		out = append(out, Violation{Start: i, End: i + 1, Fix: []Edit{{i, j + size, ". " + up}}})
	}
	return out
}

// parenthetical narrows the clauses around a joining mark inside parentheses
// down to the text in the parentheses. "(without the Wiki: prefix)" has a
// single word after the colon, however long the sentence goes on after ")".
func parenthetical(before, after string) (string, string) {
	depth := 0
	open := -1
	for i := len(before) - 1; i >= 0 && open < 0; i-- {
		switch before[i] {
		case ')':
			depth++
		case '(':
			if depth == 0 {
				open = i
			}
			depth--
		}
	}
	if open < 0 {
		return before, after
	}
	depth = 0
	for i := range len(after) {
		switch after[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return before[open+1:], after[:i]
			}
			depth--
		}
	}
	return before[open+1:], after
}

// detectSemicolon flags a semicolon that joins two sentences if at least
// one of them has two words or more: "Yes; but it failed." Two one-word
// sentences like "yes; no" read as a pair, not as a joined sentence.
func detectSemicolon(t *text) []Violation {
	return joiningPunct(t, ';', 1, 1, func(before, after string, _ int) bool {
		return (countWords(before) >= 2 || countWords(after) >= 2) && !entityRe.MatchString(before)
	})
}

// listRe matches a comma-separated list of short items: "svg, png and pdf".
var listRe = regexp.MustCompile(`^\s*[^,]{1,45}(,\s*[^,]{1,45})+$`)

// schemeRe matches a URI scheme named in prose right before its colon, as
// in "the cid: links point at the attachments".
var schemeRe = regexp.MustCompile(`(?i)\b(?:cid|mid|mailto|data|blob|file|tel|sms|urn|about|javascript|https?|wss?|ftp)$`)

func detectColon(t *text) []Violation {
	return joiningPunct(t, ':', 3, 4, func(before, after string, off int) bool {
		if t.src[off-1] == '`' || strings.HasPrefix(strings.TrimLeft(after, " "), "`") {
			return false // the colon introduces code: "the command is: `make`"
		}
		if schemeRe.MatchString(before) {
			return false
		}
		l, _ := t.pos(off)
		fromLineStart := t.src[t.line(l).start:off]
		if strings.TrimSpace(fromLineStart) == strings.TrimSpace(before) && countWords(before) <= 3 {
			return false // a label such as "Sample output: ..."
		}
		return !listRe.MatchString(after)
	})
}

// labelTagRe matches the tags people write before a colon in comments
// ("TODO:", "NOTE(kevin):", "SAFETY:", "Usage:"), and labels that end with a
// number, such as issue references ("Issue 22233:") and numbered cases
// ("Request #2:").
var labelTagRe = regexp.MustCompile(`^(?i:todo|fixme|xxx|hack|bug|note|nb|safety|warning|deprecated|output|unordered output|usage|examples?)(?:\([^)]*\))?$|^[A-Z][A-Z_]+(?:\([^)]*\))?$|\d$`)

// detectLabelColon flags the comment colons that detectColon lets through:
// a label of a word or two, or a code sample, in front of an explanation of
// 3 words or more. A list of single words ("formats: svg, png and pdf") or a
// URL can follow a colon.
func detectLabelColon(t *text) []Violation {
	joined := map[int]bool{}
	for _, v := range detectColon(t) {
		joined[v.Start] = true
	}
	return joiningPunct(t, ':', 1, 3, func(before, after string, off int) bool {
		if joined[off] || t.src[off-1] == '`' || strings.HasPrefix(strings.TrimLeft(after, " "), "`") || schemeRe.MatchString(before) {
			return false
		}
		next := off + 1 + len(after) - len(strings.TrimLeft(after, " "))
		return !t.masked(span{next, next + 1}) && !labelTagRe.MatchString(strings.TrimSpace(before)) && !wordList(after)
	})
}

var listSepRe = regexp.MustCompile(`\s*(?:,|\band\b|\bor\b)\s*`)

// wordList reports whether s lists 2 or more single words, each optionally
// with an article: "svg, png and pdf", "the logs or the backups".
func wordList(s string) bool {
	items := listSepRe.Split(strings.TrimSuffix(strings.TrimSpace(s), "."), -1)
	if len(items) < 2 {
		return false
	}
	for _, it := range items {
		f := strings.Fields(it)
		if len(f) == 2 && articleRe.MatchString(f[0]) {
			f = f[1:]
		}
		if len(f) > 1 {
			return false
		}
	}
	return true
}

func detectBulletSeparator(t *text) []Violation {
	var out []Violation
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		line := t.src[ls.start:ls.end]
		for i, r := range line {
			if r != '\u00B7' && r != '\u2022' && r != '\u2219' && r != '\u22C5' {
				continue
			}
			size := utf8.RuneLen(r)
			if i == 0 || line[i-1] != ' ' || i+size >= len(line) || line[i+size] != ' ' || strings.TrimSpace(line[:i]) == "" {
				continue
			}
			s, e := ls.start+i, ls.start+i+size
			out = append(out, Violation{Start: s, End: e, Fix: []Edit{{s - 1, e + 1, ", "}}})
		}
	}
	return out
}

var paddedRe = regexp.MustCompile(`^[ \t>#*-]*(0[1-9])(?:[.):]?[ \t]|[ \t]*[\x{2013}\x{2014}-][ \t])`)

func detectPaddedNumbers(t *text) []Violation {
	var out []Violation
	for n := 1; n <= t.lineCount(); n++ {
		ls := t.line(n)
		m := paddedRe.FindStringSubmatchIndex(t.src[ls.start:ls.end])
		if m == nil {
			continue
		}
		s, e := ls.start+m[2], ls.start+m[3]
		out = append(out, Violation{Start: s, End: e, Fix: []Edit{{s, s + 1, ""}}})
	}
	return out
}

func detectCurlyQuotes(t *text) []Violation {
	var out []Violation
	for i, r := range t.src {
		var rep string
		switch r {
		case '\u201C', '\u201D', '\u201E', '\u201F':
			rep = `"`
		case '\u2018', '\u2019', '\u201A', '\u201B':
			rep = "'"
		default:
			continue
		}
		e := i + utf8.RuneLen(r)
		out = append(out, Violation{Start: i, End: e, Fix: []Edit{{i, e, rep}}})
	}
	return out
}

func detectEllipsis(t *text) []Violation {
	var out []Violation
	for i, r := range t.src {
		if r == '\u2026' {
			e := i + utf8.RuneLen(r)
			out = append(out, Violation{Start: i, End: e, Fix: []Edit{{i, e, "..."}}})
		}
	}
	return out
}

// sp matches the space between two words, which can wrap to the next line
// in hard-wrapped text and comments.
const sp = `(?:[ \t]*\n[ \t]*|[ \t]+)`

var (
	threeItemRe = regexp.MustCompile(`(?i)\b([a-z][a-z'-]*(?:` + sp + `[a-z][a-z'-]*)?),` + sp + `([a-z][a-z'-]*(?:` + sp + `[a-z][a-z'-]*)?),?` + sp + `(?:and|or)` + sp + `([a-z][a-z'-]*(?:` + sp + `[a-z][a-z'-]*)?)\b`)
	// listWordRe matches function words that don't start or fill a list
	// item. "unless" in "a lost message, unless FromGate or ToGate puts"
	// opens a clause, not a list item.
	listWordRe = regexp.MustCompile(`(?i)^(?:and|or|but|the|a|an|to|of|in|on|for|with|is|are|was|were|be|it|this|that|which|who|unless|if|when|while|because|since|although|though|until|whereas)$`)
	// articleRe matches an article in front of a list item: "the database,
	// the logs and the backups".
	articleRe = regexp.MustCompile(`(?i)^(?:the|a|an)$`)
	// introRe matches a preposition or conjunction right before the first
	// item. In "After the reset, the attacker and the admin" the first item
	// is an intro phrase and the rest is a pair.
	introRe = regexp.MustCompile(`(?i)\b(?:after|before|in|on|at|for|with|from|during|by|since|until|of|to|into|onto|over|under|through|when|once|if|as|while)\s+$`)
)

// detectRuleOfThree reports lists of 3 items in the languages that lists
// knows.
func detectRuleOfThree(t *text) []Violation {
	l, ok := lists[t.lang]
	if !ok {
		return nil
	}
	var out []Violation
	for _, m := range l.three.FindAllStringSubmatchIndex(t.src, -1) {
		start, end := m[2], m[7]
		// Skip longer lists, where there's another comma item right before
		// the first one.
		longer := func() bool {
			return strings.HasSuffix(strings.TrimRightFunc(t.src[max(0, start-40):start], unicode.IsSpace), ",")
		}
		// Items that all open with an article ("the database, the logs and
		// the backups") make a list, unless a preposition in front turns the
		// first one into an intro phrase.
		parallel := true
		for g := 1; g <= 3; g++ {
			w, _, two := cutSpace(t.src[m[2*g]:m[2*g+1]])
			parallel = parallel && two && l.article(w)
		}
		if parallel {
			if (l.intro == nil || !l.intro.MatchString(t.src[max(0, start-15):start])) && !longer() {
				out = append(out, Violation{Start: start, End: end})
			}
			continue
		}
		// If a two-word first item starts with a function word ("is fast"),
		// it's really a one-word item with the rest of the sentence in front.
		if first, rest, two := cutSpace(t.src[m[2]:m[3]]); two && l.stop(first) {
			start = m[3] - len(rest)
		}
		if longer() {
			continue
		}
		ok := true
		for g := 2; g <= 3; g++ {
			for w := range strings.FieldsSeq(t.src[m[2*g]:m[2*g+1]]) {
				if l.stop(w) {
					ok = false
				}
			}
		}
		if ok && !l.stop(t.src[start:m[3]]) {
			out = append(out, Violation{Start: start, End: end})
		}
	}
	return out
}

// cutSpace slices s around its first run of whitespace.
func cutSpace(s string) (before, after string, found bool) {
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, "", false
	}
	return s[:i], strings.TrimLeftFunc(s[i:], unicode.IsSpace), true
}

// phrase is a pattern we report with the matched text as the violation
// detail.
type phrase struct {
	re *regexp.Regexp
	// anchors are lower-case literals, and a match has to contain at least
	// one of them. We don't scan text that has none, which saves most of
	// the time on short texts like code comments. nil means we scan all text.
	anchors []string
	// except drops a match whose reported text it matches, nil if none.
	except *regexp.Regexp
}

// anchorsOf returns the longest literal that all matches of an alternative
// contain, for each alternative. If any alternative has none, it returns
// nil.
func anchorsOf(alternatives []string) []string {
	out := make([]string, len(alternatives))
	for i, a := range alternatives {
		lit := requiredLiteral(a)
		if lit == "" {
			return nil
		}
		out[i] = strings.ToLower(lit)
	}
	return out
}

// requiredLiteral returns the longest literal element of the top-level
// concatenation of pattern ("s important to " in `it'?s important to`), or
// "" if there's none.
func requiredLiteral(pattern string) string {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return ""
	}
	re = re.Simplify()
	parts := []*syntax.Regexp{re}
	if re.Op == syntax.OpConcat {
		parts = re.Sub
	}
	best := ""
	for _, p := range parts {
		if p.Op == syntax.OpLiteral && p.Flags&syntax.FoldCase == 0 && len(string(p.Rune)) > len(best) {
			best = string(p.Rune)
		}
	}
	return best
}

// scans reports whether the text might contain a match of p.
func (p phrase) scans(t *text) bool {
	if p.anchors == nil {
		return true
	}
	lower := t.lowered()
	for _, a := range p.anchors {
		if strings.Contains(lower, a) {
			return true
		}
	}
	return false
}

// phraseDetector reports the matches of ps. If matches overlap, the one that
// starts first wins, or the longest if they start at the same byte.
func phraseDetector(ps []phrase) func(t *text) []Violation {
	return func(t *text) []Violation {
		var all []Violation
		for _, p := range ps {
			if !p.scans(t) {
				continue
			}
			for _, m := range p.re.FindAllStringSubmatchIndex(t.src, -1) {
				s, e := m[0], m[1]
				if len(m) >= 4 && m[2] >= 0 {
					s, e = m[2], m[3]
				}
				if p.except != nil && p.except.MatchString(t.src[s:e]) {
					continue
				}
				all = append(all, Violation{Start: s, End: e, Detail: t.src[s:e]})
			}
		}
		slices.SortFunc(all, func(a, b Violation) int {
			return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(b.End, a.End))
		})
		end := -1
		return slices.DeleteFunc(all, func(v Violation) bool {
			if v.Start < end {
				return true
			}
			end = v.End
			return false
		})
	}
}

// words returns a phrase that matches any of the alternatives as whole
// words. We build a single leftmost-longest expression so the text gets
// scanned once, not once for each alternative.
func words(alternatives ...string) []phrase {
	re := regexp.MustCompile(`(?i)\b(` + strings.Join(alternatives, "|") + `)\b`)
	re.Longest()
	return []phrase{{re: re, anchors: anchorsOf(alternatives)}}
}

var negativeParallelism = []phrase{
	{re: regexp.MustCompile(`(?i)\b(not (?:just|only|merely|simply) [^.;:!?\n]{1,80}?,? but\b)`), anchors: []string{"not "}},
	{re: regexp.MustCompile(`(?i)\b((?:it|this|that)(?:'s| is) not (?:just |only |merely |about )?[^.;:!?\n]{1,60}?[,;] (?:it|this|that)(?:'s| is)\b)`), anchors: []string{" not "}},
	{re: regexp.MustCompile(`(?i)\b(not only\b[^.!?\n]{1,100}?\bbut also)\b`), anchors: []string{"not only"}},
	{re: regexp.MustCompile(`(?i)\b(less [a-z'-]+(?: [a-z'-]+)?, (?:and )?more [a-z'-]+)\b`), anchors: []string{"less "}},
	{re: regexp.MustCompile(`(?i)\b(less like (?:an? |the )?[a-z'-]+(?: [a-z'-]+)?,? and more like)\b`), anchors: []string{"less like"}},
	{re: regexp.MustCompile(`(?i)\b(no [a-z'-]+, no [a-z'-]+,? (?:just|only) [a-z'-]+)\b`), anchors: []string{"no "}},
	{re: regexp.MustCompile(`(?i)\b(not because [^.;:!?\n]{1,80}?,? but because)\b`), anchors: []string{"not because"}},
	{re: regexp.MustCompile(`(?i)\b((?:isn|aren|wasn)['\x{2019}]t (?:just|only|merely|simply) [^.;:!?\n]{1,60}?[,;] (?:it|this|that|they)(?:['\x{2019}]s| is|['\x{2019}]re| are)\b)`), anchors: []string{"n't", "n\u2019t"}},
	{re: regexp.MustCompile(`(?i)\b((?:is|are|was|were|it['\x{2019}]s|that['\x{2019}]s|lies|lie|belongs|belong)` + sp + notSide + `,` + sp + `not` + sp + notSide + `)(?:[.!?;:)]|,` + sp + `(?:so|which|and|but)\b|[ \t]*(?:\r?\n|$))`), anchors: []string{", not"}, except: notClauseRe},
}

// notSide matches one side of an "X is Y, not Z" contrast. It has up to 6
// words and can open with a preposition and a determiner ("the user's",
// "ours", "in the parser").
var notSide = `(?:(?:in|on|at|with|to|for|from|of|by)` + sp + `)?(?:(?:an?|the|my|your|our|their|his|her|its|this|that)` + sp + `)?[a-z'\x{2019}-]+(?:` + sp + `[a-z'\x{2019}-]+){0,5}?`

// notClauseRe matches a "not" that negates a clause or a degree instead of
// naming an alternative: "if it is set, not all fields are copied", "it is
// done, not yet merged".
var notClauseRe = regexp.MustCompile(`(?i),\s+not\s+(?:all|every|each|any|many|much|enough|yet|only|just|merely|simply|necessarily|always|quite|really|even|so|too|very|because)\b`)

var aiVocabulary = words(
	`delv(?:e|es|ed|ing)`, `tapestr(?:y|ies)`, `testament`, `pivotal`, `vibrant`, `seamless(?:ly)?`,
	`robust(?:ly|ness)?`, `leverag(?:e|es|ed|ing)`, `intricate(?:ly)?`, `intricac(?:y|ies)`,
	`multifaceted`, `realms?`, `showcas(?:e|es|ed|ing)`, `underscoring`,
	`foster(?:s|ed|ing)?`, `bolster(?:s|ed|ing)?`, `garner(?:s|ed|ing)?`, `meticulous(?:ly)?`,
	`embark(?:s|ed|ing)?`, `elevat(?:e|es|ed|ing)`, `unleash(?:es|ed|ing)?`, `ever-evolving`,
	`game-chang(?:er|ers|ing)`, `cutting-edge`, `paramount`, `holistic(?:ally)?`, `synerg(?:y|ies|istic)`,
	`commendable`, `groundbreaking`, `transformative`, `revolutioni[sz](?:e|es|ed|ing)`, `unparalleled`,
	`unwavering`, `plethora`, `myriad`, `bustling`, `breathtaking`, `captivating`, `enigmatic`,
	`interplay`, `empower(?:s|ed|ing|ment)?`, `streamlin(?:e|es|ed|ing)`, `invaluable`,
	`dive (?:deep )?into`, `deep dive`, `navigat(?:e|es|ing) the complexities`,
)

var fillerPhrases = words(
	`it is important to (?:note|remember|understand) that`, `it'?s important to (?:note|remember|understand) that`,
	`it is worth (?:noting|mentioning) that`, `it'?s worth (?:noting|mentioning) that`, `it should be noted that`,
	`needless to say`, `in conclusion`, `in summary`, `to summari[sz]e`, `at the end of the day`, `all in all`,
	`when it comes to`, `in today'?s (?:(?:fast-paced|digital|modern|ever-changing) )?(?:world|age|landscape)`,
	`in the world of`, `let['\x{2019}]?s dive in`,
	`as previously mentioned`, `it goes without saying`, `first and foremost`, `last but not least`,
	`as you can see`,
)

// chatbotPhrases are the English assistant phrases, sentence-initial
// "Here is a ...:" introductions and bracketed template placeholders. A
// placeholder followed by "(" or "[" is link text, so we leave it.
var chatbotPhrases = append(words(
	`certainly!`, `of course!`, `absolutely!`, `great question`, `excellent question`, `good question`,
	`i hope this helps`, `hope this helps`, `let me know if`, `feel free to`, `as an ai`, `as a language model`,
	`i'?d be (?:happy|glad) to`, `i'?m (?:happy|glad) to help`, `happy to help`, `here'?s a breakdown`,
	`here is a breakdown`, `would you like me to`, `i apologi[sz]e for the confusion`,
	`you(?:['\x{2019}]?re| are) (?:absolutely |completely |totally |quite )?(?:right|correct)`, `i['\x{2019}]?d recommend`,
	`good catch`, `fair point`, `great point`, `on (?:both|all) counts`,
	`is there anything else`, `more detailed breakdown`,
	`i hope (?:this|my) (?:message|e-?mail|note) finds you well`, `for any inconvenience (?:this|that|it) may have caused`,
	`dear valued customers?`,
),
	phrase{re: regexp.MustCompile(`(?im)(?:^[ \t]*|[.!?][ \t]+)(here(?:['\x{2019}]s| is) (?:a|an|the|your) [^.:\n]{0,60}:)`), anchors: []string{"here"}},
	phrase{re: regexp.MustCompile(`(\[(?:Your|Insert|Describe|Add) [^\]\n]{1,40}\])(?:[^(\[:]|$)`), anchors: []string{"["}},
)

var knowledgeCutoff = words(
	`as of my (?:last |latest )?(?:knowledge )?(?:update|training)`, `my knowledge cut-?off`,
	`as of my knowledge cut-?off`, `i don'?t have access to real-time`, `based on (?:the )?available information`,
	`while specific details are (?:limited|scarce|not available)`, `up to my last training`,
)

// transitionOpeners lists the openers that AI text from 2026 uses more
// often than human text on the same topics. People open with
// "Additionally", "Moreover", "Furthermore", "However", "Therefore" and
// "Thus" more often than agents do, so those aren't here.
var transitionOpeners = []phrase{
	{re: regexp.MustCompile(`(?:^|[.!?]\s+|\n)((?:Importantly|Ultimately|Overall|Consequently|In short|Essentially|Simply put|That said),)`)},
}

var superficialAnalysis = []phrase{
	{re: regexp.MustCompile(`(?i)(, (?:highlighting|underscoring|emphasi[sz]ing|showcasing|reflecting|symboli[sz]ing|illustrating|signaling|signalling|signifying|cementing|solidifying|contributing to|fostering)\b[^.!?\n]*)`), anchors: []string{", "}},
}
