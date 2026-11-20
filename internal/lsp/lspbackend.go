package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

// lspBackend answers through a language server. We still take names, FQNs,
// source spans and reference scopes from the builtin index.
type lspBackend struct {
	ws      *workspace
	c       *client
	enc     encoding
	ctx     context.Context
	cancel  context.CancelFunc
	opened  map[*code.File]bool
	waited  bool
	defs    map[filePos][]location // definition results by queried position
	unknown map[filePos]*code.Decl // stand-ins for declarations the builtin index doesn't have
}

func startLSPBackend(ws *workspace, argv []string, timeout time.Duration) (*lspBackend, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	rootURI := pathToURI(ws.root)
	folders := []workspaceFolder{{URI: rootURI, Name: filepath.Base(ws.root)}}
	c, err := startClient(argv, ws.root, timeout, folders)
	if err != nil {
		cancel()
		return nil, err
	}
	params := map[string]any{
		"processId":        os.Getpid(),
		"clientInfo":       map[string]string{"name": "myoo"},
		"rootUri":          rootURI,
		"rootPath":         ws.root,
		"workspaceFolders": folders,
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{string(utf16Enc)}},
			"workspace": map[string]any{
				"applyEdit":        true,
				"configuration":    true,
				"workspaceFolders": true,
				"workspaceEdit": map[string]any{
					"documentChanges":    true,
					"resourceOperations": []string{},
					"failureHandling":    "abort",
				},
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{},
				"definition":      map[string]any{"linkSupport": true},
				"references":      map[string]any{},
				"rename":          map[string]any{"prepareSupport": false},
			},
			"window": map[string]any{"workDoneProgress": true},
		},
	}
	var res struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	if err := c.call(ctx, "initialize", params, &res); err != nil {
		c.close()
		cancel()
		return nil, err
	}
	b := &lspBackend{
		ws: ws, c: c, enc: utf16Enc, ctx: ctx, cancel: cancel,
		opened:  map[*code.File]bool{},
		defs:    map[filePos][]location{},
		unknown: map[filePos]*code.Decl{},
	}
	switch encoding(res.Capabilities.PositionEncoding) {
	case utf8Enc:
		b.enc = utf8Enc
	case utf32Enc:
		b.enc = utf32Enc
	}
	if err := c.notify("initialized", struct{}{}); err != nil {
		b.close()
		return nil, err
	}
	return b, nil
}

func (b *lspBackend) close() {
	b.c.close()
	b.cancel()
}

// open sends didOpen for f, but only the first time. After the first
// document we wait until the server finishes the indexing or package loading
// it announced, so cross-file answers are complete.
func (b *lspBackend) open(f *code.File) error {
	if b.opened[f] {
		return nil
	}
	b.opened[f] = true
	err := b.c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        pathToURI(f.Abs),
			"languageId": languageID(f),
			"version":    1,
			"text":       string(f.Src),
		},
	})
	if err != nil {
		return err
	}
	if !b.waited {
		b.waited = true
		return b.c.waitIdle(b.ctx)
	}
	return nil
}

func (b *lspBackend) at(f *code.File, off int) textDocumentPositionParams {
	return textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(f.Abs)},
		Position:     b.enc.position(f, off),
	}
}

func (b *lspBackend) resolve(o occurrence) (*code.Decl, error) {
	if o.decl != nil {
		return b.ws.canon(o.decl), nil
	}
	key := filePos{o.file.Abs, o.span.Start}
	locs, ok := b.defs[key]
	if !ok {
		if err := b.open(o.file); err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := b.c.call(b.ctx, "textDocument/definition", b.at(o.file, o.span.Start), &raw); err != nil {
			return nil, err
		}
		var err error
		if locs, err = parseLocations(raw); err != nil {
			return nil, fmt.Errorf("invalid textDocument/definition result: %w", err)
		}
		b.defs[key] = locs
	}
	for _, loc := range locs {
		path, ok := uriToPath(loc.URI)
		if !ok {
			continue
		}
		f := b.ws.file(path)
		if f == nil {
			continue
		}
		if d := b.declAt(f, b.enc.span(f, loc.Range)); d != nil {
			return b.ws.canon(d), nil
		}
	}
	return nil, nil
}

// declAt maps a definition range from the server to a declaration in the
// builtin index. Servers report either the name or the whole declaration,
// and module definitions point at the start of the file or at the package
// clause. If the index doesn't model the declaration, we make up a stand-in
// nested in the enclosing scope. That keeps FQNs and sources working.
func (b *lspBackend) declAt(f *code.File, r code.Span) *code.Decl {
	off := r.Start
	for _, d := range f.Decls {
		if !d.NameSpan.Empty() && d.NameSpan.Start <= off && off < d.NameSpan.End {
			return d
		}
	}
	for _, d := range f.Decls {
		if !d.NameSpan.Empty() && r.End > r.Start && r.Encloses(d.NameSpan) {
			return d
		}
	}
	ident := identAt(f, off)
	if off == 0 || ident != "" && ident == lastSegment(f.Module.Name) {
		return f.Module
	}
	if ident == "" {
		return nil
	}
	key := filePos{f.Abs, off}
	if d, ok := b.unknown[key]; ok {
		return d
	}
	parent := f.DeclAt(off)
	d := &code.Decl{
		Name:     ident,
		Kind:     code.KindVariable,
		File:     f,
		NameSpan: code.Span{Start: off, End: off + len(ident)},
		Span:     f.LineSpan(f.LineOf(off)),
		Parent:   parent,
		Local:    parent.Local || parent.Callable(),
	}
	b.unknown[key] = d
	return d
}

func lastSegment(name string) string {
	return name[strings.LastIndexAny(name, "./")+1:]
}

func identAt(f *code.File, off int) string {
	i := sort.Search(len(f.Tokens), func(i int) bool { return f.Tokens[i].Span.Start >= off })
	if i < len(f.Tokens) && f.Tokens[i].Span.Start == off {
		if t := f.Tokens[i]; t.Kind == code.TokIdent {
			return f.TokenText(t)
		}
		return ""
	}
	end := off
	for end < len(f.Src) {
		r, size := utf8.DecodeRune(f.Src[end:])
		if !isIdentRune(r) {
			break
		}
		end += size
	}
	return string(f.Src[off:end])
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (b *lspBackend) references(decls []*code.Decl) ([]site, error) {
	var sites []site
	for _, d := range decls {
		if d.Kind == code.KindModule {
			// A module has no name we can point the server at, but the index
			// knows its import references.
			s, err := builtinBackend{b.ws}.references([]*code.Decl{d})
			if err != nil {
				return nil, err
			}
			sites = append(sites, s...)
			continue
		}
		if err := b.open(d.File); err != nil {
			return nil, err
		}
		params := struct {
			textDocumentPositionParams
			Context struct {
				IncludeDeclaration bool `json:"includeDeclaration"`
			} `json:"context"`
		}{textDocumentPositionParams: b.at(d.File, d.NameSpan.Start)}
		var locs []location
		if err := b.c.call(b.ctx, "textDocument/references", params, &locs); err != nil {
			return nil, err
		}
		for _, loc := range locs {
			path, ok := uriToPath(loc.URI)
			if !ok {
				continue
			}
			f := b.ws.file(path)
			if f == nil {
				continue
			}
			span := b.enc.span(f, loc.Range)
			if isDeclName(b.ws.same(d), f, span.Start) {
				continue
			}
			sites = append(sites, b.siteAt(f, span))
		}
	}
	return sortSites(sites), nil
}

// siteAt describes a reference location. If the index has a reference at
// that position, we take the scope and name from it.
func (b *lspBackend) siteAt(f *code.File, span code.Span) site {
	i := sort.Search(len(f.Refs), func(i int) bool { return f.Refs[i].Span.Start >= span.Start })
	if i < len(f.Refs) && f.Refs[i].Span.Start == span.Start {
		r := f.Refs[i]
		return site{file: f, span: r.Span, name: r.Name, scope: r.Scope}
	}
	name := identAt(f, span.Start)
	if name == "" {
		name = f.Text(span)
	}
	return site{file: f, span: span, name: name, scope: scopeAt(f, span.Start)}
}

func (b *lspBackend) rename(decls []*code.Decl, newName string, plan *editPlan) error {
	b.c.setApplyEdit(func(we workspaceEdit) error { return plan.addWorkspaceEdit(b.enc, we) })
	defer b.c.setApplyEdit(nil)
	for _, d := range decls {
		if d.NameSpan.Empty() {
			continue
		}
		if err := b.open(d.File); err != nil {
			return err
		}
		params := struct {
			textDocumentPositionParams
			NewName string `json:"newName"`
		}{b.at(d.File, d.NameSpan.Start), newName}
		var we *workspaceEdit
		if err := b.c.call(b.ctx, "textDocument/rename", params, &we); err != nil {
			return err
		}
		if we != nil {
			if err := plan.addWorkspaceEdit(b.enc, *we); err != nil {
				return err
			}
		}
	}
	if err := plan.failure(); err != nil {
		return err
	}
	if plan.empty() {
		return errors.New("the language server returned no edits for the rename")
	}
	return nil
}

type filePos struct {
	abs string
	off int
}
