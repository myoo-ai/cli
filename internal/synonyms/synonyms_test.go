package synonyms

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/prose"
	"github.com/myoo-ai/cli/internal/thesaurus"
)

var update = flag.Bool("update", false, "rewrite golden files")

// setupDB builds the English fixture database into a temporary cache.
func setupDB(t *testing.T) {
	t.Helper()
	t.Setenv("MYOO_CACHE_DIR", t.TempDir())
	thes, err := os.Open(filepath.Join("testdata", "en.dat"))
	if err != nil {
		t.Fatal(err)
	}
	defer thes.Close()
	freq, err := os.Open(filepath.Join("testdata", "en_freq.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer freq.Close()
	path, err := thesaurus.Path("en")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := thesaurus.Build(&buf, "en", thes, freq); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, cmd *command.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	inv, err := command.Parse(cmd, args)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	env := &command.Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &out}
	err = cmd.Run(env, inv)
	return out.String(), err
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	if strings.HasPrefix(got, "# format: ") {
		_, got, _ = strings.Cut(got, "\n")
	}
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGolden(t *testing.T) {
	setupDB(t)
	tests := []struct {
		name   string
		cmd    *command.Command
		input  string
		args   []string
		golden string
	}{
		{"word tree of the release notes", WordTreeCommand(), "release.txt", nil, "word_tree_release.golden"},
		{"word tree of the letter", WordTreeCommand(), "letter.txt", []string{"--lang=en"}, "word_tree_letter.golden"},
		{"word tree of given words", WordTreeCommand(), "", []string{"--word=publish, Happens,children"}, "word_tree_words.golden"},
		{"rewrite of the release notes", RewriteCommand(), "release.txt", nil, "rewrite_release.golden"},
		{"rewrite of the letter", RewriteCommand(), "letter.txt", []string{"-l", "en"}, "rewrite_letter.golden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ""
			if tt.input != "" {
				input = readFixture(t, tt.input)
			}
			got, err := run(t, tt.cmd, input, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, tt.golden, got)
		})
	}
}

func TestOutputProse(t *testing.T) {
	setupDB(t)
	for _, cmd := range []*command.Command{WordTreeCommand(), RewriteCommand()} {
		t.Run(cmd.Name, func(t *testing.T) {
			out, err := run(t, cmd, "publish")
			if err != nil {
				t.Fatal(err)
			}
			if findings, err := run(t, prose.Command(), out); err != nil {
				t.Fatalf("output failed prose_lint:\n%s", findings)
			}
		})
	}
}

func TestRewritePreservesText(t *testing.T) {
	setupDB(t)
	db, err := thesaurus.Open(t.Context(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tests := []struct{ name, in, want string }{
		{"punctuation and spacing", "  Hello,\tworld!\r\n", "  [Hi] (howdy, hullo),\tworld!\r\n"},
		{"capitalized", "Publish it", "[Print] (issue, release, write) it"},
		{"upper case", "PUBLISH it", "[PRINT] (issue, release, write) it"},
		{"stopwords unchanged", "the and of", "the and of"},
		{"unknown words unchanged", "xyzzy plugh", "xyzzy plugh"},
		{"inline code unchanged", "`publish` and publish", "`publish` and [print] (issue, release, write)"},
		{"fenced code unchanged", "```\npublish\n```\n", "```\npublish\n```\n"},
		{"paths and urls unchanged", "see https://publish.example/publish or ./publish/x", "see https://publish.example/publish or ./publish/x"},
		{"identifiers unchanged", "publishRelease publish_x publish2", "publishRelease publish_x publish2"},
		{"no trailing newline", "news", "[word] (tidings, intelligence)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rewrite(db, tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("rewrite(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRewriteNeverProposesItself(t *testing.T) {
	setupDB(t)
	got, err := run(t, RewriteCommand(), "We will end it and end up ending.")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "[end]") {
		t.Errorf("word proposed as its own synonym: %q", got)
	}
}

func TestMatchCase(t *testing.T) {
	tests := []struct{ orig, repl, want string }{
		{"hello", "hi", "hi"},
		{"Hello", "hi", "Hi"},
		{"HELLO", "hi", "HI"},
		{"A", "one", "One"},
		{"Ärger", "ärgernis", "Ärgernis"},
		{"hello", "Paris", "Paris"},
		{"ÉTÉ", "saison", "SAISON"},
	}
	for _, tt := range tests {
		t.Run(tt.orig, func(t *testing.T) {
			if got := matchCase(tt.orig, tt.repl); got != tt.want {
				t.Errorf("matchCase(%q, %q) = %q, want %q", tt.orig, tt.repl, got, tt.want)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"positions in code points", "é ab\n  cd", []string{"é@1:1", "ab@1:3", "cd@2:3"}},
		{"inner apostrophes and hyphens", "don't read-only 'quoted' -dash-", []string{"don't@1:1", "read-only@1:7", "quoted@1:18"}},
		{"typographic apostrophe", "it\u2019s", []string{"it\u2019s@1:1"}},
		{"code spans", "a `b c` d ``e ` f`` g", []string{"a@1:1", "d@1:9", "g@1:21"}},
		{"unmatched backtick is literal", "a ` b", []string{"a@1:1", "b@1:5"}},
		{"tilde fenced block", "a\n~~~\npublish news\n~~~\nb", []string{"a@1:1", "b@5:1"}},
		{"code-like chunks", "x.go user@host a/b --flag k=v $HOME e.g. end.", []string{"end@1:42"}},
		{"identifiers", "camelCase snake_case abc123 Title UPPER", []string{"Title@1:29", "UPPER@1:35"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, tok := range tokenize(tt.in) {
				got = append(got, fmt.Sprintf("%s@%d:%d", tok.text, tok.line, tok.col))
				if tt.in[tok.start:tok.end] != tok.text {
					t.Errorf("offsets of %q are wrong", tok.text)
				}
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("tokenize(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	setupDB(t)
	tests := []struct {
		name    string
		cmd     *command.Command
		args    []string
		usage   bool
		message string
	}{
		{"unknown language", WordTreeCommand(), []string{"--lang=xx"}, true, "unsupported language `xx`, supported: ar, bg"},
		{"unknown language rewrite", RewriteCommand(), []string{"--lang=klingon"}, true, "unsupported language `klingon`"},
		{"missing word", WordTreeCommand(), []string{"--word=publish,xyzzy"}, false, "unable to find synonyms for `xyzzy`"},
		{"empty word list", WordTreeCommand(), []string{"--word= , "}, true, "--word needs at least one word"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := run(t, tt.cmd, "", tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.message)
			}
			if _, usage := errors.AsType[*command.UsageError](err); usage != tt.usage {
				t.Errorf("usage error = %v, want %v", usage, tt.usage)
			}
		})
	}
}
