package lsp

import (
	"fmt"
	"slices"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/command"
)

// backend answers the questions the commands need answered.
type backend interface {
	// resolve returns the canonical declaration an occurrence refers to, or
	// nil if it's defined outside the workspace or we can't resolve it.
	resolve(o occurrence) (*code.Decl, error)
	// references lists the references to decls, which all have to be
	// declarations of the same symbol, sorted by file and position.
	references(decls []*code.Decl) ([]site, error)
	// rename adds the edits that rename decls, which all declare the same
	// symbol, and their references to newName.
	rename(decls []*code.Decl, newName string, plan *editPlan) error
	close()
}

type builtinBackend struct{ ws *workspace }

func (b builtinBackend) resolve(o occurrence) (*code.Decl, error) {
	if o.decl != nil {
		return b.ws.canon(o.decl), nil
	}
	if o.ref.Target == nil {
		return nil, nil
	}
	return b.ws.canon(o.ref.Target), nil
}

func (b builtinBackend) references(decls []*code.Decl) ([]site, error) {
	var sites []site
	for _, d := range decls {
		for _, r := range b.ws.ix.RefsTo(d) {
			sites = append(sites, site{file: r.File, span: r.Span, name: r.Name, scope: r.Scope})
		}
	}
	return sortSites(sites), nil
}

func (b builtinBackend) rename(decls []*code.Decl, newName string, plan *editPlan) error {
	for _, d := range decls {
		if d.File.Text(d.NameSpan) == d.Name {
			plan.add(d.File, d.NameSpan, newName)
		}
		for _, r := range b.ws.ix.RefsTo(d) {
			if r.File.Text(r.Span) == d.Name {
				plan.add(r.File, r.Span, newName)
			}
		}
	}
	return nil
}

func (builtinBackend) close() {}

// selectBackend opens the backend the options ask for. In auto mode, if the
// language server is missing or won't start, we fall back to the builtin
// index. We only warn if it failed to start.
func selectBackend(env *command.Env, ws *workspace, opts options, lang code.Lang) (backend, error) {
	if opts.backend == backendBuiltin {
		return builtinBackend{ws}, nil
	}
	if opts.lang != "" {
		lang = opts.lang
	}
	argv := opts.server
	if argv == nil {
		var ok bool
		argv, ok = findServer(lang)
		if !ok {
			if opts.backend == backendLSP {
				return nil, fmt.Errorf("no language server for %s found on PATH, install one of %s, or pass --server", langLabel(lang), serverNames(lang))
			}
			return builtinBackend{ws}, nil
		}
	}
	be, err := startLSPBackend(ws, argv, opts.timeout)
	if err != nil {
		if opts.backend == backendLSP {
			return nil, fmt.Errorf("%s: %w", argv[0], err)
		}
		fmt.Fprintf(env.Stderr, "warning: %s: %v, using the builtin index\n", argv[0], err)
		return builtinBackend{ws}, nil
	}
	return be, nil
}

func langLabel(l code.Lang) string {
	if l == "" {
		return "this workspace"
	}
	return string(l)
}

// session is the state that definition and references share.
type session struct {
	ws     *workspace
	be     backend
	groups []*code.Decl
}

func (s *session) close() { s.be.close() }

func openSession(env *command.Env, inv *command.Invocation) (*session, error) {
	opts, err := parseOptions(env, inv, true)
	if err != nil {
		return nil, err
	}
	t, err := parseTarget(env.Dir, inv.Args[0])
	if err != nil {
		return nil, err
	}
	ws, err := loadWorkspace(env, rootFor(env, opts, t.file), t.file)
	if err != nil {
		return nil, err
	}
	occs, err := ws.occurrences(t)
	if err != nil {
		return nil, err
	}
	matched := matching(t.query, occs)
	if len(matched) == 0 {
		return nil, noMatch(t.query)
	}
	lang := ws.dominantLang()
	if t.file != "" {
		lang = matched[0].file.Lang
	}
	be, err := selectBackend(env, ws, opts, lang)
	if err != nil {
		return nil, err
	}
	groups, more, err := group(ws, be, t.query, matched, opts.limit)
	if err != nil {
		be.close()
		return nil, err
	}
	if more {
		fmt.Fprintf(env.Stderr, "note: more than %d symbols match `%s`, showing the first %d. narrow the query or raise --limit\n", opts.limit, t.query.raw, opts.limit)
	}
	return &session{ws: ws, be: be, groups: groups}, nil
}

// isDeclName reports whether off in f is the start of the name of one of
// decls. We need it because language servers can list declarations among
// the references.
func isDeclName(decls []*code.Decl, f *code.File, off int) bool {
	return slices.ContainsFunc(decls, func(d *code.Decl) bool { return d.File == f && d.NameSpan.Start == off })
}
