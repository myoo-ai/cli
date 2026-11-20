// Package command defines how the myoo dispatcher talks to the tools. It
// covers the environment they run in, how they declare commands and flags,
// and the errors that decide the process exit code.
package command

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Env is the environment we hand to each command.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Relative paths resolve against Dir.
	Dir string
	// NoFormat turns off the "# format: ..." header (-nf/--no-format).
	NoFormat bool
	Version  string
}

// Format writes the "# format: <spec>" header line, unless the user passed
// -nf/--no-format. We print spec as is, backticks included.
func (e *Env) Format(spec string) {
	if e.NoFormat {
		return
	}
	fmt.Fprintf(e.Stdout, "# format: %s\n", spec)
}

// Point formats a source position as "path:line:col", which terminals and
// editors like VS Code turn into links. If there's no path it's just
// "line:col".
func Point(path string, line, col int) string {
	if path == "" {
		return fmt.Sprintf("%d:%d", line, col)
	}
	return fmt.Sprintf("%s:%d:%d", path, line, col)
}

// Location formats a source range in the link syntax VS Code will select on
// cmd/ctrl+click: "path:line:col-endCol" if it's on a single line, and
// "path:line:col-endLine.endCol" if it spans lines. Columns are 1-based and
// the end column is exclusive, the same as VS Code selections.
func Location(path string, line, col, endLine, endCol int) string {
	if endLine == line {
		return fmt.Sprintf("%s-%d", Point(path, line, col), endCol)
	}
	return fmt.Sprintf("%s-%d.%d", Point(path, line, col), endLine, endCol)
}

// Command declares a tool, or a group of subcommands.
type Command struct {
	Name    string
	Summary string // a line to show in command lists
	// Usage is the synopsis without the leading "myoo ", for example
	// "prose_lint [file] [--fix[=<n,...>]]".
	Usage string
	// Help is optional free text that --help prints after the synopsis.
	Help  string
	Flags []Flag
	// MinArgs and MaxArgs limit the number of positional arguments. If
	// MaxArgs is < 0 there's no upper limit.
	MinArgs, MaxArgs int
	Run              func(env *Env, inv *Invocation) error
	// Subcommands make the command a group (myoo lsp definition ...). A
	// group doesn't have a Run of its own.
	Subcommands []*Command
}

// FlagKind says how a flag takes its value.
type FlagKind uint8

const (
	// Bool flags take no value: --fix.
	Bool FlagKind = iota
	// String flags need a value: --lang=en, --lang en, -l en.
	String
	// OptionalString flags can be bare or take an inline value, but not a
	// separate one: --fix and --fix=1,2.
	OptionalString
)

// Flag declares a command line flag. Long is the canonical name, the one you
// use to read the value back from an Invocation.
type Flag struct {
	Long    string // without dashes, e.g. "no-format"
	Short   string // without the dash, e.g. "nf" (can be longer than a letter)
	Kind    FlagKind
	Value   string // value placeholder for help output, e.g. "lang"
	Default string
	Usage   string
}

// Invocation holds the parsed arguments for a command run.
type Invocation struct {
	Args   []string
	values map[string]string
	set    map[string]bool
	flags  []Flag
}

// Bool reports whether a Bool or OptionalString flag was given.
func (in *Invocation) Bool(long string) bool {
	in.mustKnow(long)
	return in.set[long]
}

// String returns the value of a flag, or the default if it wasn't given. For
// an OptionalString flag given without a value it returns "".
func (in *Invocation) String(long string) string {
	in.mustKnow(long)
	if in.set[long] {
		return in.values[long]
	}
	for _, f := range in.flags {
		if f.Long == long {
			return f.Default
		}
	}
	return ""
}

// Has reports whether the flag was on the command line.
func (in *Invocation) Has(long string) bool {
	in.mustKnow(long)
	return in.set[long]
}

func (in *Invocation) mustKnow(long string) {
	for _, f := range in.flags {
		if f.Long == long {
			return
		}
	}
	panic("command: undeclared flag --" + long)
}

// UsageError reports invalid arguments. The dispatcher will exit with code 2
// and point the caller to --help.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usagef builds a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// ExitError ends the process with Code without printing anything. Linters
// use it to signal they found violations.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// ErrViolations is what linters return if they printed at least one violation.
var ErrViolations error = &ExitError{Code: 1}

// ExitCode maps an error from a command to a process exit code.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := errors.AsType[*ExitError](err); ok {
		return e.Code
	}
	if _, ok := errors.AsType[*UsageError](err); ok {
		return 2
	}
	return 1
}

// Parse parses args against the flags of cmd. Flags and positional arguments
// can be mixed, and "--" ends flag parsing.
func Parse(cmd *Command, args []string) (*Invocation, error) {
	in := &Invocation{values: map[string]string{}, set: map[string]bool{}, flags: cmd.Flags}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			in.Args = append(in.Args, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			in.Args = append(in.Args, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		f, ok := lookupFlag(cmd.Flags, name)
		if !ok {
			return nil, Usagef("unknown flag `%s`%s", a, suggestFlag(cmd.Flags, name))
		}
		switch f.Kind {
		case Bool:
			if hasValue {
				return nil, Usagef("flag `%s` does not take a value", flagName(f))
			}
		case String:
			if !hasValue {
				if i+1 >= len(args) {
					return nil, Usagef("flag `%s` needs a value", flagName(f))
				}
				i++
				value = args[i]
			}
		case OptionalString:
		}
		in.set[f.Long] = true
		in.values[f.Long] = value
	}
	if len(in.Args) < cmd.MinArgs {
		return nil, Usagef("missing arguments, usage: myoo %s", cmd.Usage)
	}
	if cmd.MaxArgs >= 0 && len(in.Args) > cmd.MaxArgs {
		return nil, Usagef("unexpected argument `%s`, usage: myoo %s", in.Args[cmd.MaxArgs], cmd.Usage)
	}
	return in, nil
}

// lookupFlag matches name against the long and the short form, so
// single-dash long names (-fix) and double-dash short names (--nf) work too.
// We need this because agents mix both styles.
func lookupFlag(flags []Flag, name string) (Flag, bool) {
	for _, f := range flags {
		if name == f.Long || (f.Short != "" && name == f.Short) {
			return f, true
		}
	}
	return Flag{}, false
}

func flagName(f Flag) string { return "--" + f.Long }

func suggestFlag(flags []Flag, name string) string {
	var names []string
	long := map[string]string{}
	for _, f := range flags {
		for _, n := range []string{f.Long, f.Short} {
			if n != "" {
				names = append(names, n)
				long[n] = f.Long
			}
		}
	}
	best, ok := Closest(name, names, 2)
	if !ok {
		return ""
	}
	return fmt.Sprintf(", did you mean `--%s`?", long[best])
}

// Closest returns the candidate with the smallest [Distance] to name, if
// that distance is at most maxDist. If there's a tie, the earlier candidate
// wins.
func Closest(name string, candidates []string, maxDist int) (string, bool) {
	return closest(name, candidates, maxDist, func(s string) string { return s })
}

// Distance returns the Levenshtein distance between a and b, in runes.
func Distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
