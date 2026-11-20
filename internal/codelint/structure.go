package codelint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/likelihood"
)

var (
	logRoots   = setOf("print", "println", "printf", "eprint", "eprintln", "puts", "slog", "logging", "logger", "log", "logs", "console", "stderr", "stdout", "traceback", "warnings", "debug", "trace", "err", "out", "cerr", "cout", "clog")
	logMethods = setOf("print", "println", "printf", "fprint", "fprintf", "fprintln", "logerror", "logwarning", "loginformation", "logdebug", "logcritical", "logtrace", "error_log")
	todoRe     = regexp.MustCompile(`(?i)\b(?:todo|fixme|xxx)\b`)
)

// isLogCall reports whether stmt is a single call that does nothing but
// print or log, like print(e), logger.warning(...), console.error(e),
// System.err.println(e) or std::cerr << e.what().
func (s *source) isLogCall(stmt []code.Token) bool {
	if len(stmt) == 0 {
		return false
	}
	var names []string
	i := 0
	for i < len(stmt) {
		t := stmt[i]
		if t.Kind != code.TokIdent && t.Kind != code.TokKeyword {
			return false
		}
		names = append(names, strings.ToLower(strings.Trim(s.text(t), "_$")))
		i++
		if i < len(stmt) {
			switch s.text(stmt[i]) {
			case ".", "::", "->", "?.":
				i++
				continue
			}
		}
		break
	}
	if i >= len(stmt) || len(names) == 0 {
		return false
	}
	method := names[len(names)-1]
	logging := method == "printstacktrace" || logMethods[method] && (len(names) == 1 || len(names) == 2 && names[0] == "fmt")
	for _, n := range names {
		logging = logging || logRoots[n] || strings.HasSuffix(n, "logger")
	}
	if !logging {
		return false
	}
	switch s.text(stmt[i]) {
	case "<<":
		return logRoots[names[len(names)-1]]
	case "(":
		return s.matchClose(stmt, i) == len(stmt)-1
	}
	return false
}

// justified reports whether one of cs explains a deliberate choice. We take
// any comment of three words or more, as long as it isn't a TODO.
func justified(cs []*comment) bool {
	for _, c := range cs {
		if c.directive || todoRe.MatchString(c.text()) {
			continue
		}
		if len(likelihood.Words(c.text())) >= 3 {
			return true
		}
	}
	return false
}

func checkSwallowed(s *source) []finding {
	switch s.f.Lang {
	case code.Python:
		return s.swallowedPython()
	case code.Go:
		return s.swallowedGo()
	case code.Rust:
		return s.swallowedRust()
	}
	return s.swallowedBraces()
}

func (s *source) swallowedPython() []finding {
	var out []finding
	for i, t := range s.code {
		if !s.isKeyword(t, "except") || !s.firstOnLine(i) {
			continue
		}
		line := s.line(t.Span.Start)
		colon := i + 1
		depth := 0
		for ; colon < len(s.code); colon++ {
			x := s.text(s.code[colon])
			if x == "(" || x == "[" {
				depth++
			} else if x == ")" || x == "]" {
				depth--
			} else if x == ":" && depth == 0 {
				break
			}
		}
		if colon >= len(s.code) {
			continue
		}
		broad := colon == i+1
		for _, h := range s.code[i+1 : colon] {
			if s.text(h) == "as" {
				break
			}
			if x := s.text(h); x == "Exception" || x == "BaseException" {
				broad = true
			}
		}
		if !broad {
			continue
		}
		indent := s.indent(line)
		end := colon + 1
		for end < len(s.code) {
			l := s.line(s.code[end].Span.Start)
			if l > line && s.firstOnLine(end) && s.indent(l) <= indent {
				break
			}
			end++
		}
		body := s.code[colon+1 : end]
		if len(body) == 0 {
			continue
		}
		sp := code.Span{Start: t.Span.Start, End: body[len(body)-1].Span.End}
		if !s.ignoresError(s.splitLogicalLines(body)) || justified(s.commentsIn(code.Span{Start: t.Span.Start, End: s.f.LineSpan(s.line(sp.End)).End})) {
			continue
		}
		out = append(out, finding{span: sp})
	}
	return out
}

// ignoresError reports whether the handler does nothing but pass, continue
// or log.
func (s *source) ignoresError(stmts [][]code.Token) bool {
	for _, st := range stmts {
		if len(st) == 1 && s.isKeyword(st[0], "pass", "continue") || s.isEllipsis(st) || s.isLogCall(st) {
			continue
		}
		return false
	}
	return true
}

func (s *source) isEllipsis(st []code.Token) bool {
	var b strings.Builder
	for _, t := range st {
		b.WriteString(s.text(t))
	}
	return b.String() == "..."
}

func (s *source) swallowedBraces() []finding {
	var out []finding
	for i, t := range s.code {
		if !s.isKeyword(t, "catch") || i > 0 && s.text(s.code[i-1]) == "." {
			continue
		}
		j := i + 1
		var header []code.Token
		if j < len(s.code) && s.text(s.code[j]) == "(" {
			cl := s.matchClose(s.code, j)
			if cl >= len(s.code) {
				continue
			}
			header = s.code[j+1 : cl]
			j = cl + 1
		}
		if j >= len(s.code) || s.text(s.code[j]) != "{" || !s.broadCatch(header) {
			continue
		}
		cl := s.matchClose(s.code, j)
		if cl >= len(s.code) {
			continue
		}
		body := s.code[j+1 : cl]
		sp := code.Span{Start: t.Span.Start, End: s.code[cl].Span.End}
		block := false
		for _, b := range body {
			block = block || s.text(b) == "{"
		}
		if block || !s.ignoresError(s.splitStatements(body)) || justified(s.commentsIn(sp)) {
			continue
		}
		out = append(out, finding{span: sp})
	}
	return out
}

// broadCatch reports whether a catch clause header catches all exceptions.
func (s *source) broadCatch(header []code.Token) bool {
	switch s.f.Lang {
	case code.JavaScript, code.TypeScript:
		return true
	}
	if len(header) == 0 {
		return s.f.Lang == code.CSharp
	}
	var b strings.Builder
	for _, t := range header {
		b.WriteString(s.text(t))
	}
	h := b.String()
	if h == "..." {
		return true
	}
	types := header
	if s.f.Lang == code.Java && len(types) > 1 {
		types = types[:len(types)-1]
	}
	for _, t := range types {
		switch s.text(t) {
		case "when":
			return false
		case "Exception", "Throwable":
			if s.f.Lang == code.Java || s.f.Lang == code.CSharp || s.f.Lang == code.PHP {
				return true
			}
		case "exception":
			if s.f.Lang == code.CPP {
				return true
			}
		}
	}
	return false
}

func isErrName(n string) bool { return n == "err" || strings.HasSuffix(n, "Err") }

func (s *source) swallowedGo() []finding {
	var out []finding
	for i, t := range s.code {
		switch {
		case s.isKeyword(t, "if") && i+4 < len(s.code):
			c := s.code[i+1 : i+5]
			if !isErrName(s.text(c[0])) || s.text(c[1]) != "!=" || s.text(c[2]) != "nil" || s.text(c[3]) != "{" {
				continue
			}
			cl := s.matchClose(s.code, i+4)
			if cl != i+5 {
				continue
			}
			sp := code.Span{Start: t.Span.Start, End: s.code[cl].Span.End}
			if !justified(s.commentsIn(sp)) {
				out = append(out, finding{span: sp})
			}
		case s.text(t) == "_" && s.firstOnLine(i) && i+2 < len(s.code) && s.text(s.code[i+1]) == "=" && isErrName(s.text(s.code[i+2])):
			if i+3 < len(s.code) && s.line(s.code[i+3].Span.Start) == s.line(t.Span.Start) {
				continue
			}
			sp := code.Span{Start: t.Span.Start, End: s.code[i+2].Span.End}
			if !s.commentJustifies(s.line(sp.Start)) {
				out = append(out, finding{span: sp})
			}
		}
	}
	return out
}

// commentJustifies reports whether there's a justifying comment on line or
// on the line above it.
func (s *source) commentJustifies(line int) bool {
	var cs []*comment
	for _, c := range s.comments {
		if c.end == line || c.end == line-1 && c.standalone {
			cs = append(cs, c)
		}
	}
	return justified(cs)
}

var suppressionRe = regexp.MustCompile(`(?:^(?:#|//+|/\*+)\s*|\s#\s*|\n[ \t]*\*[ \t]*)(type:\s*ignore(?:\[[^\]]*\])?|noqa(?::\s*[A-Z]+[0-9]+(?:\s*,\s*[A-Z]+[0-9]+)*)?|pylint:\s*disable(?:-next)?=[\w,-]+|pyright:\s*ignore(?:\[[^\]]*\])?|@ts-ignore|@ts-nocheck|@ts-expect-error|eslint-disable(?:-next-line|-line)?(?:\s+[\w/@-]+(?:\s*,\s*[\w/@-]+)*)?|nolint(?::[\w,-]+)?|lint:ignore\s+\w+|istanbul\s+ignore\s+\w+|c8\s+ignore\s+\w+|NOLINT(?:NEXTLINE|BEGIN)?(?:\([^)]*\))?|NOSONAR|noinspection\s+\w+|cppcheck-suppress\s+\w+|@phpstan-ignore(?:-line|-next-line)?(?:\s+[\w-]+\.[\w.-]+(?:\s*,\s*[\w-]+\.[\w.-]+)*)?|@psalm-suppress(?:\s+\w+(?:\s*,\s*\w+)*)?|phpcs:(?:ignoreFile|ignore|disable)(?:\s+[\w-]+\.[\w.-]+(?:\s*,\s*[\w-]+\.[\w.-]+)*)?|@codingStandardsIgnore(?:Line|Start|File)?)`)

// justificationAfter reports whether the rest of a suppression comment
// gives a reason, which we take to be two words or more.
func justificationAfter(rest string) bool {
	rest = strings.TrimLeft(rest, " \t:-#/*\u2014")
	return len(likelihood.Words(rest)) >= 2
}

func checkSuppressed(s *source) []finding {
	var out []finding
	for _, c := range s.comments {
		text := s.f.Text(c.Span)
		for _, m := range suppressionRe.FindAllStringSubmatchIndex(text, -1) {
			if justificationAfter(strings.TrimSuffix(text[m[3]:], "*/")) || s.commentAbove(c.line) {
				continue
			}
			out = append(out, finding{span: code.Span{Start: c.Span.Start + m[2], End: c.Span.Start + m[3]}})
		}
	}
	for i, t := range s.code {
		switch {
		case s.text(t) == "SuppressWarnings" && i > 0 && s.text(s.code[i-1]) == "@":
			end := i
			if i+1 < len(s.code) && s.text(s.code[i+1]) == "(" {
				end = min(s.matchClose(s.code, i+1), len(s.code)-1)
			}
			line := s.line(t.Span.Start)
			if !s.commentJustifies(line) {
				out = append(out, finding{span: code.Span{Start: s.code[i-1].Span.Start, End: s.code[end].Span.End}})
			}
		case (s.text(t) == "SuppressMessage" || s.text(t) == "SuppressMessageAttribute") && i+1 < len(s.code) && s.text(s.code[i+1]) == "(":
			end := min(s.matchClose(s.code, i+1), len(s.code)-1)
			justification := false
			for _, a := range s.code[i:end] {
				justification = justification || s.text(a) == "Justification"
			}
			if !justification {
				out = append(out, finding{span: code.Span{Start: t.Span.Start, End: s.code[end].Span.End}})
			}
		case s.text(t) == "#" && i+2 < len(s.code) && s.text(s.code[i+1]) == "pragma":
			line := s.line(t.Span.Start)
			toks := s.lineCodeTokens(line)
			var b strings.Builder
			for _, x := range toks {
				b.WriteString(s.text(x))
				b.WriteByte(' ')
			}
			p := b.String()
			if strings.Contains(p, "warning disable") || strings.Contains(p, "warning ( disable") || strings.Contains(p, "diagnostic ignored") {
				if !s.commentJustifies(line) {
					out = append(out, finding{span: spanOf(toks)})
				}
			}
		case s.f.Lang == code.Rust && s.text(t) == "#":
			if fd, ok := s.rustSuppressed(i); ok {
				out = append(out, fd)
			}
		}
	}
	return out
}

// commentAbove reports whether the line above has a standalone comment of
// two words or more that isn't a directive itself.
func (s *source) commentAbove(line int) bool {
	for _, c := range s.comments {
		if c.end == line-1 && c.standalone && !c.directive && len(likelihood.Words(c.text())) >= 2 {
			return true
		}
	}
	return false
}

var deferredImportRe = regexp.MustCompile(`(?i)circular|cycl|lazy|lazily|defer|optional|heavy|slow|startup|import time|avoid`)

func checkImportInFunction(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		if !s.isKeyword(t, "import", "from") || !s.firstOnLine(i) && (i == 0 || s.text(s.code[i-1]) != ";") {
			continue
		}
		fn := s.scopeOf(t.Span.Start)
		if fn == s.f.Module {
			continue
		}
		line := s.line(t.Span.Start)
		if s.importGuarded(fn, line) || s.commentMatches(line, deferredImportRe) {
			continue
		}
		toks := s.lineCodeTokens(line)
		out = append(out, finding{span: code.Span{Start: t.Span.Start, End: toks[len(toks)-1].Span.End}})
	}
	return out
}

// commentMatches reports whether a comment on line or right above it
// matches re.
func (s *source) commentMatches(line int, re *regexp.Regexp) bool {
	for _, c := range s.comments {
		if (c.end == line || c.end == line-1) && re.MatchString(c.text()) {
			return true
		}
	}
	return false
}

// importGuarded reports whether the import on line is in a try block of fn
// with a handler for ImportError, which is how you'd import an optional
// dependency.
func (s *source) importGuarded(fn *code.Decl, line int) bool {
	indent := s.indent(line)
	first := s.line(fn.Body.Start)
	tryLine := 0
	for l := line - 1; l >= first; l-- {
		if !s.codeLines[l] || s.indent(l) >= indent {
			continue
		}
		toks := s.lineCodeTokens(l)
		if len(toks) > 0 && s.isKeyword(toks[0], "try") {
			tryLine = l
		}
		break
	}
	if tryLine == 0 {
		return false
	}
	tryIndent := s.indent(tryLine)
	last := s.line(fn.Body.End)
	for l := line + 1; l <= last; l++ {
		if !s.codeLines[l] || s.indent(l) > tryIndent {
			continue
		}
		if s.indent(l) < tryIndent {
			return false
		}
		toks := s.lineCodeTokens(l)
		if len(toks) == 0 || !s.isKeyword(toks[0], "except") {
			return false
		}
		for _, x := range toks {
			if n := s.text(x); n == "ImportError" || n == "ModuleNotFoundError" {
				return true
			}
		}
	}
	return false
}

const maxFunctionLines = 80

// checkLongFunction skips test files, because table-driven tests have to
// keep their cases in the function.
func checkLongFunction(s *source) []finding {
	if s.test {
		return nil
	}
	var out []finding
	for _, d := range s.outermostCallables() {
		n := 0
		first, last := s.bodyLines(d)
		for l := first; l <= last; l++ {
			if s.codeLines[l] {
				n++
			}
		}
		if n > maxFunctionLines {
			out = append(out, finding{span: d.Span, detail: fmt.Sprintf("%d lines of code", n)})
		}
	}
	return out
}

// minBlankedLines is the shortest function body overblanking judges. Short
// bodies split into two or three groups have a high ratio of blank lines
// without being overblanked.
const minBlankedLines = 8

// checkOverblanking flags function bodies with a blank line for every two
// lines of code or more. People leave blank lines between groups of
// statements. Agents tend to leave one after nearly every statement.
func checkOverblanking(s *source) []finding {
	var out []finding
	for _, d := range s.outermostCallables() {
		first, last := s.bodyLines(d)
		n, blank := 0, 0
		for l := first; l <= last; l++ {
			switch {
			case s.codeLines[l]:
				n++
			case !s.commentLines[l] && strings.TrimSpace(s.f.Text(s.f.LineSpan(l))) == "":
				blank++
			}
		}
		if n >= minBlankedLines && 2*blank >= n {
			out = append(out, finding{span: d.Span, detail: fmt.Sprintf("%d blank lines between %d lines of code", blank, n)})
		}
	}
	return out
}

// bodyLines returns the first and last line of d's body. A Python body
// starts right after the header, so comments above the first statement
// count too.
func (s *source) bodyLines(d *code.Decl) (int, int) {
	first := s.line(d.Body.Start)
	if s.f.Lang == code.Python {
		if i := s.codeIndex(d.Body.Start); i > 0 {
			first = s.line(s.code[i-1].Span.End) + 1
		}
	}
	return first, s.line(d.Body.End)
}

func checkExcessiveComments(s *source) []finding {
	var out []finding
	for _, d := range s.outermostCallables() {
		first, last := s.bodyLines(d)
		docLines := map[int]bool{}
		for _, c := range s.commentsIn(d.Body) {
			if c.docstring {
				for l := c.line; l <= c.end; l++ {
					docLines[l] = true
				}
			}
		}
		comments, nonBlank := 0, 0
		for l := first; l <= last; l++ {
			switch {
			case docLines[l]:
			case s.codeLines[l]:
				nonBlank++
			case s.commentLines[l]:
				nonBlank++
				comments++
			}
		}
		if comments >= 6 && comments*10 > nonBlank*4 {
			out = append(out, finding{span: d.Span, detail: fmt.Sprintf("%d of %d non-blank lines are comments", comments, nonBlank)})
		}
	}
	return out
}

const maxNesting = 4

var (
	parenControl = setOf("if", "elseif", "for", "while", "switch", "catch", "foreach", "using", "lock", "synchronized", "fixed", "try")
	bareControl  = setOf("else", "try", "finally", "do")
	goControl    = setOf("if", "for", "switch", "select", "else")
	pyControl    = setOf("if", "elif", "else", "for", "while", "try", "except", "finally", "with", "match", "async")
)

func checkDeepNesting(s *source) []finding {
	var out []finding
	for _, d := range s.outermostCallables() {
		var fd *finding
		depth := 0
		if s.f.Lang == code.Python {
			fd, depth = s.nestingPython(d)
		} else {
			fd, depth = s.nestingBraces(d)
		}
		if fd != nil {
			fd.detail = fmt.Sprintf("depth %d", depth)
			out = append(out, *fd)
		}
	}
	return out
}

// nestingBraces returns the first control block in d that's nested deeper
// than maxNesting, plus the deepest level it reached.
func (s *source) nestingBraces(d *code.Decl) (*finding, int) {
	toks := s.bodyTokens(d)
	var stack []bool
	var fd *finding
	depth, maxDepth := 0, 0
	for i, t := range toks {
		switch s.text(t) {
		case "{":
			start, ok := s.controlHeader(toks, i)
			stack = append(stack, ok)
			if ok {
				depth++
				maxDepth = max(maxDepth, depth)
				if depth > maxNesting && fd == nil {
					fd = &finding{span: code.Span{Start: toks[start].Span.Start, End: t.Span.End}}
				}
			}
		case "}":
			if n := len(stack); n > 0 {
				if stack[n-1] {
					depth--
				}
				stack = stack[:n-1]
			}
		}
	}
	return fd, maxDepth
}

// controlHeader reports whether the brace at toks[i] opens the block of a
// control statement. If so, it also returns the index of the keyword.
func (s *source) controlHeader(toks []code.Token, i int) (int, bool) {
	if i == 0 {
		return 0, false
	}
	prev := toks[i-1]
	if prev.Kind == code.TokKeyword && bareControl[s.text(prev)] {
		return i - 1, true
	}
	if s.f.Lang == code.Go {
		// gofmt puts the brace that opens a block last on its line. A
		// composite literal brace can have its elements after it instead.
		if i+1 < len(toks) && s.line(toks[i+1].Span.Start) == s.line(toks[i].Span.Start) {
			return 0, false
		}
		depth := 0
		for j := i - 1; j >= 0; j-- {
			switch s.text(toks[j]) {
			case ")", "]", "}":
				depth++
			case "(", "[", "{":
				depth--
			}
			if depth < 0 {
				return 0, false
			}
			if depth == 0 && toks[j].Kind == code.TokKeyword && goControl[s.text(toks[j])] {
				return j, true
			}
			if depth == 0 && (j == 0 || s.line(toks[j-1].Span.End) < s.line(toks[j].Span.Start)) {
				return 0, false
			}
		}
		return 0, false
	}
	if s.f.Lang == code.Rust {
		return s.rustControlHeader(toks, i)
	}
	if s.text(prev) != ")" {
		return 0, false
	}
	k := s.matchOpen(toks, i-1)
	if k <= 0 {
		return 0, false
	}
	kw := k - 1
	if s.text(toks[kw]) == "await" && kw > 0 {
		kw--
	}
	if toks[kw].Kind == code.TokKeyword && parenControl[s.text(toks[kw])] {
		return kw, true
	}
	return 0, false
}

// nestingPython measures nesting by how far block headers are indented.
func (s *source) nestingPython(d *code.Decl) (*finding, int) {
	type block struct {
		indent  int
		control bool
	}
	var stack []block
	var fd *finding
	depth, maxDepth := 0, 0
	for _, ll := range s.splitLogicalLines(s.tokensIn(d.Body)) {
		first := ll[0]
		indent := s.indent(s.line(first.Span.Start))
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			if stack[len(stack)-1].control {
				depth--
			}
			stack = stack[:len(stack)-1]
		}
		if s.text(ll[len(ll)-1]) != ":" && !s.inlineSuite(ll) {
			continue
		}
		control := first.Kind == code.TokKeyword && pyControl[s.text(first)] || s.text(first) == "match"
		stack = append(stack, block{indent, control})
		if control {
			depth++
			maxDepth = max(maxDepth, depth)
			if depth > maxNesting && fd == nil {
				fd = &finding{span: spanOf(ll)}
			}
		}
	}
	return fd, maxDepth
}

// inlineSuite reports whether a logical line is a block header with the
// body on the same line ("if x: return").
func (s *source) inlineSuite(ll []code.Token) bool {
	if ll[0].Kind != code.TokKeyword {
		return false
	}
	depth := 0
	for _, t := range ll {
		switch s.text(t) {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ":":
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

var (
	notImplementedRe = regexp.MustCompile(`(?i)\bnot (?:yet )?implemented\b|^\W*(?:todo|fixme|implement me|unimplemented|stub)\b`)
	stubCommentRe    = regexp.MustCompile(`(?i)^(?:(?:todo|fixme)\b:?\s*)?(?:implement(?:ation)?(?:\s+(?:me|this|it|here|later))?|placeholder|stub|not (?:yet )?implemented(?: yet)?|to be implemented|implement me)[.!]?$`)
)

// checkPlaceholderCode flags functions outside abstract types whose whole
// body is a "not implemented" error, and empty bodies with only a TODO
// comment in them.
func checkPlaceholderCode(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() {
			continue
		}
		body := s.bodyTokens(d)
		if d.Body.Empty() {
			body = s.expressionBody(d)
			if body == nil {
				continue
			}
		}
		if len(body) == 0 || s.f.Lang == code.Python && (len(body) == 1 && s.isKeyword(body[0], "pass") || s.isEllipsis(body)) {
			sp := d.Body
			if s.f.Lang == code.Python {
				first, last := s.bodyLines(d)
				sp = code.Span{Start: s.f.LineSpan(first).Start, End: s.f.LineSpan(last).End}
			}
			for _, c := range s.commentsIn(sp) {
				if (todoRe.MatchString(c.text()) || stubCommentRe.MatchString(c.text())) && !c.docstring {
					out = append(out, finding{span: c.Span})
				}
			}
			continue
		}
		stmt := body
		if s.text(stmt[len(stmt)-1]) == ";" {
			stmt = stmt[:len(stmt)-1]
		}
		if s.notImplemented(stmt) && !s.abstractContext(d) {
			out = append(out, finding{span: spanOf(stmt)})
		}
	}
	return out
}

// expressionBody returns the tokens after "=>" of a C# expression-bodied
// member or a braceless arrow function, or nil if there's none.
func (s *source) expressionBody(d *code.Decl) []code.Token {
	toks := s.tokensIn(code.Span{Start: d.NameSpan.End, End: d.Span.End})
	for i, t := range toks {
		if s.text(t) == "=>" && i+1 < len(toks) {
			return toks[i+1:]
		}
	}
	return nil
}

// notImplemented reports whether stmt is a single statement that raises a
// "not implemented" error.
func (s *source) notImplemented(stmt []code.Token) bool {
	if len(stmt) < 2 {
		return false
	}
	head := s.text(stmt[0])
	switch {
	case head == "raise" && s.f.Lang == code.Python:
		if s.text(stmt[1]) == "NotImplementedError" {
			return len(stmt) == 2 || s.text(stmt[2]) == "(" && s.matchClose(stmt, 2) == len(stmt)-1
		}
	case head == "throw":
		for _, t := range stmt[1:] {
			if n := s.text(t); n == "NotImplementedException" || n == "NotImplementedError" {
				return true
			}
		}
	case head == "panic" && (s.f.Lang == code.Go || s.f.Lang == code.Rust && s.text(stmt[1]) == "!"):
	case (head == "todo" || head == "unimplemented") && s.f.Lang == code.Rust:
		return len(stmt) >= 4 && s.text(stmt[1]) == "!" && s.matchClose(stmt, 2) == len(stmt)-1
	default:
		return false
	}
	open := slices.IndexFunc(stmt, func(t code.Token) bool { return s.text(t) == "(" })
	if open < 0 || s.matchClose(stmt, open) != len(stmt)-1 {
		return false
	}
	for _, t := range stmt[open:] {
		if t.Kind == code.TokString && notImplementedRe.MatchString(stringContent(s.text(t))) {
			return true
		}
	}
	return false
}

var abstractNameRe = regexp.MustCompile(`^(?:Abstract|Base)[A-Z_]|(?:Base|Mixin|Interface|Protocol|ABC)$`)

// abstractContext reports whether d is meant to be overridden. That's the
// case if it's decorated abstract, declared in an interface, an abstract or
// protocol class, or in a class that other linted classes extend.
func (s *source) abstractContext(d *code.Decl) bool {
	for _, t := range s.tokensIn(code.Span{Start: d.Span.Start, End: d.NameSpan.Start}) {
		switch s.text(t) {
		case "abstractmethod", "abstractproperty", "abstract", "overload", "virtual":
			return true
		}
	}
	p := d.Parent
	if p == nil || !p.IsType() {
		return false
	}
	if p.Kind == code.KindInterface || abstractNameRe.MatchString(p.Name) || s.extended[p.Name] {
		return true
	}
	for _, b := range p.Bases {
		if abstractNameRe.MatchString(b) || strings.HasSuffix(b, "ABC") || strings.Contains(b, "Protocol") || strings.Contains(b, "ABCMeta") {
			return true
		}
	}
	for _, t := range s.tokensIn(code.Span{Start: p.Span.Start, End: p.NameSpan.Start}) {
		if s.text(t) == "abstract" {
			return true
		}
	}
	return false
}
