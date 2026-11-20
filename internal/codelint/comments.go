package codelint

import (
	"bytes"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/likelihood"
	proselint "github.com/myoo-ai/cli/internal/prose"
)

var licenseRe = regexp.MustCompile(`(?i)copyright|\blicen[cs]e|spdx-|all rights reserved|\(c\)\s*\d{4}`)

var useDirectiveRe = regexp.MustCompile(`^["']use [a-z ]+["']$`)

// checkFileHeader flags explanatory comments and docstrings that come before
// the first code in a file. We accept shebangs and other directives, license
// headers, one-line summaries, Go package doc comments of up to
// goPackageDocLines lines or any length in doc.go, and Java package-info.java
// files, since those exist to carry the package documentation. In PHP the
// open tag and declare statements don't count as code, and a docblock above
// the namespace statement is a file header.
func checkFileHeader(s *source) []finding {
	if s.f.Lang == code.Java && path.Base(s.f.Path) == "package-info.java" {
		return nil
	}
	first := len(s.f.Src)
	for i := s.phpPreambleEnd(); i < len(s.code); i++ {
		t := s.code[i]
		if (s.f.Lang == code.JavaScript || s.f.Lang == code.TypeScript) && t.Kind == code.TokString && useDirectiveRe.MatchString(s.text(t)) {
			continue
		}
		if t.Kind == code.TokPunct && s.text(t) == ";" && i > 0 && s.code[i-1].Kind == code.TokString {
			continue
		}
		first = t.Span.Start
		break
	}
	var header []*comment
	for _, c := range s.comments {
		if c.Span.Start >= first {
			break
		}
		if c.directive || c.license || len(c.lines) == 0 || s.f.Src[c.Span.Start] == '<' {
			continue
		}
		header = append(header, c)
	}
	if s.f.Lang == code.Rust {
		header = s.dropRustModuleDoc(header)
	}
	if s.f.Lang == code.Go && len(header) > 0 {
		if path.Base(s.f.Path) == "doc.go" {
			return nil
		}
		header = dropGoPackageDoc(s, header, first)
	} else if s.f.Lang != code.Python && first < len(s.f.Src) && s.declStarts()[s.line(first)] && !s.phpNamespaceAt(first) {
		// The comment directly above the first declaration documents it.
		header = header[:attachedGroup(header, s.line(first))]
	}
	if len(header) == 0 {
		return nil
	}
	lines := 0
	for _, c := range header {
		lines += c.contentLines()
	}
	base := path.Base(s.f.Path)
	if lines < 2 && !strings.HasPrefix(strings.ToLower(header[0].lines[0]), strings.ToLower(base)) {
		return nil
	}
	return []finding{{span: code.Span{Start: header[0].Span.Start, End: header[len(header)-1].Span.End}}}
}

// goPackageDocLines is the longest Go package doc comment we accept outside
// doc.go. godoc shows it as the package synopsis, so it should stay short.
const goPackageDocLines = 5

// dropGoPackageDoc removes the comment group that ends on the line above the
// package clause if it's goPackageDocLines lines or shorter.
func dropGoPackageDoc(s *source, header []*comment, pkg int) []*comment {
	start := attachedGroup(header, s.line(pkg))
	lines := 0
	for _, c := range header[start:] {
		lines += c.contentLines()
	}
	if lines > goPackageDocLines {
		return header
	}
	return header[:start]
}

// attachedGroup returns the index of the first comment of the group that
// ends on the line above line, or len(header) if there's none.
func attachedGroup(header []*comment, line int) int {
	end := len(header)
	if end == 0 || header[end-1].end != line-1 {
		return end
	}
	start := end - 1
	for start > 0 && header[start-1].end == header[start].line-1 {
		start--
	}
	return start
}

const decoChars = "=-*#~_+/\\"

func isDecoRune(r rune) bool {
	return strings.ContainsRune(decoChars, r) || r >= 0x2500 && r <= 0x259F
}

// decoRun returns the length in runes of the run of identical decoration
// characters at the start of s.
func decoRun(s string) int {
	r, _ := utf8.DecodeRuneInString(s)
	if !isDecoRune(r) {
		return 0
	}
	n := 0
	for _, c := range s {
		if c != r {
			break
		}
		n++
	}
	return n
}

func reverse(s string) string {
	rs := []rune(s)
	slices.Reverse(rs)
	return string(rs)
}

// isDecorativeLine reports whether l is a separator line ("=====", "-----")
// or a titled banner ("===== Helpers =====",
// "\u2500\u2500 Section \u2500\u2500", "Helpers ------").
func isDecorativeLine(l string) bool {
	l = strings.TrimSpace(l)
	if l == "" {
		return false
	}
	left, right := decoRun(l), decoRun(reverse(l))
	n := utf8.RuneCountInString(l)
	if left == n {
		return n >= 4
	}
	r, _ := utf8.DecodeRuneInString(l)
	box := r >= 0x2500
	minRun := 3
	if box {
		minRun = 2
	}
	title := strings.TrimSpace(string([]rune(l)[left : n-right]))
	if !strings.ContainsFunc(title, unicode.IsLetter) {
		return false
	}
	switch {
	case left >= minRun && right >= minRun:
		return true
	case left >= 4 && right == 0, left == 0 && right >= 4:
		return true
	}
	return false
}

func checkDecorative(s *source) []finding {
	var out []finding
	for _, c := range s.comments {
		if c.docstring || c.directive || c.license {
			continue
		}
		for i, l := range c.raw {
			if !isDecorativeLine(l) || s.goCodeBlockLine(c.line+i) {
				continue
			}
			ls := s.f.LineSpan(c.line + i)
			sp := code.Span{Start: max(ls.Start, c.Span.Start), End: min(ls.End, c.Span.End)}
			for sp.Start < sp.End && (s.f.Src[sp.Start] == ' ' || s.f.Src[sp.Start] == '\t') {
				sp.Start++
			}
			out = append(out, finding{span: sp})
		}
	}
	return out
}

// isolated reports whether c is a one-line comment, and the lines right
// above and below it have no comment.
func (s *source) isolated(c *comment) bool {
	return c.line == c.end && !s.commentLines[c.line-1] && !s.commentLines[c.end+1]
}

// prose reports whether c is a regular comment that the comment rules judge
// by its wording. Docstrings and doc comments aren't, nor are directives.
func prose(c *comment) bool {
	return !c.docstring && c.Kind != code.DocComment && !c.directive && len(c.lines) > 0
}

// scopeOf returns the innermost callable containing off, or the module.
func (s *source) scopeOf(off int) *code.Decl {
	d := s.f.DeclAt(off)
	for d != nil && d != s.f.Module && !d.Callable() {
		d = d.Parent
	}
	if d == nil {
		return s.f.Module
	}
	return d
}

var (
	stepRe     = regexp.MustCompile(`(?i)^(?:step|phase|stage)\s*#?\d+[a-z]?\b`)
	numberedRe = regexp.MustCompile(`^(\d{1,2})[.)]\s+\p{L}`)
	sequenceRe = regexp.MustCompile(`(?i)^(first|firstly|second|secondly|third|thirdly|then|next|after that|afterwards|finally|lastly)\b,?\s`)
)

// checkStepComments flags "Step 1:" comments. It also flags numbered or
// first/then/finally comments if a function has two or more of them.
func checkStepComments(s *source) []finding {
	var out []finding
	numbered := map[*code.Decl][]*comment{}
	sequenced := map[*code.Decl][]*comment{}
	for _, c := range s.comments {
		if !prose(c) {
			continue
		}
		first := c.lines[0]
		if stepRe.MatchString(first) {
			out = append(out, finding{span: c.Span})
			continue
		}
		if !c.standalone || !s.isolated(c) {
			continue
		}
		scope := s.scopeOf(c.Span.Start)
		if numberedRe.MatchString(first) {
			numbered[scope] = append(numbered[scope], c)
		} else if sequenceRe.MatchString(first) {
			sequenced[scope] = append(sequenced[scope], c)
		}
	}
	for _, cs := range numbered {
		nums := map[string]bool{}
		for _, c := range cs {
			nums[numberedRe.FindStringSubmatch(c.lines[0])[1]] = true
		}
		if len(nums) >= 2 {
			for _, c := range cs {
				out = append(out, finding{span: c.Span})
			}
		}
	}
	for _, cs := range sequenced {
		var opener, follower bool
		for _, c := range cs {
			switch strings.ToLower(sequenceRe.FindStringSubmatch(c.lines[0])[1]) {
			case "first", "firstly":
				opener = true
			default:
				follower = true
			}
		}
		if opener && follower {
			for _, c := range cs {
				out = append(out, finding{span: c.Span})
			}
		}
	}
	return out
}

var (
	narratingRe = regexp.MustCompile(`(?i)^(?:now,?\s+(?:we|let'?s|let us|i)\b|here,?\s+(?:we|i)\b|here'?s\s+(?:where|how)\s+(?:we|i)\b|let'?s\s|let us\s|we(?:'ll|\s+will)\s+now\b|(?:next|then|first|finally),?\s+(?:we|let'?s|i)\s|we\s+(?:need|have|want)\s+to\s|(?:this\s+is\s+)?(?:a\s+)?helper\s+(?:function|method)\s+(?:to|that|for|which)\b|the\s+following\s+(?:code|function|method|block|lines?|loop|section)\b|this\s+(?:code|block|section|snippet|loop|line)\s+(?:will|is|does|handles|checks|creates|makes|ensures|performs|sets|gets|calls|iterates|loops|adds|updates|returns|initiali[sz]es)\b)`)
	thisDeclRe  = regexp.MustCompile(`(?i)^this\s+(?:function|method|class|constructor|helper|struct|interface|module|file|script|component|hook|endpoint|handler|test)\b`)
	reasonRe    = regexp.MustCompile(`(?i)\b(?:because|since|otherwise|so that|in order to|or else|to avoid|to prevent|due to|as long as|unless)\b`)
	letsRe      = regexp.MustCompile(`(?i)^(?:let'?s|let us|here,?\s+we|here'?s)\b`)
)

func checkNarrating(s *source) []finding {
	var out []finding
	for _, c := range s.comments {
		if !prose(c) {
			continue
		}
		first := c.lines[0]
		hit := narratingRe.MatchString(first)
		if !hit && thisDeclRe.MatchString(first) && c.standalone {
			hit = s.declStarts()[s.nextCodeLine(c.end)]
		}
		if hit && (letsRe.MatchString(first) || !reasonRe.MatchString(c.text())) {
			out = append(out, finding{span: c.Span})
		}
	}
	return out
}

var (
	noteRe = regexp.MustCompile(`(?i)\b(?:todo|fixme|note|hack|xxx|bug|nb|see|warning|deprecated|safety)\b|://|[\x60"'{}()\[\]=<>]`)
	// fillerWords don't carry any content of their own in a comment.
	fillerWords = setOf("the", "a", "an", "to", "of", "and", "or", "for", "in", "on", "at", "by", "from",
		"with", "into", "onto", "this", "that", "these", "those", "it", "its", "is", "are", "be", "been",
		"we", "our", "us", "i", "my", "you", "your", "then", "now", "here", "there", "all", "each", "some",
		"as", "up", "out", "variable", "var", "function", "method", "call", "calls", "value", "just",
		"simply", "do", "does", "will", "should", "can", "if", "else", "not", "no")
	// operatorWords name what operators and keywords do.
	operatorWords = map[string][]string{
		"+=": {"increment", "increase", "add", "append"}, "++": {"increment", "increase"},
		"-=": {"decrement", "decrease", "subtract"}, "--": {"decrement", "decrease"},
		"=": {"set", "assign", "store", "initialize", "save"}, ":=": {"set", "assign", "store", "initialize", "create", "save"},
		"==": {"compare", "check", "equal"}, "===": {"compare", "check", "equal"}, "!=": {"compare", "check", "differ"},
		"+": {"add", "sum", "plus", "concatenate"}, "-": {"subtract", "minus"}, "*": {"multiply", "times"}, "/": {"divide"},
		"return": {"return", "result"}, "if": {"check", "whether"}, "for": {"loop", "iterate", "over", "every"},
		"while": {"loop", "iterate", "until"}, "new": {"create", "instantiate", "new"}, "raise": {"raise", "throw", "error"},
		"throw": {"throw", "raise", "error"}, "import": {"import"}, "del": {"delete", "remove"},
		"delete": {"delete", "remove"}, "try": {"try"}, "break": {"break", "stop", "exit"}, "continue": {"continue", "skip"},
		"print": {"print", "output", "show", "display"}, "true": {"true", "enable"}, "false": {"false", "disable"},
		"defer": {"defer", "close"}, "go": {"start", "spawn"}, "await": {"wait", "await"}, "in": {"in", "contains"},
		"append": {"append", "add"}, "len": {"length", "count", "size"},
	}
)

// lineVocabulary returns the words a reader sees in toks. Next to identifier
// parts and keywords it adds string contents and what the operators do.
func (s *source) lineVocabulary(toks []code.Token) map[string]bool {
	vocab := map[string]bool{}
	for _, t := range toks {
		text := s.text(t)
		switch t.Kind {
		case code.TokIdent:
			vocab[strings.ToLower(text)] = true
			for _, w := range identWords(text) {
				vocab[w] = true
			}
		case code.TokString:
			for _, w := range likelihood.Words(text) {
				vocab[w] = true
			}
		case code.TokKeyword, code.TokNumber:
			vocab[strings.ToLower(text)] = true
		}
		for _, w := range operatorWords[strings.ToLower(text)] {
			vocab[w] = true
		}
	}
	return vocab
}

// checkObvious flags one-line comments if all their content words appear in
// the code line they describe.
func checkObvious(s *source) []finding {
	var out []finding
	for _, c := range s.comments {
		if !prose(c) || c.line != c.end || noteRe.MatchString(c.text()) {
			continue
		}
		var toks []code.Token
		if c.standalone {
			if !s.isolated(c) || !s.codeLines[c.line+1] || s.declStarts()[c.line+1] {
				continue
			}
			toks = s.lineCodeTokens(c.line + 1)
		} else {
			for _, t := range s.lineCodeTokens(c.line) {
				if t.Span.Start < c.Span.Start {
					toks = append(toks, t)
				}
			}
		}
		if len(toks) == 0 {
			continue
		}
		var content []string
		for _, w := range likelihood.Words(c.text()) {
			if !fillerWords[w] {
				content = append(content, w)
			}
		}
		if len(content) < 2 {
			continue
		}
		if covered(content, s.lineVocabulary(toks), nil) {
			out = append(out, finding{span: c.Span})
		}
	}
	return out
}

var docFillers = setOf("the", "a", "an", "this", "that", "function", "method", "class", "constructor",
	"returns", "return", "it", "its", "of", "for", "given", "specified", "object", "instance", "new",
	"helper", "is", "which", "will", "type", "struct", "interface", "enum", "property", "field",
	"get", "gets", "set", "sets", "or", "and", "to", "value", "creates", "create", "summary")

var xmlTagRe = regexp.MustCompile(`<[^>]*>`)

// docOf returns the docstring of d, or else the comment group that ends right
// above the declaration.
func (s *source) docOf(d *code.Decl) []*comment {
	if s.f.Lang == code.Python {
		for _, c := range s.comments {
			if c.docstring && c.Span.Start == d.Body.Start {
				return []*comment{c}
			}
		}
		return nil
	}
	var group []*comment
	end := d.Span.Start
	lineLike := func(c *comment) bool { b := s.f.Src[c.Span.Start]; return b == '#' || s.f.Src[c.Span.Start+1] == '/' }
	for _, c := range slices.Backward(s.comments) {
		if c.Span.End > end {
			continue
		}
		if strings.TrimSpace(string(s.f.Src[c.Span.End:end])) != "" || s.line(end)-c.end > 1 || !c.standalone || c.directive {
			break
		}
		if len(group) > 0 && (!lineLike(c) || !lineLike(group[0])) {
			break
		}
		group = append([]*comment{c}, group...)
		end = c.Span.Start
	}
	return group
}

// checkRedundantDoc flags the documentation of functions and types if all
// its words come from the declared name plus filler words.
func checkRedundantDoc(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() && !d.IsType() {
			continue
		}
		doc := s.docOf(d)
		if len(doc) == 0 {
			continue
		}
		var text []string
		for _, c := range doc {
			text = append(text, c.lines...)
		}
		ws := likelihood.Words(xmlTagRe.ReplaceAllString(strings.Join(text, " "), " "))
		if len(ws) == 0 {
			continue
		}
		vocab := setOf(identWords(d.Name)...)
		vocab[strings.ToLower(d.Name)] = true
		if covered(ws, vocab, docFillers) {
			out = append(out, finding{span: code.Span{Start: doc[0].Span.Start, End: doc[len(doc)-1].Span.End}})
		}
	}
	return out
}

// definitionVerbs follow a member's name in a comment that defines the
// member by its name, as in "ctx is the innermost declaration".
var definitionVerbs = setOf("is", "are", "holds", "stores", "keeps", "contains", "lists", "records", "tracks",
	"maps", "counts", "caches", "collects", "describes", "represents", "marks", "names", "identifies",
	"indicates", "means", "says", "tells", "remembers", "specifies", "controls", "determines")

// minDefinitions is how many members of a type need a comment that defines
// them by name before we report them. Go code opens a field comment with
// the field name now and then. "X is the ..., Y is the ..." on member after
// member is the pattern.
const minDefinitions = 2

// checkDefinitionComments flags comments on fields and properties, and on enum
// members, that define the member by its own name, if a type has
// minDefinitions of them and they make up at least half of its documented
// members. Godoc expects the comments of exported Go names to start with the
// name, so we skip those.
func checkDefinitionComments(s *source) []finding {
	documented := map[*code.Decl]int{}
	defs := map[*code.Decl][]finding{}
	var owners []*code.Decl
	for _, d := range s.f.Decls {
		if d.Kind != code.KindField && d.Kind != code.KindProperty && d.Kind != code.KindEnumMember || d.Parent == nil {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(d.Name); s.f.Lang == code.Go && unicode.IsUpper(r) {
			continue
		}
		doc := s.docOf(d)
		if len(doc) == 0 {
			continue
		}
		documented[d.Parent]++
		ws := strings.Fields(xmlTagRe.ReplaceAllString(doc[0].text(), " "))
		if len(ws) < 2 || strings.Trim(ws[0], "`") != d.Name || !definitionVerbs[strings.ToLower(ws[1])] {
			continue
		}
		if len(defs[d.Parent]) == 0 {
			owners = append(owners, d.Parent)
		}
		sp := code.Span{Start: doc[0].Span.Start, End: doc[len(doc)-1].Span.End}
		defs[d.Parent] = append(defs[d.Parent], finding{span: sp, detail: "`" + d.Name + " " + ws[1] + "`"})
	}
	var out []finding
	for _, o := range owners {
		if n := len(defs[o]); n >= minDefinitions && 2*n >= documented[o] {
			out = append(out, defs[o]...)
		}
	}
	return out
}

// commentProseSkip lists the prose_lint rules that comment-prose leaves out.
// We have our own rules for the characters. Some layout rules misread
// constants and numbered formats, and markdown layout doesn't matter in a
// comment.
var commentProseSkip = setOf("emdash", "utf8-chars-as-icons", "emoji", "capitals-as-heading",
	"left-padded-numbers", "inline-header-list", "title-case-heading")

// commentProseUnscored lists the prose_lint rules that comment-prose started
// reporting after we fit the likelihood rates, so they don't count there.
var commentProseUnscored = setOf("rule-of-three", "label-colon", "nominalization")

// minTriples is how many lists of 3 a file needs before we report them.
// People write one now and then ("svg, png and pdf"), agents write them in
// comment after comment.
const minTriples = 2

// checkCommentProse reports the prose_lint findings in comments and
// docstrings. We leave out quoted text and Go doc code blocks.
func checkCommentProse(s *source) []finding {
	var out, triples []finding
	for _, g := range s.proseBlocks() {
		first, last := g[0], g[len(g)-1]
		block := code.Span{Start: first.Span.Start, End: last.Span.End}
		text := s.proseSpanText(block, first.docstring)
		for _, v := range lintCommentText(text, first.docstring) {
			sp := code.Span{Start: block.Start + v.Start, End: block.Start + v.End}
			if commentProseSkip[v.Rule.ID] || s.quotedAt(sp.Start) {
				continue
			}
			quote := strings.Join(strings.Fields(text[v.Start:v.End]), " ")
			fd := finding{span: sp, detail: v.Rule.ID + " `" + quote + "`", unscored: commentProseUnscored[v.Rule.ID]}
			if v.Rule.ID == "rule-of-three" {
				triples = append(triples, fd)
				continue
			}
			out = append(out, fd)
		}
	}
	if len(triples) >= minTriples {
		out = append(out, triples...)
	}
	return out
}

// proseBlocks returns the comments comment-prose reads, grouped into the
// texts a reader sees. A block of "//" or "#" lines on consecutive lines is
// one text, so a sentence can wrap.
func (s *source) proseBlocks() [][]*comment {
	var blocks [][]*comment
	prev := -1
	for i, c := range s.comments {
		if c.directive || c.license || len(c.lines) == 0 || commentedOutCode(c) {
			continue
		}
		if prev >= 0 && prev == i-1 && s.lineComment(c) && s.lineComment(s.comments[prev]) && s.continues(prev, i) {
			blocks[len(blocks)-1] = append(blocks[len(blocks)-1], c)
		} else {
			blocks = append(blocks, []*comment{c})
		}
		prev = i
	}
	return blocks
}

// lineComment reports whether c is a "//" or "#" comment, as opposed to a
// block comment or docstring.
func (s *source) lineComment(c *comment) bool {
	b := s.f.Src[c.Span.Start]
	return !c.docstring && (b == '#' || b == '/' && s.f.Src[c.Span.Start+1] == '/')
}

// lintCommentText runs prose_lint on the prose text of a comment. Python
// docstrings list arguments as "name: description", so they don't get the
// comment rules.
func lintCommentText(text string, docstring bool) []proselint.Violation {
	if docstring {
		return proselint.LintString(text, "en")
	}
	return proselint.LintComment(text, "en")
}

var (
	docstringOpenRe = regexp.MustCompile(`^[rRuUbBfF]*("""|'''|"|')`)
	lineDelimRe     = regexp.MustCompile(`^[ \t]*(?:/{2,3}!?|#+!?|\*(?:[ \t]|$))`)
)

// proseText returns the source of c with its delimiters, docstring quotes,
// javadoc stars and XML doc tags turned into spaces. That way an offset into
// it is still an offset from c.Span.Start.
func (s *source) proseText(c *comment) string { return s.proseSpanText(c.Span, c.docstring) }

// proseSpanText is proseText for the comments in sp, which can be a block of
// line comments.
func (s *source) proseSpanText(sp code.Span, docstring bool) string {
	b := []byte(s.f.Text(sp))
	blank := func(i, j int) {
		for k := max(i, 0); k < j && k < len(b); k++ {
			if b[k] != '\n' && b[k] != '\r' {
				b[k] = ' '
			}
		}
	}
	switch {
	case docstring:
		if m := docstringOpenRe.FindSubmatchIndex(b); m != nil {
			blank(0, m[1])
			blank(len(b)-(m[3]-m[2]), len(b))
		}
	case strings.HasPrefix(string(b), "/*"):
		open := 2
		if len(b) > 2 && (b[2] == '*' || b[2] == '!') {
			open = 3
		}
		blank(0, open)
		if len(b) >= open+2 && string(b[len(b)-2:]) == "*/" {
			blank(len(b)-2, len(b))
		}
	case strings.HasPrefix(string(b), "<!--"):
		blank(0, 4)
		if bytes.HasSuffix(b, []byte("-->")) {
			blank(len(b)-3, len(b))
		}
	}
	start := 0
	for start <= len(b) {
		end := start
		for end < len(b) && b[end] != '\n' {
			end++
		}
		if m := lineDelimRe.FindIndex(b[start:end]); m != nil && !docstring {
			blank(start, start+m[1])
		}
		start = end + 1
	}
	for _, m := range xmlTagRe.FindAllIndex(b, -1) {
		blank(m[0], m[1])
	}
	return string(b)
}
