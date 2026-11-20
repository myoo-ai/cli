// Package prose implements prose_lint, a best-effort linter for patterns
// that make written text read as AI generated.
package prose

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/likelihood"
)

const (
	lintFormat  = "`#<nth violation> [<file>:]<line>:<column>-[<end line>.]<end column> - <rule ID>: <description + proposed fix (if applicable)>`"
	rulesFormat = "`<rule ID>[ (manual)]: <description>`"
)

// Command returns the prose_lint command.
func Command() *command.Command {
	return &command.Command{
		Name:    "prose_lint",
		Summary: "lint prose (written text) for generic AI patterns",
		Usage:   "prose_lint [file] [--fix[=<n,...>]] [--ignore=<rule,...>] [--lang=<code>] [--list-rules [--include-recommendations]]",
		Help: `Reads the text from the file argument or stdin and prints one line per
violation, the evidence of AI authorship, then the estimated likelihood that
the text was written by AI ("(likely) AI patterns: <n> %"). Exits with status
1 when violations were found. With a file argument every location starts
with the file path, so terminals such as VS Code's open it at the violation
on cmd/ctrl+click.

The likelihood combines the density of the scored violations, the density
of bold in markdown, and for English the voice of the text: AI prose is
dense and declarative, people write "we", "if", "will", "to". Each evidence
line shows what a signal counted and how far it moved the estimate. Short
texts give little evidence and stay near 10 %.

The language comes from the most frequent function words of the text
unless --lang names it. The word-based rules exist for English, Czech,
Dutch, French, German, Italian, Polish, Portuguese, Russian, Slovak and
Spanish, the rest run on text in any language.

--fix prints the input with every automatically fixable violation fixed;
--fix=1,2 fixes only those violations (numbers from a plain run on the same
input). Rules marked (manual) in --list-rules need human or agent judgment.`,
		MaxArgs: 1,
		Flags: []command.Flag{
			{Long: "fix", Kind: command.OptionalString, Value: "n,...", Usage: "print the fixed text instead of violations (optionally only the listed violations)"},
			{Long: "ignore", Kind: command.String, Value: "rule,...", Usage: "disable rules"},
			{Long: "lang", Kind: command.String, Value: "code", Default: "auto", Usage: "language of the prose, auto detects it from the text"},
			{Long: "list-rules", Kind: command.Bool, Usage: "list all rules"},
			{Long: "include-recommendations", Kind: command.Bool, Usage: "with --list-rules, also print how to fix each rule"},
		},
		Run: run,
	}
}

var langRe = regexp.MustCompile(`^[a-z]{2,3}$`)

func run(env *command.Env, inv *command.Invocation) error {
	if inv.Bool("list-rules") {
		listRules(env, inv.Bool("include-recommendations"))
		return nil
	}
	lang := inv.String("lang")
	if lang != "auto" && !langRe.MatchString(lang) {
		return command.Usagef("invalid --lang `%s`, expected auto or a language code such as en, de, sk", lang)
	}
	ignored, err := parseIgnore(inv.String("ignore"))
	if err != nil {
		return err
	}
	src, err := readInput(env, inv.Args)
	if err != nil {
		return err
	}
	t := newText(src)
	if lang == "auto" {
		lang = detectLang(t.words())
	}
	vs := Lint(t, lang, ignored)
	if inv.Bool("fix") {
		out, err := applyFixes(t, vs, inv.String("fix"))
		if err != nil {
			return err
		}
		_, err = io.WriteString(env.Stdout, out)
		return err
	}
	ev, words := aiEvidence(t, lang, vs)
	if len(vs) == 0 {
		fmt.Fprintln(env.Stdout, "no violations found")
	} else {
		env.Format(lintFormat)
		file := ""
		if len(inv.Args) > 0 && inv.Args[0] != "-" {
			file = inv.Args[0]
		}
		for i, v := range vs {
			sl, sc := t.pos(v.Start)
			el, ec := t.pos(v.End)
			desc := v.Rule.Description
			if v.Detail != "" {
				desc += fmt.Sprintf(" (`%s`).", v.Detail)
			}
			fmt.Fprintf(env.Stdout, "#%d %s - %s: %s %s\n", i+1, command.Location(file, sl, sc, el, ec), v.Rule.ID, desc, v.Rule.Recommendation)
		}
	}
	pct := 0.0 // with no words there's nothing to read as AI
	if words > 0 {
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

func readInput(env *command.Env, args []string) (string, error) {
	if len(args) == 0 || args[0] == "-" {
		data, err := io.ReadAll(env.Stdin)
		if err != nil {
			return "", fmt.Errorf("unable to read stdin: %w", err)
		}
		return string(data), nil
	}
	path := args[0]
	if !filepath.IsAbs(path) {
		path = filepath.Join(env.Dir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unable to read `%s`: %w", args[0], err)
	}
	return string(data), nil
}

func parseIgnore(s string) (map[string]bool, error) {
	ids := func(yield func(string) bool) {
		for id := range strings.SplitSeq(s, ",") {
			id = strings.TrimSpace(id)
			if id != "" && !yield(id) {
				return
			}
		}
	}
	return command.ParseRuleIgnore(ids, Rules, func(r *Rule) string { return r.ID }, "prose_lint")
}

// Lint runs the rules that apply to lang and aren't ignored, drops findings
// inside code, URLs, e-mail addresses and paths, and returns the violations
// sorted by position.
func Lint(t *text, lang string, ignored map[string]bool) []Violation {
	t.lang = lang
	var vs []Violation
	for _, r := range Rules {
		if ignored[r.ID] || !r.appliesTo(lang) || r.Comment && !t.comment {
			continue
		}
		for _, v := range r.detect(t) {
			if !r.Unmasked && t.masked(span{v.Start, v.End}) {
				continue
			}
			v.Rule = r
			vs = append(vs, v)
		}
	}
	slices.SortStableFunc(vs, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.End, b.End))
	})
	return vs
}

// LintString is Lint on src with all rules enabled except the ProseOnly
// and Comment ones. code_lint runs it on docstrings.
func LintString(src, lang string) []Violation { return Lint(newText(src), lang, proseOnlyRules) }

// LintComment is LintString plus the Comment rules. code_lint runs it on
// comments.
func LintComment(src, lang string) []Violation {
	t := newText(src)
	t.comment = true
	return Lint(t, lang, proseOnlyRules)
}

var proseOnlyRules = func() map[string]bool {
	m := map[string]bool{}
	for _, r := range Rules {
		if r.ProseOnly {
			m[r.ID] = true
		}
	}
	return m
}()

// applyFixes returns the input with the fixes of the selected violations
// applied. If selection is "", we fix all fixable violations and skip a fix
// that overlaps an earlier one. Otherwise it's a comma list of 1-based
// violation numbers, and they all have to be fixable.
func applyFixes(t *text, vs []Violation, selection string) (string, error) {
	type pick struct {
		n int
		v Violation
	}
	var picks []pick
	if selection == "" {
		for i, v := range vs {
			if v.Fix != nil {
				picks = append(picks, pick{i + 1, v})
			}
		}
	} else {
		seen := map[int]bool{}
		for f := range strings.SplitSeq(selection, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(f), "#")))
			if err != nil {
				return "", command.Usagef("invalid --fix value `%s`, expected violation numbers such as --fix=1,3", selection)
			}
			if n < 1 || n > len(vs) {
				return "", fmt.Errorf("violation #%d does not exist, the input has %d violations", n, len(vs))
			}
			if vs[n-1].Fix == nil {
				return "", fmt.Errorf("violation #%d (%s) has no automatic fix, edit it by hand", n, vs[n-1].Rule.ID)
			}
			if !seen[n] {
				seen[n] = true
				picks = append(picks, pick{n, vs[n-1]})
			}
		}
	}
	type edit struct {
		Edit
		n int
	}
	var edits []edit
	for _, p := range picks {
		for _, e := range p.v.Fix {
			edits = append(edits, edit{e, p.n})
		}
	}
	slices.SortStableFunc(edits, func(a, b edit) int { return cmp.Compare(a.Start, b.Start) })
	var b strings.Builder
	last := 0
	for _, e := range edits {
		if e.Start < last {
			if selection != "" {
				return "", fmt.Errorf("fixes of the selected violations overlap (#%d), fix them in separate runs", e.n)
			}
			continue
		}
		b.WriteString(t.src[last:e.Start])
		b.WriteString(e.Text)
		last = e.End
	}
	b.WriteString(t.src[last:])
	return b.String(), nil
}

func listRules(env *command.Env, recommendations bool) {
	env.Format(rulesFormat)
	for _, r := range Rules {
		manual := ""
		if r.Manual {
			manual = " (manual)"
		}
		desc := r.Description
		if !strings.HasSuffix(desc, ".") && !strings.HasSuffix(desc, ")") {
			desc += "."
		}
		line := fmt.Sprintf("%s%s: %s", r.ID, manual, desc)
		if recommendations {
			line += " " + r.Recommendation
		}
		fmt.Fprintln(env.Stdout, line)
	}
}
