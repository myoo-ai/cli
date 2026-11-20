package prose

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The rules in this file come from house styles for written deliverables
// such as reports, advisories and customer mail. They flag wording that
// rates, hedges or dramatizes a fact instead of stating it. People write
// "just", "actually" and "lets you" in code comments all the time, so these
// rules are ProseOnly and code_lint leaves them out.

// exceptFollowedBy drops findings of detect when re matches the text right
// after them.
func exceptFollowedBy(detect func(t *text) []Violation, re *regexp.Regexp) func(t *text) []Violation {
	return func(t *text) []Violation {
		return slices.DeleteFunc(detect(t), func(v Violation) bool {
			return re.MatchString(t.src[v.End:min(len(t.src), v.End+30)])
		})
	}
}

// emptyIntensifiers leaves out "really", which people use in speech-like
// writing at least as often as AI does.
var emptyIntensifiers = words(`very`, `extremely`, `highly`, `incredibly`, `truly`, `significantly`,
	`remarkably`, `exceptionally`, `tremendously`, `immensely`)

var (
	theBeforeRe        = regexp.MustCompile(`(?i)\bthe\s+$`)
	highlyIdiomAfterRe = regexp.MustCompile(`(?i)^\s+(?:available|availability|concurrent|parallel|configurable)\b`)
)

// detectIntensifiers reports emptyIntensifiers, except the idioms "the very
// first" and "highly available", where the word names a property.
func detectIntensifiers(t *text) []Violation {
	return slices.DeleteFunc(phraseDetector(emptyIntensifiers)(t), func(v Violation) bool {
		switch strings.ToLower(v.Detail) {
		case "very":
			return theBeforeRe.MatchString(t.src[max(0, v.Start-10):v.Start])
		case "highly":
			return highlyIdiomAfterRe.MatchString(t.src[v.End:min(len(t.src), v.End+20)])
		}
		return false
	})
}

var credibilityAdverbs = words(`genuinely`, `actually`, `in fact`, `indeed`)

// worthConstruction ends in a gerund, so "worth nothing" and the other
// "-thing" words match it too. thingRe takes them out again.
var worthConstruction = []phrase{
	{re: regexp.MustCompile(`(?i)\b(worth (?:a )?[a-z]+ing)\b`), anchors: []string{"worth "}},
	words(`(?:a|an) (?:notable|noteworthy|interesting) (?:exception|detail|point|aspect|twist|wrinkle)`)[0],
}

var thingRe = regexp.MustCompile(`(?i)thing$`)

var falseEase = words(`simply`, `easily`, `effortlessly`,
	`just (?:run|add|use|call|set|install|type|click|copy|paste|replace|change|open|delete|remove|edit|restart|pass|put|enable|disable|rotate|update|upgrade|drop|import|point|select|follow|do)`)

var stackedHedges = words(
	`(?:could|may|might|would) (?:potentially|possibly|perhaps|conceivably)`,
	`(?:potentially|possibly|perhaps) (?:could|may|might)`,
	`(?:appears?|seems?) to (?:potentially|possibly|perhaps|likely|probably)`,
)

// participialClosers are the trailing clauses that tack a consequence onto
// a sentence. superficial-analysis already reports the ones that add vague
// significance.
var participialClosers = []phrase{
	{re: regexp.MustCompile(`(?i)(, (?:making|allowing|enabling|ensuring|helping(?: to)?|leaving|giving|letting|resulting in|paving the way for|thereby [a-z]+ing)\b[^.!?\n]*)`), anchors: []string{", "}},
}

var ratherThanRe = regexp.MustCompile(`(?i)\b([a-z]+) (rather than ([a-z]+ing))\b`)

// notGerunds end in "ing" without being a gerund.
var notGerunds = map[string]bool{
	"during": true, "nothing": true, "something": true, "anything": true, "everything": true,
	"morning": true, "evening": true, "ceiling": true, "string": true, "spring": true,
}

// detectRatherThan flags "rather than" with a gerund after a word of another
// form: "verified rather than assuming". The mismatch announces a method
// where a result belongs. Matching forms ("reading rather than writing")
// are a plain comparison.
func detectRatherThan(t *text) []Violation {
	if !strings.Contains(t.lowered(), "rather than ") {
		return nil
	}
	var out []Violation
	for _, m := range ratherThanRe.FindAllStringSubmatchIndex(t.src, -1) {
		before, gerund := strings.ToLower(t.src[m[2]:m[3]]), strings.ToLower(t.src[m[6]:m[7]])
		if strings.HasSuffix(before, "ing") || len(gerund) < 6 || notGerunds[gerund] {
			continue
		}
		out = append(out, Violation{Start: m[4], End: m[5], Detail: t.src[m[4]:m[5]]})
	}
	return out
}

var hollowAdjectives = words(`powerful`, `comprehensive(?:ly)?`, `world-class`, `best-in-class`,
	`state-of-the-art`, `industry-leading`, `next-generation`, `enterprise-grade`)

// personifiedObjects leaves out the phrasal verbs "carry out", "carry on"
// and "carry over", and the carry flag and bit of arithmetic.
var personifiedObjects = words(`carr(?:y|ies|ied|ying)`, `surviv(?:e|es|ed|ing)`)

var phrasalAfterRe = regexp.MustCompile(`(?i)^\s+(?:out|on|over|forward|flag|bit)\b`)

var journeyImagery = words(
	`(?:first|last|final) line of defen[cs]e`, `(?:at|through|knocking on) the front door`,
	`stops? (?:[a-z]+ )?in (?:its|their) tracks`, `before (?:they|it|attackers) (?:ever )?gets? (?:close|near)`,
	`on (?:its|their) way (?:to|into)`,
)

var shopTalk = words(`blast radius`, `dwell time`, `in the wild`, `landed (?:on|in)`, `attack surface`)

var thingAsSubject = words(
	`(?:lands?|landed|landing) (?:with|on (?:your|their|the) desk|in (?:your|their) inbox)`,
	`finds? (?:its|their) way (?:to|into)`,
	`reach(?:es)? the right (?:people|person|team|inbox)`,
	`(?:needs?|requires?|deserves?|warrants?|merits?) (?:[^\s.]+ ){0,2}attention`,
	`(?:warrants?|deserves?|merits?) (?:a )?(?:closer|second|further|careful) look`,
	`(?:deserves?|warrants?) priority`,
	`lets (?:you|your [a-z]+|users?|the (?:user|team|reader|admin)s?)`,
	`(?:allows|enables) (?:you|your [a-z]+|users?|the (?:user|team|reader|admin)s?) to`,
	`gives (?:you|your [a-z]+) (?:a|an|the) (?:starting point|overview|head start|way)`,
)

// chattyVerdicts matches "is the one" only at the end of a clause, since
// "she is the one who wrote it" is a plain statement.
var chattyVerdicts = append(words(
	`(?:is|are) the way to go`, `(?:your|the) best bet`, `that['\x{2019}]?s where [a-z0-9 ]{1,30} comes? in`,
	`(?:has|have|got) you covered`, `(?:is|are) a no-brainer`,
),
	phrase{re: regexp.MustCompile(`(?i)\b((?:is|are) the one)(?:[.!,;]|$)`), anchors: []string{"the one"}},
)

var sloganCompression = words(
	`(?:one|a single) (?:command|click|line|step|flag|call|switch|setting|toggle) (?:turns|restores|fixes|disables|enables|undoes|removes|deploys|sets|handles|runs|brings|puts|gets|does|rolls|reverts|switches|installs)`,
	`(?:two|three|four|a few|a couple of) (?:commands|clicks|lines|steps|flags|calls|settings) (?:turn|restore|fix|disable|enable|undo|remove|deploy|set|handle|run|bring|put|get|do|roll|revert|switch|install)`,
)

var clauseForAdjective = words(
	`(?:nobody|no one) (?:has|had|have) (?:yet |so far |ever |since )?(?:[a-z]+ed|seen|done|read|taken|written|run|looked at)`,
)

var regardInstruction = words(
	`(?:treat(?:s|ed|ing)?|consider(?:s|ed|ing)?|regard(?:s|ed|ing)?|assum(?:e|es|ed|ing)) (?:[a-z0-9.'-]+ ){0,4}(?:as |to be |is |are )?(?:compromised|affected|suspect|suspicious|untrusted|tainted|exposed|breached|infected|leaked)`,
)

var corporateJargon = words(
	`utili[sz](?:e|es|ed|ing|ation)`, `facilitat(?:e|es|ed|ing)`, `initiat(?:e|es|ed|ing)`,
	`obtain(?:s|ed|ing)?`, `regarding`, `with regards? to`, `purchas(?:e|es|ed|ing)`, `demonstrat(?:e|es|ed|ing)`,
	`(?:argument|case|claim|conclusion|this|that|it) rests on`,
)

var headingCommentRe = regexp.MustCompile(`(?i),\s*(?:stated (?:up front|honestly|plainly|briefly)|in brief|honestly|explained|and why (?:it|this|that) matters|briefly|up front)\s*$`)

// detectHeadingCommentary flags headings that end in a remark about the
// document ("The timestamp problem, stated up front").
func detectHeadingCommentary(t *text) []Violation {
	var out []Violation
	for _, h := range t.headings() {
		if m := headingCommentRe.FindStringIndex(t.src[h.start:h.end]); m != nil {
			s := h.start + m[0]
			out = append(out, Violation{Start: s, End: h.end, Detail: t.src[s:h.end]})
		}
	}
	return out
}

// numberValues maps the English cardinals that words-instead-of-numbers
// flags to their value. "one" only counts inside a compound ("twenty-one"):
// on its own it is as often a pronoun or an idiom ("no one", "one of them",
// "in one call") as a count, and "1" reads badly there. "zero" is left out
// ("zero-day"), and so are ordinals, fractions and the scale words, which
// follow digits as often as words ("1.2 million").
var numberValues = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
	"ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20, "thirty": 30, "forty": 40, "fifty": 50,
	"sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
}

var (
	numberWordRe = regexp.MustCompile(`(?i)\b(?:(?:twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety)(?:-(?:one|two|three|four|five|six|seven|eight|nine))?|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen)\b`)
	// compoundAfterRe matches a hyphenated compound ("two-factor",
	// "twenty-first"), where the word is no count.
	compoundAfterRe = regexp.MustCompile(`^-\pL`)
	scaleAfterRe    = regexp.MustCompile(`(?i)^\s+(?:hundred|thousand|million|billion)\b`)
)

// detectNumberWords flags cardinals written as words ("three stores"). It
// fixes them to digits, except before a scale word ("two hundred"), where
// the digits depend on the words that follow.
func detectNumberWords(t *text) []Violation {
	var out []Violation
	for _, m := range numberWordRe.FindAllStringIndex(t.src, -1) {
		s, e := m[0], m[1]
		after := t.src[e:min(len(t.src), e+20)]
		if strings.HasSuffix(t.src[:s], "-") || compoundAfterRe.MatchString(after) {
			continue
		}
		v := Violation{Start: s, End: e, Detail: t.src[s:e]}
		if !scaleAfterRe.MatchString(after) {
			tens, unit, _ := strings.Cut(strings.ToLower(t.src[s:e]), "-")
			v.Fix = []Edit{{s, e, strconv.Itoa(numberValues[tens] + numberValues[unit])}}
		}
		out = append(out, v)
	}
	return out
}
