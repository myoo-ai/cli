// Package codelint implements code_lint, a linter for patterns that make
// source code read as AI generated. It looks for things like narrating and
// decorative comments, placeholders, swallowed errors, icons in strings and
// copy-pasted blocks.
package codelint

import (
	"cmp"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/langs"
	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/likelihood"
)

const (
	lintFormat  = "`#<nth violation> <file>:<line>:<column>-[<end line>.]<end column> - <rule ID>: <description + proposed fix (if applicable)>`"
	rulesFormat = "`<rule ID> [<languages>]: <description>`"
)

// Command returns the code_lint command.
func Command() *command.Command {
	return &command.Command{
		Name:    "code_lint",
		Summary: "lint source code for AI patterns",
		Usage:   "code_lint [path...] [--ignore=<rule,...>] [-i/--ignore-globs=<pattern,...>] [--list-rules [--include-recommendations]]",
		Help: `Lints the given files and directories (default: the current directory)
recursively. It honors .gitignore and --ignore-globs, and skips testdata
directories and generated or minified files. Prints one line per
violation, the evidence of AI authorship, then the estimated likelihood that
the code was written by AI ("(likely) AI patterns: <n> %"), and exits with
status 1 when violations were found. Locations use the file:line:column form
that terminals such as VS Code's open on cmd/ctrl+click.

The likelihood combines signals measured on human and AI written codebases:
the voice of English comments (AI comments are dense and declarative, people
write "we", "if", "will", "to"), comment-prose violations, TODO markers,
questions and informal words in comments (which people leave and agents do
not), em dashes, icons and step comments. Each evidence line shows what a
signal counted and how far it moved the estimate. Code with few comments
gives little evidence and stays low.

Rules that judge what a comment says report it only when it reads as
generated: prose_lint violations in the comment, an upper-case start, several
lines and 30 or more characters each raise its score, and a comment needs 2
such signals. The phrase rules (change-narration-comment,
reviewer-addressed-comment, demo-grade-comment) match wording people rarely
use in comments and report every match.

Rules whose IDs start with a language (ts-, py-, go-, java-, cs-, c-, cpp-,
php-, rust-) flag code that has a more idiomatic replacement, adapted from
SonarQube, staticcheck, gopls, pylint, clang-tidy, PHPStan, Rector,
PHP_CodeSniffer and Clippy. The detail shows the rewrite.

A comment containing myoo-ignore suppresses violations that start on the same
or the next line. myoo-ignore: rule-a, rule-b limits it to those rules.
myoo-ignore-file[: rules] anywhere in a file applies to the whole file.

--ignore-globs takes comma-separated .gitignore patterns relative to the
project root (fixture/, *.pb.go, web/legacy/**). They add to the defaults,
which skip folders of dependencies and build output, virtualenvs, test
fixtures, folders of generated code and minified bundles:
` + strings.Join(lintIgnore, ",") + `
A pattern starting with ! lints a path the defaults skip (!vendor/). Files
named on the command line are linted even when a pattern matches them.`,
		MaxArgs: -1,
		Flags: []command.Flag{
			{Long: "ignore", Kind: command.String, Value: "rule,...", Usage: "disable rules"},
			{Long: "ignore-globs", Short: "i", Kind: command.String, Value: "pattern,...", Usage: "skip paths matching these .gitignore patterns, in addition to the defaults"},
			{Long: "list-rules", Kind: command.Bool, Usage: "list all rules"},
			{Long: "include-recommendations", Kind: command.Bool, Usage: "with --list-rules, also print how to fix each rule"},
		},
		Run: run,
	}
}

// lintIgnore adds test fixtures, generated code folders and minified bundles
// to the dependency and build folders the loader already skips.
var lintIgnore = slices.Concat(code.DefaultIgnore(), []string{"fixture/", "fixtures/", "__fixtures__/", "generated/", "*.min.*"})

func run(env *command.Env, inv *command.Invocation) error {
	if inv.Bool("list-rules") {
		listRules(env, inv.Bool("include-recommendations"))
		return nil
	}
	ignored, err := parseIgnore(inv.String("ignore"))
	if err != nil {
		return err
	}
	args := inv.Args
	if len(args) == 0 {
		args = []string{"."}
	}
	paths := make([]string, len(args))
	for i, a := range args {
		paths[i] = a
		if !filepath.IsAbs(a) {
			paths[i] = filepath.Join(env.Dir, a)
		}
	}
	var ignoreGlobs []string
	for g := range strings.SplitSeq(inv.String("ignore-globs"), ",") {
		if g = strings.TrimSpace(g); g != "" {
			if err := code.CheckIgnorePattern(g); err != nil {
				return command.Usagef("invalid --ignore-globs: %v", err)
			}
			ignoreGlobs = append(ignoreGlobs, g)
		}
	}
	ix, err := code.Load(code.LoadOptions{
		Root: code.FindRoot(paths[0]), Paths: paths, Frontends: langs.All(),
		Ignore: slices.Concat(lintIgnore, ignoreGlobs),
	})
	if err != nil {
		return err
	}
	for _, e := range ix.Errors {
		fmt.Fprintf(env.Stderr, "warning: %s\n", e)
	}
	var srcs []*source
	for _, f := range ix.Files {
		if inTestdata(f.Abs, paths) || generated(f) {
			continue
		}
		srcs = append(srcs, newSource(f, displayPath(env.Dir, f.Abs)))
	}
	vs, crashes := lint(srcs, ignored)
	for _, e := range crashes {
		fmt.Fprintf(env.Stderr, "warning: %s\n", e)
	}
	ev := aiEvidence(srcs, vs)
	if len(vs) == 0 {
		fmt.Fprintln(env.Stdout, "no violations found")
	} else {
		env.Format(lintFormat)
		for i, v := range vs {
			from, to := v.src.f.Pos(v.span.Start), v.src.f.Pos(v.span.End)
			fmt.Fprintf(env.Stdout, "#%d %s - %s: %s %s\n",
				i+1, command.Location(v.src.name, from.Line, from.Col, to.Line, to.Col), v.rule.ID, v.description(), v.rule.Recommendation)
		}
	}
	pct := 0.0 // if there are no supported source files, nothing reads as AI
	if len(srcs) > 0 {
		env.Format(likelihood.EvidenceFormat)
		likelihood.WriteEvidence(env.Stdout, aiPrior, ev)
		pct = likelihood.Percent(aiPrior, ev)
	}
	if err := likelihood.Write(env.Stdout, pct); err != nil {
		return err
	}
	if len(vs) > 0 {
		return command.ErrViolations
	}
	return nil
}

// lint runs the rules that aren't ignored over srcs. It returns the
// violations that survive inline suppression and, for scored rules, the
// comment score, sorted by file and position, plus an error for each rule
// that crashed.
func lint(srcs []*source, ignored map[string]bool) ([]violation, []error) {
	extended := map[string]bool{}
	for _, s := range srcs {
		for _, d := range s.f.Decls {
			for _, b := range d.Bases {
				extended[baseName(b)] = true
			}
		}
	}
	for _, s := range srcs {
		s.extended = extended
	}
	var vs []violation
	for _, r := range Rules {
		if ignored[r.ID] || r.checkAll == nil {
			continue
		}
		var mine []*source
		for _, s := range srcs {
			if r.appliesTo(s.f.Lang) {
				mine = append(mine, s)
			}
		}
		for _, fd := range r.checkAll(mine) {
			vs = append(vs, violation{finding: fd, rule: r})
		}
	}
	// Per-file rules don't share any state across files except the
	// read-only extended set, so we can lint the files in parallel.
	perFile := make([][]violation, len(srcs))
	crashed := make([][]error, len(srcs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, s := range srcs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			for _, r := range Rules {
				if ignored[r.ID] || r.checkAll != nil || !r.appliesTo(s.f.Lang) {
					continue
				}
				fds, err := checkFile(r, s)
				if err != nil {
					crashed[i] = append(crashed[i], err)
				}
				for _, fd := range fds {
					fd.src = s
					if v := (violation{finding: fd, rule: r}); s.reported(v) {
						perFile[i] = append(perFile[i], v)
					}
				}
			}
		})
	}
	wg.Wait()
	vs = slices.DeleteFunc(vs, func(v violation) bool { return !v.src.reported(v) })
	vs = append(vs, slices.Concat(perFile...)...)
	crashes := slices.Concat(crashed...)
	vs = dropShadowed(vs)
	slices.SortStableFunc(vs, func(a, b violation) int {
		return cmp.Or(
			cmp.Compare(a.src.name, b.src.name),
			cmp.Compare(a.span.Start, b.span.Start),
			cmp.Compare(a.span.End, b.span.End),
			cmp.Compare(a.rule.order, b.rule.order),
		)
	})
	return slices.CompactFunc(vs, func(a, b violation) bool {
		return a.src == b.src && a.span == b.span && a.rule == b.rule
	}), crashes
}

func (s *source) reported(v violation) bool {
	return !s.suppressed(v) && (!v.rule.scored || s.aiScore(v.span) > aiScoreThreshold)
}

// checkFile runs r on s. If a rule crashes on a file, we drop its findings
// for that file instead of aborting the whole run.
func checkFile(r *Rule, s *source) (fds []finding, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%s: rule %s crashed, its findings for the file are missing: %v", s.name, r.ID, p)
		}
	}()
	return r.check(s), nil
}

// baseName strips qualifiers and type arguments from a base type as
// written: "pkg.Base[T]" and "ns::Base<T>" become "Base".
func baseName(b string) string {
	if i := strings.IndexAny(b, "<[("); i >= 0 {
		b = b[:i]
	}
	if i := strings.LastIndexAny(b, ".:"); i >= 0 {
		b = b[i+1:]
	}
	return strings.TrimSpace(b)
}

// dropShadowed removes violations that repeat a more specific finding on
// the same text. That covers box-drawing icons inside a decorative comment,
// and several comment-wording rules firing on the same comment, where the
// earlier rule wins.
func dropShadowed(vs []violation) []violation {
	type key struct {
		src   *source
		start int
	}
	commentRule := map[key]*Rule{}
	var decorative []violation
	for _, v := range vs {
		if v.rule.commentWording {
			k := key{v.src, v.span.Start}
			if r, ok := commentRule[k]; !ok || v.rule.order < r.order {
				commentRule[k] = v.rule
			}
		}
		if v.rule.ID == "decorative-comment" {
			decorative = append(decorative, v)
		}
	}
	return slices.DeleteFunc(vs, func(v violation) bool {
		if v.rule.commentWording {
			return commentRule[key{v.src, v.span.Start}] != v.rule
		}
		if v.rule.ID == "utf8-icons-in-code" {
			for _, d := range decorative {
				if d.src == v.src && d.span.Encloses(v.span) {
					return true
				}
			}
		}
		return false
	})
}

// displayPath returns abs relative to dir if it's inside dir, else abs.
func displayPath(dir, abs string) string {
	if d, err := filepath.Abs(dir); err == nil {
		if rel, err := filepath.Rel(d, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(abs)
}

// inTestdata reports whether we reached abs by walking a requested
// directory through a testdata directory. Fixtures in there are odd on
// purpose, but if the user names a path inside testdata, we lint it as usual.
func inTestdata(abs string, paths []string) bool {
	found := false
	for _, p := range paths {
		rel, err := filepath.Rel(p, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rel == "." {
			return false
		}
		if !slices.Contains(strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/"), "testdata") {
			return false
		}
		found = true
	}
	return found
}

func parseIgnore(s string) (map[string]bool, error) {
	ids := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	return command.ParseRuleIgnore(slices.Values(ids), Rules, func(r *Rule) string { return r.ID }, "code_lint")
}

func listRules(env *command.Env, recommendations bool) {
	env.Format(rulesFormat)
	for _, r := range Rules {
		line := fmt.Sprintf("%s [%s]: %s", r.ID, r.langList(), r.Description)
		if recommendations {
			line += " " + r.Recommendation
		}
		fmt.Fprintln(env.Stdout, line)
	}
}
