package golang

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// walker makes two passes over a file. The first creates the declarations
// and the second turns the identifiers left over into references.
type walker struct {
	f  *code.File
	tf *token.File
	// Identifiers in skip are neither declarations nor references, like
	// labels, the package clause and blank or dot import names.
	declared map[*ast.Ident]bool
	skip     map[*ast.Ident]bool
	calls    map[*ast.Ident]bool
	// Ref.Scope of the identifiers below each node
	scopes map[ast.Node]*code.Decl
	// names declared in each open block, to tell a new variable of `:=` from
	// a redeclared one
	blocks []map[string]bool
	// Anything declared inside a function body is local.
	funcDepth int
}

func newWalker(f *code.File, tf *token.File) *walker {
	return &walker{
		f:        f,
		tf:       tf,
		declared: map[*ast.Ident]bool{},
		skip:     map[*ast.Ident]bool{},
		calls:    map[*ast.Ident]bool{},
		scopes:   map[ast.Node]*code.Decl{},
	}
}

func (w *walker) off(p token.Pos) int {
	if !p.IsValid() {
		return 0
	}
	return min(max(w.tf.Offset(p), 0), len(w.f.Src))
}

func (w *walker) span(n ast.Node) code.Span {
	return code.Span{Start: w.off(n.Pos()), End: w.off(n.End())}
}

// text returns the source of n with whitespace runs collapsed. We use it
// for Decl.Type.
func (w *walker) text(n ast.Node) string {
	if n == nil {
		return ""
	}
	return strings.Join(strings.Fields(w.f.Text(w.span(n))), " ")
}

func (w *walker) newDecl(parent *code.Decl, kind code.DeclKind, id *ast.Ident, start, end token.Pos) *code.Decl {
	w.declared[id] = true
	d := w.f.NewDecl(parent, kind, id.Name, w.span(id), code.Span{Start: w.off(start), End: w.off(end)})
	if w.funcDepth > 0 || kind == code.KindParameter || kind == code.KindTypeParameter {
		d.Local = true
	}
	return d
}

func (w *walker) push() { w.blocks = append(w.blocks, map[string]bool{}) }
func (w *walker) pop()  { w.blocks = w.blocks[:len(w.blocks)-1] }

func (w *walker) declare(name string) {
	if len(w.blocks) > 0 {
		w.blocks[len(w.blocks)-1][name] = true
	}
}

func (w *walker) inBlock(name string) bool {
	return len(w.blocks) > 0 && w.blocks[len(w.blocks)-1][name]
}

func (w *walker) file(af *ast.File) {
	w.skip[af.Name] = true
	for _, d := range af.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			w.genDecl(d, w.f.Module)
		case *ast.FuncDecl:
			w.funcDecl(d, w.f.Module)
		}
	}
	w.refs(af)
}

func (w *walker) genDecl(d *ast.GenDecl, parent *code.Decl) {
	for _, spec := range d.Specs {
		start := spec.Pos()
		if !d.Lparen.IsValid() {
			start = d.TokPos
		}
		switch s := spec.(type) {
		case *ast.ImportSpec:
			if s.Name == nil {
				continue
			}
			if s.Name.Name == "_" || s.Name.Name == "." {
				w.skip[s.Name] = true
				continue
			}
			imp := w.newDecl(parent, code.KindImport, s.Name, start, s.End())
			imp.Type, _ = strconv.Unquote(s.Path.Value)
		case *ast.TypeSpec:
			w.typeSpec(s, parent, start)
		case *ast.ValueSpec:
			w.valueSpec(d.Tok, s, parent, start)
		}
	}
}

func (w *walker) valueSpec(tok token.Token, s *ast.ValueSpec, parent *code.Decl, start token.Pos) {
	kind := code.KindVariable
	if tok == token.CONST {
		kind = code.KindConstant
	}
	w.expr(s.Type, parent)
	paired := len(s.Values) == len(s.Names)
	var named []*code.Decl
	for i, name := range s.Names {
		named = append(named, nil)
		if name.Name == "_" {
			continue
		}
		if lit, ok := funcLitValue(s.Values, i, paired); ok && kind == code.KindVariable && s.Type == nil {
			fn := w.newDecl(parent, code.KindFunction, name, start, s.End())
			fn.Body = w.span(lit.Body)
			named[i] = fn
			continue
		}
		d := w.newDecl(parent, kind, name, start, s.End())
		d.Type = w.text(s.Type)
		if d.Type == "" && paired {
			d.Type = w.initType(s.Values[i])
		}
	}
	for i, v := range s.Values {
		if i < len(named) && named[i] != nil {
			w.funcLit(v.(*ast.FuncLit), named[i], named[i])
			continue
		}
		w.expr(v, parent)
	}
	for _, name := range s.Names {
		w.declare(name.Name)
	}
}

func funcLitValue(values []ast.Expr, i int, paired bool) (*ast.FuncLit, bool) {
	if !paired {
		return nil, false
	}
	lit, ok := values[i].(*ast.FuncLit)
	return lit, ok && lit.Body != nil
}

// initType returns the type of a composite literal initializer like `T{}` or
// `&T{}`, which is how Go spells a constructor call.
func (w *walker) initType(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	if cl, ok := e.(*ast.CompositeLit); ok && cl.Type != nil {
		return w.text(cl.Type)
	}
	return ""
}

func (w *walker) typeSpec(s *ast.TypeSpec, parent *code.Decl, start token.Pos) {
	kind := code.KindTypeAlias
	switch s.Type.(type) {
	case *ast.StructType:
		kind = code.KindStruct
	case *ast.InterfaceType:
		kind = code.KindInterface
	}
	d := w.newDecl(parent, kind, s.Name, start, s.End())
	w.declare(s.Name.Name)
	w.typeParams(s.TypeParams, d)
	switch t := s.Type.(type) {
	case *ast.StructType:
		w.typeBody(s, t.Fields, d)
		for _, fld := range fieldList(t.Fields) {
			if len(fld.Names) == 0 {
				d.Bases = append(d.Bases, w.text(unstar(fld.Type)))
			}
		}
		w.fields(t.Fields, d)
	case *ast.InterfaceType:
		w.typeBody(s, t.Methods, d)
		for _, fld := range fieldList(t.Methods) {
			switch fld.Type.(type) {
			case *ast.Ident, *ast.SelectorExpr:
				if len(fld.Names) == 0 {
					d.Bases = append(d.Bases, w.text(fld.Type))
				}
			}
		}
		w.ifaceMethods(t, d)
	default:
		d.Type = w.text(s.Type)
		w.expr(s.Type, d)
	}
}

func (w *walker) typeBody(s *ast.TypeSpec, fields *ast.FieldList, d *code.Decl) {
	w.scopes[s] = d
	if fields != nil && fields.Closing.IsValid() {
		d.Body = code.Span{Start: w.off(fields.Opening), End: w.off(fields.Closing) + 1}
	}
}

func fieldList(fl *ast.FieldList) []*ast.Field {
	if fl == nil {
		return nil
	}
	return fl.List
}

func unstar(e ast.Expr) ast.Expr {
	if s, ok := e.(*ast.StarExpr); ok {
		return s.X
	}
	return e
}

// embeddedName returns the identifier that names an embedded field, which is
// T in `T`, `*T`, `pkg.T` and `T[int]`.
func embeddedName(e ast.Expr) *ast.Ident {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.SelectorExpr:
			return t.Sel
		case *ast.Ident:
			return t
		default:
			return nil
		}
	}
}

// fields declares struct fields under parent. An embedded field gets a
// field named after its type, and its identifier also counts as a
// reference to that type.
func (w *walker) fields(fl *ast.FieldList, parent *code.Decl) {
	for _, fld := range fieldList(fl) {
		if len(fld.Names) == 0 {
			if id := embeddedName(fld.Type); id != nil {
				d := w.f.NewDecl(parent, code.KindField, id.Name, w.span(id), w.span(fld))
				d.Local = d.Local || w.funcDepth > 0
				d.Type = w.text(fld.Type)
			}
			w.expr(fld.Type, parent)
			continue
		}
		var first *code.Decl
		for _, name := range fld.Names {
			if name.Name == "_" {
				continue
			}
			d := w.newDecl(parent, code.KindField, name, fld.Pos(), fld.End())
			d.Type = w.text(fld.Type)
			if first == nil {
				first = d
			}
		}
		if first == nil {
			first = parent
		}
		w.expr(fld.Type, first)
	}
}

func (w *walker) ifaceMethods(t *ast.InterfaceType, parent *code.Decl) {
	for _, fld := range fieldList(t.Methods) {
		ft, ok := fld.Type.(*ast.FuncType)
		if len(fld.Names) == 0 || !ok {
			w.expr(fld.Type, parent)
			continue
		}
		for _, name := range fld.Names {
			m := w.newDecl(parent, code.KindMethod, name, fld.Pos(), fld.End())
			m.Type = w.results(ft)
			w.params(ft.Params, m)
			w.params(ft.Results, m)
		}
	}
}

func (w *walker) results(ft *ast.FuncType) string {
	if ft == nil || ft.Results == nil {
		return ""
	}
	return w.text(ft.Results)
}

// receiverType returns the receiver's base type name and the identifiers
// of its type parameters.
func receiverType(e ast.Expr) (*ast.Ident, []ast.Expr) {
	e = unstar(e)
	if p, ok := e.(*ast.ParenExpr); ok {
		e = unstar(p.X)
	}
	switch t := e.(type) {
	case *ast.IndexExpr:
		id, _ := t.X.(*ast.Ident)
		return id, []ast.Expr{t.Index}
	case *ast.IndexListExpr:
		id, _ := t.X.(*ast.Ident)
		return id, t.Indices
	case *ast.Ident:
		return t, nil
	}
	return nil, nil
}

func (w *walker) funcDecl(d *ast.FuncDecl, parent *code.Decl) {
	kind, recvName := code.KindFunction, ""
	var recv *ast.Field
	var recvParams []ast.Expr
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind, recv = code.KindMethod, d.Recv.List[0]
		var id *ast.Ident
		id, recvParams = receiverType(recv.Type)
		if id != nil {
			recvName = id.Name
		}
	}
	fn := w.newDecl(parent, kind, d.Name, d.Pos(), d.End())
	fn.Receiver = recvName
	fn.Type = w.results(d.Type)
	if d.Body != nil {
		fn.Body = w.span(d.Body)
		w.scopes[d] = fn
	}
	w.push()
	if recv != nil {
		for _, name := range recv.Names {
			if name.Name != "_" {
				p := w.newDecl(fn, code.KindParameter, name, recv.Pos(), recv.End())
				p.Type = w.text(recv.Type)
				w.declare(name.Name)
			}
		}
		for _, tp := range recvParams {
			if id, ok := tp.(*ast.Ident); ok && id.Name != "_" {
				w.newDecl(fn, code.KindTypeParameter, id, id.Pos(), id.End())
				w.declare(id.Name)
			}
		}
	}
	w.typeParams(d.Type.TypeParams, fn)
	w.params(d.Type.Params, fn)
	w.params(d.Type.Results, fn)
	if d.Body != nil {
		w.funcDepth++
		w.stmts(d.Body.List, fn)
		w.funcDepth--
	}
	w.pop()
}

func (w *walker) typeParams(fl *ast.FieldList, parent *code.Decl) {
	w.parameterFields(fl, parent, code.KindTypeParameter)
}

func (w *walker) params(fl *ast.FieldList, parent *code.Decl) {
	w.parameterFields(fl, parent, code.KindParameter)
}

func (w *walker) parameterFields(fl *ast.FieldList, parent *code.Decl, kind code.DeclKind) {
	for _, fld := range fieldList(fl) {
		for _, name := range fld.Names {
			if name.Name == "_" {
				continue
			}
			p := w.newDecl(parent, kind, name, fld.Pos(), fld.End())
			p.Type = w.text(fld.Type)
			w.declare(name.Name)
		}
		w.expr(fld.Type, parent)
	}
}

// funcLit walks a function literal. If the literal is assigned to a name,
// named is set and the literal becomes that function. An anonymous
// literal's parameters and locals belong to the enclosing declaration.
func (w *walker) funcLit(lit *ast.FuncLit, parent, named *code.Decl) {
	owner := parent
	if named != nil {
		owner = named
		w.scopes[lit] = named
	}
	w.push()
	w.typeParams(lit.Type.TypeParams, owner)
	w.params(lit.Type.Params, owner)
	w.params(lit.Type.Results, owner)
	if named != nil {
		named.Type = w.results(lit.Type)
	}
	w.funcDepth++
	w.stmts(lit.Body.List, owner)
	w.funcDepth--
	w.pop()
}

func (w *walker) stmts(list []ast.Stmt, parent *code.Decl) {
	for _, s := range list {
		w.stmt(s, parent)
	}
}

func (w *walker) block(b *ast.BlockStmt, parent *code.Decl) {
	if b == nil {
		return
	}
	w.push()
	w.stmts(b.List, parent)
	w.pop()
}

func (w *walker) stmt(s ast.Stmt, parent *code.Decl) {
	switch s := s.(type) {
	case nil:
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok {
			w.genDecl(gd, parent)
		}
	case *ast.AssignStmt:
		w.assign(s, parent)
	case *ast.BlockStmt:
		w.block(s, parent)
	case *ast.IfStmt:
		w.push()
		w.stmt(s.Init, parent)
		w.expr(s.Cond, parent)
		w.block(s.Body, parent)
		w.stmt(s.Else, parent)
		w.pop()
	case *ast.ForStmt:
		w.push()
		w.stmt(s.Init, parent)
		w.expr(s.Cond, parent)
		w.stmt(s.Post, parent)
		w.block(s.Body, parent)
		w.pop()
	case *ast.RangeStmt:
		w.rangeStmt(s, parent)
	case *ast.SwitchStmt:
		w.push()
		w.stmt(s.Init, parent)
		w.expr(s.Tag, parent)
		w.clauses(s.Body, parent)
		w.pop()
	case *ast.TypeSwitchStmt:
		w.push()
		w.stmt(s.Init, parent)
		if as, ok := s.Assign.(*ast.AssignStmt); ok && as.Tok == token.DEFINE && len(as.Lhs) == 1 {
			for _, r := range as.Rhs {
				w.expr(r, parent)
			}
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
				w.newDecl(parent, code.KindVariable, id, as.Pos(), as.End())
			}
		} else {
			w.stmt(s.Assign, parent)
		}
		w.clauses(s.Body, parent)
		w.pop()
	case *ast.SelectStmt:
		w.clauses(s.Body, parent)
	case *ast.LabeledStmt:
		w.skip[s.Label] = true
		w.stmt(s.Stmt, parent)
	case *ast.BranchStmt:
		if s.Label != nil {
			w.skip[s.Label] = true
		}
	default:
		w.expr(s, parent)
	}
}

func (w *walker) rangeStmt(s *ast.RangeStmt, parent *code.Decl) {
	w.push()
	w.expr(s.X, parent)
	if s.Tok == token.DEFINE {
		end := s.Range
		if s.X != nil {
			end = s.X.End()
		}
		for _, e := range []ast.Expr{s.Key, s.Value} {
			if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
				w.newDecl(parent, code.KindVariable, id, s.Key.Pos(), end)
				w.declare(id.Name)
			}
		}
	} else {
		w.expr(s.Key, parent)
		w.expr(s.Value, parent)
	}
	w.block(s.Body, parent)
	w.pop()
}

func (w *walker) clauses(body *ast.BlockStmt, parent *code.Decl) {
	if body == nil {
		return
	}
	for _, c := range body.List {
		w.push()
		switch c := c.(type) {
		case *ast.CaseClause:
			for _, e := range c.List {
				w.expr(e, parent)
			}
			w.stmts(c.Body, parent)
		case *ast.CommClause:
			w.stmt(c.Comm, parent)
			w.stmts(c.Body, parent)
		}
		w.pop()
	}
}

func (w *walker) assign(s *ast.AssignStmt, parent *code.Decl) {
	if s.Tok != token.DEFINE {
		for _, e := range s.Rhs {
			w.expr(e, parent)
		}
		for _, e := range s.Lhs {
			w.expr(e, parent)
		}
		return
	}
	paired := len(s.Lhs) == len(s.Rhs)
	var named *code.Decl
	if id, ok := s.Lhs[0].(*ast.Ident); ok && len(s.Lhs) == 1 && id.Name != "_" && !w.inBlock(id.Name) {
		if lit, ok := funcLitValue(s.Rhs, 0, paired); ok {
			named = w.newDecl(parent, code.KindFunction, id, s.Pos(), s.End())
			named.Body = w.span(lit.Body)
			w.funcLit(lit, named, named)
			w.declare(id.Name)
			return
		}
	}
	for _, e := range s.Rhs {
		w.expr(e, parent)
	}
	for i, e := range s.Lhs {
		id, ok := e.(*ast.Ident)
		if !ok {
			w.expr(e, parent)
			continue
		}
		if id.Name == "_" || w.inBlock(id.Name) {
			continue
		}
		d := w.newDecl(parent, code.KindVariable, id, s.Pos(), s.End())
		if paired {
			d.Type = w.initType(s.Rhs[i])
		}
		w.declare(id.Name)
	}
}

// expr walks an expression or type to mark callee identifiers and to find
// the declarations nested in it, like struct fields, interface methods and
// the parameters of function literals and function types.
func (w *walker) expr(n ast.Node, parent *code.Decl) {
	if n == nil {
		return
	}
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			w.funcLit(n, parent, nil)
			return false
		case *ast.StructType:
			w.fields(n.Fields, parent)
			return false
		case *ast.InterfaceType:
			w.ifaceMethods(n, parent)
			return false
		case *ast.FuncType:
			w.typeParams(n.TypeParams, parent)
			w.params(n.Params, parent)
			w.params(n.Results, parent)
			return false
		case *ast.CallExpr:
			w.markCall(n.Fun)
		}
		return true
	})
}

// markCall flags the callee identifier, like f in f(), x.f(), f[T]() or
// (*T)(x).
func (w *walker) markCall(e ast.Expr) {
	for {
		switch t := e.(type) {
		case *ast.ParenExpr:
			e = t.X
		case *ast.StarExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.SelectorExpr:
			w.calls[t.Sel] = true
			return
		case *ast.Ident:
			w.calls[t] = true
			return
		default:
			return
		}
	}
}

// literalNames are predeclared identifiers that act like literals, so we
// don't report them as references.
var literalNames = map[string]bool{"_": true, "nil": true, "true": true, "false": true, "iota": true}

// refs records the identifiers that don't name a declaration. An import
// path gives a reference named after its last element.
func (w *walker) refs(af *ast.File) {
	var stack []ast.Node
	scopes := []*code.Decl{w.f.Module}
	ast.Inspect(af, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if _, ok := w.scopes[top]; ok {
				scopes = scopes[:len(scopes)-1]
			}
			return true
		}
		switch n := n.(type) {
		case *ast.ImportSpec:
			w.importRef(n, scopes[len(scopes)-1])
			return false
		case *ast.Ident:
			if !w.declared[n] && !w.skip[n] && !literalNames[n.Name] && n.Pos().IsValid() {
				r := w.f.NewRef(scopes[len(scopes)-1], n.Name, w.span(n))
				r.Call = w.calls[n]
			}
		}
		stack = append(stack, n)
		if d, ok := w.scopes[n]; ok {
			scopes = append(scopes, d)
		}
		return true
	})
}

func (w *walker) importRef(s *ast.ImportSpec, scope *code.Decl) {
	p, err := strconv.Unquote(s.Path.Value)
	if err != nil {
		return
	}
	name := p[strings.LastIndexByte(p, '/')+1:]
	if !token.IsIdentifier(name) {
		return
	}
	end := w.off(s.Path.End()) - 1
	r := w.f.NewRef(scope, name, code.Span{Start: end - len(name), End: end})
	r.Import = true
}
