package codelint

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

var closerOf = map[string]string{"(": ")", "[": "]", "{": "}"}

// matchClose returns the index of the bracket that closes the one at
// toks[i], or len(toks) if it's never closed.
func (s *source) matchClose(toks []code.Token, i int) int {
	open := s.text(toks[i])
	cl := closerOf[open]
	depth := 0
	for j := i; j < len(toks); j++ {
		switch s.text(toks[j]) {
		case open:
			depth++
		case cl:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(toks)
}

// matchOpen returns the index of the bracket that opens the one at toks[i],
// or -1 if there isn't one.
func (s *source) matchOpen(toks []code.Token, i int) int {
	cl := s.text(toks[i])
	var open string
	for o, c := range closerOf {
		if c == cl {
			open = o
		}
	}
	depth := 0
	for j := i; j >= 0; j-- {
		switch s.text(toks[j]) {
		case cl:
			depth++
		case open:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

func (s *source) firstOnLine(i int) bool {
	return i == 0 || s.line(s.code[i-1].Span.End) < s.line(s.code[i].Span.Start)
}

func (s *source) isKeyword(t code.Token, kws ...string) bool {
	if t.Kind != code.TokKeyword && t.Kind != code.TokIdent {
		return false
	}
	text := s.text(t)
	return slices.Contains(kws, text)
}

// stringContent returns the text of a string literal with the prefixes and
// quotes stripped. We also drop the \n, \r and \t escapes.
func stringContent(lit string) string {
	lit = strings.TrimLeft(lit, "rRuUbBfFL8@$")
	for _, q := range []string{`"""`, `'''`, `"`, `'`, "`"} {
		if strings.HasPrefix(lit, q) && strings.HasSuffix(lit, q) && len(lit) >= 2*len(q) {
			lit = lit[len(q) : len(lit)-len(q)]
			break
		}
	}
	return strings.NewReplacer(`\n`, "", `\r`, "", `\t`, "").Replace(lit)
}

// splitStatements splits toks at semicolons that aren't inside brackets.
func (s *source) splitStatements(toks []code.Token) [][]code.Token { return s.split(toks, false) }

// splitLogicalLines splits Python tokens into logical lines. A new line
// starts after a semicolon, or at the first token of a physical line if
// we're not inside brackets.
func (s *source) splitLogicalLines(toks []code.Token) [][]code.Token { return s.split(toks, true) }

func (s *source) split(toks []code.Token, atLines bool) [][]code.Token {
	var out [][]code.Token
	depth, start := 0, 0
	for i, t := range toks {
		if atLines && i > start && depth == 0 && s.line(toks[i-1].Span.End) < s.line(t.Span.Start) {
			out = append(out, toks[start:i])
			start = i
		}
		switch s.text(t) {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth = max(depth-1, 0)
		case ";":
			if depth == 0 {
				if i > start {
					out = append(out, toks[start:i])
				}
				start = i + 1
			}
		}
	}
	if start < len(toks) {
		out = append(out, toks[start:])
	}
	return out
}

func spanOf(toks []code.Token) code.Span {
	return code.Span{Start: toks[0].Span.Start, End: toks[len(toks)-1].Span.End}
}

// outermostCallables returns the functions with a body that aren't nested
// in another function.
func (s *source) outermostCallables() []*code.Decl {
	var out []*code.Decl
	for _, d := range s.f.Decls {
		if !d.Callable() || d.Body.Empty() {
			continue
		}
		nested := false
		for p := d.Parent; p != nil; p = p.Parent {
			if p.Callable() {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, d)
		}
	}
	return out
}

// bodyTokens returns the code tokens of d's body. In brace languages the
// braces themselves are left out.
func (s *source) bodyTokens(d *code.Decl) []code.Token {
	toks := s.tokensIn(d.Body)
	if len(toks) >= 2 && s.text(toks[0]) == "{" && s.text(toks[len(toks)-1]) == "}" {
		toks = toks[1 : len(toks)-1]
	}
	return toks
}

func (s *source) commentsIn(sp code.Span) []*comment {
	var out []*comment
	for _, c := range s.comments {
		if sp.Encloses(c.Span) {
			out = append(out, c)
		}
	}
	return out
}
