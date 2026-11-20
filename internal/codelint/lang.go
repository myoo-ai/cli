package codelint

import (
	"iter"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// Language rules (lang_*.go) match token patterns of a single language
// family. We adapted them from SonarQube, staticcheck, pylint, Clippy and
// similar linters. Their IDs start with the family, which is one of ts-
// (TypeScript and JavaScript), py-, go-, java-, cs-, c- (C and C++), cpp-
// (C++ only), php- and rust-. A finding's detail shows the code and its
// replacement as "`old` -> `new`".

var (
	langsTS = []code.Lang{code.JavaScript, code.TypeScript}
	langsC  = []code.Lang{code.C, code.CPP}
)

// tok returns the text of code token i, or "" outside s.code.
func (s *source) tok(i int) string {
	if i < 0 || i >= len(s.code) {
		return ""
	}
	return s.text(s.code[i])
}

// seq reports whether the code tokens starting at i match texts.
func (s *source) seq(i int, texts ...string) bool {
	for k, t := range texts {
		if s.tok(i+k) != t {
			return false
		}
	}
	return true
}

// sameTokens reports whether the n code tokens from i and from j read the
// same.
func (s *source) sameTokens(i, j, n int) bool {
	for k := range n {
		if s.tok(i+k) != s.tok(j+k) {
			return false
		}
	}
	return true
}

func (s *source) tokSpan(i, j int) code.Span {
	return code.Span{Start: s.code[i].Span.Start, End: s.code[j-1].Span.End}
}

// codeText returns the source of the code tokens [i, j) on a single line,
// with line breaks and indentation collapsed to single spaces.
func (s *source) codeText(i, j int) string {
	return strings.Join(strings.Fields(s.f.Text(s.tokSpan(i, j))), " ")
}

// chainEnd returns the index right after the member chain that starts at
// code token i. A chain is an identifier, this, self or super, followed by
// member accesses, calls and index expressions (a.b?.c(d)[e], p->q, ns::f).
// If no chain starts there, it returns i.
func (s *source) chainEnd(i int) int {
	if i >= len(s.code) {
		return i
	}
	if t := s.code[i]; t.Kind != code.TokIdent && !s.isKeyword(t, "this", "self", "super") {
		return i
	}
	j := i + 1
	for j < len(s.code) {
		switch s.tok(j) {
		case ".", "?.", "->", "::":
			if j+1 < len(s.code) && (s.code[j+1].Kind == code.TokIdent || s.code[j+1].Kind == code.TokKeyword) {
				j += 2
				continue
			}
			if s.tok(j) == "?." && (s.tok(j+1) == "(" || s.tok(j+1) == "[") {
				j++
				continue
			}
			return j
		case "(", "[":
			k := s.matchClose(s.code, j)
			if k >= len(s.code) {
				return j
			}
			j = k + 1
		default:
			return j
		}
	}
	return j
}

// chainStart is the mirror of chainEnd. It returns the index of the first
// token of the member chain that ends before code token end, like the `a` of
// `a.b(c)[d]`, or end if no chain ends there.
func (s *source) chainStart(end int) int {
	j := end - 1
	for j >= 0 {
		if t := s.tok(j); t == ")" || t == "]" {
			k := s.matchOpen(s.code, j)
			if k <= 0 {
				return end
			}
			j = k - 1
			continue
		}
		sep := s.tok(j - 1)
		member := sep == "." || sep == "?." || sep == "->" || sep == "::"
		if t := s.code[j]; t.Kind != code.TokIdent && !(member && t.Kind == code.TokKeyword) && !s.isKeyword(t, "this", "self", "super", "base") {
			return end
		}
		if !member {
			return j
		}
		j -= 2
	}
	return end
}

// enclosingBraces yields the indices of the unmatched `{` code tokens before
// code token i, innermost first. Those are the blocks that hold i.
func (s *source) enclosingBraces(i int) iter.Seq[int] {
	return func(yield func(int) bool) {
		depth := 0
		for j := i - 1; j >= 0; j-- {
			switch s.tok(j) {
			case "}":
				depth++
			case "{":
				if depth > 0 {
					depth--
				} else if !yield(j) {
					return
				}
			}
		}
	}
}

// hasTopLevel reports whether the code tokens [from, to) hold op outside
// any brackets.
func (s *source) hasTopLevel(from, to int, op string) bool {
	depth := 0
	for k := from; k < to; k++ {
		switch s.tok(k) {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case op:
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

var loopKeywords = setOf("for", "foreach", "while", "do")

// loopKeyword returns the index of the keyword of the innermost for,
// foreach, while or do loop of a brace language whose body holds code token
// i. It returns -1 if there's no such loop, or if a function or lambda body
// sits in between, since a deferred call or a string append inside a
// callback runs once per call, not once per iteration.
func (s *source) loopKeyword(i int) int {
	for j := range s.enclosingBraces(i) {
		if s.funcBodies()[s.code[j].Span.Start] || s.lambdaBody(j) {
			return -1
		}
		if kw, ok := s.controlHeader(s.code, j); ok && loopKeywords[s.tok(kw)] {
			return kw
		}
	}
	return -1
}

// funcBodies returns the start offsets of the bodies of the functions,
// methods and constructors of the file.
func (s *source) funcBodies() map[int]bool {
	if s.bodyStarts == nil {
		s.bodyStarts = map[int]bool{}
		for _, d := range s.f.Decls {
			if d.Callable() && !d.Body.Empty() {
				s.bodyStarts[d.Body.Start] = true
			}
		}
	}
	return s.bodyStarts
}

// visibleDecl returns the variable, parameter, field or property called name
// that's visible at off. That's the latest declaration before off in the
// innermost scope that declares name.
func (s *source) visibleDecl(name string, off int) *code.Decl {
	for p := s.f.DeclAt(off); p != nil; p = p.Parent {
		var found *code.Decl
		for _, c := range p.Children {
			if c.Name != name || (c.Local && c.Span.Start > off) {
				continue
			}
			switch c.Kind {
			case code.KindVariable, code.KindParameter, code.KindField, code.KindProperty:
				found = c
			}
		}
		if found != nil {
			return found
		}
	}
	return nil
}

var stmtStarts = setOf(";", "{", "}", ")", "else", ":")

// stringConcatInLoop flags `x += ...` and `x = x + ...` statements in a loop
// body if x has one of the string types and is declared before the loop,
// since each pass copies the whole string built so far. The detail suggests
// appending to a builder with the method appendCall.
func (s *source) stringConcatInLoop(types map[string]bool, appendCall string) []finding {
	var out []finding
	for i := range s.code {
		if s.code[i].Kind != code.TokIdent || !stmtStarts[s.tok(i-1)] {
			continue
		}
		name := s.tok(i)
		var rest int
		switch {
		case s.tok(i+1) == "+=":
			rest = i + 2
		case s.seq(i+1, "=", name, "+"):
			rest = i + 4
		default:
			continue
		}
		loop := s.loopKeyword(i)
		if loop < 0 {
			continue
		}
		d := s.visibleDecl(name, s.code[i].Span.Start)
		if d == nil || !types[d.Type] || d.Local && d.Span.Start > s.code[loop].Span.Start {
			continue
		}
		end := rest
		for depth := 0; end < len(s.code); end++ {
			t := s.tok(end)
			if t == "(" || t == "[" || t == "{" {
				depth++
			} else if t == ")" || t == "]" || t == "}" {
				depth--
			}
			if depth < 0 || (depth == 0 && t == ";") {
				break
			}
		}
		if end == rest {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, end),
			detail: "`" + s.codeText(i, end) + "` -> `" + name + "Builder." + appendCall + "(" + s.codeText(rest, end) + ")`",
		})
	}
	return out
}

// lambdaBody reports whether the brace at code token j opens the body of an
// arrow function, a Java lambda or a Go function literal.
func (s *source) lambdaBody(j int) bool {
	if p := s.tok(j - 1); p == "=>" || p == "->" {
		return true
	}
	if s.f.Lang != code.Go {
		return false
	}
	line := s.line(s.code[j].Span.Start)
	for k := j - 1; k >= 0 && s.line(s.code[k].Span.Start) == line; k-- {
		if s.isKeyword(s.code[k], "func") {
			return true
		}
	}
	return false
}
