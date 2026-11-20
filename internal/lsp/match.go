package lsp

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/command"
)

// target is a parsed <location> argument.
type target struct {
	file     string // absolute path, "" for the whole workspace
	from, to int    // inclusive 1-based line range, 0 if not given
	query    query
}

var lineRange = regexp.MustCompile(`^L(\d+)(?:-L?(\d+))?:`)

// parseTarget parses [<file>:][L<from>[-L<to>]:]<query>. We only treat a
// ':' as the end of the file if the text before it is an existing file, so
// drive letters and C++ scope operators don't get split.
func parseTarget(dir, s string) (target, error) {
	var t target
	rest := s
	for i := range len(rest) {
		if rest[i] != ':' || i == 0 {
			continue
		}
		left := rest[:i]
		if abs := absPath(dir, left); fileExists(abs) {
			t.file, rest = abs, rest[i+1:]
			break
		}
		if _, _, ok := frontendFor(left); ok && !strings.Contains(left, "::") {
			return t, fmt.Errorf("unable to find file `%s`, check the path (relative to %s)", left, dir)
		}
	}
	if m := lineRange.FindStringSubmatch(rest); m != nil {
		t.from, _ = strconv.Atoi(m[1])
		t.to = t.from
		if m[2] != "" {
			t.to, _ = strconv.Atoi(m[2])
		}
		if t.from < 1 || t.to < t.from {
			return t, command.Usagef("invalid line range `%s` in `%s`, use L<from>-L<to> with from <= to", strings.TrimSuffix(m[0], ":"), s)
		}
		rest = rest[len(m[0]):]
	}
	q, err := parseQuery(rest)
	if err != nil {
		return t, err
	}
	t.query = q
	return t, nil
}

// query is the symbol part of a location.
type query struct {
	raw   string
	exact bool
	name  string // what we match against identifier occurrences
	qual  string // normalized, "."-separated, "" if unqualified
}

func parseQuery(s string) (query, error) {
	q := query{raw: s}
	body := s
	if strings.HasPrefix(body, "!") {
		q.exact, body = true, body[1:]
	}
	body = strings.ReplaceAll(body, "::", ".")
	q.name = body
	if i := max(strings.LastIndex(body, "."), strings.LastIndex(body, "->")); i >= 0 {
		q.qual = body
		q.name = strings.TrimPrefix(strings.TrimPrefix(body[i:], "->"), ".")
	}
	if strings.TrimSpace(q.name) == "" {
		return q, command.Usagef("missing symbol name in `%s`, see `myoo lsp definition --help`", s)
	}
	return q, nil
}

type matchMode uint8

const (
	matchExact matchMode = iota
	matchContains
	matchSubsequence
)

func (q query) matchName(name string, mode matchMode) bool {
	switch mode {
	case matchExact:
		return name == q.name
	case matchContains:
		return strings.Contains(strings.ToLower(name), strings.ToLower(q.name))
	}
	return isSubsequence(strings.ToLower(q.name), strings.ToLower(name))
}

// matchFQN filters the resolved symbols for a qualified query.
func (q query) matchFQN(fqn string) bool {
	if q.qual == "" {
		return true
	}
	if q.exact {
		return fqn == q.qual || strings.HasSuffix(fqn, "."+q.qual) || strings.HasSuffix(fqn, "->"+q.qual)
	}
	return strings.Contains(strings.ToLower(fqn), strings.ToLower(q.qual))
}

func isSubsequence(needle, hay string) bool {
	hr := []rune(hay)
	i := 0
	for _, r := range needle {
		for i < len(hr) && hr[i] != r {
			i++
		}
		if i == len(hr) {
			return false
		}
		i++
	}
	return true
}

// occurrence is an identifier in the source, either a declaration name or
// a reference.
type occurrence struct {
	file *code.File
	span code.Span
	name string
	decl *code.Decl
	ref  *code.Ref
}

// occurrences lists the identifiers in the target's file and line range, or
// in all files if the target doesn't name one, in file and source order.
func (ws *workspace) occurrences(t target) ([]occurrence, error) {
	files := ws.ix.Files
	if t.file != "" {
		f := ws.file(t.file)
		if f == nil {
			return nil, fmt.Errorf("`%s` is not a source file myoo can read (unsupported language, too large or binary)", ws.display(t.file))
		}
		files = []*code.File{f}
	}
	var out []occurrence
	for _, f := range files {
		start := len(out)
		inRange := func(off int) bool {
			if t.from == 0 {
				return true
			}
			l := f.LineOf(off)
			return l >= t.from && l <= t.to
		}
		for _, d := range f.Decls {
			if !d.NameSpan.Empty() && inRange(d.NameSpan.Start) {
				out = append(out, occurrence{file: f, span: d.NameSpan, name: d.Name, decl: d})
			}
		}
		for _, r := range f.Refs {
			if inRange(r.Span.Start) {
				out = append(out, occurrence{file: f, span: r.Span, name: r.Name, ref: r})
			}
		}
		slices.SortStableFunc(out[start:], func(a, b occurrence) int { return cmp.Compare(a.span.Start, b.span.Start) })
	}
	return out, nil
}

// matching filters occurrences by name. A "!" query has to match exactly.
// Otherwise we match by substring, and if nothing matches, by subsequence.
func matching(q query, occs []occurrence) []occurrence {
	modes := []matchMode{matchContains, matchSubsequence}
	if q.exact {
		modes = []matchMode{matchExact}
	}
	for _, mode := range modes {
		var out []occurrence
		for _, o := range occs {
			if q.matchName(o.name, mode) {
				out = append(out, o)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func noMatch(q query) error {
	return fmt.Errorf("unable to find symbols matching `%s`, try widening the query", q.raw)
}

// group resolves the matched occurrences to their definitions, in order of
// first occurrence, and keeps up to limit of them. more is true if there
// were other symbols that matched too.
func group(ws *workspace, be backend, q query, matched []occurrence, limit int) (groups []*code.Decl, more bool, err error) {
	seen := map[*code.Decl]bool{}
	var external []string
	for _, o := range matched {
		d, err := be.resolve(o)
		if err != nil {
			return nil, false, err
		}
		if d == nil {
			if !slices.Contains(external, o.name) {
				external = append(external, o.name)
			}
			continue
		}
		if seen[d] || !q.matchFQN(d.FQN()) {
			continue
		}
		seen[d] = true
		if len(groups) == limit {
			more = true
			break
		}
		groups = append(groups, d)
	}
	if len(groups) == 0 {
		if len(external) == 0 {
			return nil, false, noMatch(q)
		}
		for i, n := range external {
			external[i] = "`" + n + "`"
		}
		return nil, false, fmt.Errorf("symbols matching `%s` are defined outside the workspace: %s", q.raw, strings.Join(external, ", "))
	}
	return groups, more, nil
}
