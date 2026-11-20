// Package clike is the language frontend for Java and C#. The two are close
// enough to share a lexer and a tolerant recursive-descent parser, with
// switches for the dialect differences.
package clike

import (
	"path/filepath"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type frontend struct {
	code.FileParser[fileInfo]
}

// New returns the Java and C# frontend.
func New() code.Frontend {
	return &frontend{FileParser: code.NewFileParser(parse)}
}

func (*frontend) Lang(path string) (code.Lang, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".java":
		return code.Java, true
	case ".cs":
		return code.CSharp, true
	}
	return "", false
}

func (fe *frontend) Resolve(ix *code.Index, files []*code.File) {
	infos := make(map[*code.File]*fileInfo, len(files))
	fe.Drain(files, func(f *code.File, fi *fileInfo) { infos[f] = fi })
	newResolver(files, infos).run()
}

// want is the kind of entity a reference position can name.
type want uint8

const (
	wantValue want = iota // an expression operand tries variables, then types, then methods
	wantCall
	wantType
)

type qualKind uint8

const (
	qualNone     qualKind = iota
	qualThis              // this.x
	qualSuper             // super.x, base.x
	qualRef               // q.x
	qualCall              // q().x
	qualType              // value.x in a C# setter
	qualOpaque            // (a + b).x
	qualExternal          // "s".x, int.class, typeof(T).x
)

type qual struct {
	kind qualKind
	ref  *code.Ref
	typ  string
}

// refInfo is what the parser knows about a reference beyond code.Ref.
type refInfo struct {
	local *code.Decl // lexical binding
	// Resolution walks outwards from the innermost declaration around the
	// reference.
	ctx *code.Decl
	// Set for names in the bases and type parameter bounds of a type, where
	// its own members aren't in scope yet.
	header *code.Decl
	anon   *anonCtx
	qual   qual
	want   want
	arity  int // argument count of a call, -1 if unknown
	// A switch label can name an enum member of the switched type without
	// qualifying it.
	caseLabel bool
	attr      bool // C# attribute name, which can resolve to Name or NameAttribute
	imp       bool // part of an import, resolveImports handles it
}

// anonCtx describes a Java anonymous class body or enum constant body. We
// declare the members as locals of the enclosing declaration.
type anonCtx struct {
	base    string     // the instantiated type as written
	ctx     *code.Decl // where we resolve base
	members []*code.Decl
	parent  *anonCtx
}

type importInfo struct {
	refs   []*code.Ref // a ref for each name segment
	static bool
	star   bool       // Java on-demand import (a.b.*)
	alias  *code.Decl // C# using alias
	global bool       // C# global using
}

type arity struct {
	n       int
	varargs bool
}

type fileInfo struct {
	refs    map[*code.Ref]*refInfo
	imports []importInfo
	arity   map[*code.Decl]arity
	ext     []*code.Decl // C# extension methods
	// Locals of Java initializer blocks are children of the class, but not
	// members.
	hidden map[*code.Decl]bool
}
