// Package golang is the Go frontend. We get tokens from go/scanner,
// declarations and references from go/parser, and resolution from running
// go/types over the workspace packages from source.
package golang

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type frontend struct{}

// New returns the Go frontend.
func New() code.Frontend { return frontend{} }

func (frontend) Lang(p string) (code.Lang, bool) {
	if strings.HasSuffix(p, ".go") {
		return code.Go, true
	}
	return "", false
}

const parseMode = parser.ParseComments | parser.AllErrors | parser.SkipObjectResolution

func (frontend) Parse(f *code.File) error {
	f.Tokens, f.Comments = scan(f.Src)
	fset := token.NewFileSet()
	af, _ := parser.ParseFile(fset, f.Abs, f.Src, parseMode)
	f.SetModule(moduleName(f.Abs, f.Path, af.Name.Name))
	markDocComments(f, fset.File(af.FileStart), af)
	newWalker(f, fset.File(af.FileStart)).file(af)
	return nil
}

// scan tokenizes src with go/scanner. We drop automatic semicolons because
// they have no source text.
func scan(src []byte) ([]code.Token, []code.Comment) {
	var (
		s        scanner.Scanner
		toks     []code.Token
		comments []code.Comment
	)
	tf := token.NewFileSet().AddFile("", -1, len(src))
	s.Init(tf, src, func(token.Position, string) {}, scanner.ScanComments)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit != ";" {
			continue
		}
		start := tf.Offset(pos)
		sp := code.Span{Start: start, End: tokenEnd(src, start, tok, lit)}
		toks = append(toks, code.Token{Kind: tokenKind(tok), Span: sp})
		if tok == token.COMMENT {
			kind := code.LineComment
			if src[start+1] == '*' {
				kind = code.BlockComment
			}
			comments = append(comments, code.Comment{Kind: kind, Span: sp})
		}
	}
	return toks, comments
}

// tokenEnd has to compute the end offset from the source, because the
// scanner strips carriage returns from comments and raw strings.
func tokenEnd(src []byte, start int, tok token.Token, lit string) int {
	end := start + len(lit)
	switch {
	case tok == token.COMMENT && src[start+1] == '/':
		end = len(src)
		if i := bytes.IndexByte(src[start:], '\n'); i >= 0 {
			end = start + i
		}
		if end > start && src[end-1] == '\r' {
			end--
		}
	case tok == token.COMMENT:
		end = len(src)
		if i := bytes.Index(src[start+2:], []byte("*/")); i >= 0 {
			end = start + 2 + i + 2
		}
	case tok == token.STRING && src[start] == '`':
		end = len(src)
		if i := bytes.IndexByte(src[start+1:], '`'); i >= 0 {
			end = start + 1 + i + 1
		}
	case lit == "":
		end = start + len(tok.String())
	}
	return min(max(end, start+1), len(src))
}

func tokenKind(tok token.Token) code.TokenKind {
	switch {
	case tok == token.IDENT:
		return code.TokIdent
	case tok.IsKeyword():
		return code.TokKeyword
	case tok == token.STRING || tok == token.CHAR:
		return code.TokString
	case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
		return code.TokNumber
	case tok == token.COMMENT:
		return code.TokComment
	}
	return code.TokPunct
}

// markDocComments turns the comment groups go/ast attaches as docs to the
// package clause, declarations, specs or fields into DocComments.
func markDocComments(f *code.File, tf *token.File, af *ast.File) {
	doc := map[int]bool{}
	add := func(g *ast.CommentGroup) {
		if g == nil {
			return
		}
		for _, c := range g.List {
			doc[tf.Offset(c.Slash)] = true
		}
	}
	add(af.Doc)
	ast.Inspect(af, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GenDecl:
			add(n.Doc)
		case *ast.FuncDecl:
			add(n.Doc)
		case *ast.TypeSpec:
			add(n.Doc)
		case *ast.ValueSpec:
			add(n.Doc)
		case *ast.ImportSpec:
			add(n.Doc)
		case *ast.Field:
			add(n.Doc)
		}
		return true
	})
	for i, c := range f.Comments {
		if doc[c.Span.Start] {
			f.Comments[i].Kind = code.DocComment
		}
	}
}

// moduleName names a package after its directory relative to the nearest
// go.mod. In the module root we use the package name instead. External
// test packages get a "_test" suffix.
func moduleName(abs, rel, pkgName string) string {
	dir := path.Dir(rel)
	if modDir := findModDir(filepath.Dir(abs)); modDir != "" {
		if r, err := filepath.Rel(modDir, filepath.Dir(abs)); err == nil {
			dir = filepath.ToSlash(r)
		}
	}
	if dir == "." || dir == "" {
		return pkgName
	}
	if strings.HasSuffix(pkgName, "_test") && strings.HasSuffix(abs, "_test.go") {
		return dir + "_test"
	}
	return dir
}

func findModDir(dir string) string {
	for {
		if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func modulePath(modDir string) string {
	data, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(data)) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module")
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		rest, _, _ = strings.Cut(rest, "//")
		return strings.Trim(strings.TrimSpace(rest), `"`+"`")
	}
	return ""
}
