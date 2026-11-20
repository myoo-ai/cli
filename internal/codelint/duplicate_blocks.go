package codelint

import (
	"bytes"

	"github.com/myoo-ai/cli/internal/code"
)

const (
	dupStatementStart byte = 1 << iota
	dupStatementEnd
	dupEligible
)

type duplicateContext struct {
	parent, end int32
	kind        byte
}

func (nf *normalized) prepareBlocks() {
	n := len(nf.toks)
	nf.contexts = make([]int32, n+1)
	nf.edges = make([]byte, n+1)
	nf.blocks = []duplicateContext{{parent: -1, end: int32(n)}}
	var current int32
	for i, t := range nf.toks {
		nf.contexts[i] = current
		nf.edges[i] = nf.blockBoundary(i)
		text := nf.src.text(t)
		switch text {
		case "(", "[", "{":
			nf.blocks = append(nf.blocks, duplicateContext{parent: current, end: int32(n), kind: text[0]})
			current = int32(len(nf.blocks) - 1)
		case ")", "]", "}":
			opening := byte('(')
			if text == "]" {
				opening = '['
			} else if text == "}" {
				opening = '{'
			}
			nf.blocks[current].end = int32(i)
			if nf.blocks[current].kind == opening {
				current = nf.blocks[current].parent
			} else {
				// A region can start inside a declaration if we cut off its
				// opening.
				nf.blocks = append(nf.blocks, duplicateContext{parent: -1, end: int32(n)})
				current = int32(len(nf.blocks) - 1)
			}
		}
	}
	nf.contexts[n] = current
	nf.edges[n] = nf.blockBoundary(n)
}

func (nf *normalized) blockBoundary(at int) byte {
	if at == 0 {
		return dupStatementStart
	}
	previous := nf.toks[at-1]
	before := nf.src.text(previous)
	if at == len(nf.toks) {
		if nf.src.duplicateCanEnd(previous) {
			return dupStatementEnd
		}
		return 0
	}
	next := nf.toks[at]
	after := nf.src.text(next)
	newline := bytes.IndexByte(nf.src.f.Src[previous.Span.End:next.Span.Start], '\n') >= 0
	lineEnd := newline && nf.src.duplicateCanEnd(previous) && !duplicateContinues(after)
	var edge byte
	if before == "{" || before == ";" || before == "\n" || lineEnd || newline && before == ":" && nf.src.f.Lang == code.Python {
		edge |= dupStatementStart
	}
	if before == ";" || before == "\n" || lineEnd || after == "}" && nf.src.duplicateCanEnd(previous) {
		edge |= dupStatementEnd
	}
	return edge
}

func (s *source) duplicateCanEnd(t code.Token) bool {
	if t.Kind == code.TokIdent || t.Kind == code.TokString || t.Kind == code.TokNumber {
		return true
	}
	switch s.text(t) {
	case ")", "]", "}", ";", "\n", "++", "--", "return", "break", "continue", "pass", "raise", "fallthrough", "nil", "null", "true", "false", "None", "True", "False", "this", "self", "super":
		return true
	}
	return false
}

func duplicateContinues(text string) bool {
	switch text {
	case "(", "[", "{", ".", "?.", ",", ":", "?", "=", ":=", "+=", "-=", "*=", "/=", "+", "-", "*", "/", "%", "==", "!=", "<", ">", "<=", ">=", "&", "|", "^", "&&", "||", "??", "<<", ">>", "=>", "else", "elif", "catch", "except", "finally":
		return true
	}
	return false
}

func (nf *normalized) blockStart(at int) bool {
	if nf.src.text(nf.toks[at]) == "{" {
		return true
	}
	kind := nf.blocks[nf.contexts[at]].kind
	return nf.edges[at]&dupStatementStart != 0 && (kind == 0 || kind == '{')
}

func (nf *normalized) blockLimit(at int) int {
	if nf.src.text(nf.toks[at]) == "{" {
		return min(len(nf.toks), int(nf.blocks[nf.contexts[at+1]].end)+1)
	}
	return int(nf.blocks[nf.contexts[at]].end)
}

// completeDuplicate trims partial expressions and doesn't let the match
// leave the scope it starts in. We don't count a single registration call as
// an algorithm.
func completeDuplicate(a, b *normalized, at, bt, n int) (int, int, bool) {
	start := 0
	if a.src.text(a.toks[at]) == "{" {
		start++
	}
	for start < n && (!a.blockStart(at+start) || !b.blockStart(bt+start)) {
		start++
	}
	if start == n {
		return 0, 0, false
	}
	ac, bc := a.contexts[at+start], b.contexts[bt+start]
	end := n
	for end > start && (a.edges[at+end]&dupStatementEnd == 0 || b.edges[bt+end]&dupStatementEnd == 0 || a.contexts[at+end] != ac || b.contexts[bt+end] != bc) {
		end--
	}
	if end <= start || !a.executableBlock(at+start, at+end) || !b.executableBlock(bt+start, bt+end) {
		return 0, 0, false
	}
	if start == 1 && end == n-1 && a.src.text(a.toks[at]) == "{" && a.src.text(a.toks[at+n-1]) == "}" && a.contexts[at] == a.contexts[at+n] && b.contexts[bt] == b.contexts[bt+n] {
		return 0, n, true
	}
	return start, end - start, end-start >= dupMinTokens
}

func (nf *normalized) executableBlock(start, end int) bool {
	context := nf.contexts[start]
	statements := 0
	control := false
	for i := start; i < end; i++ {
		if nf.contexts[i] == context && nf.src.isKeyword(nf.toks[i], "if", "for", "while", "do", "switch", "select", "try", "with", "match") {
			control = true
		}
		if nf.contexts[i+1] == context && nf.edges[i+1]&dupStatementEnd != 0 {
			statements++
		}
	}
	return statements >= 2 || control && statements >= 1
}
