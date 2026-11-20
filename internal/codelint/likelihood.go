package codelint

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/likelihood"
)

// We fit the rates below on 20 human codebases (projects with anti-AI
// contribution policies and code from before 2023, in Go, Java, Python,
// TypeScript, JavaScript, C# and C) and 12 codebases written by AI agents.
// We checked them on 8 human and 3 AI codebases held out from the fit,
// though we did raise the labels bound after seeing one of the AI ones.
// Most of the signal is in the comments. Agents write them in a dense,
// declarative voice and like to join clauses with semicolons and colons,
// while people write the way they talk ("we need this if the cache is
// cold", "TODO: why does this break?"). Structure metrics like function
// length, nesting and duplication didn't separate the two groups, so we
// don't use them.

// aiPrior is the log-odds of AI authorship before we've seen any evidence
// (10 %), so input with no comments or tells reads as human.
const aiPrior = -2.2

var (
	// commentVoice is the share of human-style words among the function
	// words of English comments (see likelihood.CountVoice).
	commentVoice = likelihood.Share{
		Signal: "comment voice", Human: 0.81, AI: 0.33, Spread: 0.56,
		MaxAI: 3, MaxHuman: 3,
	}
	// commentProse counts comment-prose violations per 1000 comment words.
	commentProse = likelihood.Rate{
		Signal: "comment prose patterns", Human: 1, AI: 8, Spread: 0.92,
		MaxAI: 3, MaxHuman: 2,
	}
	// The human markers below are counted per 1000 comment words. People
	// leave them and agents don't, but not finding any is weak evidence. An
	// agent told to write terse comments won't leave any either.
	todoMarkers = likelihood.Rate{
		Signal: "TODO markers", Human: 1.5, AI: 0.025, Spread: 1.4,
		MaxAI: 0.75, MaxHuman: 2.5,
	}
	questions = likelihood.Rate{
		Signal: "questions in comments", Human: 1, AI: 0.025, Spread: 1,
		MaxAI: 0.75, MaxHuman: 2.5,
	}
	informal = likelihood.Rate{
		Signal: "first person, contractions and interjections", Human: 4.2, AI: 0.3, Spread: 1.1,
		MaxAI: 0.75, MaxHuman: 2.5,
	}
	// The tells are counted per 1000 lines of code. Their AI rates come from
	// agents that use them. Recent models avoid them, so not finding any
	// doesn't tell us anything.
	emdashes = likelihood.Rate{
		Signal: "em dashes", Human: 0.03, AI: 10, Spread: 1.5,
		MaxAI: 4,
	}
	icons = likelihood.Rate{
		Signal: "utf-8 icons", Human: 0.08, AI: 3, Spread: 1.5,
		MaxAI: 1.5,
	}
	steps = likelihood.Rate{
		Signal: "step comments", Human: 0.02, AI: 0.8, Spread: 1.2,
		MaxAI: 1.5,
	}
	// labels are short comments in function bodies that name the next block
	// with a capitalized verb, like "// Validate input".
	labels = likelihood.Rate{
		Signal: "block label comments", Human: 0.5, AI: 12, Spread: 1.3,
		MaxAI: 2.5,
	}
)

var (
	// englishWords tell us a comment is in English. We need that check
	// because the function word counts would misread other languages ("to"
	// and "do" are Slovak words).
	englishWords = setOf("the", "is", "are", "of", "and", "this", "that", "with", "for", "from", "when",
		"which", "will", "be", "not", "it", "an", "by", "if", "to")
	markerRe   = regexp.MustCompile(`\b(?:TODO|FIXME|XXX|HACK)\b`)
	questionRe = regexp.MustCompile(`\p{L}[)"'\x60]?\?(?:\s|$)`)
	informalRe = regexp.MustCompile(`\b(?:I|I'm|I've|I'd|I'll|don't|can't|won't|isn't|doesn't|didn't|aren't|wasn't|shouldn't|wouldn't|couldn't|haven't|it's|that's|there's|we're|you're|they're|let's|lol|LOL|wtf|WTF|damn|hmm+|oops|ugh|meh|yay|dunno|gonna|wanna|kinda|sorta)\b|[!?]{2,}|(?:^|\s)(?::\)|:\(|:D|;\)|xD)`)
	labelVerbs = setOf("validate", "determine", "print", "check", "create", "initialize", "initialise", "init",
		"set", "get", "build", "parse", "load", "update", "handle", "process", "ensure", "add", "remove", "find",
		"calculate", "compute", "return", "convert", "extract", "apply", "run", "start", "stop", "wait", "send",
		"read", "write", "open", "close", "clean", "cleanup", "prepare", "format", "render", "store", "save",
		"fetch", "generate", "collect", "filter", "sort", "merge", "split", "resolve", "register", "try", "skip",
		"use", "compare", "verify", "track", "emit", "log", "mark", "reset", "clear", "copy", "move", "map", "wrap",
		"call", "invoke", "notify", "show", "hide", "display", "detect", "normalize", "append", "insert", "delete",
		"drain", "flush", "configure", "setup", "define", "declare", "assign", "iterate", "loop", "increment",
		"decrement", "allocate", "release", "lock", "unlock", "spawn", "launch", "connect", "disconnect", "listen",
		"accept", "trim", "strip", "join", "attach", "detach", "enable", "disable", "toggle", "select", "pick",
		"choose", "match", "group", "count", "sum", "measure", "cache", "persist", "restore", "sync", "upload",
		"download", "install", "schedule", "queue", "dispatch", "publish", "subscribe", "broadcast", "fire",
		"trigger", "capture", "record", "report", "warn", "fail", "abort", "exit", "retry", "refresh", "reload",
		"rebuild", "recompute", "recalculate", "inject", "replace", "rename", "scan", "walk", "visit", "traverse",
		"collapse", "expand", "decode", "encode", "serialize", "deserialize", "marshal", "unmarshal", "hash",
		"sign", "encrypt", "decrypt", "compress", "decompress", "pad", "align", "clamp", "round", "truncate",
		"limit", "throttle", "debounce", "guard", "bail")
)

type corpusStats struct {
	codeLines                 int
	commentWords              int
	humanFunc, aiFunc         int
	markers, quest, informals int
	labels                    int
}

func collectStats(srcs []*source) corpusStats {
	var st corpusStats
	for _, s := range srcs {
		for l := 1; l <= s.f.LineCount(); l++ {
			if s.codeLines[l] {
				st.codeLines++
			}
		}
		for _, c := range s.comments {
			if c.directive || c.license || len(c.lines) == 0 || commentedOutCode(c) {
				continue
			}
			text := strings.ReplaceAll(c.text(), "\u2019", "'")
			ws := likelihood.Words(text)
			st.commentWords += len(ws)
			st.markers += len(markerRe.FindAllString(text, -1))
			st.quest += len(questionRe.FindAllString(text, -1))
			st.informals += countInformal(text)
			if s.labelComment(c, ws) {
				st.labels++
			}
			if english(text, ws) {
				h, a := likelihood.CountVoice(ws)
				st.humanFunc += h
				st.aiFunc += a
			}
		}
	}
	return st
}

// countInformal counts informalRe matches, but skips the "I" in "I/O".
func countInformal(text string) int {
	n := 0
	for _, m := range informalRe.FindAllStringIndex(text, -1) {
		if text[m[0]:m[1]] == "I" && m[1] < len(text) && text[m[1]] == '/' {
			continue
		}
		n++
	}
	return n
}

// labelComment reports whether c is a standalone one-line comment of up to
// six words inside a function body, not above a declaration, that starts
// with a capitalized verb and doesn't end with a period.
func (s *source) labelComment(c *comment, ws []string) bool {
	if !prose(c) || !c.standalone || c.line != c.end || len(ws) == 0 || len(ws) > 6 || !labelVerbs[ws[0]] || strings.HasSuffix(c.text(), ".") {
		return false
	}
	if r, _ := utf8.DecodeRuneInString(c.text()); !unicode.IsUpper(r) {
		return false
	}
	return s.scopeOf(c.Span.Start) != s.f.Module && !s.declStarts()[s.nextCodeLine(c.end)]
}

// english reports whether a comment is written in English. It has to use an
// English function word, and at most 2 % of its letters can be non-ASCII.
func english(text string, ws []string) bool {
	letters, foreign := 0, 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
			if r > unicode.MaxASCII {
				foreign++
			}
		}
	}
	if foreign*50 > letters {
		return false
	}
	for _, w := range ws {
		if englishWords[w] {
			return true
		}
	}
	return false
}

var codeLineRe = regexp.MustCompile(`[;{}]\s*$|^[\w.\[\]]+\s*(?:[-+*/]?=|\()|^\$\w+\s*(?:->|::|\[|[-+*/.]?=|\()|^(?:if|for|foreach|while|return|var|let|const|def|func|fn|impl|import|from|public|private|static|int|float|bool|void|else|elseif)\b.*[({=:;]`)

// commentedOutCode reports whether most lines of c look like code. We
// shouldn't count those words as prose.
func commentedOutCode(c *comment) bool {
	if c.docstring {
		return false
	}
	n := 0
	for _, l := range c.lines {
		if codeLineRe.MatchString(l) {
			n++
		}
	}
	return n*2 > len(c.lines)
}

func aiEvidence(srcs []*source, vs []violation) []likelihood.Evidence {
	st := collectStats(srcs)
	count := map[string]int{}
	for _, v := range vs {
		if !v.unscored {
			count[v.rule.ID]++
		}
	}
	kcw := float64(st.commentWords) / 1000
	kloc := float64(st.codeLines) / 1000
	ev := []likelihood.Evidence{
		commentProse.Evidence(count["comment-prose"], kcw, fmt.Sprintf("%d comment-prose violations in %d comment words", count["comment-prose"], st.commentWords)),
		todoMarkers.Evidence(st.markers, kcw, fmt.Sprintf("%d TODO/FIXME/XXX/HACK markers in %d comment words", st.markers, st.commentWords)),
		questions.Evidence(st.quest, kcw, fmt.Sprintf("%d questions in %d comment words", st.quest, st.commentWords)),
		informal.Evidence(st.informals, kcw, fmt.Sprintf("%d first-person words, contractions and interjections in %d comment words", st.informals, st.commentWords)),
		emdashes.Evidence(count["emdash-in-code"], kloc, fmt.Sprintf("%d em dashes in %d lines of code", count["emdash-in-code"], st.codeLines)),
		icons.Evidence(count["utf8-icons-in-code"], kloc, fmt.Sprintf("%d utf-8 icons in %d lines of code", count["utf8-icons-in-code"], st.codeLines)),
		steps.Evidence(count["step-comment"], kloc, fmt.Sprintf("%d step comments in %d lines of code", count["step-comment"], st.codeLines)),
		labels.Evidence(st.labels, kloc, fmt.Sprintf("%d block label comments (\"// Validate input\") in %d lines of code", st.labels, st.codeLines)),
	}
	if funcs := st.humanFunc + st.aiFunc; funcs > 0 {
		share := float64(st.humanFunc) / float64(funcs)
		ev = append(ev, commentVoice.Evidence(st.humanFunc, funcs, fmt.Sprintf("%.0f %% of %d function words in English comments are human-style (will, to, if, we, ...), human code has ~80 %%, AI code ~35 %%", share*100, funcs)))
	}
	return ev
}
