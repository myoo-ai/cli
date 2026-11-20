package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/command"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		stdin    string
		code     int
		stdout   string // substring
		stderr   string // exact
		noStdout bool
	}{
		{name: "overview", args: nil, stdout: "usage: myoo <command>"},
		{name: "version", args: []string{"--version"}, stdout: "myoo test\n"},
		{name: "unknown command", args: []string{"prose_lnt"}, code: 2, noStdout: true,
			stderr: "err: unknown command `prose_lnt`, did you mean `prose_lint`? (see `myoo --help`)\n"},
		{name: "group help", args: []string{"lsp"}, stdout: "subcommands:"},
		{name: "group without subcommand", args: []string{"out", "--"}, code: 2, noStdout: true,
			stderr: "err: missing subcommand, usage: myoo out <subcommand> ... (see `myoo --help`)\n"},
		{name: "subcommand help", args: []string{"out", "code_graph", "--help"}, stdout: "usage: myoo out code_graph"},
		{name: "global no-format anywhere", args: []string{"prose_lint", "-nf"}, stdin: "Plain text.\n"},
		{name: "format header for violations", args: []string{"prose_lint"}, stdin: "Wait\u2026\n", code: 1, stdout: "# format: "},
		{name: "violations exit 1 silently", args: []string{"prose_lint", "--nf"}, stdin: "Wait\u2026\n", code: 1, stdout: "#1 1:5-6 - unicode-ellipsis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			env := &command.Env{Stdin: strings.NewReader(tt.stdin), Stdout: &out, Stderr: &errOut, Dir: t.TempDir(), Version: "test"}
			code := Run(tt.args, env)
			if code != tt.code {
				t.Errorf("Run(%q) = %d; want %d (stderr %q)", tt.args, code, tt.code, errOut.String())
			}
			if errOut.String() != tt.stderr {
				t.Errorf("stderr = %q; want %q", errOut.String(), tt.stderr)
			}
			if !strings.Contains(out.String(), tt.stdout) {
				t.Errorf("stdout = %q; want it to contain %q", out.String(), tt.stdout)
			}
			if tt.noStdout && out.Len() > 0 {
				t.Errorf("stdout = %q; want nothing", out.String())
			}
			if tt.stdout == "" && !tt.noStdout && tt.code == 0 && strings.Contains(out.String(), "# format:") {
				t.Errorf("format header printed despite -nf: %q", out.String())
			}
		})
	}
}

func TestBuiltinOutputProse(t *testing.T) {
	checkOutputProse(t, []string{"--help"})
	checkCommandProse(t, Commands(), "")
}

func checkCommandProse(t *testing.T, commands []*command.Command, prefix string) {
	t.Helper()
	for _, c := range commands {
		path := strings.TrimSpace(prefix + " " + c.Name)
		t.Run(path, func(t *testing.T) {
			checkOutputProse(t, append(strings.Fields(path), "--help"))
			for _, flag := range c.Flags {
				if flag.Long != "list-rules" {
					continue
				}
				checkOutputProse(t, append(strings.Fields(path), "--list-rules"))
				checkOutputProse(t, append(strings.Fields(path), "--list-rules", "--include-recommendations"))
			}
			checkCommandProse(t, c.Subcommands, path)
		})
	}
}

func checkOutputProse(t *testing.T, args []string) {
	t.Helper()
	var output, diagnostics bytes.Buffer
	env := &command.Env{
		Stdin: strings.NewReader(""), Stdout: &output, Stderr: &diagnostics,
		Dir: t.TempDir(),
	}
	if code := Run(args, env); code != 0 || diagnostics.Len() != 0 {
		t.Fatalf("Run(%q) = %d, stderr %q", args, code, diagnostics.String())
	}
	env.Stdin = &output
	env.Stdout = &diagnostics
	if code := Run([]string{"prose_lint", "-nf"}, env); code != 0 {
		t.Errorf("Run(%q) output failed prose_lint:\n%s", args, diagnostics.String())
	}
}
