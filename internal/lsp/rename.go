package lsp

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/command"
)

type edit struct {
	span code.Span
	text string
}

type fileEdits struct {
	file  *code.File
	edits []edit
}

// editPlan collects text edits by file. It has to be safe for concurrent
// use, since language servers can add edits through workspace/applyEdit.
type editPlan struct {
	ws    *workspace
	mu    sync.Mutex
	files map[string]*fileEdits // keyed by File.Abs
	err   error                 // the first edit we couldn't take
}

func newPlan(ws *workspace) *editPlan {
	return &editPlan{ws: ws, files: map[string]*fileEdits{}}
}

func (p *editPlan) add(f *code.File, span code.Span, text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addLocked(f, span, text)
}

func (p *editPlan) addLocked(f *code.File, span code.Span, text string) {
	fe := p.files[f.Abs]
	if fe == nil {
		fe = &fileEdits{file: f}
		p.files[f.Abs] = fe
	}
	fe.edits = append(fe.edits, edit{span, text})
}

func (p *editPlan) empty() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.files) == 0
}

func (p *editPlan) failure() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// overlaps reports whether span overlaps an edit we've already planned in f.
func (p *editPlan) overlaps(f *code.File, span code.Span) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	fe := p.files[f.Abs]
	if fe == nil {
		return false
	}
	return slices.ContainsFunc(fe.edits, func(e edit) bool { return e.span.Start < span.End && span.Start < e.span.End })
}

// addWorkspaceEdit adds the text edits from a language server's workspace
// edit. We refuse edits outside the workspace.
func (p *editPlan) addWorkspaceEdit(enc encoding, we workspaceEdit) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.addWorkspaceEditLocked(enc, we)
	if err != nil && p.err == nil {
		p.err = err
	}
	return err
}

func (p *editPlan) addWorkspaceEditLocked(enc encoding, we workspaceEdit) error {
	byURI, err := we.textEdits()
	if err != nil {
		return err
	}
	for uri, edits := range byURI {
		path, ok := uriToPath(uri)
		if !ok {
			return fmt.Errorf("the language server wants to edit `%s`, which is not a file", uri)
		}
		if !p.ws.inside(path) {
			return fmt.Errorf("the rename would also change `%s` outside the workspace %s, refusing", path, p.ws.root)
		}
		f, err := p.fileFor(path)
		if err != nil {
			return err
		}
		for _, e := range edits {
			p.addLocked(f, enc.span(f, e.Range), e.NewText)
		}
	}
	return nil
}

// fileFor returns the indexed file at path. If the index left it out because
// a server edited a file we don't support or ignore, we read it from disk.
func (p *editPlan) fileFor(path string) (*code.File, error) {
	if f := p.ws.file(path); f != nil {
		return f, nil
	}
	if fe := p.files[path]; fe != nil {
		return fe.file, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(p.ws.root, path)
	if err != nil {
		rel = path
	}
	_, lang, _ := frontendFor(path)
	return code.NewFile(filepath.ToSlash(rel), path, lang, src), nil
}

// fileResult is what the plan does to a file.
type fileResult struct {
	file  *code.File
	src   []byte
	edits []edit // sorted, with duplicates removed
}

// results applies the plan in memory, file by file in path order.
func (p *editPlan) results() ([]fileResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []fileResult
	for _, fe := range p.files {
		edits := slices.Clone(fe.edits)
		slices.SortFunc(edits, func(a, b edit) int {
			return cmp.Or(cmp.Compare(a.span.Start, b.span.Start), cmp.Compare(a.span.End, b.span.End))
		})
		edits = slices.Compact(edits)
		for i := 1; i < len(edits); i++ {
			if edits[i].span.Start < edits[i-1].span.End || edits[i].span == edits[i-1].span {
				pos := fe.file.Pos(edits[i].span.Start)
				return nil, fmt.Errorf("conflicting edits at %s", command.Point(fe.file.Path, pos.Line, pos.Col))
			}
		}
		src := fe.file.Src
		// We apply from the end backwards so earlier offsets stay valid.
		for _, e := range slices.Backward(edits) {
			src = slices.Concat(src[:e.span.Start], []byte(e.text), src[e.span.End:])
		}
		out = append(out, fileResult{file: fe.file, src: src, edits: edits})
	}
	slices.SortFunc(out, func(a, b fileResult) int { return cmp.Compare(a.file.Path, b.file.Path) })
	return out, nil
}

// shift maps an offset in the original source to the edited source.
func (r fileResult) shift(off int) int {
	delta := 0
	for _, e := range r.edits {
		if e.span.Start >= off {
			break
		}
		if e.span.End > off {
			return e.span.Start + delta
		}
		delta += len(e.text) - (e.span.End - e.span.Start)
	}
	return off + delta
}

func runRename(env *command.Env, inv *command.Invocation) error {
	opts, err := parseOptions(env, inv, false)
	if err != nil {
		return err
	}
	fqn, newName := inv.Args[0], inv.Args[1]
	ws, err := loadWorkspace(env, rootFor(env, opts, ""), "")
	if err != nil {
		return err
	}
	decls := ws.ix.Lookup(fqn)
	if len(decls) == 0 {
		return unknownSymbol(ws, fqn)
	}
	decls = ws.same(decls[0])
	first := decls[0]
	if first.Kind == code.KindModule {
		return fmt.Errorf("`%s` is a module. myoo does not rename modules or files", fqn)
	}
	oldName := first.Name
	if newName == oldName {
		return fmt.Errorf("`%s` is already named `%s`", fqn, newName)
	}
	if err := validateName(first.File, newName); err != nil {
		return err
	}
	newFQN := strings.TrimSuffix(fqn, oldName) + newName
	if clash := ws.ix.Lookup(newFQN); len(clash) > 0 {
		c := clash[0]
		return fmt.Errorf("`%s` already exists (%s %s), choose another name", newFQN, c.Kind, ws.position(c.File, c.NameSpan.Start))
	}
	be, err := selectBackend(env, ws, opts, first.File.Lang)
	if err != nil {
		return err
	}
	defer be.close()
	plan := newPlan(ws)
	if err := be.rename(decls, newName, plan); err != nil {
		return err
	}
	if inv.Bool("include-comments") {
		renameInComments(plan, decls, oldName, newName)
	}
	results, err := plan.results()
	if err != nil {
		return err
	}
	if !inv.Bool("dry-run") {
		for _, r := range results {
			if err := writeAtomic(r.file.Abs, r.src); err != nil {
				return fmt.Errorf("unable to write %s: %w", ws.display(r.file.Abs), err)
			}
		}
	}

	env.Format("`<fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)\\n\\t<new source, max. 50 lines counting from top to bottom>\\n\\n- <changed file> (<n> edits)`")
	res := fileResult{file: first.File, src: first.File.Src}
	for _, r := range results {
		if r.file == first.File {
			res = r
		}
	}
	nf, span, nameOff := renamedDecl(res, first, newFQN)
	fmt.Fprintf(env.Stdout, "%s (%s)\n", newFQN, ws.position(nf, nameOff))
	writeSource(env.Stdout, nf, span)
	fmt.Fprintln(env.Stdout)
	for _, r := range results {
		unit := "edits"
		if len(r.edits) == 1 {
			unit = "edit"
		}
		fmt.Fprintf(env.Stdout, "- %s (%d %s)\n", ws.display(r.file.Abs), len(r.edits), unit)
	}
	return nil
}

// renamedDecl finds the renamed declaration in the re-parsed file and
// returns the file, the source span of its owner and its name offset. If
// the parser can't find it anymore, we map the old spans through the edits.
func renamedDecl(res fileResult, old *code.Decl, newFQN string) (*code.File, code.Span, int) {
	nameOff := res.shift(old.NameSpan.Start)
	if nf, err := reparse(old.File, res.src); err == nil {
		for _, d := range nf.Decls {
			if d.NameSpan.Start == nameOff && d.FQN() == newFQN {
				return nf, sourceSpan(d.Owner()), nameOff
			}
		}
	}
	nf := code.NewFile(old.File.Path, old.File.Abs, old.File.Lang, res.src)
	owner := sourceSpan(old.Owner())
	return nf, code.Span{Start: res.shift(owner.Start), End: res.shift(owner.End)}, nameOff
}

// validateName checks that name lexes as a single identifier in the
// language of f, and isn't a keyword.
func validateName(f *code.File, name string) error {
	fe, lang, ok := frontendFor(f.Abs)
	if !ok || name == "" {
		return fmt.Errorf("`%s` is not a valid identifier", name)
	}
	nf := code.NewFile(f.Path, f.Abs, lang, []byte(name))
	var parseErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				parseErr = fmt.Errorf("%v", r)
			}
		}()
		parseErr = fe.Parse(nf)
	}()
	var toks []code.Token
	for _, t := range nf.Tokens {
		if t.Kind != code.TokComment {
			toks = append(toks, t)
		}
	}
	if parseErr == nil && len(toks) == 1 && toks[0].Span == (code.Span{Start: 0, End: len(name)}) {
		switch toks[0].Kind {
		case code.TokIdent:
			return nil
		case code.TokKeyword:
			return fmt.Errorf("`%s` is a reserved word in %s, choose another name", name, lang)
		}
	}
	return fmt.Errorf("`%s` is not a valid %s identifier", name, lang)
}

// unknownSymbol reports an FQN that doesn't exist and suggests close ones.
func unknownSymbol(ws *workspace, fqn string) error {
	type cand struct {
		fqn  string
		dist int
	}
	seen := map[string]bool{}
	var cands []cand
	limit := max(2, utf8.RuneCountInString(fqn)/3)
	for d := range ws.ix.Decls() {
		f := d.FQN()
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		if strings.HasSuffix(f, "."+fqn) || strings.HasSuffix(f, "->"+fqn) {
			cands = append(cands, cand{f, 0})
			continue
		}
		if diff := len(f) - len(fqn); diff > limit || -diff > limit {
			continue
		}
		if dist := command.Distance(fqn, f); dist <= limit {
			cands = append(cands, cand{f, dist})
		}
	}
	if len(cands) == 0 {
		return fmt.Errorf("unknown symbol `%s`, rename takes a fully qualified name as printed by `myoo lsp definition <query>`", fqn)
	}
	slices.SortFunc(cands, func(a, b cand) int { return cmp.Or(cmp.Compare(a.dist, b.dist), cmp.Compare(a.fqn, b.fqn)) })
	var names []string
	for _, c := range cands[:min(3, len(cands))] {
		names = append(names, "`"+c.fqn+"`")
	}
	return fmt.Errorf("unknown symbol `%s`, did you mean %s?", fqn, strings.Join(names, " or "))
}

// renameInComments adds whole-word replacements of oldName in comments. For
// a local symbol we only look at the comments in the owning declaration,
// including the doc comments right above it. Otherwise we look at all the
// files we're editing.
func renameInComments(p *editPlan, decls []*code.Decl, oldName, newName string) {
	type region struct {
		file *code.File
		span code.Span
	}
	var regions []region
	if decls[0].Local {
		for _, d := range decls {
			o := d.Owner()
			regions = append(regions, region{d.File, withLeadingComments(d.File, o.Span)})
		}
	} else {
		p.mu.Lock()
		for _, fe := range p.files {
			regions = append(regions, region{fe.file, code.Span{Start: 0, End: len(fe.file.Src)}})
		}
		p.mu.Unlock()
	}
	for _, r := range regions {
		for _, c := range r.file.Comments {
			if !r.span.Encloses(c.Span) {
				continue
			}
			text := r.file.Text(c.Span)
			for _, off := range wordOffsets(text, oldName) {
				s := code.Span{Start: c.Span.Start + off, End: c.Span.Start + off + len(oldName)}
				if !p.overlaps(r.file, s) {
					p.add(r.file, s, newName)
				}
			}
		}
	}
}

// withLeadingComments extends span up over the comments right above it, like
// doc comments and JSDoc @param lines.
func withLeadingComments(f *code.File, span code.Span) code.Span {
	for _, c := range slices.Backward(f.Comments) {
		if c.Span.End > span.Start {
			continue
		}
		if strings.TrimSpace(string(f.Src[c.Span.End:span.Start])) != "" {
			break
		}
		span.Start = c.Span.Start
	}
	return span
}

// wordOffsets returns the byte offsets where word appears in text as a
// whole word.
func wordOffsets(text, word string) []int {
	var out []int
	for i := 0; i <= len(text)-len(word); {
		j := strings.Index(text[i:], word)
		if j < 0 {
			break
		}
		s, e := i+j, i+j+len(word)
		before, _ := utf8.DecodeLastRuneInString(text[:s])
		after, _ := utf8.DecodeRuneInString(text[e:])
		if (s == 0 || !isIdentRune(before)) && (e == len(text) || !isIdentRune(after)) {
			out = append(out, s)
		}
		i = s + 1
	}
	return out
}

// writeAtomic follows symlinks at path and replaces the file there with
// data, keeping its permissions.
func writeAtomic(path string, data []byte) error {
	path = realPath(path)
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".myoo-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	err = errors.Join(werr, tmp.Chmod(fi.Mode().Perm()), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}
