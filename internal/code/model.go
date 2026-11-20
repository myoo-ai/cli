// Package code is the language-neutral source model shared by code_lint, the
// LSP bridge and the code graph. Language frontends turn source files into
// tokens, comments, declarations and references, so consumers don't have to
// look at language syntax.
package code

import (
	"cmp"
	"slices"
	"strings"
)

// Lang identifies a supported programming language.
type Lang string

const (
	Go         Lang = "go"
	Python     Lang = "python"
	JavaScript Lang = "javascript"
	TypeScript Lang = "typescript"
	Java       Lang = "java"
	CSharp     Lang = "csharp"
	C          Lang = "c"
	CPP        Lang = "cpp"
	Rust       Lang = "rust"
	PHP        Lang = "php"
)

// Span is a half-open byte range [Start, End) into File.Src.
type Span struct{ Start, End int }

// Contains reports whether off lies inside s.
func (s Span) Contains(off int) bool { return off >= s.Start && off < s.End }

// Encloses reports whether o lies entirely inside s.
func (s Span) Encloses(o Span) bool { return o.Start >= s.Start && o.End <= s.End }

// Empty reports whether s covers no bytes. Frontends use the zero Span to mean
// "absent", for example for a declaration without a body.
func (s Span) Empty() bool { return s.End <= s.Start }

// Pos is a 1-based line and column. Columns count Unicode code points.
type Pos struct{ Line, Col int }

// TokenKind classifies lexical tokens.
type TokenKind uint8

const (
	TokIdent TokenKind = iota + 1
	TokKeyword
	TokString // string, char, template, raw and regex literals
	TokNumber
	TokPunct // operators, delimiters, preprocessor '#'
	TokComment
)

func (k TokenKind) String() string {
	switch k {
	case TokIdent:
		return "ident"
	case TokKeyword:
		return "keyword"
	case TokString:
		return "string"
	case TokNumber:
		return "number"
	case TokPunct:
		return "punct"
	case TokComment:
		return "comment"
	}
	return "invalid"
}

// Token is one lexical token. Its text is File.Src[Span.Start:Span.End].
type Token struct {
	Kind TokenKind
	Span Span
}

// CommentKind distinguishes comment syntaxes.
type CommentKind uint8

const (
	LineComment  CommentKind = iota + 1 // //, #
	BlockComment                        // /* */
	// DocComment is documentation: /** */, /// and Python docstrings. The
	// docstrings are also emitted as TokString tokens.
	DocComment
)

// Comment is a comment or docstring, including its delimiters.
type Comment struct {
	Kind CommentKind
	Span Span
}

// DeclKind classifies declarations.
type DeclKind uint8

const (
	KindModule DeclKind = iota + 1 // file, module or package root
	KindNamespace
	KindClass // classes and records
	KindInterface
	KindStruct // structs and unions
	KindEnum
	KindTypeAlias // typedef, type X = Y, Go defined types that are not struct/interface
	KindFunction
	KindMethod
	KindConstructor
	KindField
	KindProperty
	KindEnumMember
	KindVariable
	KindConstant
	KindParameter
	KindTypeParameter
	// KindImport is a file-local alias created by an import, like the `z` in
	// `from x import y as z` or `import numpy as np`. Imports without an alias
	// don't create a declaration. Their references target the imported entity.
	KindImport
	KindMacro // C/C++ #define
)

var declKindNames = [...]string{
	KindModule:        "module",
	KindNamespace:     "namespace",
	KindClass:         "class",
	KindInterface:     "interface",
	KindStruct:        "struct",
	KindEnum:          "enum",
	KindTypeAlias:     "type",
	KindFunction:      "function",
	KindMethod:        "method",
	KindConstructor:   "constructor",
	KindField:         "field",
	KindProperty:      "property",
	KindEnumMember:    "enum_member",
	KindVariable:      "variable",
	KindConstant:      "constant",
	KindParameter:     "parameter",
	KindTypeParameter: "type_parameter",
	KindImport:        "import",
	KindMacro:         "macro",
}

func (k DeclKind) String() string {
	if int(k) < len(declKindNames) && declKindNames[k] != "" {
		return declKindNames[k]
	}
	return "invalid"
}

// Decl is a named declaration.
type Decl struct {
	Name string
	Kind DeclKind
	File *File
	// NameSpan covers the identifier. The module declaration of a file has
	// an empty NameSpan at offset 0.
	NameSpan Span
	// Span covers the whole declaration, from leading decorators/annotations
	// and modifiers to the end of the body or statement. Leading comments
	// aren't included.
	Span Span
	// Body covers the {...} block or the indented suite, if any.
	Body   Span
	Parent *Decl // nil only for File.Module
	// Children are the declarations nested directly inside, in source order.
	Children []*Decl
	// Local marks declarations inside function bodies, parameters included.
	// FQNs join local segments with "->" instead of ".".
	Local bool
	// Receiver names the type of a method declared outside the type body, as
	// Go methods ("Calc") and C++ out-of-line definitions ("ns::Calc" is
	// written as "ns.Calc") are. Parent stays the enclosing scope.
	Receiver string
	// Type is the declared type of a variable, parameter, field or function
	// result as written, if known. Frontends use it to resolve member access.
	Type string
	// Bases lists base classes and implemented interfaces as written.
	Bases []string
}

// FQN returns the fully qualified name, with segments joined by "." and
// local symbols appended with "->", as in "calculator.calculate->a" or
// "internal/calc.Calc.Add".
func (d *Decl) FQN() string {
	var chain []*Decl
	for p := d; p != nil; p = p.Parent {
		chain = append(chain, p)
	}
	var b strings.Builder
	for _, p := range slices.Backward(chain) {
		if p.Name == "" {
			continue
		}
		if b.Len() > 0 {
			if p.Local {
				b.WriteString("->")
			} else {
				b.WriteByte('.')
			}
		}
		if p.Receiver != "" {
			b.WriteString(p.Receiver)
			b.WriteByte('.')
		}
		b.WriteString(p.Name)
	}
	return b.String()
}

// Owner returns d, or the nearest enclosing non-local declaration if d is
// local.
func (d *Decl) Owner() *Decl {
	for p := d; p != nil; p = p.Parent {
		if !p.Local {
			return p
		}
	}
	return d
}

// Callable reports whether d is a function or method, constructors included.
func (d *Decl) Callable() bool {
	return d.Kind == KindFunction || d.Kind == KindMethod || d.Kind == KindConstructor
}

// IsType reports whether d declares a type.
func (d *Decl) IsType() bool {
	switch d.Kind {
	case KindClass, KindInterface, KindStruct, KindEnum, KindTypeAlias:
		return true
	}
	return false
}

// Ref is an identifier that refers to a declaration.
type Ref struct {
	Name string
	Span Span
	File *File
	// Scope is the innermost enclosing declaration with a body, or the file's
	// Module.
	Scope *Decl
	// Target is nil if the reference is unresolved or points outside the
	// workspace, like into the standard library or a third-party package.
	Target *Decl
	// Call marks the callee of a call or of an instantiation like new F().
	Call bool
	// Import marks identifiers inside import statements.
	Import bool
}

// File is one parsed source file.
type File struct {
	Path     string // slash-separated, relative to Index.Root
	Abs      string // absolute filesystem path
	Lang     Lang
	Src      []byte
	Tokens   []Token // all tokens including comments, in source order
	Comments []Comment
	Module   *Decl
	Decls    []*Decl // all declarations except Module, in source order
	Refs     []*Ref  // in source order
	lines    []int   // byte offset of each line start
}

// NewFile creates a file with its line table. Frontends fill the rest.
func NewFile(path, abs string, lang Lang, src []byte) *File {
	f := &File{Path: path, Abs: abs, Lang: lang, Src: src}
	f.lines = append(f.lines, 0)
	for i, c := range src {
		if c == '\n' {
			f.lines = append(f.lines, i+1)
		}
	}
	return f
}

// SetModule creates the root declaration named name, the module part of FQNs.
// name is "" if the language has no file-level namespace.
func (f *File) SetModule(name string) *Decl {
	f.Module = &Decl{Name: name, Kind: KindModule, File: f, Span: Span{0, len(f.Src)}}
	return f.Module
}

// NewDecl creates a declaration, links it under parent and records it in
// f.Decls. Declarations nested in a function body or in another local
// declaration are marked Local, but frontends can override that.
func (f *File) NewDecl(parent *Decl, kind DeclKind, name string, nameSpan, span Span) *Decl {
	d := &Decl{Name: name, Kind: kind, File: f, NameSpan: nameSpan, Span: span, Parent: parent}
	if parent != nil {
		parent.Children = append(parent.Children, d)
		d.Local = parent.Local || parent.Callable()
	}
	f.Decls = append(f.Decls, d)
	return d
}

// NewRef records a reference.
func (f *File) NewRef(scope *Decl, name string, span Span) *Ref {
	r := &Ref{Name: name, Span: span, File: f, Scope: scope}
	f.Refs = append(f.Refs, r)
	return r
}

// Text returns the source text of s.
func (f *File) Text(s Span) string { return string(f.Src[s.Start:s.End]) }

// TokenText returns the source text of t.
func (f *File) TokenText(t Token) string { return f.Text(t.Span) }

// sortDecls restores source order after a frontend appended out of order.
func (f *File) sortDecls() {
	slices.SortStableFunc(f.Decls, func(a, b *Decl) int { return cmp.Compare(a.NameSpan.Start, b.NameSpan.Start) })
	slices.SortStableFunc(f.Refs, func(a, b *Ref) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	var sortChildren func(d *Decl)
	sortChildren = func(d *Decl) {
		slices.SortStableFunc(d.Children, func(a, b *Decl) int { return cmp.Compare(a.NameSpan.Start, b.NameSpan.Start) })
		for _, c := range d.Children {
			sortChildren(c)
		}
	}
	if f.Module != nil {
		sortChildren(f.Module)
	}
}

// DeclAt returns the innermost declaration of f whose Span contains off, or
// f.Module.
func (f *File) DeclAt(off int) *Decl {
	best := f.Module
	for _, d := range f.Decls {
		if d.Span.Contains(off) && (best == f.Module || best.Span.Encloses(d.Span)) {
			best = d
		}
	}
	return best
}

// Frontend parses and resolves the files of one or more languages.
type Frontend interface {
	// Lang reports the language of path, mostly by its extension. The C/C++
	// frontend also looks at neighbouring sources and header contents to
	// classify .h files.
	Lang(path string) (Lang, bool)
	// Parse fills f.Tokens, f.Comments, f.Module, f.Decls and f.Refs. It
	// sets every field of a Ref except Target. It has to tolerate syntax
	// errors, and should only return an error if there's nothing useful to
	// extract.
	Parse(f *File) error
	// Resolve sets Ref.Target for the refs of files, which are all of this
	// frontend's files in ix. It can look at any file in ix.
	Resolve(ix *Index, files []*File)
}
