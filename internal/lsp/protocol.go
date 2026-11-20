package lsp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

// workspaceEdit is what textDocument/rename returns, and also the payload of
// workspace/applyEdit.
type workspaceEdit struct {
	Changes         map[string][]textEdit `json:"changes,omitempty"`
	DocumentChanges []json.RawMessage     `json:"documentChanges,omitempty"`
}

// textEdits flattens both edit shapes into text edits by document URI. We
// reject resource operations, which create, rename or delete files.
func (we workspaceEdit) textEdits() (map[string][]textEdit, error) {
	out := map[string][]textEdit{}
	for uri, edits := range we.Changes {
		out[uri] = append(out[uri], edits...)
	}
	for _, raw := range we.DocumentChanges {
		var dc struct {
			Kind         string                 `json:"kind"`
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Edits        []textEdit             `json:"edits"`
		}
		if err := json.Unmarshal(raw, &dc); err != nil {
			return nil, fmt.Errorf("invalid workspace edit: %w", err)
		}
		if dc.Kind != "" {
			return nil, fmt.Errorf("the server wants to %s files, which myoo does not do. rename the file by hand", dc.Kind)
		}
		out[dc.TextDocument.URI] = append(out[dc.TextDocument.URI], dc.Edits...)
	}
	return out, nil
}

// parseLocations decodes a definition result, which can be null, a
// Location, a Location array or a LocationLink array.
func parseLocations(raw json.RawMessage) ([]location, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil, nil
	}
	type item struct {
		location
		TargetURI            string    `json:"targetUri"`
		TargetSelectionRange *lspRange `json:"targetSelectionRange"`
	}
	var items []item
	if strings.HasPrefix(s, "[") {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	} else {
		var one item
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, err
		}
		items = append(items, one)
	}
	locs := make([]location, 0, len(items))
	for _, it := range items {
		if it.TargetURI != "" && it.TargetSelectionRange != nil {
			locs = append(locs, location{URI: it.TargetURI, Range: *it.TargetSelectionRange})
			continue
		}
		locs = append(locs, it.location)
	}
	return locs, nil
}

func pathToURI(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/x -> /C:/x
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// uriToPath converts a file URI into a filesystem path. If the URI has some
// other scheme, ok is false.
func uriToPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	p := u.Path
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), true
}

// encoding is the position encoding we agreed on with the server.
type encoding string

const (
	utf8Enc  encoding = "utf-8"
	utf16Enc encoding = "utf-16"
	utf32Enc encoding = "utf-32"
)

func (e encoding) position(f *code.File, off int) position {
	switch e {
	case utf8Enc:
		p := f.Pos(off)
		return position{Line: p.Line - 1, Character: off - f.LineSpan(p.Line).Start}
	case utf32Enc:
		p := f.Pos(off)
		return position{Line: p.Line - 1, Character: p.Col - 1}
	}
	line, char := f.UTF16Pos(off)
	return position{Line: line, Character: char}
}

func (e encoding) offset(f *code.File, p position) int {
	switch e {
	case utf8Enc:
		ls := f.LineSpan(p.Line + 1)
		return min(ls.Start+p.Character, ls.End)
	case utf32Enc:
		return f.Offset(code.Pos{Line: p.Line + 1, Col: p.Character + 1})
	}
	return f.OffsetUTF16(p.Line, p.Character)
}

func (e encoding) span(f *code.File, r lspRange) code.Span {
	s, end := e.offset(f, r.Start), e.offset(f, r.End)
	return code.Span{Start: s, End: max(s, end)}
}

// languageID returns the LSP language identifier for a file.
func languageID(f *code.File) string {
	switch ext := strings.ToLower(filepath.Ext(f.Path)); {
	case ext == ".tsx":
		return "typescriptreact"
	case ext == ".jsx":
		return "javascriptreact"
	}
	return string(f.Lang)
}
