// Package app dispatches myoo command lines to the registered tools.
package app

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/codegraph"
	"github.com/myoo-ai/cli/internal/codelint"
	"github.com/myoo-ai/cli/internal/command"
	"github.com/myoo-ai/cli/internal/lsp"
	"github.com/myoo-ai/cli/internal/prose"
	"github.com/myoo-ai/cli/internal/synonyms"
)

// Commands returns the top-level command table in help order.
func Commands() []*command.Command {
	return []*command.Command{
		prose.Command(),
		synonyms.WordTreeCommand(),
		synonyms.RewriteCommand(),
		codelint.Command(),
		lsp.Command(),
		{
			Name:        "out",
			Summary:     "outputs readable by humans and agents (rendered diagrams)",
			Usage:       "out <subcommand> ...",
			Subcommands: []*command.Command{codegraph.Command()},
		},
	}
}

// Run executes one command line and returns the process exit code.
func Run(args []string, env *command.Env) int {
	args, env.NoFormat = stripGlobal(args)
	cmds := Commands()
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printOverview(env.Stdout, cmds)
		return 0
	}
	if args[0] == "--version" || args[0] == "-v" || args[0] == "version" {
		fmt.Fprintf(env.Stdout, "myoo %s\n", env.Version)
		return 0
	}
	path, cmd, rest, err := resolve(cmds, args)
	if err != nil {
		return fail(env, err)
	}
	if wantsHelp(rest) || len(cmd.Subcommands) > 0 && len(rest) == 0 {
		printHelp(env.Stdout, path, cmd)
		return 0
	}
	if cmd.Run == nil {
		return fail(env, command.Usagef("missing subcommand, usage: myoo %s", cmd.Usage))
	}
	inv, err := command.Parse(cmd, rest)
	if err != nil {
		return fail(env, err)
	}
	return fail(env, cmd.Run(env, inv))
}

func fail(env *command.Env, err error) int {
	code := command.ExitCode(err)
	if err == nil || code == 0 {
		return code
	}
	if _, silent := errors.AsType[*command.ExitError](err); silent {
		return code
	}
	msg := err.Error()
	if _, ok := errors.AsType[*command.UsageError](err); ok && !strings.Contains(msg, "--help") {
		msg += " (see `myoo --help`)"
	}
	fmt.Fprintf(env.Stderr, "err: %s\n", msg)
	return code
}

// resolve walks command groups until it reaches a runnable command.
func resolve(cmds []*command.Command, args []string) (path []string, cmd *command.Command, rest []string, err error) {
	rest = args
	for {
		name := rest[0]
		var found *command.Command
		for _, c := range cmds {
			if c.Name == name {
				found = c
				break
			}
		}
		if found == nil {
			return nil, nil, nil, command.Usagef("unknown command `%s`%s", strings.Join(append(path, name), " "), suggestCommand(cmds, name))
		}
		path, cmd, rest = append(path, name), found, rest[1:]
		if len(found.Subcommands) == 0 || len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
			return path, cmd, rest, nil
		}
		cmds = found.Subcommands
	}
}

func suggestCommand(cmds []*command.Command, name string) string {
	return command.Suggest(name, cmds, func(c *command.Command) string { return c.Name }, ", did you mean `%s`?")
}

func stripGlobal(args []string) ([]string, bool) {
	noFormat := false
	out := make([]string, 0, len(args))
	for i, a := range args {
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		switch a {
		case "-nf", "--nf", "--no-format", "-no-format":
			noFormat = true
		default:
			out = append(out, a)
		}
	}
	return out, noFormat
}

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func printOverview(w io.Writer, cmds []*command.Command) {
	fmt.Fprint(w, "myoo - command line tools for AI agents\n\nusage: myoo <command> [flags] [-nf/--no-format]\n\ncommands:\n")
	width := 0
	for _, c := range cmds {
		for _, line := range commandLines(c, "") {
			width = max(width, len(line[0]))
		}
	}
	for _, c := range cmds {
		for _, line := range commandLines(c, "") {
			fmt.Fprintf(w, "  %-*s  %s\n", width, line[0], line[1])
		}
	}
	fmt.Fprint(w, "\nglobal flags:\n  -nf, --no-format  omit the \"# format: ...\" line from outputs\n  -h, --help        show help for any command\n  --version         print the version\n")
}

func commandLines(c *command.Command, prefix string) [][2]string {
	name := strings.TrimSpace(prefix + " " + c.Name)
	if len(c.Subcommands) == 0 {
		return [][2]string{{name, c.Summary}}
	}
	var lines [][2]string
	for _, s := range c.Subcommands {
		lines = append(lines, commandLines(s, name)...)
	}
	return lines
}

func printHelp(w io.Writer, path []string, c *command.Command) {
	if len(c.Subcommands) > 0 {
		fmt.Fprintf(w, "usage: myoo %s\n\n%s\n\nsubcommands:\n", c.Usage, c.Summary)
		for _, s := range c.Subcommands {
			fmt.Fprintf(w, "  %-12s %s\n", s.Name, s.Summary)
		}
		if c.Help != "" {
			fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(c.Help))
		}
		fmt.Fprintf(w, "\nrun `myoo %s <subcommand> --help` for details\n", strings.Join(path, " "))
		return
	}
	fmt.Fprintf(w, "usage: myoo %s\n\n%s\n", c.Usage, c.Summary)
	if c.Help != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(c.Help))
	}
	flags := slices.Clone(c.Flags)
	if len(flags) == 0 {
		return
	}
	fmt.Fprint(w, "\nflags:\n")
	labels := make([]string, len(flags))
	width := 0
	for i, f := range flags {
		labels[i] = flagLabel(f)
		width = max(width, len(labels[i]))
	}
	for i, f := range flags {
		usage := f.Usage
		if f.Default != "" {
			usage += fmt.Sprintf(" (default %s)", f.Default)
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, labels[i], usage)
	}
}

func flagLabel(f command.Flag) string {
	var b strings.Builder
	if f.Short != "" {
		b.WriteString("-" + f.Short + ", ")
	}
	b.WriteString("--" + f.Long)
	value := f.Value
	if value == "" {
		value = "value"
	}
	switch f.Kind {
	case command.String:
		b.WriteString("=<" + value + ">")
	case command.OptionalString:
		b.WriteString("[=<" + value + ">]")
	case command.Bool:
	}
	return b.String()
}
