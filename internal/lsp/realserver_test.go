package lsp

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRealServers runs the installed language servers on small fixtures and
// expects the same answers we get from the builtin index. If a server isn't
// installed, we skip it.
func TestRealServers(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real language servers")
	}
	tests := []struct {
		server   string
		fixture  string
		commands []string
	}{
		{"gopls", "gomod", []string{
			"definition main.go:Add",
			"definition main.go:calc",
			"definition main.go:L13:!total",
			"references calc/calc.go:!Calc",
			"references !Add",
			"references main.go:!total",
			// gopls also renames the doc comment that starts with the
			// method name, and with -c the builtin index does that too.
			"rename calc.Calc.Add Sum -c",
			"rename calc.Calc.Add->a x -c",
			"rename main.main->total sum",
		}},
		{"clangd", "cpp", []string{
			"definition src/main.cpp:add",
			"definition src/calc.cpp:L7:total",
			"references include/calc.h:!Calc",
			"references src/calc.cpp:!add",
			"rename src/calc.shop.Calc.add sum",
			"rename include/calc.shop.Calc.add sum",
			"rename src/calc.shop.Calc.add->a x -c",
			"rename include/calc.shop.Calc Counter",
		}},
		{"pyright-langserver", "readme", []string{
			"definition main.py:calc",
			"definition calculator.py:L4:!a",
			"references main.py:alcu",
			"rename calculator.calculate add",
		}},
		{"typescript-language-server", "ts", []string{
			"definition src/index.ts:add",
			"references src/math.ts:!Counter",
			"rename src/math.add sum",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.server, func(t *testing.T) {
			path, err := exec.LookPath(tt.server)
			if err != nil {
				t.Skipf("%s not installed", tt.server)
			}
			server := `--server="` + path + `"`
			if tt.server != "gopls" && tt.server != "clangd" {
				server = `--server="` + path + `" --stdio`
			}
			for _, cmd := range tt.commands {
				t.Run(cmd, func(t *testing.T) {
					args := strings.Fields(cmd)
					want := withoutCommand(transcript(t, tt.fixture, append(args, "--backend=builtin")))
					got := withoutCommand(transcript(t, tt.fixture, append(args, server, "--timeout=60s")))
					if got != want {
						t.Errorf("%s output differs from builtin:\n--- %s\n%s\n--- builtin\n%s", tt.server, tt.server, got, want)
					}
				})
			}
		})
	}
}
