// Package python is the Python frontend. A hand-written lexer and parser
// extract declarations and references, and the resolver implements LEGB
// scoping, workspace imports and member access on known classes.
package python

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type frontend struct{}

// New returns the Python frontend.
func New() code.Frontend { return frontend{} }

func (frontend) Lang(p string) (code.Lang, bool) {
	if strings.HasSuffix(p, ".py") || strings.HasSuffix(p, ".pyi") {
		return code.Python, true
	}
	return "", false
}

func (frontend) Parse(f *code.File) error {
	parseFile(f)
	return nil
}

// Resolve re-parses the files to rebuild their scopes. That way Parse
// doesn't have to keep state between the two phases.
func (frontend) Resolve(_ *code.Index, files []*code.File) {
	resolve(files)
}

// moduleName returns the dotted module name of a file, from its path
// relative to the workspace root. We drop a leading src/ directory, and
// pkg/__init__.py names the package. isPkg reports an __init__ file.
func moduleName(rel, abs string) (name string, isPkg bool) {
	p := filepath.ToSlash(rel)
	if filepath.IsAbs(rel) {
		p = path.Base(p)
	}
	if s, ok := strings.CutSuffix(p, ".pyi"); ok {
		p = s
	} else {
		p = strings.TrimSuffix(p, ".py")
	}
	p = strings.TrimPrefix(p, "src/")
	if path.Base(p) == "__init__" {
		isPkg = true
		p = path.Dir(p)
		if p == "." {
			root := strings.TrimSuffix(filepath.ToSlash(abs), "/"+filepath.ToSlash(rel))
			p = path.Base(root)
		}
	}
	return strings.ReplaceAll(p, "/", "."), isPkg
}
