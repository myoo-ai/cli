package lsp

import (
	"bufio"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

func TestReadMessage(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr string
	}{
		{"two messages", "Content-Length: 2\r\n\r\n{}Content-Length: 7\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n[1,2,3]", []string{"{}", "[1,2,3]"}, ""},
		{"lowercase header", "content-length: 4\r\n\r\nnull", []string{"null"}, ""},
		{"utf-8 body counts bytes", "Content-Length: 4\r\n\r\n\"\u00e9\"", []string{"\"\u00e9\""}, ""},
		{"bad length", "Content-Length: x\r\n\r\n{}", nil, "invalid Content-Length"},
		{"truncated body", "Content-Length: 10\r\n\r\n{}", nil, "reading message body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tt.in))
			var got []string
			var err error
			for {
				var data []byte
				if data, err = readMessage(r); err != nil {
					break
				}
				got = append(got, string(data))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("messages %q, want %q", got, tt.want)
			}
			if tt.wantErr == "" && !errors.Is(err, io.EOF) || tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"pyright-langserver --stdio", []string{"pyright-langserver", "--stdio"}},
		{`"/opt/my tools/gopls" -rpc.trace`, []string{"/opt/my tools/gopls", "-rpc.trace"}},
		{`C:\tools\clangd.exe --log=error`, []string{`C:\tools\clangd.exe`, "--log=error"}},
		{`a\ b 'c d' "e\"f"`, []string{"a b", "c d", `e"f`}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := splitCommand(tt.in)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	for _, bad := range []string{"", "  ", `"open`} {
		if _, err := splitCommand(bad); err == nil {
			t.Errorf("splitCommand(%q) succeeded", bad)
		}
	}
}

func TestParseTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.cpp"), []byte("int x;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in        string
		file      string
		from, to  int
		exact     bool
		name      string
		qual      string
		wantError string
	}{
		{in: "calc", name: "calc"},
		{in: "a.cpp:calc", file: "a.cpp", name: "calc"},
		{in: "a.cpp:L4:!a", file: "a.cpp", from: 4, to: 4, exact: true, name: "a"},
		{in: "a.cpp:L4-L9:ns::name", file: "a.cpp", from: 4, to: 9, name: "name", qual: "ns.name"},
		{in: "L2-3:x", from: 2, to: 3, name: "x"},
		{in: "ns::Calc::add", name: "add", qual: "ns.Calc.add"},
		{in: "!calculator.calculate->a", exact: true, name: "a", qual: "calculator.calculate->a"},
		{in: filepath.Join(dir, "a.cpp") + ":x", file: "a.cpp", name: "x"},
		{in: "missing.py:calc", wantError: "unable to find file `missing.py`"},
		{in: "a.cpp:L9-L4:x", wantError: "invalid line range"},
		{in: "a.cpp:L4:", wantError: "missing symbol name"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseTarget(dir, tt.in)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantFile := ""
			if tt.file != "" {
				wantFile = filepath.Join(dir, tt.file)
			}
			q := got.query
			if got.file != wantFile || got.from != tt.from || got.to != tt.to || q.exact != tt.exact || q.name != tt.name || q.qual != tt.qual {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestURIRoundTrip(t *testing.T) {
	for _, p := range []string{"/tmp/a b/c#d.py", "/x/\u00e9.go"} {
		uri := pathToURI(p)
		back, ok := uriToPath(uri)
		if !ok || back != filepath.FromSlash(p) {
			t.Errorf("%s -> %s -> %s", p, uri, back)
		}
	}
	if got, _ := uriToPath("file:///C:/x/a.py"); got != filepath.FromSlash("C:/x/a.py") {
		t.Errorf("drive letter uri gave %s", got)
	}
	if _, ok := uriToPath("jdt://contents/x"); ok {
		t.Error("non-file uri accepted")
	}
}

func TestEncodings(t *testing.T) {
	f := code.NewFile("a.py", "/a.py", code.Python, []byte("x = \"\U0001F600\u00e9\" + y\n"))
	off := strings.Index(string(f.Src), "y")
	tests := []struct {
		enc  encoding
		char int
	}{{utf16Enc, 12}, {utf8Enc, 15}, {utf32Enc, 11}}
	for _, tt := range tests {
		t.Run(string(tt.enc), func(t *testing.T) {
			p := tt.enc.position(f, off)
			if p.Line != 0 || p.Character != tt.char {
				t.Errorf("position %+v, want char %d", p, tt.char)
			}
			if back := tt.enc.offset(f, p); back != off {
				t.Errorf("offset %d, want %d", back, off)
			}
		})
	}
}

func fakeServerArg(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return `--server="` + exe + `"`
}

// withoutCommand drops the command line from a transcript.
func withoutCommand(s string) string {
	_, rest, _ := strings.Cut(s, "\n")
	return rest
}

func TestFakeServerMatchesBuiltin(t *testing.T) {
	commands := []string{
		"definition main.py:calc",
		"definition calculator.py:L4:!a",
		"references main.py:alcu",
		"references calculator.py:!b",
		"rename calculator.calculate add",
		"rename calculator.calculate->a first_number -c",
		"rename calculator.calculate add --dry-run",
	}
	for _, mode := range []string{"changes", "documentChanges", "links", "applyedit"} {
		for _, cmd := range commands {
			t.Run(mode+"/"+cmd, func(t *testing.T) {
				t.Setenv(fakeServerEnv, mode)
				args := strings.Fields(cmd)
				want := withoutCommand(transcript(t, "readme", append(args, "--backend=builtin")))
				got := withoutCommand(transcript(t, "readme", append(args, fakeServerArg(t), "--timeout=20s")))
				if got != want {
					t.Errorf("language server output differs from builtin:\n--- lsp\n%s\n--- builtin\n%s", got, want)
				}
			})
		}
	}
}

func TestFakeServerFailures(t *testing.T) {
	tests := []struct {
		mode    string
		args    string
		wantErr []string
	}{
		{"hang", "definition main.py:calc --timeout=300ms", []string{"timed out after 300ms waiting for an answer to textDocument/definition"}},
		{"hang", "references calculator.py:!calculate --timeout=300ms", []string{"timed out after 300ms waiting for an answer to textDocument/references"}},
		{"crash", "definition main.py:calc", []string{"server exited", "boom"}},
		{"resourceop", "rename calculator.calculate add", []string{"the server wants to rename files", "rename the file by hand"}},
		{"initfail", "definition main.py:calc --backend=lsp", []string{"server exited", "fake server refuses to start"}},
	}
	for _, tt := range tests {
		t.Run(tt.mode+"/"+tt.args, func(t *testing.T) {
			t.Setenv(fakeServerEnv, tt.mode)
			dir := copyFixture(t, "readme")
			before := snapshot(t, dir)
			args := append(strings.Fields(tt.args), fakeServerArg(t))
			_, _, err := run(dir, args...)
			if err == nil {
				t.Fatal("command succeeded")
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %q", err, w)
				}
			}
			if after := snapshot(t, dir); !maps.Equal(before, after) {
				t.Error("a failed command changed files")
			}
		})
	}
}

func TestAutoFallsBackToBuiltin(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	saved := registry[code.Python]
	registry[code.Python] = [][]string{{exe}}
	t.Cleanup(func() { registry[code.Python] = saved })
	t.Setenv(fakeServerEnv, "initfail")

	dir := copyFixture(t, "readme")
	got, stderr, err := run(dir, "definition", "main.py:calc")
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := run(dir, "definition", "main.py:calc", "--backend=builtin")
	if got != want {
		t.Errorf("auto output differs from builtin:\n%s\n---\n%s", got, want)
	}
	if !strings.HasPrefix(stderr, "warning: "+exe+": ") || !strings.HasSuffix(stderr, ", using the builtin index\n") {
		t.Errorf("stderr %q", stderr)
	}

	t.Setenv(fakeServerEnv, "changes")
	got, stderr, err = run(dir, "definition", "main.py:calc")
	if err != nil || stderr != "" || got != want {
		t.Errorf("auto with a working server: %v, stderr %q, output:\n%s", err, stderr, got)
	}
}
