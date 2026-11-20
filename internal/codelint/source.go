package codelint

import (
	"path"
	"regexp"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// source is a file prepared for the rules.
type source struct {
	f    *code.File
	name string // display path
	test bool
	code []code.Token // no comments or Python docstrings
	// Whether a 1-based line has code tokens or comments, docstrings included.
	codeLines    []bool
	commentLines []bool
	comments     []*comment
	ignores      []ignoreDirective
	fileIgnores  []ignoreDirective
	declLines    map[int]bool
	bodyStarts   map[int]bool   // see funcBodies
	groupScores  map[[2]int]int // aiScore by the first and last comment of a group
	// Names of the types that some linted type extends or implements, shared
	// by the sources of a run.
	extended map[string]bool
}

func (s *source) declStarts() map[int]bool {
	if s.declLines == nil {
		s.declLines = map[int]bool{}
		for _, d := range s.f.Decls {
			if d.Kind != code.KindParameter && d.Kind != code.KindVariable && d.Kind != code.KindTypeParameter {
				s.declLines[s.line(d.Span.Start)] = true
			}
		}
	}
	return s.declLines
}

type comment struct {
	code.Comment
	lines      []string // without delimiters, indentation or leading javadoc stars
	raw        []string // only the delimiters removed
	line       int      // first line
	end        int      // last line
	standalone bool     // only whitespace before it on its line
	// Tool directives like myoo-ignore, linter comments, pragmas, encodings
	// and build tags aren't prose.
	directive bool
	docstring bool
	license   bool // copyright or license text, see markLicenses
}

func (c *comment) text() string { return strings.TrimSpace(strings.Join(c.lines, " ")) }

func (c *comment) contentLines() int {
	n := 0
	for _, l := range c.lines {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

type ignoreDirective struct {
	line  int
	span  code.Span
	rules map[string]bool // nil means all rules
}

func (d *ignoreDirective) covers(rule string) bool { return d.rules == nil || d.rules[rule] }

var ignoreRe = regexp.MustCompile(`myoo-ignore(-file)?(?:\s*:\s*([a-z0-9-]+(?:\s*,\s*[a-z0-9-]+)*))?`)

func newSource(f *code.File, name string) *source {
	s := &source{
		f:            f,
		name:         name,
		test:         isTestFile(f),
		codeLines:    make([]bool, f.LineCount()+2),
		commentLines: make([]bool, f.LineCount()+2),
	}
	docstrings := map[code.Span]bool{}
	for _, c := range f.Comments {
		if c.Kind == code.DocComment && f.Src[c.Span.Start] != '/' {
			docstrings[c.Span] = true
		}
	}
	for _, t := range f.Tokens {
		if t.Kind == code.TokComment || t.Kind == code.TokString && docstrings[t.Span] {
			continue
		}
		s.code = append(s.code, t)
		for l, e := f.LineOf(t.Span.Start), f.LineOf(t.Span.End); l <= e; l++ {
			s.codeLines[l] = true
		}
	}
	for _, c := range f.Comments {
		cm := s.newComment(c, docstrings[c.Span])
		s.comments = append(s.comments, cm)
		for l := cm.line; l <= cm.end; l++ {
			s.commentLines[l] = true
		}
		s.addIgnores(c)
	}
	s.markLicenses()
	return s
}

// licenseGroupRe matches phrases you'd only find in license and copyright
// notices. We need it because a notice written as "//" lines is a separate
// comment for each line, and most of those lines don't say "license".
var licenseGroupRe = regexp.MustCompile(`(?i)copyright|spdx-license|all rights reserved|\(c\)\s*\d{4}|licensed under|permission is hereby granted|without (?:any )?warrant|general public license|apache license|mozilla public license|free software foundation`)

// markLicenses marks any comment that mentions a license, plus all comments
// in a group of adjacent comments that reads as a notice.
func (s *source) markLicenses() {
	for i := 0; i < len(s.comments); {
		j := i + 1
		for j < len(s.comments) && s.comments[j].Kind == s.comments[i].Kind && s.comments[j].standalone && s.comments[j].line == s.comments[j-1].end+1 {
			j++
		}
		var b strings.Builder
		for _, c := range s.comments[i:j] {
			b.WriteString(strings.Join(c.lines, " "))
			b.WriteByte(' ')
		}
		notice := licenseGroupRe.MatchString(b.String())
		for _, c := range s.comments[i:j] {
			c.license = notice || licenseRe.MatchString(strings.Join(c.lines, " "))
		}
		i = j
	}
}

func (s *source) addIgnores(c code.Comment) {
	text := s.f.Text(c.Span)
	for _, m := range ignoreRe.FindAllStringSubmatchIndex(text, -1) {
		d := ignoreDirective{line: s.line(c.Span.Start + m[0]), span: c.Span}
		if m[4] >= 0 {
			d.rules = map[string]bool{}
			for id := range strings.SplitSeq(text[m[4]:m[5]], ",") {
				d.rules[strings.TrimSpace(id)] = true
			}
		}
		if m[2] >= 0 {
			s.fileIgnores = append(s.fileIgnores, d)
		} else {
			s.ignores = append(s.ignores, d)
		}
	}
}

func (s *source) newComment(c code.Comment, docstring bool) *comment {
	f := s.f
	text := f.Text(c.Span)
	cm := &comment{Comment: c, line: f.LineOf(c.Span.Start), end: f.LineOf(c.Span.End), docstring: docstring}
	ls := f.LineSpan(cm.line)
	cm.standalone = strings.TrimSpace(string(f.Src[ls.Start:c.Span.Start])) == ""
	var body string
	switch {
	case docstring:
		body = strings.TrimLeft(text, "rRuUbBfF")
		q := body[:1]
		if strings.HasPrefix(body, q+q+q) {
			q += q + q
		}
		body = strings.TrimSuffix(strings.TrimPrefix(body, q), q)
	case strings.HasPrefix(text, "/*"):
		body = strings.TrimPrefix(text, "/*")
		body = strings.TrimSuffix(body, "*/")
		if strings.HasPrefix(body, "*") && !strings.HasPrefix(body, "**") || strings.HasPrefix(body, "!") {
			body = body[1:]
		}
	case strings.HasPrefix(text, "///"):
		body = strings.TrimPrefix(text, "///")
	case strings.HasPrefix(text, "//!"):
		body = strings.TrimPrefix(text, "//!")
	case strings.HasPrefix(text, "//"):
		body = strings.TrimPrefix(text, "//")
	case strings.HasPrefix(text, "#"):
		body = strings.TrimPrefix(text, "#")
	case strings.HasPrefix(text, "<!--"):
		body = strings.TrimSuffix(strings.TrimPrefix(text, "<!--"), "-->")
	default:
		body = text
	}
	cm.raw = strings.Split(strings.ReplaceAll(body, "\r", ""), "\n")
	for i, l := range cm.raw {
		l = strings.TrimSpace(l)
		if cm.line != cm.end && i > 0 && (l == "*" || strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "*\t")) {
			l = strings.TrimSpace(l[1:])
		}
		cm.lines = append(cm.lines, l)
	}
	for len(cm.lines) > 0 && cm.lines[0] == "" {
		cm.lines = cm.lines[1:]
	}
	for len(cm.lines) > 0 && cm.lines[len(cm.lines)-1] == "" {
		cm.lines = cm.lines[:len(cm.lines)-1]
	}
	cm.directive = isDirective(cm, text)
	return cm
}

var directiveRe = regexp.MustCompile(`(?i)^(?:myoo-ignore|go:|\+build|nolint|lint:|eslint|jshint|jslint|global |globals |exported |istanbul |c8 |prettier-ignore|biome-ignore|@ts-|@flow|@jsx|@jsxImportSource|@refresh|@vite-ignore|webpackChunkName|#region|#endregion|region\b|endregion\b|pylint:|pyright:|mypy:|type:|noqa|fmt:|isort:|pragma:|nosec|noinspection|NOLINT|NOSONAR|clang-format|resharper|ReSharper|cspell|spell-checker|-\*-|vim?:|ex:|coding[:=]|language=|@formatter|CHECKSTYLE|checkstyle|<auto-generated|nolint|phpcs:|@phpstan-|@psalm-|@codingStandards)`)

func isDirective(c *comment, text string) bool {
	if strings.HasPrefix(text, "#!") {
		return true
	}
	if len(c.lines) == 0 {
		return false
	}
	first := strings.TrimSpace(c.lines[0])
	return directiveRe.MatchString(first) || strings.Contains(text, "myoo-ignore")
}

// suppressed reports whether v is covered by a file-wide myoo-ignore
// directive, or one on the same line, the line before or overlapping v.
func (s *source) suppressed(v violation) bool {
	for _, d := range s.fileIgnores {
		if d.covers(v.rule.ID) {
			return true
		}
	}
	line := s.f.LineOf(v.span.Start)
	for _, d := range s.ignores {
		if !d.covers(v.rule.ID) {
			continue
		}
		if d.line == line || d.line+1 == line || d.span.Start < v.span.End && v.span.Start < d.span.End {
			return true
		}
	}
	return false
}

// nextCodeLine returns the first line after line that has code on it,
// skipping blank and comment lines. If it hits anything else first, or the
// end of the file, it returns 0.
func (s *source) nextCodeLine(line int) int {
	for l := line + 1; l < len(s.codeLines); l++ {
		if s.codeLines[l] {
			return l
		}
		if !s.commentLines[l] && strings.TrimSpace(s.f.Line(l)) != "" {
			return 0
		}
	}
	return 0
}

// codeIndex returns the index into s.code of the first code token at or
// after off.
func (s *source) codeIndex(off int) int {
	lo, hi := 0, len(s.code)
	for lo < hi {
		m := (lo + hi) / 2
		if s.code[m].Span.Start < off {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

func (s *source) tokensIn(sp code.Span) []code.Token {
	i := s.codeIndex(sp.Start)
	j := i
	for j < len(s.code) && s.code[j].Span.End <= sp.End {
		j++
	}
	return s.code[i:j]
}

func (s *source) text(t code.Token) string { return s.f.TokenText(t) }

func (s *source) line(off int) int { return s.f.LineOf(off) }

func (s *source) lineCodeTokens(line int) []code.Token {
	ls := s.f.LineSpan(line)
	i := s.codeIndex(ls.Start)
	j := i
	for j < len(s.code) && s.code[j].Span.Start < ls.End {
		j++
	}
	return s.code[i:j]
}

// indent returns the width of the leading whitespace of line. Tabs go to
// the next multiple of 8 columns, the way Python counts them.
func (s *source) indent(line int) int {
	n := 0
	for _, c := range s.f.Line(line) {
		switch c {
		case ' ':
			n++
		case '\t':
			n = n/8*8 + 8
		default:
			return n
		}
	}
	return n
}

var testNameRe = regexp.MustCompile(`(?i)(?:_test\.go$|^test_.*\.py$|_test\.py$|^conftest\.py$|\.(?:test|spec)\.[cm]?[jt]sx?$|tests?\.(?:java|cs|php)$|_tests?\.(?:c|cc|cpp|cxx)$)`)

func isTestFile(f *code.File) bool {
	if testNameRe.MatchString(path.Base(f.Path)) {
		return true
	}
	for seg := range strings.SplitSeq(path.Dir(f.Path), "/") {
		switch strings.ToLower(seg) {
		case "test", "tests", "__tests__", "spec", "testing":
			return true
		}
	}
	return false
}

var generatedRe = regexp.MustCompile(`(?is)code generated .{0,300}?do not edit|@generated|<auto-generated|autogenerated file|this file (?:is|was) (?:auto-?)?generated|generated by .{0,300}?do not edit`)

// generated reports whether f is a minified bundle or a file whose leading
// comments say so. We read the comments of the first 40 lines as a single
// text, because a marker can wrap onto the next line comment
// ("// Generated by gen.ts from ...\n// Do not edit.").
func generated(f *code.File) bool {
	base := path.Base(f.Path)
	if strings.Contains(base, ".min.") {
		return true
	}
	var lead []byte
	for _, c := range f.Comments {
		if f.LineOf(c.Span.Start) > 40 {
			break
		}
		lead = append(append(lead, f.Src[c.Span.Start:c.Span.End]...), '\n')
	}
	return generatedRe.Match(lead)
}
