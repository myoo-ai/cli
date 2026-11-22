package codelint

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/likelihood"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// lintIn runs code_lint in dir with args and returns stdout, stderr and the
// error. "-nf" sets Env.NoFormat, like the dispatcher does.
func lintIn(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := &command.Env{Stdout: &stdout, Stderr: &stderr, Dir: dir}
	var rest []string
	for _, a := range args {
		if a == "-nf" {
			env.NoFormat = true
			continue
		}
		rest = append(rest, a)
	}
	cmd := Command()
	inv, err := command.Parse(cmd, rest)
	if err != nil {
		return "", "", err
	}
	err = cmd.Run(env, inv)
	return stdout.String(), stderr.String(), err
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGolden(t *testing.T) {
	for _, lang := range []string{"python", "go", "javascript", "typescript", "java", "csharp", "c", "cpp", "php", "rust"} {
		t.Run(lang, func(t *testing.T) {
			out, stderr, err := lintIn(t, fixture(t, lang))
			if !errors.Is(err, command.ErrViolations) {
				t.Fatalf("err = %v, want ErrViolations", err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q", stderr)
			}
			out = diagnosticReport(out)
			golden := filepath.Join("testdata", lang+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if out != string(want) {
				t.Errorf("output differs from %s (run with -update to regenerate):\n%s", golden, out)
			}
		})
	}
}

// diagnosticReport strips the recommendation from each finding line, so the
// tests keep the findings and details but don't pin recommendation wording.
func diagnosticReport(out string) string {
	var b strings.Builder
	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")
		_, detail, ok := strings.Cut(line, " - ")
		if !ok || strings.HasPrefix(line, "# format: ") {
			continue
		}
		id, _, _ := strings.Cut(detail, ": ")
		if r := ruleByID(id); r != nil {
			line = strings.TrimSuffix(line, " "+r.Recommendation)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestNegativesClean(t *testing.T) {
	for _, lang := range []string{"python", "go", "javascript", "typescript", "java", "csharp", "c", "cpp", "php", "rust"} {
		t.Run(lang, func(t *testing.T) {
			out, _, _ := lintIn(t, fixture(t, lang), "-nf")
			for line := range strings.Lines(out) {
				if strings.Contains(strings.ToLower(line), "negative.") {
					t.Errorf("negative fixture reported: %s", line)
				}
			}
		})
	}
}

func TestReadmeMaze(t *testing.T) {
	out, _, err := lintIn(t, fixture(t, "python"), "maze.py")
	if !errors.Is(err, command.ErrViolations) {
		t.Fatalf("err = %v, want ErrViolations", err)
	}
	want := "# format: " + lintFormat + "\n" +
		"#1 maze.py:2:1-10.4 - comments-in-file-header: use of docstring/explanatory comments in file header. in general, explanations should be part of the documentation outside of the code itself (external applications like Notion or wikis, or markdown files), however there are sometimes cases where this is warranted (eg. for purposes of generating an automated documentation), in that case it is acceptable to ignore this. "
	violations, evidence, ok := strings.Cut(out, "# format: "+likelihood.EvidenceFormat+"\n")
	if !ok || !strings.HasPrefix(violations, want) || strings.Count(violations, "\n") != 2 ||
		!strings.HasPrefix(evidence, "-2.20 prior: ") || !strings.HasSuffix(evidence, "\n(likely) AI patterns: 7.2 %\n") {
		t.Errorf("got:\n%s\nwant prefix:\n%s", out, want)
	}
}

func TestSuppression(t *testing.T) {
	out, _, err := lintIn(t, fixture(t, "suppress"), "-nf")
	if !errors.Is(err, command.ErrViolations) {
		t.Fatalf("err = %v, want ErrViolations", err)
	}
	var got []string
	for line := range strings.Lines(out) {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		file, rest, _ := strings.Cut(strings.SplitN(line, " ", 2)[1], " - ")
		id, _, _ := strings.Cut(rest, ":")
		got = append(got, file+" "+id)
	}
	want := []string{
		"inline.py:8:5-54 step-comment",
		"inline.py:11:12-13 emoji-in-code",
		"whole.py:3:10-11 emdash-in-code",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestClean(t *testing.T) {
	// A single short docstring is next to no evidence, so the estimate
	// should stay near the prior.
	evidence := "-2.20 prior: before any evidence\n" +
		"-0.03 possessives: 0 possessives (\"the inbox's\", \"own\") in 10 comment words of files commented in English\n" +
		"-0.02 comment prose patterns: 0 comment-prose violations in 10 comment words\n" +
		"+0.01 human markers: 0 TODO/FIXME/XXX/HACK markers, 0 questions and 0 first-person words and interjections in 10 comment words of files commented in English\n" +
		"(likely) AI patterns: 9.6 %\n"
	for _, tt := range []struct {
		args []string
		want string
	}{
		{nil, "no violations found\n# format: " + likelihood.EvidenceFormat + "\n" + evidence},
		{[]string{"-nf"}, "no violations found\n" + evidence},
	} {
		out, stderr, err := lintIn(t, fixture(t, "clean"), tt.args...)
		if err != nil || stderr != "" {
			t.Fatalf("err = %v, stderr = %q", err, stderr)
		}
		if out != tt.want {
			t.Errorf("args %v: got %q, want %q", tt.args, out, tt.want)
		}
	}
}

func TestIgnoreFlag(t *testing.T) {
	out, _, _ := lintIn(t, fixture(t, "python"), "-nf", "--ignore=step-comment, emoji-in-code")
	for _, id := range []string{"step-comment", "emoji-in-code"} {
		if strings.Contains(out, " - "+id+":") {
			t.Errorf("ignored rule %s reported", id)
		}
	}
	if !strings.Contains(out, " - narrating-comment:") {
		t.Error("other rules were dropped too")
	}
	tests := []struct {
		arg, want string
	}{
		{"--ignore=step-coment", "unknown rule `step-coment` (did you mean `step-comment`?)"},
		{"--ignore=nonsense", "unknown rule `nonsense`, see"},
		{"--ignore=\tstep-comment", "unknown rule `\tstep-comment`"},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			_, _, err := lintIn(t, fixture(t, "clean"), tt.arg)
			if _, ok := errors.AsType[*command.UsageError](err); !ok || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want usage error containing %q", err, tt.want)
			}
		})
	}
}

// TestIgnoreGlobs checks the default and the user patterns of -i. Test
// fixtures should be skipped if they aren't negated, and user patterns add
// to that.
func TestIgnoreGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"fixture/sample.py", "src/app.py", "legacy/old.py"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x = \"done \u2705\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	linted := func(args ...string) []string {
		out, _, _ := lintIn(t, dir, append([]string{"-nf"}, args...)...)
		var files []string
		for line := range strings.Lines(out) {
			if strings.HasPrefix(line, "#") {
				files = append(files, strings.SplitN(strings.Fields(line)[1], ":", 2)[0])
			}
		}
		return files
	}
	tests := []struct {
		args []string
		want []string
	}{
		{nil, []string{"legacy/old.py", "src/app.py"}},
		{[]string{"-i", "legacy/"}, []string{"src/app.py"}},
		{[]string{"--ignore-globs=legacy/**, !fixture/"}, []string{"fixture/sample.py", "src/app.py"}},
		{[]string{"-i", "*.py", "fixture/sample.py"}, []string{"fixture/sample.py"}},
	}
	for _, tt := range tests {
		if got := linted(tt.args...); !slices.Equal(got, tt.want) {
			t.Errorf("%q: linted %q, want %q", tt.args, got, tt.want)
		}
	}
	// A malformed pattern would skip nothing, so we want a usage error.
	_, _, err := lintIn(t, dir, "-i", "src/[ab")
	if _, ok := errors.AsType[*command.UsageError](err); !ok || !strings.Contains(err.Error(), "malformed pattern `src/[ab`") {
		t.Errorf("malformed pattern: err = %v, want a usage error naming it", err)
	}
}

func TestListRules(t *testing.T) {
	out, _, err := lintIn(t, ".", "--list-rules")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if lines[0] != "# format: "+rulesFormat {
		t.Errorf("header = %q", lines[0])
	}
	if len(lines) != len(Rules)+1 {
		t.Fatalf("%d lines for %d rules", len(lines), len(Rules))
	}
	if want := "import-inside-function [python]: import statement inside a function body."; lines[1+ruleByID("import-inside-function").order] != want {
		t.Errorf("got %q, want %q", lines[1+ruleByID("import-inside-function").order], want)
	}
	if strings.Contains(out, Rules[0].Recommendation) {
		t.Error("recommendations printed without --include-recommendations")
	}
	out, _, _ = lintIn(t, ".", "--list-rules", "--include-recommendations")
	for _, r := range Rules {
		if !strings.Contains(out, r.ID+" ["+r.langList()+"]: "+r.Description+" "+r.Recommendation+"\n") {
			t.Errorf("rule %s missing its recommendation", r.ID)
		}
	}
}

func TestWalk(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.py":             "# Step 1: Report the value to the user\nx = 1\n",
		"testdata/fixture.py": "# Step 1: Skip this fixture during the walk\nx = 1\n",
		"README.md":           "# Step 1: not code\n",
	}
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, _, _ := lintIn(t, dir, "-nf")
	if !strings.Contains(out, "#1 main.py:1:1-39 - step-comment") || strings.Contains(out, "fixture.py") {
		t.Errorf("directory walk:\n%s", out)
	}
	out, _, _ = lintIn(t, dir, "-nf", "testdata")
	if !strings.Contains(out, "#1 testdata/fixture.py:1:1") {
		t.Errorf("explicit testdata path not linted:\n%s", out)
	}
	_, stderr, _ := lintIn(t, dir, "README.md")
	if !strings.HasPrefix(stderr, "warning: ") || !strings.Contains(stderr, "unsupported file type") {
		t.Errorf("stderr = %q", stderr)
	}
	out, _, _ = lintIn(t, filepath.Join(dir, "testdata"), "-nf", filepath.Join(dir, "main.py"))
	if !strings.Contains(out, "#1 "+filepath.ToSlash(filepath.Join(dir, "main.py"))+":1:1") {
		t.Errorf("file outside the working directory is not shown with an absolute path:\n%s", out)
	}
}

func TestIsDecorativeLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"==========", true},
		{"----", true},
		{"===== Helpers =====", true},
		{"\u2500\u2500 Section \u2500\u2500", true},
		{"Helpers ------", true},
		{"***********", true},
		{"---", false},
		{"+------+     +------+", false},
		{"a -> b", false},
		{"-*- coding: utf-8 -*-", false},
		{"| --- | --- |", false},
		{"== x", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := isDecorativeLine(tt.line); got != tt.want {
				t.Errorf("isDecorativeLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestIdentWords(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"getHTTPServer_v2", "get http server v 2"},
		{"get_user", "get user"},
		{"GetUser", "get user"},
		{"URL", "url"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := strings.Join(identWords(tt.id), " "); got != tt.want {
				t.Errorf("identWords(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

// aiPercent returns the number of the "(likely) AI patterns" line.
func aiPercent(t *testing.T, out string) float64 {
	t.Helper()
	_, last, ok := strings.Cut(out, "(likely) AI patterns: ")
	if !ok {
		t.Fatalf("no AI patterns line in %q", out)
	}
	pct, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(last), " %"), 64)
	if err != nil {
		t.Fatal(err)
	}
	return pct
}

// TestLikelihood checks the estimate on the same cache written twice, once
// with the comments people write (we, if, TODO, questions) and once in the
// dense declarative voice of agents, with clauses joined by semicolons.
func TestLikelihood(t *testing.T) {
	dir := fixture(t, "likelihood")
	human, _, _ := lintIn(t, dir, "human")
	ai, _, _ := lintIn(t, dir, "ai")
	if p := aiPercent(t, human); p > 5 {
		t.Errorf("human-voiced code: %.1f %%, want at most 5 %%", p)
	}
	if p := aiPercent(t, ai); p < 90 {
		t.Errorf("agent-voiced code: %.1f %%, want at least 90 %%", p)
	}
	if strings.Contains(human, " - comment-prose:") || !strings.Contains(ai, " - comment-prose:") {
		t.Errorf("comment-prose should flag only the agent-voiced comments:\nhuman:\n%s\nai:\n%s", human, ai)
	}
}

// TestEvidence checks that the evidence section prints its format header,
// the prior and the signals before the estimate, and keeps the exit status.
// If there are no supported files, there's nothing to weigh.
func TestEvidence(t *testing.T) {
	out, _, err := lintIn(t, fixture(t, "likelihood"), "ai")
	if !errors.Is(err, command.ErrViolations) {
		t.Fatalf("err = %v, want ErrViolations", err)
	}
	_, evidence, ok := strings.Cut(out, "# format: "+likelihood.EvidenceFormat+"\n")
	if !ok {
		t.Fatalf("no evidence format header in:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(evidence), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "-2.20 prior: ") || !strings.Contains(evidence, " comment voice: ") ||
		!strings.Contains(evidence, " comment prose patterns: ") || !strings.HasPrefix(lines[len(lines)-1], "(likely) AI patterns: ") {
		t.Errorf("evidence lines = %q", lines)
	}
	if out, _, _ = lintIn(t, t.TempDir()); out != "no violations found\n(likely) AI patterns: 0 %\n" {
		t.Errorf("empty directory: got %q", out)
	}
}

// TestSpreadFiles checks that a file reads as AI written when its marked
// comments run through the file, and not when the same number of them sit
// together at its top.
func TestSpreadFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, bodies []int, marked []bool) {
		var b strings.Builder
		b.WriteString("package p\n\n")
		for i, n := range bodies {
			if marked[i] {
				fmt.Fprintf(&b, "// f%d returns the buffer's next value.\n", i)
			} else {
				fmt.Fprintf(&b, "// f%d reads the next value from the buffer.\n", i)
			}
			fmt.Fprintf(&b, "func f%d() int {\n", i)
			for j := range n {
				fmt.Fprintf(&b, "\tx%d := %d\n\t_ = x%d\n", j, j, j)
			}
			b.WriteString("\treturn 0\n}\n\n")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("spread.go", []int{4, 4, 4, 4, 4, 4}, []bool{true, false, true, false, true, false})
	write("bunched.go", []int{0, 0, 0, 8, 8, 8}, []bool{true, true, true, false, false, false})
	out, _, _ := lintIn(t, dir, "-nf")
	_, line, ok := strings.Cut(out, " files with AI-style comments throughout: ")
	if !ok {
		t.Fatalf("no spread evidence in:\n%s", out)
	}
	line, _, _ = strings.Cut(line, "\n")
	if !strings.HasPrefix(line, "1 of 2 files ") || !strings.HasSuffix(line, ": spread.go") {
		t.Errorf("spread evidence = %q, want only spread.go flagged", line)
	}
}

// TestCommentLanguage checks that the signals we fit on English comments
// (human markers, possessives and spread) skip files commented in other
// languages, with or without diacritics, and count a short TODO in a file
// commented in English, which is too short to tell its language on its own.
func TestCommentLanguage(t *testing.T) {
	lint := func(comments ...string) string {
		dir := t.TempDir()
		var b strings.Builder
		b.WriteString("package p\n\n")
		for i, c := range comments {
			fmt.Fprintf(&b, "// %s\nfunc f%d() int { return %d }\n\n", c, i, i)
		}
		if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, _ := lintIn(t, dir, "-nf")
		return out
	}
	slovak := lint(
		"TODO: prečo sa vyrovnávacia pamäť niekedy vyprázdni?",
		"vráti ďalšiu hodnotu z vyrovnávacej pamäte",
		"načíta konfiguráciu zo súboru na disku",
		"uloží výsledok do databázy pre neskoršie použitie",
		"skontroluje, či používateľ má prístup k schránke",
		"odošle správu všetkým členom tímu",
	)
	// German written without umlauts, with the English function word "an".
	german := lint(
		"TODO: warum wird der Puffer manchmal geleert?",
		"gibt den naechsten Wert aus dem Puffer zurueck",
		"laedt die Konfiguration aus der Datei auf der Platte",
		"speichert das Ergebnis in der Datenbank",
		"prueft ob der Benutzer Zugriff auf das Postfach hat",
		"sendet eine Nachricht an alle Mitglieder des Teams",
	)
	for lang, out := range map[string]string{"Slovak": slovak, "German": german} {
		for _, signal := range []string{" human markers: ", " possessives: ", " files with AI-style comments throughout: "} {
			if strings.Contains(out, signal) {
				t.Errorf("%s comments weighed by%s\n%s", lang, strings.TrimSuffix(signal, ": "), out)
			}
		}
	}
	english := lint(
		"TODO: should probably return here",
		"f1 reads the next value from the buffer.",
		"f2 loads the config from a file on disk.",
	)
	if !strings.Contains(english, " human markers: 1 TODO/FIXME/XXX/HACK markers, ") {
		t.Errorf("short TODO in an English file not counted:\n%s", english)
	}
}

// TestStringProse checks that string-prose reads strings that are sentences
// of running prose, placeholders included. It leaves alone error chains that
// aren't sentences, SQL, CSS classes and short labels, even when they have a
// colon or semicolon.
func TestStringProse(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n)\n\n" +
		"var errSave = errors.New(\"Saving the draft failed; the server did not answer in time.\")\n" +
		"var copied = fmt.Sprintf(\"Copied %d files to %s; the rest of them were duplicates.\", 1, \"x\")\n" +
		"var errOpen = fmt.Errorf(\"open the config file of %s: the file was removed by another process %w\", \"x\", errSave)\n" +
		"const query = \"SELECT id, name FROM users WHERE id = ? AND deleted_at IS NULL; DELETE FROM sessions\"\n" +
		"const classes = \"group flex h-12 cursor-pointer items-center gap-3 border-b px-3; hidden\"\n" +
		"const label = \"Save; close\"\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ := lintIn(t, dir, "-nf")
	var lines []string
	for l := range strings.Lines(out) {
		if _, rest, ok := strings.Cut(l, " p.go:"); ok && strings.Contains(l, " - string-prose: ") {
			line, _, _ := strings.Cut(rest, ":")
			lines = append(lines, line)
		}
	}
	if !slices.Equal(lines, []string{"8", "9"}) {
		t.Errorf("string-prose reported on lines %v, want [8 9]:\n%s", lines, out)
	}
}

// TestLicenseNotice checks that we don't lint a license notice written as
// "//" lines as prose, even if only some of its lines mention a license.
func TestLicenseNotice(t *testing.T) {
	dir := t.TempDir()
	src := "// Copyright (C) 2021 Someone\n" +
		"//\n" +
		"// This program is distributed in the hope that it will be useful,\n" +
		"// but WITHOUT ANY WARRANTY; without even the implied warranty of\n" +
		"// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.\n\n" +
		"package notice\n"
	if err := os.WriteFile(filepath.Join(dir, "notice.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ := lintIn(t, dir, "-nf")
	if !strings.HasPrefix(out, "no violations found\n") {
		t.Errorf("license notice reported:\n%s", out)
	}
}
