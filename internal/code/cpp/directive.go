package cpp

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// activeTokens drops comments and directives and keeps only the first branch
// of each #if chain, or the #else branch of `#if 0`. That way alternative
// branches can't unbalance the braces.
func activeTokens(l *lexer) []tok {
	type frame struct{ active, taken bool }
	var stack []frame
	inactive := 0
	out := make([]tok, 0, len(l.toks))
	for i := 0; i < len(l.toks); i++ {
		t := l.toks[i]
		if t.dir != 0 {
			d := l.dirs[t.dir-1]
			if i != d.first {
				continue
			}
			name, cond := l.directiveWords(d)
			switch name {
			case "if", "ifdef", "ifndef":
				f := frame{active: true, taken: true}
				if name == "if" && (cond == "0" || cond == "false") {
					f = frame{}
				}
				stack = append(stack, f)
				if !f.active {
					inactive++
				}
			case "elif", "elifdef", "elifndef", "else":
				n := len(stack)
				if n == 0 {
					break
				}
				f := &stack[n-1]
				was := f.active
				f.active = !f.taken
				f.taken = true
				if was && !f.active {
					inactive++
				} else if !was && f.active {
					inactive--
				}
			case "endif":
				n := len(stack)
				if n == 0 {
					break
				}
				if !stack[n-1].active {
					inactive--
				}
				stack = stack[:n-1]
			}
			i = d.last - 1
			continue
		}
		if t.kind == code.TokComment || inactive > 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}

// directiveWords returns the directive name and, if the condition is a single
// token, that token.
func (l *lexer) directiveWords(d directive) (name, cond string) {
	var words []string
	for _, t := range l.toks[d.first+1 : d.last] {
		if t.kind != code.TokComment {
			words = append(words, string(l.src[t.s:t.e]))
		}
	}
	if len(words) > 0 {
		name = words[0]
	}
	if len(words) == 2 {
		cond = words[1]
	}
	return name, cond
}

// directives records macros, includes and macro references for all
// directives, including the ones in inactive branches.
func (p *parser) directives(l *lexer) {
	mod := p.f.Module
	for _, d := range l.dirs {
		var toks []tok
		for _, t := range l.toks[d.first+1 : d.last] {
			if t.kind != code.TokComment {
				toks = append(toks, t)
			}
		}
		if len(toks) == 0 {
			continue
		}
		text := func(t tok) string { return p.src[t.s:t.e] }
		switch name := text(toks[0]); name {
		case "define":
			p.define(d, toks[1:])
		case "include", "include_next", "import":
			if len(toks) < 2 {
				continue
			}
			t := toks[1]
			s := text(t)
			if t.kind == code.TokString && len(s) >= 2 {
				inner := code.Span{Start: t.s + 1, End: t.e - 1}
				r := p.f.NewRef(mod, s[1:len(s)-1], inner)
				r.Import = true
				p.fi.refs[r] = &refInfo{sc: p.fi.root, kind: refInclude, arity: -1}
				p.fi.includes = append(p.fi.includes, include{ref: r, path: r.Name, system: s[0] == '<'})
				continue
			}
			p.macroRefs(p.fi.root, mod, toks[1:])
		case "undef", "ifdef", "ifndef", "if", "elif", "elifdef", "elifndef":
			p.macroRefs(p.fi.root, mod, toks[1:])
		}
	}
}

func (p *parser) define(d directive, toks []tok) {
	if len(toks) == 0 || toks[0].kind != code.TokIdent && toks[0].kind != code.TokKeyword {
		return
	}
	nameTok := toks[0]
	m := p.f.NewDecl(p.f.Module, code.KindMacro, p.src[nameTok.s:nameTok.e],
		code.Span{Start: nameTok.s, End: nameTok.e}, d.span)
	sc := newScope(scopeMacro, p.fi.root, m, m)
	p.fi.decls[m] = &declInfo{sc: p.fi.root, def: true, arity: -1, body: sc}
	p.fi.macros = append(p.fi.macros, m)
	body := toks[1:]
	if len(body) > 0 && body[0].s == nameTok.e && p.src[body[0].s] == '(' {
		arity, j := 0, 1
		for ; j < len(body); j++ {
			t := body[j]
			s := p.src[t.s:t.e]
			if s == ")" {
				j++
				break
			}
			if s == "..." {
				p.fi.decls[m].variadic = true
			}
			if t.kind == code.TokIdent {
				arity++
				prm := p.f.NewDecl(m, code.KindParameter, s, code.Span{Start: t.s, End: t.e}, code.Span{Start: t.s, End: t.e})
				prm.Local = true
				p.fi.decls[prm] = &declInfo{sc: sc, def: true, arity: -1}
				sc.add(prm)
			}
		}
		p.fi.decls[m].arity = arity
		body = body[j:]
	}
	// We scope references in the replacement list to the macro and treat it
	// like the body of a function.
	p.macroRefs(sc, m, body)
}

// macroRefs records references for the identifiers of directive tokens.
func (p *parser) macroRefs(sc *scope, owner *code.Decl, toks []tok) {
	var last *code.Ref
	for k, t := range toks {
		if t.kind != code.TokIdent {
			continue
		}
		s := p.src[t.s:t.e]
		if s == "defined" || s == "__VA_ARGS__" || s == "__VA_OPT__" || strings.HasPrefix(s, "__has_") {
			continue
		}
		kind, prev := refPlain, (*code.Ref)(nil)
		if k > 0 {
			switch p.src[toks[k-1].s:toks[k-1].e] {
			case ".", "->":
				kind = refUnknown
			case "::":
				if last != nil && k > 1 && last.Span.End == toks[k-2].e {
					kind, prev = refScoped, last
				}
			}
		}
		r := p.f.NewRef(owner, s, code.Span{Start: t.s, End: t.e})
		ri := &refInfo{sc: sc, kind: kind, prev: prev, arity: -1}
		if k+1 < len(toks) && p.src[toks[k+1].s] == '(' && toks[k+1].e-toks[k+1].s == 1 {
			r.Call = true
		}
		p.fi.refs[r] = ri
		last = r
	}
}
