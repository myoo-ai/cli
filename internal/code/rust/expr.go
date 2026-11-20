package rust

import (
	"github.com/myoo-ai/cli/internal/code"
)

var (
	stopSemi  = code.StringSet(";")
	stopComma = code.StringSet(",")
	stopList  = code.StringSet(",", ";")
	stopArm   = code.StringSet("=>", "if")
	stopLet   = code.StringSet("=", ":", ";")
	stopEq    = code.StringSet("=")
	stopIn    = code.StringSet("in")
	stopParam = code.StringSet(",", ":")
	stopClos  = code.StringSet(",", "|", ":")
	stopChain = code.StringSet("&&", "||")
	stopElse  = code.StringSet(";", "else")
	stopFat   = code.StringSet("=>")
)

// We don't create references to the built-in types.
var primitives = code.StringSet("i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64",
	"u128", "usize", "f32", "f64", "bool", "char", "str")

// wrappers are smart pointers and cells. The resolver sees through them to
// their first type argument, so a Box<Foo> has the members of Foo.
var wrappers = code.StringSet("Box", "Rc", "Arc", "RefCell", "Cell", "Mutex", "RwLock", "Cow", "Pin")

// block parses the block that opens at p.i and closes at cl.
func (p *parser) block(sc *scope, cl int) {
	p.i++
	bs := newScope(scopeBlock, sc, sc.owner)
	for p.i < cl {
		start := p.i
		switch {
		case p.is(p.i, ";"):
			p.i++
		case p.is(p.i, "let"):
			p.letStmt(bs, cl)
		case p.itemStarts(p.i):
			if !p.item(bs, cl, nil) {
				p.exprStmt(bs, cl)
			}
		default:
			p.exprStmt(bs, cl)
		}
		if p.i == start {
			p.i++
		}
	}
	p.i = cl + 1
}

// blockExpr parses the block at p.i and leaves p.i after it.
func (p *parser) blockExpr(sc *scope, end int) {
	if !p.is(p.i, "{") {
		return
	}
	cl := p.closeOf(p.i, end)
	p.block(sc, cl)
	p.i = min(cl+1, end)
}

func (p *parser) exprStmt(sc *scope, end int) {
	p.expr(sc, end, stopSemi, false, true)
	if p.is(p.i, ";") {
		p.i++
	}
}

func (p *parser) letStmt(sc *scope, end int) {
	p.i++
	ds := p.typedPattern(sc, end, stopLet, code.KindVariable)
	if p.is(p.i, "=") {
		p.i++
		init := p.expr(sc, end, stopElse, false, false)
		if len(ds) == 1 {
			p.fi.decls[ds[0]].init = init
		}
	}
	if p.is(p.i, "else") {
		p.i++
		p.blockExpr(sc, end)
	}
	if p.is(p.i, ";") {
		p.i++
	}
	for _, d := range ds {
		p.fi.decls[d].visible = p.endOf(p.i)
		d.Span.End = p.endOf(p.i)
	}
}

// exprList parses comma or semicolon separated expressions up to cl.
func (p *parser) exprList(sc *scope, cl int) {
	for p.i < cl {
		start := p.i
		p.expr(sc, cl, stopList, false, false)
		if p.is(p.i, ",") || p.is(p.i, ";") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
}

// expr scans an expression up to a stop token or end. If the expression is a
// single operand chain like a.f() or A::new(), it returns what the chain
// evaluates to. noStruct turns off struct literals for conditions, where `{`
// opens the body. With stmt set, a leading block-like expression such as a
// match ends the statement if no `.` or `?` follows it.
func (p *parser) expr(sc *scope, end int, stops map[string]bool, noStruct, stmt bool) object {
	atStart, first, simple := true, true, true
	var cur object
	for p.i < end {
		s := p.cur()
		if stops[s] {
			break
		}
		kind := p.t[p.i].kind
		blockLike := false
		switch {
		case s == ")" || s == "]" || s == "}":
			return object{}
		case p.isLifetime(p.i):
			p.i++
			if p.is(p.i, ":") {
				p.i++
			}
			continue
		case atStart && (s == "|" || s == "||"):
			p.closure(sc, end, stops, noStruct)
			cur, simple = object{}, false
		case (s == "move" || s == "static" || s == "async") && (p.is(p.i+1, "|") || p.is(p.i+1, "||") || p.is(p.i+1, "move")):
			p.i++
			continue
		case s == "{" && !atStart && noStruct:
			return cur
		case p.blockLike(sc, end):
			cur, blockLike = object{}, true
		case s == "let":
			p.letCond(sc, end)
			cur, simple = object{}, false
		case s == "return" || s == "break" || s == "continue" || s == "yield" || s == "become":
			p.i++
			atStart, simple = true, false
			continue
		case s == "as":
			p.i++
			p.typ(sc, end)
			cur, simple = object{}, false
		case s == ".":
			p.i++
			cur = p.member(sc, end, cur)
		case s == "?":
			p.i++
		case s == "(" || s == "[":
			p.exprGroup(sc, end)
			cur = object{}
		case s == "<" && atStart:
			last := p.qpath(sc, end, false)
			var bl bool
			cur, bl = p.pathTail(sc, end, last, "", noStruct)
			blockLike = bl
		case p.isIdent(p.i) || kind == code.TokKeyword && (s == "self" || s == "Self" || s == "super" || s == "crate") || s == "::" && atStart:
			var bl bool
			cur, bl = p.pathExpr(sc, end, noStruct)
			blockLike = bl
		case kind == code.TokString || kind == code.TokNumber || s == "true" || s == "false":
			p.i++
			cur = object{}
		default:
			// Operators, plus tokens we don't recognize.
			p.i++
			atStart, simple = true, false
			cur = object{}
			first = false
			continue
		}
		atStart = false
		if blockLike && stmt && first && !p.is(p.i, ".") && !p.is(p.i, "?") {
			break
		}
		first = false
	}
	if !simple {
		return object{}
	}
	return cur
}

// blockLike parses a block, if, match, loop, while or for expression at
// p.i. These can end a statement without a semicolon. It returns false if
// there's none at p.i.
func (p *parser) blockLike(sc *scope, end int) bool {
	s := p.cur()
	switch {
	case s == "loop", (s == "async" || s == "unsafe" || s == "const" || s == "try") && p.is(p.i+1, "{"):
		p.i++
		p.blockExpr(sc, end)
	case s == "{":
		p.blockExpr(sc, end)
	case s == "if":
		p.ifExpr(sc, end)
	case s == "match":
		p.matchExpr(sc, end)
	case s == "while":
		p.whileExpr(sc, end)
	case s == "for":
		p.forExpr(sc, end)
	default:
		return false
	}
	return true
}

// member parses what follows a `.` at p.i, with obj as the receiver.
func (p *parser) member(sc *scope, end int, obj object) object {
	if !p.isIdent(p.i) {
		if p.i < end && (p.t[p.i].kind == code.TokNumber || p.is(p.i, "await")) {
			p.i++
		}
		return object{}
	}
	r := p.newRef(sc, p.i, refMember)
	p.fi.refs[r].obj = obj
	p.i++
	if p.is(p.i, "::") && p.is(p.i+1, "<") {
		p.i++
		p.genericArgs(sc, end)
	}
	if p.is(p.i, "(") {
		r.Call = true
		return object{ref: r, call: true}
	}
	return object{ref: r}
}

// path parses path segments starting at p.i. prev is set if the path
// continues a qualified type. With types set, `<` can open generic
// arguments without `::`, like in type positions. It returns the last
// segment's reference, the keyword prefix if the path is only keywords and
// the head of the last segment's first type argument.
func (p *parser) path(sc *scope, end int, prev *code.Ref, kw string, supers int, types bool) (*code.Ref, string, *code.Ref) {
	var argHead *code.Ref
	if prev == nil && p.is(p.i, "::") {
		p.i++
	}
	for p.i < end {
		s := p.cur()
		switch {
		case p.isIdent(p.i):
			argHead = nil
			if s == "_" {
				p.i++
				return prev, kw, nil
			}
			if prev == nil && kw == "" && primitives[s] {
				// A built-in type. It can qualify an associated item like
				// u8::MAX, but we can't resolve that to anything.
				p.i++
				if !p.is(p.i, "::") {
					return nil, "", nil
				}
				kw = "prim"
				break
			}
			kind := refSeg
			switch {
			case prev == nil && kw == "prim":
			case prev == nil && kw != "":
				kind = refPrefix
			case prev == nil:
				kind = refPlain
			}
			r := p.newRef(sc, p.i, kind)
			ri := p.fi.refs[r]
			ri.prev, ri.prefix, ri.supers = prev, kw, supers
			prev = r
			p.i++
		case s == "super":
			supers++
			if kw == "" {
				kw = "super"
			}
			p.i++
		case s == "crate" || s == "self" || s == "Self":
			kw = s
			p.i++
		default:
			return prev, kw, argHead
		}
		if types && p.is(p.i, "<") {
			argHead = p.genericArgs(sc, end)
		}
		if types && p.is(p.i, "(") && prev != nil {
			// Fn(A, B) -> C sugar.
			cl := p.closeOf(p.i, end)
			p.i++
			p.typeList(sc, cl)
			p.i = min(cl+1, end)
			if p.is(p.i, "->") {
				p.i++
				p.typ(sc, end)
			}
			return prev, kw, nil
		}
		if !p.is(p.i, "::") {
			break
		}
		if p.is(p.i+1, "<") {
			p.i++
			argHead = p.genericArgs(sc, end)
			if !p.is(p.i, "::") {
				break
			}
		}
		if n := p.text(p.i + 1); !p.isIdent(p.i+1) && n != "super" && n != "self" && n != "Self" && n != "crate" {
			break
		}
		p.i++
	}
	return prev, kw, argHead
}

// pathExpr parses a path in expression position and a macro invocation, a
// call or a struct literal after it.
func (p *parser) pathExpr(sc *scope, end int, noStruct bool) (object, bool) {
	last, kw, _ := p.path(sc, end, nil, "", 0, false)
	return p.pathTail(sc, end, last, kw, noStruct)
}

func (p *parser) pathTail(sc *scope, end int, last *code.Ref, kw string, noStruct bool) (object, bool) {
	obj := object{ref: last}
	if last == nil {
		obj = object{self: kw == "self" || kw == "Self"}
	}
	switch {
	case p.macroBang() && last != nil:
		p.markMacro(last)
		p.i++
		brace := p.is(p.i, "{")
		p.exprGroup(sc, end)
		return object{}, brace
	case p.is(p.i, "("):
		if last != nil {
			last.Call = true
		}
		cl := p.closeOf(p.i, end)
		p.i++
		p.exprList(sc, cl)
		p.i = min(cl+1, end)
		obj.call = true
	case p.is(p.i, "{") && !noStruct && (last != nil || kw == "Self"):
		p.structLit(sc, end, obj)
		obj.call = true
	}
	return obj, false
}

// structLit parses the fields of a struct literal of obj at p.i.
func (p *parser) structLit(sc *scope, end int, obj object) {
	cl := p.closeOf(p.i, end)
	p.i++
	for p.i < cl {
		start := p.i
		p.attrs(cl)
		switch {
		case p.is(p.i, ".."):
			p.i++
			p.expr(sc, cl, stopComma, false, false)
		case (p.isIdent(p.i) || p.i < cl && p.t[p.i].kind == code.TokNumber) && p.is(p.i+1, ":"):
			if p.isIdent(p.i) {
				r := p.newRef(sc, p.i, refField)
				p.fi.refs[r].obj = obj
			}
			p.i += 2
			p.expr(sc, cl, stopComma, false, false)
		default:
			p.expr(sc, cl, stopComma, false, false)
		}
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
	p.i = min(cl+1, end)
}

// qpath parses `<T as Trait>` at p.i and the `::` after it. It returns the
// reference that qualifies the next segment.
func (p *parser) qpath(sc *scope, end int, types bool) *code.Ref {
	p.i++
	head := p.typ(sc, end)
	if p.is(p.i, "as") {
		p.i++
		if t := p.typ(sc, end); t != nil {
			head = t
		}
	}
	p.closeAngle()
	if p.is(p.i, "::") {
		p.i++
	}
	last, _, _ := p.path(sc, end, head, "", 0, types)
	if last == nil {
		return head
	}
	return last
}

// genericArgs parses <...> arguments at p.i and returns the head of the
// first type argument.
func (p *parser) genericArgs(sc *scope, end int) *code.Ref {
	p.i++
	var first *code.Ref
	n := 0
	for p.i < end && !p.angleClose() {
		start := p.i
		h := p.genericArg(sc, end)
		if n == 0 {
			first = h
		}
		n++
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			if p.is(p.i, ";") || p.is(p.i, ")") || p.is(p.i, "]") || p.is(p.i, "}") || p.is(p.i, "{") {
				return first
			}
			p.i++
		}
	}
	p.closeAngle()
	return first
}

func (p *parser) genericArg(sc *scope, end int) *code.Ref {
	switch {
	case p.isLifetime(p.i):
		p.i++
	case p.is(p.i, "{"):
		p.blockExpr(sc, end)
	case p.i < end && (p.t[p.i].kind == code.TokNumber || p.t[p.i].kind == code.TokString || p.is(p.i, "-") || p.is(p.i, "true") || p.is(p.i, "false")):
		p.i++
		if p.i < end && p.t[p.i].kind == code.TokNumber {
			p.i++
		}
	case p.isIdent(p.i) && (p.is(p.i+1, "=") || p.is(p.i+1, ":")):
		bound := p.is(p.i+1, ":")
		p.i += 2
		if bound {
			p.bounds(sc, end)
		} else {
			p.typ(sc, end)
		}
	default:
		return p.typ(sc, end)
	}
	return nil
}

// typeList parses comma separated types up to cl.
func (p *parser) typeList(sc *scope, cl int) (n int, head *code.Ref) {
	for p.i < cl {
		start := p.i
		if p.isIdent(p.i) && p.is(p.i+1, ":") {
			p.i += 2 // named parameter of a fn pointer type
		}
		head = p.typ(sc, cl)
		n++
		if p.is(p.i, ",") {
			p.i++
		} else if p.i < cl {
			n++
		}
		if p.i == start {
			p.i++
		}
	}
	return n, head
}

// typ parses a type at p.i and returns the reference to its head type. The
// head can sit behind references and pointers or inside a smart pointer wrapper.
func (p *parser) typ(sc *scope, end int) *code.Ref {
	if p.i >= end {
		return nil
	}
	switch s := p.cur(); {
	case s == "&" || s == "&&":
		p.i++
		if p.isLifetime(p.i) {
			p.i++
		}
		if p.is(p.i, "mut") {
			p.i++
		}
		return p.typ(sc, end)
	case s == "*":
		p.i++
		if p.is(p.i, "const") || p.is(p.i, "mut") {
			p.i++
		}
		return p.typ(sc, end)
	case s == "(":
		cl := p.closeOf(p.i, end)
		p.i++
		n, head := p.typeList(sc, cl)
		p.i = min(cl+1, end)
		if n == 1 {
			return head
		}
		return nil
	case s == "[":
		cl := p.closeOf(p.i, end)
		p.i++
		p.typ(sc, cl)
		if p.is(p.i, ";") {
			p.i++
			p.expr(sc, cl, nil, false, false)
		}
		p.i = min(cl+1, end)
		return nil
	case s == "!" || s == "_":
		p.i++
		return nil
	case s == "unsafe" || s == "extern" || s == "fn":
		p.fnPointerType(sc, end)
		return nil
	case s == "impl" || s == "dyn":
		p.i++
		return p.bounds(sc, end)
	case s == "for" && p.is(p.i+1, "<"):
		p.i++
		p.skipAngles(end)
		return p.typ(sc, end)
	case s == "<":
		return p.qpath(sc, end, true)
	case p.isLifetime(p.i):
		p.i++
		return nil
	case p.isIdent(p.i) || s == "self" || s == "Self" || s == "super" || s == "crate" || s == "::":
		last, _, arg := p.path(sc, end, nil, "", 0, true)
		if p.macroBang() {
			p.markMacro(last)
			p.i++
			p.exprGroup(sc, end)
			return nil
		}
		if last != nil && wrappers[last.Name] && arg != nil {
			return arg
		}
		return last
	}
	return nil
}

// fnPointerType parses `unsafe extern "C" fn(A) -> B` at p.i.
func (p *parser) fnPointerType(sc *scope, end int) {
	for p.is(p.i, "unsafe") || p.is(p.i, "extern") || p.i < end && p.t[p.i].kind == code.TokString {
		p.i++
	}
	if !p.is(p.i, "fn") {
		return
	}
	p.i++
	if p.is(p.i, "(") {
		cl := p.closeOf(p.i, end)
		p.i++
		p.typeList(sc, cl)
		p.i = min(cl+1, end)
	}
	if p.is(p.i, "->") {
		p.i++
		p.typ(sc, end)
	}
}

// bounds parses trait bounds joined by `+` and returns the head of the
// first one.
func (p *parser) bounds(sc *scope, end int) *code.Ref {
	var first *code.Ref
	for p.i < end {
		start := p.i
		switch {
		case p.is(p.i, "?") || p.is(p.i, "~") || p.is(p.i, "const") || p.is(p.i, "async"):
			p.i++
			continue
		case p.isLifetime(p.i):
			p.i++
		case p.is(p.i, "(") && p.match[p.i] > p.i:
			cl := p.closeOf(p.i, end)
			p.i++
			if h := p.bounds(sc, cl); first == nil {
				first = h
			}
			p.i = min(cl+1, end)
		default:
			if h := p.typ(sc, end); first == nil {
				first = h
			}
		}
		if p.i == start || !p.is(p.i, "+") {
			return first
		}
		p.i++
	}
	return first
}

func (p *parser) closure(sc *scope, end int, stops map[string]bool, noStruct bool) {
	cs := newScope(scopeBlock, sc, sc.owner)
	if p.is(p.i, "||") {
		p.i++
	} else {
		p.i++
		for p.i < end && !p.is(p.i, "|") {
			start := p.i
			p.typedPattern(cs, end, stopClos, code.KindParameter)
			if p.is(p.i, ",") {
				p.i++
			}
			if p.i == start {
				p.i++
			}
		}
		if p.is(p.i, "|") {
			p.i++
		}
	}
	if p.is(p.i, "->") {
		p.i++
		p.typ(cs, end)
	}
	p.expr(cs, end, stops, noStruct, false)
}

func (p *parser) ifExpr(sc *scope, end int) {
	p.i++
	cs := newScope(scopeBlock, sc, sc.owner)
	p.expr(cs, end, nil, true, false)
	p.blockExpr(cs, end)
	if p.is(p.i, "else") {
		p.i++
		if p.is(p.i, "if") {
			p.ifExpr(sc, end)
		} else {
			p.blockExpr(sc, end)
		}
	}
}

// letCond parses `let PAT = EXPR` in an if or while condition. The bindings
// go into sc, which has to be the scope of the condition and the body.
func (p *parser) letCond(sc *scope, end int) {
	p.i++
	var ds []*code.Decl
	p.pattern(sc, end, stopEq, func(i int) {
		ds = append(ds, p.binding(sc, code.KindVariable, i, len(p.src)))
	})
	if p.is(p.i, "=") {
		p.i++
		p.expr(sc, end, stopChain, true, false)
	}
	for _, d := range ds {
		p.fi.decls[d].visible = p.startOf(p.i)
	}
}

func (p *parser) whileExpr(sc *scope, end int) {
	p.i++
	cs := newScope(scopeBlock, sc, sc.owner)
	p.expr(cs, end, nil, true, false)
	p.blockExpr(cs, end)
}

func (p *parser) forExpr(sc *scope, end int) {
	p.i++
	fs := newScope(scopeBlock, sc, sc.owner)
	var ds []*code.Decl
	p.pattern(fs, end, stopIn, func(i int) {
		ds = append(ds, p.binding(fs, code.KindVariable, i, len(p.src)))
	})
	if p.is(p.i, "in") {
		p.i++
	}
	p.expr(sc, end, nil, true, false)
	for _, d := range ds {
		p.fi.decls[d].visible = p.startOf(p.i)
	}
	p.blockExpr(fs, end)
}

func (p *parser) matchExpr(sc *scope, end int) {
	p.i++
	p.expr(sc, end, nil, true, false)
	if !p.is(p.i, "{") {
		return
	}
	cl := p.closeOf(p.i, end)
	p.i++
	for p.i < cl {
		start := p.i
		p.attrs(cl)
		as := newScope(scopeBlock, sc, sc.owner)
		if p.is(p.i, "|") {
			p.i++
		}
		p.pattern(as, cl, stopArm, func(i int) {
			p.binding(as, code.KindVariable, i, p.span(i).Start)
		})
		if p.is(p.i, "if") {
			p.i++
			p.expr(as, cl, stopFat, false, false)
		}
		if p.is(p.i, "=>") {
			p.i++
		}
		p.expr(as, cl, stopComma, false, true)
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
	p.i = min(cl+1, end)
}

// pattern parses a pattern up to a stop token and calls declare once for
// each bound name, since alternatives have to bind the same names.
func (p *parser) pattern(sc *scope, end int, stops map[string]bool, declare func(i int)) {
	seen := map[string]bool{}
	p.patternSeq(sc, end, stops, func(i int) {
		if n := p.name(i); !seen[n] {
			seen[n] = true
			declare(i)
		}
	})
}

func (p *parser) patternSeq(sc *scope, end int, stops map[string]bool, declare func(i int)) {
	for p.i < end {
		s := p.cur()
		if stops[s] {
			return
		}
		kind := p.t[p.i].kind
		switch {
		case s == ")" || s == "]" || s == "}":
			return
		case s == "(" || s == "[":
			p.patternGroup(sc, end, declare)
		case s == "ref" || s == "mut":
			p.i++
			if p.isIdent(p.i) && !p.is(p.i+1, "::") && !p.is(p.i+1, "(") && !p.is(p.i+1, "{") {
				declare(p.i)
				p.i++
			}
		case p.isIdent(p.i) && s != "_" && !p.is(p.i+1, "::") && !p.is(p.i+1, "(") && !p.is(p.i+1, "{") && !p.is(p.i+1, "!") && bindingName(s):
			declare(p.i)
			p.i++
		case p.isIdent(p.i) && s != "_" || kind == code.TokKeyword && (s == "self" || s == "Self" || s == "super" || s == "crate") || s == "::" || s == "<":
			p.pathPattern(sc, end, declare)
		default:
			// Literals, `_`, `..`, `&`, `|`, `@`, `-` and `box`.
			p.i++
		}
	}
}

// pathPattern parses a pattern that starts with a path. The path names a
// constant or macro, or a variant with or without fields.
func (p *parser) pathPattern(sc *scope, end int, declare func(i int)) {
	var last *code.Ref
	kw := ""
	if p.is(p.i, "<") {
		last = p.qpath(sc, end, false)
	} else {
		last, kw, _ = p.path(sc, end, nil, "", 0, false)
	}
	switch {
	case p.macroBang():
		p.markMacro(last)
		p.i++
		p.skipBracket(end)
	case p.is(p.i, "("):
		p.patternGroup(sc, end, declare)
	case p.is(p.i, "{"):
		p.structPattern(sc, end, object{ref: last, self: last == nil && kw == "Self"}, declare)
	}
}

func (p *parser) macroBang() bool {
	return p.is(p.i, "!") && (p.is(p.i+1, "(") || p.is(p.i+1, "[") || p.is(p.i+1, "{"))
}

func (p *parser) markMacro(last *code.Ref) {
	if last == nil {
		return
	}
	last.Call = true
	if ri := p.fi.refs[last]; ri.kind == refPlain {
		ri.kind = refMacro
	}
}

// bindingName reports whether a lone identifier in a pattern binds a new
// name. It doesn't if it names a constant, a unit struct or a variant. We go
// by the naming conventions the compiler warns about.
func bindingName(s string) bool {
	c := s[0]
	if c == 'r' && len(s) > 2 && s[1] == '#' {
		c = s[2]
	}
	return c >= 'a' && c <= 'z' || c == '_' || c >= 0x80
}

// patternGroup parses the elements of a pattern like (a, b), [a, b] or
// S(a, b). The bracket has to be at p.i.
func (p *parser) patternGroup(sc *scope, end int, declare func(i int)) {
	cl := p.closeOf(p.i, end)
	p.i++
	for p.i < cl {
		start := p.i
		p.patternSeq(sc, cl, stopComma, declare)
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
	p.i = min(cl+1, end)
}

// structPattern parses `{ a, b: pat, ref c, .. }` of obj at p.i.
func (p *parser) structPattern(sc *scope, end int, obj object, declare func(i int)) {
	cl := p.closeOf(p.i, end)
	p.i++
	for p.i < cl {
		start := p.i
		p.attrs(cl)
		for p.is(p.i, "ref") || p.is(p.i, "mut") || p.is(p.i, "box") {
			p.i++
		}
		switch {
		case p.is(p.i, ".."):
			p.i++
		case (p.isIdent(p.i) || p.i < cl && p.t[p.i].kind == code.TokNumber) && p.is(p.i+1, ":"):
			if p.isIdent(p.i) {
				r := p.newRef(sc, p.i, refField)
				p.fi.refs[r].obj = obj
			}
			p.i += 2
			p.patternSeq(sc, cl, stopComma, declare)
		case p.isIdent(p.i):
			declare(p.i)
			p.i++
		}
		for p.i < cl && !p.is(p.i, ",") {
			p.i++
		}
		if p.is(p.i, ",") {
			p.i++
		}
		if p.i == start {
			p.i++
		}
	}
	p.i = min(cl+1, end)
}
