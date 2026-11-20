// Package php is the PHP frontend. A hand-written lexer separates code from
// inline HTML, and a tolerant parser extracts declarations and references.
// We resolve names against the namespace and use imports the way PHP does,
// and members through self, parent, $this and known types.
package php

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

type frontend struct {
	code.FileParser[fileInfo]
}

// New returns the PHP frontend.
func New() code.Frontend {
	return &frontend{FileParser: code.NewFileParser(parse)}
}

func (*frontend) Lang(path string) (code.Lang, bool) {
	p := strings.ToLower(path)
	if strings.HasSuffix(p, ".php") || strings.HasSuffix(p, ".phtml") {
		return code.PHP, true
	}
	return "", false
}

func (fe *frontend) Resolve(_ *code.Index, files []*code.File) {
	infos := map[*code.File]*fileInfo{}
	fe.Drain(files, func(f *code.File, info *fileInfo) { infos[f] = info })
	resolve(files, infos)
}

// fileInfo is what the parser hands to the resolver on top of code.File.
type fileInfo struct {
	refs []refInfo
	// Lower-case fully qualified names of classes and functions
	// ("app\models\user"). For constants only the namespace part is lower
	// case ("app\VERSION").
	keys map[*code.Decl]string
	// Class key of the declared or constructed type of each variable,
	// parameter, property or function result.
	types map[*code.Decl]string
	// Keys of the classes and interfaces a type extends or implements, and
	// of the traits it uses.
	bases map[*code.Decl][]string
}

type refKind uint8

const (
	refVar    refKind = iota + 1 // variable, target known while parsing
	refClass                     // class, interface, trait or enum name
	refFunc                      // function call
	refConst                     // constant
	refMember                    // class member, including class constants
)

type memberKind uint8

const (
	memMethod memberKind = iota + 1
	memProp
	memConst
)

type recvKind uint8

const (
	recvNone  recvKind = iota
	recvClass          // a class decl: $this, self, static
	recvKey            // a class key: Foo::, parent::
	recvRef            // the target of another reference: $x->, $a->b->
)

type refInfo struct {
	ref  *code.Ref
	kind refKind
	// Target of a variable, or the import an aliased name refers to.
	decl *code.Decl
	// Candidate keys of a class, function or constant, tried in order.
	keys   []string
	member memberKind
	recv   recvKind
	cls    *code.Decl
	key    string
	recvAt int // index into fileInfo.refs
}

// constKey returns the key of the constant with the fully qualified name
// full (no leading backslash). Namespace names are case-insensitive, but
// constant names aren't.
func constKey(full string) string {
	if i := strings.LastIndexByte(full, '\\'); i >= 0 {
		return strings.ToLower(full[:i]) + full[i:]
	}
	return full
}
