package codelint

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/likelihood"
)

// We fit the rates below on 20 human codebases (projects with anti-AI
// contribution policies and code from before 2023, in Go, Java, Python,
// TypeScript, JavaScript, C# and C) and 12 codebases written by AI agents.
// The rates of the 3 newer signals below come from the same human codebases
// and 10 of the AI ones. We picked their markers, the
// thresholds of spreadFile and all the bounds while looking at the scores of
// those and of a reference set of 29 more human codebases from the Slopfree
// Software Index and 6 more AI codebases, so the reference set doesn't show
// how well the estimate generalizes.
// Most of the signal is in the comments. Agents write them in a dense,
// declarative voice and like to join clauses with semicolons and colons,
// while people write the way they talk ("we need this if the cache is
// cold", "TODO: why does this break?"). An agent told to write like a person
// fixes the comment-prose violations and writes contractions and "we", but
// it keeps hanging possessives on the things the code handles ("the inbox's
// own address") and replaces semicolons with ", so", in a third of its
// comments, all through the file. Structure metrics like function length,
// nesting, duplication, blank lines and name length didn't separate the two
// groups, so we don't use them.

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
	// Agents that run code_lint fix them, so finding none is weak evidence.
	commentProse = likelihood.Rate{
		Signal: "comment prose patterns", Human: 1, AI: 8, Spread: 0.92,
		MaxAI: 3, MaxHuman: 0.5,
	}
	// humanMarkers counts TODO markers, questions, first person singular and
	// interjections per 1000 comment words of the files commented in English,
	// the language we fit it on. People leave them and agents don't, but not finding any is weak
	// evidence. An agent told to write terse comments won't leave any either.
	// Contractions don't count, agents asked to sound human write them as
	// often as people do.
	humanMarkers = likelihood.Rate{
		Signal: "human markers", Human: 4.2, AI: 0.15, Spread: 1,
		MaxAI: 1, MaxHuman: 3,
	}
	// possessives counts possessive 's and "own" per 1000 comment words of
	// the files commented in English.
	// Agents hang possessives on the things the code handles ("the inbox's
	// own address", "the message's author"), people rarely do.
	possessives = likelihood.Rate{
		Signal: "possessives", Human: 1.8, AI: 9.1, Spread: 0.82,
		MaxAI: 2, MaxHuman: 1.5,
	}
	// spreadFiles is the share of files whose AI-style comments run through
	// the whole file (see spreadFile), among the files commented in English
	// with enough comments to tell.
	spreadFiles = likelihood.Share{
		Signal: "files with AI-style comments throughout", Human: 0.011, AI: 0.13, Spread: 1.39,
		MaxAI: 2.5, MaxHuman: 0.5,
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
	// casualRe matches first person singular, interjections and emoticons.
	// It's English, "my" is "we" in Slovak and Czech.
	casualRe = regexp.MustCompile(`\b(?:I|I'm|I've|I'd|I'll|me|my|lol|LOL|wtf|WTF|damn|hmm+|oops|ugh|meh|yay|dunno|gonna|wanna|kinda|sorta|hacky|ugly|stupid|weird|nasty|crap)\b|[!?]{2,}|(?:^|\s)(?::\)|:\(|:D|;\)|xD)`)
	// possessiveRe matches a word with 's. notPossessive lists the words
	// where the 's is a contraction.
	possessiveRe  = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9_]*'s\b|(?i:\bown\b)`)
	notPossessive = setOf("it's", "that's", "there's", "let's", "what's", "here's", "who's", "he's", "she's", "where's", "how's", "one's")
	// contrastRe matches the clauses agents join comments with when they
	// avoid semicolons and dashes: ", so", "so that", ", not", "rather than"
	// and "instead of".
	contrastRe = regexp.MustCompile(`, so\b|\bso that\b|, not\b|(?i:\brather than\b|\binstead of\b)`)
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
	codeLines           int
	commentWords        int
	englishWords        int // comment words of the files whose comments read as English
	humanFunc, aiFunc   int
	markers, quest      int
	casual, possessives int
	labels              int
	judged              int // files spreadFile could judge
	spread              []spreadStat
}

// spreadStat is a file that spreadFile flagged, with the share of its
// comments that have a marker.
type spreadStat struct {
	name  string
	share float64
}

func collectStats(srcs []*source) corpusStats {
	var st corpusStats
	for _, s := range srcs {
		for l := 1; l <= s.f.LineCount(); l++ {
			if s.codeLines[l] {
				st.codeLines++
			}
		}
		var words, markers, quest, casual, possessives int
		for _, c := range s.comments {
			if !c.textual() {
				continue
			}
			text := strings.ReplaceAll(c.text(), "\u2019", "'")
			st.commentWords += len(c.words)
			if s.labelComment(c, c.words) {
				st.labels++
			}
			if english(text, c.words) {
				h, a := likelihood.CountVoice(c.words)
				st.humanFunc += h
				st.aiFunc += a
			}
			words += len(c.words)
			markers += len(markerRe.FindAllString(text, -1))
			quest += len(questionRe.FindAllString(text, -1))
			casual += countCasual(text)
			possessives += countPossessives(text)
		}
		// The human markers, possessives and spread are English.
		if s.lang != "en" {
			continue
		}
		st.englishWords += words
		st.markers += markers
		st.quest += quest
		st.casual += casual
		st.possessives += possessives
		if marked, blocks, judged, flagged := s.spreadFile(); judged {
			st.judged++
			if flagged {
				st.spread = append(st.spread, spreadStat{s.name, float64(marked) / float64(blocks)})
			}
		}
	}
	return st
}

// countCasual counts casualRe matches, but skips the "I" in "I/O".
func countCasual(text string) int {
	n := 0
	for _, m := range casualRe.FindAllStringIndex(text, -1) {
		if text[m[0]:m[1]] == "I" && m[1] < len(text) && text[m[1]] == '/' {
			continue
		}
		n++
	}
	return n
}

func countPossessives(text string) int {
	n := 0
	for _, m := range possessiveRe.FindAllString(text, -1) {
		if !notPossessive[strings.ToLower(m)] {
			n++
		}
	}
	return n
}

// spreadMinComments is the number of comments of 3 or more words a file
// needs before spreadFile judges it.
const spreadMinComments = 5

// spreadFile reports whether the comments of s read as AI written throughout
// the file: a third of them have a possessive, "own" or a contrastRe clause,
// and those comments span a third of its lines. A few such comments in one
// place, like a long doc comment, don't make the file AI written. judged is
// false if the file has fewer than spreadMinComments comments. A block of
// "//" lines counts as one comment. The markers are English, so we only
// call it for files whose comments read as English.
func (s *source) spreadFile() (marked, blocks int, judged, flagged bool) {
	first, last := 0, 0
	for _, b := range s.proseBlocks() {
		parts := make([]string, len(b))
		for i, c := range b {
			parts[i] = c.text()
		}
		text := strings.ReplaceAll(strings.Join(parts, " "), "\u2019", "'")
		if len(likelihood.Words(text)) < 3 {
			continue
		}
		blocks++
		if countPossessives(text) == 0 && !contrastRe.MatchString(text) {
			continue
		}
		if marked == 0 {
			first = b[0].line
		}
		marked++
		last = b[len(b)-1].end
	}
	if blocks < spreadMinComments {
		return marked, blocks, false, false
	}
	return marked, blocks, true, marked*3 >= blocks && (last-first+1)*3 >= s.f.LineCount()
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
		emdashes.Evidence(count["emdash-in-code"], kloc, fmt.Sprintf("%d em dashes in %d lines of code", count["emdash-in-code"], st.codeLines)),
		icons.Evidence(count["utf8-icons-in-code"], kloc, fmt.Sprintf("%d utf-8 icons in %d lines of code", count["utf8-icons-in-code"], st.codeLines)),
		steps.Evidence(count["step-comment"], kloc, fmt.Sprintf("%d step comments in %d lines of code", count["step-comment"], st.codeLines)),
		labels.Evidence(st.labels, kloc, fmt.Sprintf("%d block label comments (\"// Validate input\") in %d lines of code", st.labels, st.codeLines)),
	}
	if funcs := st.humanFunc + st.aiFunc; funcs > 0 {
		share := float64(st.humanFunc) / float64(funcs)
		ev = append(ev, commentVoice.Evidence(st.humanFunc, funcs, fmt.Sprintf("%.0f %% of %d function words in English comments are human-style (will, to, if, we, ...), human code has ~80 %%, AI code ~35 %%", share*100, funcs)))
	}
	if st.englishWords > 0 {
		kew := float64(st.englishWords) / 1000
		ev = append(ev,
			humanMarkers.Evidence(st.markers+st.quest+st.casual, kew, fmt.Sprintf("%d TODO/FIXME/XXX/HACK markers, %d questions and %d first-person words and interjections in %d comment words of files commented in English", st.markers, st.quest, st.casual, st.englishWords)),
			possessives.Evidence(st.possessives, kew, fmt.Sprintf("%d possessives (\"the inbox's\", \"own\") in %d comment words of files commented in English", st.possessives, st.englishWords)))
	}
	if st.judged > 0 {
		basis := fmt.Sprintf("%d of %d files commented in English with %d or more comments have a possessive, \"own\", \", so\" or \"rather than\" in a third of their comments, spread over a third of the file, human code has ~1 %%, AI code ~13 %%", len(st.spread), st.judged, spreadMinComments)
		if len(st.spread) > 0 {
			slices.SortStableFunc(st.spread, func(a, b spreadStat) int { return cmp.Compare(b.share, a.share) })
			names := make([]string, 0, 3)
			for _, f := range st.spread[:min(3, len(st.spread))] {
				names = append(names, f.name)
			}
			basis += ": " + strings.Join(names, ", ")
			if len(st.spread) > 3 {
				basis += ", ..."
			}
		}
		ev = append(ev, spreadFiles.Evidence(len(st.spread), st.judged, basis))
	}
	return ev
}
