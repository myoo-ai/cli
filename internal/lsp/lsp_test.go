package lsp

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/command"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeServerEnv); mode != "" {
		os.Exit(runFakeServer(mode))
	}
	os.Exit(m.Run())
}

// copyFixture copies testdata/<name> into a fresh directory marked as a
// project root and returns it. We replace "${DIR}" in compile_commands.json
// with that directory, and resolve symlinks in the temp dir, like /var on
// macOS, so clangd and myoo see the paths compile_commands.json names.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	dst := realPath(t.TempDir())
	src := filepath.Join("testdata", name)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if d.Name() == "compile_commands.json" {
			quoted, _ := json.Marshal(dst)
			data = bytes.ReplaceAll(data, []byte("${DIR}"), quoted[1:len(quoted)-1])
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dst, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dst
}

func run(dir string, args ...string) (stdout, stderr string, err error) {
	var sub *command.Command
	for _, c := range Command().Subcommands {
		if c.Name == args[0] {
			sub = c
		}
	}
	if sub == nil {
		return "", "", fmt.Errorf("unknown subcommand %s", args[0])
	}
	inv, err := command.Parse(sub, args[1:])
	if err != nil {
		return "", "", err
	}
	var out, errOut bytes.Buffer
	env := &command.Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Dir: dir}
	err = sub.Run(env, inv)
	return out.String(), errOut.String(), err
}

// snapshot reads all regular files below dir. It skips hidden directories,
// like the .git marker and language server caches.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			files[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// transcript runs a command in a fresh copy of fixture and renders what the
// command printed, its error and the files it changed.
func transcript(t *testing.T, fixture string, args []string) string {
	t.Helper()
	dir := copyFixture(t, fixture)
	before := snapshot(t, dir)
	stdout, stderr, err := run(dir, args...)
	var b strings.Builder
	fmt.Fprintf(&b, "$ myoo lsp %s\n%s", strings.Join(args, " "), stdout)
	if stderr != "" {
		fmt.Fprintf(&b, "[stderr]\n%s", stderr)
	}
	if err != nil {
		fmt.Fprintf(&b, "[err] %v (exit %d)\n", err, command.ExitCode(err))
	}
	after := snapshot(t, dir)
	var changed []string
	for name, content := range after {
		if before[name] != content {
			changed = append(changed, name)
		}
	}
	slices.Sort(changed)
	for _, name := range changed {
		fmt.Fprintf(&b, "[changed %s]\n%s", name, after[name])
	}
	return b.String()
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func TestGolden(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		args    string
	}{
		{"readme_definition_calc", "readme", "definition main.py:calc"},
		{"readme_definition_exact_a", "readme", "definition calculator.py:L4:!a"},
		{"readme_references_alcu", "readme", "references main.py:alcu"},
		{"readme_rename_param_comments", "readme", "rename calculator.calculate->a first_number -c"},
		{"readme_rename_function", "readme", "rename calculator.calculate add"},
		{"go_definition_method", "gomod", "definition main.go:Add"},
		{"go_definition_local", "gomod", "definition main.go:L13:!total"},
		{"go_references_type", "gomod", "references calc/calc.go:!Calc"},
		{"go_references_method", "gomod", "references !Add"},
		{"go_rename_method", "gomod", "rename calc.Calc.Add Sum"},
		{"go_rename_param_comments", "gomod", "rename calc.Calc.Add->a x -c"},
		{"go_rename_keyword", "gomod", "rename main.main->total func"},
		{"go_rename_conflict", "gomod", "rename calc.New Calc"},
		{"go_rename_unknown", "gomod", "rename calc.Calc.Ad Sum"},
		{"ts_definition_import", "ts", "definition src/index.ts:add"},
		{"ts_references_class", "ts", "references src/math.ts:!Counter"},
		{"ts_references_fuzzy_workspace", "ts", "references incr"},
		{"ts_rename_function", "ts", "rename src/math.add sum"},
		{"ts_rename_param_jsdoc", "ts", "rename src/math.add->a x -c --dry-run"},
		{"java_definition_overloads", "java", "definition com/shop/Main.java:add"},
		{"java_references_class", "java", "references com/shop/Calc.java:!Calc"},
		{"java_rename_overloads", "java", "rename com.shop.Calc.add sum"},
		{"java_rename_param_comments", "java", "rename com.shop.Calc.add->a x -c"},
		{"cpp_definition_method", "cpp", "definition src/main.cpp:add"},
		{"cpp_references_class", "cpp", "references include/calc.h:!Calc"},
		{"cpp_rename_method", "cpp", "rename src/calc.shop.Calc.add sum"},
		{"cpp_rename_from_header", "cpp", "rename include/calc.shop.Calc.add sum --dry-run"},
		{"cpp_rename_param_comments", "cpp", "rename src/calc.shop.Calc.add->a x -c"},
		{"cpp_rename_class", "cpp", "rename include/calc.shop.Calc Counter"},
		{"cpp_rename_qualified_query", "cpp", "definition !shop::Calc::add"},
		{"no_match", "readme", "definition zzzqqq"},
		{"external_only", "readme", "definition main.py:!print"},
		{"subsequence_fallback", "readme", "definition clcltr"},
		{"no_references", "gomod", "references !main"},
		{"limit", "gomod", "definition a --limit=2"},
		{"rename_module_refused", "readme", "rename calculator x"},
		{"rename_invalid_identifier", "readme", "rename calculator.calculate 1x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(strings.Fields(tt.args), "--backend=builtin")
			checkGolden(t, tt.name, transcript(t, tt.fixture, args))
		})
	}
}

func TestExitCodes(t *testing.T) {
	dir := copyFixture(t, "readme")
	_, _, err := run(dir, "definition", "zzzqqq", "--backend=builtin")
	if err == nil || command.ExitCode(err) != 1 {
		t.Fatalf("got %v", err)
	}
	_, _, err = run(dir, "definition", "x", "--backend=nope")
	if _, ok := errors.AsType[*command.UsageError](err); !ok {
		t.Fatalf("want usage error, got %v", err)
	}
}
