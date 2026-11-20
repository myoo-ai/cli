package command

import (
	"errors"
	"slices"
	"testing"
)

func testCommand() *Command {
	return &Command{
		Name:    "t",
		Usage:   "t <arg>...",
		MinArgs: 1,
		MaxArgs: 2,
		Flags: []Flag{
			{Long: "fix", Kind: OptionalString},
			{Long: "lang", Short: "l", Kind: String, Default: "en"},
			{Long: "include-comments", Short: "c", Kind: Bool},
		},
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantArgs []string
		fix      bool
		fixValue string
		lang     string
		comments bool
	}{
		{name: "positional only", args: []string{"a"}, wantArgs: []string{"a"}, lang: "en"},
		{name: "bare optional", args: []string{"--fix", "a"}, wantArgs: []string{"a"}, fix: true, lang: "en"},
		{name: "optional with value", args: []string{"a", "--fix=1,2"}, wantArgs: []string{"a"}, fix: true, fixValue: "1,2", lang: "en"},
		{name: "string separate value", args: []string{"--lang", "de", "a"}, wantArgs: []string{"a"}, lang: "de"},
		{name: "short string inline", args: []string{"-l=sk", "a"}, wantArgs: []string{"a"}, lang: "sk"},
		{name: "interleaved", args: []string{"x.y->a", "b", "-c"}, wantArgs: []string{"x.y->a", "b"}, comments: true, lang: "en"},
		{name: "double dash short", args: []string{"--c", "a"}, wantArgs: []string{"a"}, comments: true, lang: "en"},
		{name: "terminator", args: []string{"--", "-c"}, wantArgs: []string{"-c"}, lang: "en"},
		{name: "lone dash is positional", args: []string{"-"}, wantArgs: []string{"-"}, lang: "en"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := Parse(testCommand(), tt.args)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.args, err)
			}
			if !slices.Equal(in.Args, tt.wantArgs) {
				t.Errorf("Args = %q; want %q", in.Args, tt.wantArgs)
			}
			if got := in.Bool("fix"); got != tt.fix {
				t.Errorf("Bool(fix) = %v; want %v", got, tt.fix)
			}
			if got := in.String("fix"); got != tt.fixValue {
				t.Errorf("String(fix) = %q; want %q", got, tt.fixValue)
			}
			if got := in.String("lang"); got != tt.lang {
				t.Errorf("String(lang) = %q; want %q", got, tt.lang)
			}
			if got := in.Bool("include-comments"); got != tt.comments {
				t.Errorf("Bool(include-comments) = %v; want %v", got, tt.comments)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag suggests", args: []string{"a", "--fx"}, want: "unknown flag `--fx`, did you mean `--fix`?"},
		{name: "missing value", args: []string{"a", "--lang"}, want: "flag `--lang` needs a value"},
		{name: "bool with value", args: []string{"a", "-c=yes"}, want: "flag `--include-comments` does not take a value"},
		{name: "too few", args: nil, want: "missing arguments, usage: myoo t <arg>..."},
		{name: "too many", args: []string{"a", "b", "c"}, want: "unexpected argument `c`, usage: myoo t <arg>..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(testCommand(), tt.args)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded; want error %q", tt.args, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Parse(%q) error = %q; want %q", tt.args, err, tt.want)
			}
			if ExitCode(err) != 2 {
				t.Errorf("ExitCode = %d; want 2", ExitCode(err))
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(ErrViolations); got != 1 {
		t.Errorf("ExitCode(ErrViolations) = %d; want 1", got)
	}
	if got := ExitCode(errors.New("boom")); got != 1 {
		t.Errorf("ExitCode(plain) = %d; want 1", got)
	}
	if got := ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d; want 0", got)
	}
}

func TestLocation(t *testing.T) {
	tests := []struct {
		path                   string
		line, col, eLine, eCol int
		want                   string
	}{
		{"maze.py", 2, 1, 10, 4, "maze.py:2:1-10.4"},
		{"a/b.go", 3, 5, 3, 9, "a/b.go:3:5-9"},
		{"", 1, 1, 1, 22, "1:1-22"},
	}
	for _, tt := range tests {
		if got := Location(tt.path, tt.line, tt.col, tt.eLine, tt.eCol); got != tt.want {
			t.Errorf("Location(%q, %d, %d, %d, %d) = %q; want %q", tt.path, tt.line, tt.col, tt.eLine, tt.eCol, got, tt.want)
		}
	}
	if got := Point("main.py", 1, 6); got != "main.py:1:6" {
		t.Errorf("Point = %q; want %q", got, "main.py:1:6")
	}
}
