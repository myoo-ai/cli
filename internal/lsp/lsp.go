// Package lsp implements `myoo lsp`, which finds definitions and references
// and renames symbols for AI agents. If the project's language server is
// installed we ask it, otherwise we answer from the builtin code index.
package lsp

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/command"
)

const locationHelp = `<location> selects symbols: [<file>:][L<from>[-L<to>]:]<query>
  <file>     optional source file. without it the whole workspace is searched
  L<from>    optional line or line range (L4, L4-L9) inside the file
  <query>    case-insensitive substring of the symbol name, falling back to a
             subsequence match ("clc" finds "calculate"). prefix with ! for an
             exact, case-sensitive name ("!a"). qualify with "." or "::" to
             filter by fully qualified name ("Calc.Add", "ns::name")

Examples:
  myoo lsp definition main.py:calc
  myoo lsp definition calculator.py:L4:!a
  myoo lsp references src/app.ts:L10-L20:render
  myoo lsp definition 'C:\work\app\main.py:calc'`

const backendHelpText = `
Backends:
  --backend=auto     (default) use the language server of the language when
                     one is installed, else the builtin index
  --backend=builtin  answer from myoo's own parser and resolver
  --backend=lsp      require a language server
  --server=<cmd>     run this language server command (implies lsp), for
                     example --server="pyright-langserver --stdio"
Known servers, first one on PATH wins:`

func backendHelp() string { return backendHelpText + knownServers() }

func commonFlags() []command.Flag {
	return []command.Flag{
		{Long: "backend", Kind: command.String, Value: "auto|builtin|lsp", Default: "auto", Usage: "where answers come from"},
		{Long: "server", Kind: command.String, Value: "cmd", Usage: "language server command line, implies --backend=lsp"},
		{Long: "lang", Kind: command.String, Value: "lang", Usage: "language whose server to use: go, python, javascript, typescript, java, csharp, c, cpp, php, rust"},
		{Long: "root", Kind: command.String, Value: "dir", Usage: "workspace root (nearest project root above the working directory by default)"},
		{Long: "timeout", Kind: command.String, Value: "duration", Default: "60s", Usage: "language server deadline, for example 30s or 2m"},
	}
}

var limitFlag = command.Flag{Long: "limit", Kind: command.String, Value: "n", Default: "20", Usage: "maximum number of symbols to show"}

// Command returns the lsp command group.
func Command() *command.Command {
	return &command.Command{
		Name:    "lsp",
		Summary: "find definitions and references, rename symbols (language server or builtin index)",
		Usage:   "lsp <definition|references|rename> ...",
		Help:    "Runs the language server of the project when one is installed, else myoo's builtin code index.\n" + backendHelp(),
		Subcommands: []*command.Command{
			{
				Name:    "definition",
				Summary: "show the definition and source of matching symbols",
				Usage:   "lsp definition <location> [--limit=<n>] [--backend=auto|builtin|lsp] [--server=<cmd>] [--lang=<lang>] [--root=<dir>] [--timeout=<duration>]",
				Help:    locationHelp + "\n" + backendHelp(),
				Flags:   append(commonFlags(), limitFlag),
				MinArgs: 1, MaxArgs: 1,
				Run: runDefinition,
			},
			{
				Name:    "references",
				Summary: "list the references to matching symbols",
				Usage:   "lsp references <location> [--limit=<n>] [--backend=auto|builtin|lsp] [--server=<cmd>] [--lang=<lang>] [--root=<dir>] [--timeout=<duration>]",
				Help:    locationHelp + "\n" + backendHelp(),
				Flags:   append(commonFlags(), limitFlag),
				MinArgs: 1, MaxArgs: 1,
				Run: runReferences,
			},
			{
				Name:    "rename",
				Summary: "rename a symbol and every reference to it",
				Usage:   "lsp rename <fqn>[-><local symbol>] <new name> [-c/--include-comments] [--dry-run] [--backend=auto|builtin|lsp] [--server=<cmd>] [--lang=<lang>] [--root=<dir>] [--timeout=<duration>]",
				Help: `<fqn> is the exact fully qualified name printed by definition and
references, for example calculator.calculate or calculator.calculate->a for
the parameter a of calculate.

  -c, --include-comments  also rename whole-word mentions inside comments and
                          docstrings (of the function for local symbols, of
                          the edited files otherwise). this is best-effort
  --dry-run               print the result without writing files

Examples:
  myoo lsp rename calculator.calculate add
  myoo lsp rename calculator.calculate->a first_number -c
` + backendHelp(),
				Flags: append(commonFlags(),
					command.Flag{Long: "include-comments", Short: "c", Kind: command.Bool, Usage: "rename mentions in comments too"},
					command.Flag{Long: "dry-run", Kind: command.Bool, Usage: "print the result without writing files"},
				),
				MinArgs: 2, MaxArgs: 2,
				Run: runRename,
			},
		},
	}
}

type backendKind string

const (
	backendAuto    backendKind = "auto"
	backendBuiltin backendKind = "builtin"
	backendLSP     backendKind = "lsp"
)

type options struct {
	backend backendKind
	server  []string
	lang    code.Lang
	root    string
	timeout time.Duration
	limit   int
}

var langNames = []code.Lang{code.Go, code.Python, code.JavaScript, code.TypeScript, code.Java, code.CSharp, code.C, code.CPP, code.PHP, code.Rust}

func parseOptions(env *command.Env, inv *command.Invocation, withLimit bool) (options, error) {
	o := options{backend: backendKind(inv.String("backend"))}
	switch o.backend {
	case backendAuto, backendBuiltin, backendLSP:
	default:
		return o, command.Usagef("unknown backend `%s`, use auto, builtin or lsp", o.backend)
	}
	if s := inv.String("server"); s != "" {
		if o.backend == backendBuiltin {
			return o, command.Usagef("--server needs a language server, it cannot be combined with --backend=builtin")
		}
		argv, err := splitCommand(s)
		if err != nil {
			return o, command.Usagef("invalid --server: %v", err)
		}
		o.server, o.backend = argv, backendLSP
	}
	if l := inv.String("lang"); l != "" {
		o.lang = code.Lang(strings.ToLower(l))
		if !slices.Contains(langNames, o.lang) {
			names := make([]string, len(langNames))
			for i, n := range langNames {
				names[i] = string(n)
			}
			return o, command.Usagef("unknown language `%s`, use one of %s", l, strings.Join(names, ", "))
		}
	}
	if r := inv.String("root"); r != "" {
		o.root = absPath(env.Dir, r)
	}
	t := inv.String("timeout")
	d, err := time.ParseDuration(t)
	if err != nil {
		secs, serr := strconv.ParseFloat(t, 64)
		if serr != nil {
			return o, command.Usagef("invalid --timeout `%s`, use a duration such as 30s or 2m", t)
		}
		d = time.Duration(secs * float64(time.Second))
	}
	if d <= 0 {
		return o, command.Usagef("--timeout must be positive")
	}
	o.timeout = d
	if withLimit {
		n, err := strconv.Atoi(inv.String("limit"))
		if err != nil || n < 1 {
			return o, command.Usagef("invalid --limit `%s`, use a positive number", inv.String("limit"))
		}
		o.limit = n
	}
	return o, nil
}

func absPath(dir, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return filepath.Clean(p)
}

func runDefinition(env *command.Env, inv *command.Invocation) error {
	s, err := openSession(env, inv)
	if err != nil {
		return err
	}
	defer s.close()
	env.Format("(for each matching symbol) `<symbol>:<fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)\\n\\t<source, max. 50 lines counting from top to bottom>\\n\\n`")
	for i, d := range s.groups {
		if i > 0 {
			fmt.Fprintln(env.Stdout)
		}
		owner := d.Owner()
		symbol := owner.Name
		if symbol == "" {
			symbol = d.Name
		}
		fmt.Fprintf(env.Stdout, "%s:%s (%s)\n", symbol, d.FQN(), s.ws.position(d.File, d.NameSpan.Start))
		writeSource(env.Stdout, owner.File, sourceSpan(owner))
	}
	return nil
}

func runReferences(env *command.Env, inv *command.Invocation) error {
	s, err := openSession(env, inv)
	if err != nil {
		return err
	}
	defer s.close()
	type entry struct {
		decl  *code.Decl
		sites []site
	}
	var entries []entry
	for _, d := range s.groups {
		sites, err := s.be.references(s.ws.members(d))
		if err != nil {
			return err
		}
		if len(sites) > 0 {
			entries = append(entries, entry{d, sites})
		}
	}
	if len(entries) == 0 {
		names := make([]string, len(s.groups))
		for i, d := range s.groups {
			names[i] = "`" + d.FQN() + "`"
		}
		return fmt.Errorf("no references to %s found", strings.Join(names, ", "))
	}
	env.Format("(for each matching symbol with one or more references) `<fqn>[-><local/temporary symbols>]\\n- <reference fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)`")
	for i, e := range entries {
		if i > 0 {
			fmt.Fprintln(env.Stdout)
		}
		fmt.Fprintln(env.Stdout, e.decl.FQN())
		for _, st := range e.sites {
			fmt.Fprintf(env.Stdout, "- %s (%s)\n", st.label(), s.ws.position(st.file, st.span.Start))
		}
	}
	return nil
}
