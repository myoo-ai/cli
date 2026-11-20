package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/langs"
)

// fakeServerEnv makes the test binary act as a language server. The value
// picks what it does:
//
//	changes, documentChanges  answer rename with that WorkspaceEdit shape
//	links                     answer definition with LocationLinks
//	applyedit                 deliver the rename through workspace/applyEdit
//	resourceop                answer rename with a file rename operation
//	hang                      never answer definition or references
//	crash                     exit while handling definition
//	initfail                  exit before initialize
//
// The server answers from the builtin index. It also checks the client's
// side of the protocol, and exits with status 3 if the client gets it wrong.
const fakeServerEnv = "MYOO_FAKE_LSP"

type fakeServer struct {
	mode    string
	out     *bufio.Writer
	wmu     sync.Mutex
	ix      *code.Index
	opened  map[string]bool
	nextID  int
	pending map[string]func(*message)
}

func runFakeServer(mode string) int {
	if mode == "initfail" {
		fmt.Fprintln(os.Stderr, "fake server refuses to start")
		return 1
	}
	s := &fakeServer{
		mode:    mode,
		out:     bufio.NewWriter(os.Stdout),
		opened:  map[string]bool{},
		pending: map[string]func(*message){},
	}
	r := bufio.NewReader(os.Stdin)
	for {
		data, err := readMessage(r)
		if err != nil {
			return 0
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			s.violation("unparsable message: %v", err)
		}
		if m.JSONRPC != "2.0" {
			s.violation("jsonrpc version %q", m.JSONRPC)
		}
		hasID := len(m.ID) > 0
		switch {
		case hasID && m.Method == "":
			cb := s.pending[string(m.ID)]
			delete(s.pending, string(m.ID))
			if cb == nil {
				s.violation("response to unknown request %s", m.ID)
			}
			cb(&m)
		case hasID:
			s.request(&m)
		default:
			s.notification(&m)
		}
	}
}

func (s *fakeServer) violation(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "protocol violation: "+format+"\n", args...)
	os.Exit(3)
}

func (s *fakeServer) send(m *message) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := writeMessage(s.out, m); err != nil {
		os.Exit(4)
	}
	_ = s.out.Flush()
}

func (s *fakeServer) reply(id json.RawMessage, result any) {
	data, _ := json.Marshal(result)
	s.send(&message{ID: id, Result: data})
}

func (s *fakeServer) notify(method string, params any) {
	data, _ := json.Marshal(params)
	s.send(&message{Method: method, Params: data})
}

// ask sends a server-to-client request, and check validates the answer.
func (s *fakeServer) ask(method string, params any, check func(*message)) {
	s.nextID++
	id := `"srv-` + strconv.Itoa(s.nextID) + `"`
	s.pending[id] = check
	data, _ := json.Marshal(params)
	s.send(&message{ID: json.RawMessage(id), Method: method, Params: data})
}

func (s *fakeServer) notification(m *message) {
	switch m.Method {
	case "initialized":
		s.afterInitialized()
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI        string `json:"uri"`
				LanguageID string `json:"languageId"`
				Text       string `json:"text"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.TextDocument.LanguageID == "" || p.TextDocument.Text == "" {
			s.violation("didOpen without languageId or text")
		}
		s.opened[p.TextDocument.URI] = true
	case "exit":
		os.Exit(0)
	}
}

// afterInitialized sends all the server-to-client requests the client has
// to answer, and announces background work that ends a moment later.
func (s *fakeServer) afterInitialized() {
	s.notify("window/logMessage", map[string]any{"type": 3, "message": "fake server ready"})
	s.ask("workspace/configuration", map[string]any{"items": []any{map[string]string{"section": "a"}, map[string]string{"section": "b"}}}, func(m *message) {
		if string(m.Result) != "[null,null]" {
			s.violation("workspace/configuration answered %s", m.Result)
		}
	})
	s.ask("client/registerCapability", map[string]any{"registrations": []any{}}, func(m *message) {
		if m.Error != nil || string(m.Result) != "null" {
			s.violation("client/registerCapability answered %s", m.Result)
		}
	})
	s.ask("window/workDoneProgress/create", map[string]any{"token": "load"}, func(m *message) {
		if m.Error != nil {
			s.violation("workDoneProgress/create failed: %v", m.Error)
		}
		s.notify("$/progress", map[string]any{"token": "load", "value": map[string]string{"kind": "begin", "title": "loading"}})
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.notify("$/progress", map[string]any{"token": "load", "value": map[string]string{"kind": "end"}})
		}()
	})
	s.ask("custom/unknown", nil, func(m *message) {
		if m.Error == nil || m.Error.Code != -32601 {
			s.violation("unknown request not rejected with MethodNotFound")
		}
	})
}

func (s *fakeServer) request(m *message) {
	switch m.Method {
	case "initialize":
		var p struct {
			RootURI          string            `json:"rootUri"`
			WorkspaceFolders []workspaceFolder `json:"workspaceFolders"`
			Capabilities     struct {
				General struct {
					PositionEncodings []string `json:"positionEncodings"`
				} `json:"general"`
				Workspace struct {
					ApplyEdit bool `json:"applyEdit"`
				} `json:"workspace"`
			} `json:"capabilities"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if len(p.WorkspaceFolders) != 1 || p.WorkspaceFolders[0].URI != p.RootURI {
			s.violation("workspaceFolders %v do not match rootUri %s", p.WorkspaceFolders, p.RootURI)
		}
		if len(p.Capabilities.General.PositionEncodings) != 1 || p.Capabilities.General.PositionEncodings[0] != "utf-16" || !p.Capabilities.Workspace.ApplyEdit {
			s.violation("unexpected client capabilities %s", m.Params)
		}
		root, _ := uriToPath(p.RootURI)
		ix, err := code.Load(code.LoadOptions{Root: root, Frontends: langs.All(), Resolve: true})
		if err != nil {
			s.violation("loading %s: %v", root, err)
		}
		s.ix = ix
		s.reply(m.ID, map[string]any{
			"capabilities": map[string]any{"definitionProvider": true, "referencesProvider": true, "renameProvider": true},
			"serverInfo":   map[string]string{"name": "fake"},
		})
	case "shutdown":
		s.reply(m.ID, nil)
	case "textDocument/definition":
		switch s.mode {
		case "hang":
			return
		case "crash":
			fmt.Fprintln(os.Stderr, "boom")
			os.Exit(2)
		}
		f, off := s.position(m.Params)
		var result any
		if d := s.target(f, off); d != nil {
			loc := s.location(d.File, d.NameSpan)
			if s.mode == "links" {
				result = []map[string]any{{"targetUri": loc.URI, "targetRange": s.location(d.File, d.Span).Range, "targetSelectionRange": loc.Range}}
			} else {
				result = loc
			}
		}
		s.reply(m.ID, result)
	case "textDocument/references":
		if s.mode == "hang" {
			return
		}
		var p struct {
			Context struct {
				IncludeDeclaration *bool `json:"includeDeclaration"`
			} `json:"context"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.Context.IncludeDeclaration == nil || *p.Context.IncludeDeclaration {
			s.violation("references must not include the declaration")
		}
		f, off := s.position(m.Params)
		locs := []location{}
		if d := s.target(f, off); d != nil {
			// Real servers sometimes list the declaration anyway, so we do too.
			locs = append(locs, s.location(d.File, d.NameSpan))
			for _, r := range s.ix.RefsTo(d) {
				locs = append(locs, s.location(r.File, r.Span))
			}
		}
		s.reply(m.ID, locs)
	case "textDocument/rename":
		var p struct {
			NewName string `json:"newName"`
		}
		_ = json.Unmarshal(m.Params, &p)
		f, off := s.position(m.Params)
		d := s.target(f, off)
		if d == nil {
			s.send(&message{ID: m.ID, Error: &rpcError{Code: -32602, Message: "no symbol at position"}})
			return
		}
		changes := map[string][]textEdit{}
		add := func(f *code.File, span code.Span) {
			l := s.location(f, span)
			changes[l.URI] = append(changes[l.URI], textEdit{Range: l.Range, NewText: p.NewName})
		}
		add(d.File, d.NameSpan)
		for _, r := range s.ix.RefsTo(d) {
			add(r.File, r.Span)
		}
		switch s.mode {
		case "documentChanges", "resourceop":
			var dcs []any
			for uri, edits := range changes {
				dcs = append(dcs, map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1}, "edits": edits})
			}
			if s.mode == "resourceop" {
				dcs = append(dcs, map[string]any{"kind": "rename", "oldUri": "file:///a.py", "newUri": "file:///b.py"})
			}
			s.reply(m.ID, map[string]any{"documentChanges": dcs})
		case "applyedit":
			id := m.ID
			s.ask("workspace/applyEdit", map[string]any{"edit": map[string]any{"changes": changes}}, func(resp *message) {
				var r applyEditResult
				_ = json.Unmarshal(resp.Result, &r)
				if !r.Applied {
					s.violation("applyEdit not applied: %s", r.FailureReason)
				}
				s.reply(id, nil)
			})
		default:
			s.reply(m.ID, map[string]any{"changes": changes})
		}
	default:
		s.send(&message{ID: m.ID, Error: &rpcError{Code: -32601, Message: "unsupported"}})
	}
}

func (s *fakeServer) position(params json.RawMessage) (*code.File, int) {
	var p textDocumentPositionParams
	_ = json.Unmarshal(params, &p)
	if !s.opened[p.TextDocument.URI] {
		s.violation("query on %s before didOpen", p.TextDocument.URI)
	}
	path, _ := uriToPath(p.TextDocument.URI)
	f := s.ix.File(path)
	if f == nil {
		s.violation("unknown document %s", p.TextDocument.URI)
	}
	return f, utf16Enc.offset(f, p.Position)
}

// target returns the declaration that's named or referenced at off.
func (s *fakeServer) target(f *code.File, off int) *code.Decl {
	for _, d := range f.Decls {
		if d.NameSpan.Contains(off) {
			return d
		}
	}
	for _, r := range f.Refs {
		if r.Span.Contains(off) {
			return r.Target
		}
	}
	return nil
}

func (s *fakeServer) location(f *code.File, span code.Span) location {
	return location{URI: pathToURI(f.Abs), Range: lspRange{Start: utf16Enc.position(f, span.Start), End: utf16Enc.position(f, span.End)}}
}
