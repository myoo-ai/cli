package javascript

import "github.com/myoo-ai/cli/internal/code"

type scopeKind uint8

const (
	scopeModule scopeKind = iota
	scopeFunction
	scopeArrow
	scopeBlock
	scopeClass
	scopeNamespace
)

// scope is a lexical scope. Declarations bind names. An unaliased import
// binds a name to a module export, which we look up during resolution.
type scope struct {
	parent  *scope
	kind    scopeKind
	names   map[string][]*code.Decl
	imports map[string]importBinding
}

type importBinding struct {
	spec string // module specifier as written
	name string // exported name
}

func (s *scope) bind(name string, d *code.Decl) {
	if s.names == nil {
		s.names = map[string][]*code.Decl{}
	}
	s.names[name] = append(s.names[name], d)
}

// shadow makes name resolve to nothing inside s. We need this for type
// variables that have no declaration (mapped type keys, infer).
func (s *scope) shadow(name string) {
	if s.names == nil {
		s.names = map[string][]*code.Decl{}
	}
	if _, ok := s.names[name]; !ok {
		s.names[name] = nil
	}
}

func (s *scope) bindImport(name string, b importBinding) {
	if s.imports == nil {
		s.imports = map[string]importBinding{}
	}
	s.imports[name] = b
}

// varScope returns the scope that `var` declarations and parameters bind
// into.
func (s *scope) varScope() *scope {
	for s.parent != nil && (s.kind == scopeBlock || s.kind == scopeClass) {
		s = s.parent
	}
	return s
}

type objKind uint8

const (
	objNone    objKind = iota // not a member access
	objRef                    // obj is a path whose last identifier is objRef
	objThis                   // this.x
	objSuper                  // super.x
	objRequire                // require('spec').x
	objOther                  // any other expression
)

// refMeta is the extra information about a reference that resolution needs
// on top of the code.Ref fields.
type refMeta struct {
	sc       *scope
	obj      objKind
	objRef   *code.Ref
	thisDecl *code.Decl // enclosing class or object literal owner for this/super
	spec     string     // module specifier, if the ref names an export of that module
	typePos  bool       // the ref is in a type position
	state    uint8      // resolution state: 0 pending, 1 in progress, 2 done
}

// alias describes what a KindImport declaration stands for.
type alias struct {
	spec string // module specifier, where name "*" means the module itself
	name string
	sc   *scope    // local alias whose name is looked up in sc
	ref  *code.Ref // TS `import A = B.C`: the target of ref
}

// export is one entry of a module's export table.
type export struct {
	decl *code.Decl // the exported declaration
	sc   *scope     // otherwise the exported local name, looked up in sc
	name string     // local name, or the name exported by spec
	spec string     // re-export from another module
}

// parseResult is the side information Resolve needs from a parsed file.
type parseResult struct {
	module    *scope
	refs      map[*code.Ref]*refMeta
	typeRef   map[*code.Decl]*code.Ref   // head of the declared or constructed type
	bases     map[*code.Decl][]*code.Ref // heads of extends/implements clauses
	aliases   map[*code.Decl]alias
	shorthand map[*code.Decl]map[string]*code.Ref // {name} entries of object literals
	exports   map[string]export
	stars     []string // export * from
	globals   []*code.Decl
	esm       bool // has import/export statements or CommonJS require/exports
	cjsWhole  bool // module.exports = v or `export = v`: members of v are module members
}

func newParseResult() *parseResult {
	return &parseResult{
		refs:      map[*code.Ref]*refMeta{},
		typeRef:   map[*code.Decl]*code.Ref{},
		bases:     map[*code.Decl][]*code.Ref{},
		aliases:   map[*code.Decl]alias{},
		shorthand: map[*code.Decl]map[string]*code.Ref{},
		exports:   map[string]export{},
	}
}
