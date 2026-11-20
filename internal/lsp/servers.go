package lsp

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// registry lists the language servers for each language, in order of
// preference. We use the first one we find on PATH.
var registry = map[code.Lang][][]string{
	code.Go:         {{"gopls"}},
	code.Python:     {{"basedpyright-langserver", "--stdio"}, {"pyright-langserver", "--stdio"}, {"pylsp"}, {"jedi-language-server"}},
	code.JavaScript: {{"typescript-language-server", "--stdio"}, {"vtsls", "--stdio"}},
	code.TypeScript: {{"typescript-language-server", "--stdio"}, {"vtsls", "--stdio"}},
	code.C:          {{"clangd"}},
	code.CPP:        {{"clangd"}},
	code.Java:       {{"jdtls"}},
	code.CSharp:     {{"csharp-ls"}, {"OmniSharp", "-lsp"}},
	code.PHP:        {{"intelephense", "--stdio"}, {"phpactor", "language-server"}},
	code.Rust:       {{"rust-analyzer"}},
}

func findServer(lang code.Lang) ([]string, bool) {
	for _, argv := range registry[lang] {
		if _, err := exec.LookPath(argv[0]); err == nil {
			return argv, true
		}
	}
	return nil, false
}

func serverNames(lang code.Lang) string {
	var names []string
	for _, argv := range registry[lang] {
		names = append(names, argv[0])
	}
	if len(names) == 0 {
		return "a language server"
	}
	return strings.Join(names, ", ")
}

// knownServers describes the registry for the help text.
func knownServers() string {
	var b strings.Builder
	for _, l := range langNames {
		b.WriteString("\n  " + string(l) + ": ")
		for i, argv := range registry[l] {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strings.Join(argv, " "))
		}
	}
	return b.String()
}

// splitCommand splits a command line into arguments and honors single and
// double quotes. A backslash escapes a quote, backslash or space after it.
// Before anything else it's kept as is, so Windows paths work unquoted.
func splitCommand(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quote   rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			if !strings.ContainsRune(`"'\ `, r) {
				cur.WriteRune('\\')
			}
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}
