package codelint

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/command"
)

const (
	dupMinTokens   = 60
	dupMinLines    = 8
	dupMinDistinct = 10
	// dupMinVariety is the share of distinct 4-grams a match needs. Table
	// rows repeat the same few, so they don't get there.
	dupMinVariety = 0.5
	// dupMaxCandidates caps how many earlier windows with the same hash we
	// verify, so highly repetitive input stays linear.
	dupMaxCandidates = 32
)

// normalized keeps the names of nonlocal calls but normalizes other
// identifiers and literals. We leave import statements out.
type normalized struct {
	src      *source
	ids      []int32
	toks     []code.Token
	contexts []int32
	blocks   []duplicateContext
	edges    []byte
}

type dupPos struct{ file, at int32 }

// checkDuplicates finds maximal runs of at least dupMinTokens normalized
// tokens over dupMinLines lines that occur more than once. It reports all
// occurrences after the first. We skip test files, since golden file updates
// and table loops in tests repeat by design.
func checkDuplicates(srcs []*source) []finding {
	vocab := map[string]int32{}
	var files []normalized
	for _, s := range srcs {
		if !s.test {
			files = append(files, s.normalize(vocab).declarationRegions()...)
		}
	}
	const base = 1000003
	var pow uint64 = 1
	for range dupMinTokens - 1 {
		pow *= base
	}
	hashes := make([][]uint64, len(files))
	index := map[uint64][]dupPos{}
	for fi := range files {
		nf := &files[fi]
		if len(nf.ids) < dupMinTokens {
			continue
		}
		nf.prepareBlocks()
		hs := make([]uint64, len(nf.ids)-dupMinTokens+1)
		var h uint64
		for i, id := range nf.ids {
			if i >= dupMinTokens {
				h -= uint64(nf.ids[i-dupMinTokens]+1) * pow
			}
			h = h*base + uint64(id+1)
			if i >= dupMinTokens-1 {
				at := i - dupMinTokens + 1
				hs[at] = h
				if nf.blockStart(at) && nf.blockLimit(at)-at >= dupMinTokens {
					nf.edges[at] |= dupEligible
					index[h] = append(index[h], dupPos{int32(fi), int32(at)})
				}
			}
		}
		hashes[fi] = hs
	}
	var out []finding
	for fi := range files {
		nf := &files[fi]
		for i := 0; i < len(hashes[fi]); {
			if nf.edges[i]&dupEligible == 0 {
				i++
				continue
			}
			p, n, shift, ok := findEarlier(files, index[hashes[fi][i]], fi, i)
			if !ok {
				i++
				continue
			}
			i += shift
			q := nf.toks[i : i+n]
			first := files[p.file].toks[p.at : int(p.at)+n]
			if acceptableDuplicate(nf, i, n, q) {
				fs := files[p.file].src
				at := fs.f.Pos(first[0].Span.Start)
				out = append(out, finding{
					src:  nf.src,
					span: spanOf(q),
					detail: fmt.Sprintf("first occurrence at %s, lines %d-%d", command.Point(fs.name, at.Line, at.Col),
						at.Line, fs.line(first[len(first)-1].Span.End)),
				})
			}
			i += n
		}
	}
	return out
}

// findEarlier returns the earliest occurrence before (fi, at) that has the
// same window as (fi, at), with the size and start shift of the match after
// completeDuplicate trims it. The returned position already includes the shift.
func findEarlier(files []normalized, cands []dupPos, fi, at int) (dupPos, int, int, bool) {
	cur := files[fi].ids
	limit := files[fi].blockLimit(at)
	checked := 0
	for _, p := range cands {
		if int(p.file) > fi || int(p.file) == fi && int(p.at)+dupMinTokens > at {
			break
		}
		if checked++; checked > dupMaxCandidates {
			break
		}
		prev := files[p.file].ids
		previousLimit := files[p.file].blockLimit(int(p.at))
		if !slices.Equal(prev[p.at:int(p.at)+dupMinTokens], cur[at:at+dupMinTokens]) {
			continue
		}
		n := dupMinTokens
		for at+n < limit && int(p.at)+n < previousLimit && prev[int(p.at)+n] == cur[at+n] && (int(p.file) != fi || int(p.at)+n < at) {
			n++
		}
		shift, size, ok := completeDuplicate(&files[fi], &files[p.file], at, int(p.at), n)
		if !ok {
			continue
		}
		p.at += int32(shift)
		return p, size, shift, true
	}
	return dupPos{}, 0, 0, false
}

// acceptableDuplicate rejects matches that span too few lines. It also
// rejects repetitive data like table rows or word lists, where the
// normalized sequence is a few 4-grams repeated over and over.
func acceptableDuplicate(nf *normalized, at, n int, q []code.Token) bool {
	s := nf.src
	if s.line(q[len(q)-1].Span.End)-s.line(q[0].Span.Start)+1 < dupMinLines {
		return false
	}
	seq := nf.ids[at : at+n]
	distinct := map[int32]bool{}
	grams := map[[4]int32]bool{}
	for i, id := range seq {
		distinct[id] = true
		if i >= 3 {
			grams[[4]int32{seq[i-3], seq[i-2], seq[i-1], id}] = true
		}
	}
	return len(distinct) >= dupMinDistinct && float64(len(grams)) >= dupMinVariety*float64(n-3)
}

// normalize maps s's code tokens to token classes, skipping imports.
func (s *source) normalize(vocab map[string]int32) normalized {
	nf := normalized{src: s}
	skip := s.importTokens()
	refAt := 0
	for i, t := range s.code {
		if skip[i] {
			continue
		}
		var call *code.Ref
		for refAt < len(s.f.Refs) && s.f.Refs[refAt].Span.Start <= t.Span.Start {
			ref := s.f.Refs[refAt]
			if ref.Span.Start == t.Span.Start && ref.Call && !s.localCall(ref, i) {
				call = ref
			}
			refAt++
		}
		var key string
		switch t.Kind {
		case code.TokIdent:
			key = "ID"
			if call != nil {
				key = "CALL:" + s.text(t)
			}
		case code.TokString, code.TokNumber:
			key = "LIT"
		default:
			key = s.text(t)
		}
		id, ok := vocab[key]
		if !ok {
			id = int32(len(vocab))
			vocab[key] = id
		}
		nf.ids = append(nf.ids, id)
		nf.toks = append(nf.toks, t)
	}
	return nf
}

func (s *source) localCall(ref *code.Ref, at int) bool {
	if ref.Target != nil {
		return ref.Target.Local
	}
	if at > 0 {
		switch s.text(s.code[at-1]) {
		case ".", "?.", "::", "->":
			return false
		}
	}
	for scope := ref.Scope; scope != nil; scope = scope.Parent {
		for _, d := range scope.Children {
			if d.Name == ref.Name && (d.Callable() || d.NameSpan.Start <= ref.Span.Start) {
				return d.Local
			}
		}
	}
	return false
}

// declarationRegions separates callable bodies from signatures, type-only
// declarations and member fields. Field initializers declare state, so runs
// of them share a shape, not an algorithm you could extract. Nested closures
// stay part of the enclosing algorithm.
func (nf normalized) declarationRegions() []normalized {
	var cuts []code.Span
	covered := -1
	for _, d := range nf.src.f.Decls {
		if d.Span.Empty() {
			continue
		}
		if d.IsType() && !typeHasCallable(d) || d.Kind == code.KindField && !d.Local {
			cuts = append(cuts, d.Span)
			continue
		}
		if !d.Callable() || d.Span.Start < covered {
			continue
		}
		body := d.Body
		if body.Empty() {
			// Expression bodies might not have a separate body span.
			body = d.Span
		}
		cuts = append(cuts, code.Span{Start: d.Span.Start, End: body.Start}, code.Span{Start: body.End, End: d.Span.End})
		covered = d.Span.End
	}
	if len(cuts) == 0 {
		return []normalized{nf}
	}
	slices.SortFunc(cuts, func(a, b code.Span) int {
		return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.End, b.End))
	})
	out := make([]normalized, 0, len(cuts)+1)
	start := 0
	for _, cut := range cuts {
		next := nf.tokenBoundary(start, cut.Start)
		out = nf.appendRegion(out, start, next)
		start = nf.tokenBoundary(next, cut.End)
	}
	return nf.appendRegion(out, start, len(nf.ids))
}

func typeHasCallable(d *code.Decl) bool {
	for _, child := range d.Children {
		if child.Callable() || typeHasCallable(child) {
			return true
		}
	}
	return false
}

func (nf normalized) tokenBoundary(at, offset int) int {
	for at < len(nf.toks) && nf.toks[at].Span.Start < offset {
		at++
	}
	return at
}

func (nf normalized) appendRegion(out []normalized, start, end int) []normalized {
	if start < end {
		out = append(out, normalized{src: nf.src, ids: nf.ids[start:end], toks: nf.toks[start:end]})
	}
	return out
}

// importTokens marks the indices of s.code that belong to package, import,
// using and #include statements.
func (s *source) importTokens() map[int]bool {
	skip := map[int]bool{}
	for i := 0; i < len(s.code); i++ {
		if !s.firstOnLine(i) {
			continue
		}
		if end, ok := s.importEnd(i); ok {
			for k := i; k <= end && k < len(s.code); k++ {
				skip[k] = true
			}
			i = end
		}
	}
	return skip
}

// importEnd reports whether an import-like statement starts at s.code[i]. If
// so, it also returns the index of its last token.
func (s *source) importEnd(i int) (int, bool) {
	text := s.text(s.code[i])
	next := s.tok(i + 1)
	switch s.f.Lang {
	case code.Python:
		if (text == "import" || text == "from") && s.indent(s.line(s.code[i].Span.Start)) == 0 {
			return i + len(s.splitLogicalLines(s.code[i:min(len(s.code), i+400)])[0]) - 1, true
		}
	case code.Go:
		switch {
		case text == "import" && next == "(":
			return s.matchClose(s.code, i+1), true
		case text == "import" || text == "package":
			return s.lineEnd(i), true
		}
	case code.JavaScript, code.TypeScript:
		if text == "import" || text == "export" && s.reexport(i) {
			j := i
			for j < len(s.code) && s.code[j].Kind != code.TokString {
				j++
			}
			if j+1 < len(s.code) && s.text(s.code[j+1]) == ";" {
				j++
			}
			return j, true
		}
		end := s.lineEnd(i)
		for k := i; k+2 <= end; k++ {
			if s.text(s.code[k]) == "require" && s.text(s.code[k+1]) == "(" && s.code[k+2].Kind == code.TokString {
				return end, true
			}
		}
	case code.Java:
		if text == "import" || text == "package" {
			return s.untilSemicolon(i), true
		}
	case code.CSharp:
		if text == "using" && next != "(" && next != "var" {
			return s.untilSemicolon(i), true
		}
	case code.C, code.CPP:
		if text == "#" && (next == "include" || next == "import") {
			return s.lineEnd(i), true
		}
	case code.PHP:
		if s.isPhpKeyword(i, "use") && next != "(" || s.isPhpKeyword(i, "namespace") && next != "{" && next != "\\" {
			return s.untilSemicolon(i), true
		}
	case code.Rust:
		if text == "use" || text == "pub" && next == "use" || text == "extern" && next == "crate" || text == "mod" && s.tok(i+2) == ";" {
			return s.untilSemicolon(i), true
		}
	}
	return 0, false
}

// lineEnd returns the index of the last code token on the line of
// s.code[i].
func (s *source) lineEnd(i int) int {
	l := s.line(s.code[i].Span.Start)
	for i+1 < len(s.code) && s.line(s.code[i+1].Span.Start) == l {
		i++
	}
	return i
}

func (s *source) untilSemicolon(i int) int {
	for i < len(s.code) && s.text(s.code[i]) != ";" {
		i++
	}
	return i
}

// reexport reports whether the export statement at i re-exports from
// another module (export { a } from "./a").
func (s *source) reexport(i int) bool {
	for j := i + 1; j < len(s.code) && j < i+200; j++ {
		switch s.text(s.code[j]) {
		case "from":
			return true
		case ";", "function", "class", "const", "let", "var", "default", "interface", "type", "enum":
			return false
		}
	}
	return false
}
