package codelint

import (
	"bytes"
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

var tsRules = []*Rule{
	{
		ID:             "ts-missing-optional-chain-expression",
		Langs:          langsTS,
		Description:    "prefer using an optional chain expression instead, as it's more concise and easier to read.",
		Recommendation: "replace the guard with `?.`, for example `a && a.b` becomes `a?.b`. where the result is used as a value, `a?.b` gives undefined instead of the falsy `a`.",
		check:          checkOptionalChain,
	},
	{
		ID:             "ts-explicit-any",
		Langs:          []code.Lang{code.TypeScript},
		Description:    "specify a different type instead of `any`, as `any` turns off type checking for every value it reaches.",
		Recommendation: "use the precise type, a generic parameter or `unknown`, which forces a check before the value is used.",
		check:          checkExplicitAny,
	},
	{
		ID:             "ts-non-null-assertion",
		Langs:          []code.Lang{code.TypeScript},
		Description:    "avoid non-null assertions, as they hide possible null and undefined values from the compiler.",
		Recommendation: "check the value before using it, use `?.` or `??`, or fix the declared type so that it excludes null.",
		check:          checkNonNullAssertion,
	},
	{
		ID:             "ts-var-declaration",
		Langs:          langsTS,
		Description:    "use `let` or `const` instead of `var`, as block-scoped declarations avoid hoisting surprises.",
		Recommendation: "declare the variable with `const` when it is never reassigned and with `let` otherwise. `var` is scoped to the whole function, so check that no code uses the variable before its declaration or outside the block that declares it.",
		check:          checkVarDeclaration,
	},
	{
		ID:             "ts-loose-equality",
		Langs:          langsTS,
		Description:    "use `===` and `!==` instead of `==` and `!=`, as loose equality converts the operands in surprising ways.",
		Recommendation: "compare with `===` or `!==`. when one side may be a number or a string of a different type, convert it explicitly first. comparisons with null, of 2 literals and of a `typeof` result are allowed.",
		check:          checkLooseEquality,
	},
	{
		ID:             "ts-prefer-array-find",
		Langs:          langsTS,
		Description:    "prefer `.find()` over taking the first element of `.filter()`, as it stops at the first match and states the intent.",
		Recommendation: "call `.find()` with the same callback. it does not call the callback for the elements after the first match, which matters only when the callback has side effects.",
		check:          checkPreferArrayFind,
	},
	{
		ID:             "ts-prefer-array-some",
		Langs:          langsTS,
		Description:    "prefer `.some()` over comparing the length of `.filter()`, as it stops at the first match and returns a boolean.",
		Recommendation: "call `.some()` with the same callback and negate it to test that no element matches.",
		check:          checkPreferArraySome,
	},
	{
		ID:             "ts-prefer-includes",
		Langs:          langsTS,
		Description:    "prefer `.includes()` over comparing the result of `.indexOf()`, as it states the intent directly.",
		Recommendation: "call `.includes()` with the same arguments and negate it to test for absence. unlike `.indexOf()`, the array method `.includes()` also finds `NaN`.",
		check:          checkPreferIncludes,
	},
	{
		ID:             "ts-prefer-at",
		Langs:          langsTS,
		Description:    "prefer `.at()` with a negative index over subtracting from `.length`, as it avoids repeating the receiver.",
		Recommendation: "write `xs.at(-1)` for the last element. `.at()` needs ES2022 and cannot be assigned to.",
		check:          checkPreferAt,
	},
	{
		ID:             "ts-prefer-object-has-own",
		Langs:          langsTS,
		Description:    "prefer `Object.hasOwn()` over calling `hasOwnProperty` through the prototype, as it is shorter and easier to read.",
		Recommendation: "call `Object.hasOwn(object, key)` with the same arguments. it needs ES2022.",
		check:          checkPreferObjectHasOwn,
	},
	{
		ID:             "ts-prefer-node-protocol",
		Langs:          langsTS,
		Description:    "prefer the `node:` protocol when importing a Node.js builtin module, as it makes clear that the module is a builtin.",
		Recommendation: "prefix the module name with `node:`. names such as `buffer` or `events` are only reported in files that also import an unambiguous builtin, since browser bundles resolve them to npm packages.",
		check:          checkPreferNodeProtocol,
	},
	{
		ID:             "ts-debugger-statement",
		Langs:          langsTS,
		Description:    "remove this `debugger` statement, as it pauses the program whenever developer tools are open.",
		Recommendation: "delete the statement and set a breakpoint in the debugger instead.",
		check:          checkDebuggerStatement,
	},
	{
		ID:             "ts-typeof-undefined",
		Langs:          langsTS,
		Description:    "compare with `undefined` directly instead of using `typeof`, as it is shorter and easier to read.",
		Recommendation: "write `x === undefined`. `typeof` is only needed for globals that may not be declared, which are not reported.",
		check:          checkTypeofUndefined,
	},
	{
		ID:             "ts-prefer-string-replace-all",
		Langs:          langsTS,
		Description:    "prefer `.replaceAll()` over `.replace()` with a global regex, as it states the intent.",
		Recommendation: "call `.replaceAll()` with the same arguments, or with a string when the pattern has no special characters. it needs ES2021.",
		check:          checkPreferReplaceAll,
	},
	{
		ID:             "ts-number-static-methods",
		Langs:          langsTS,
		Description:    "prefer the static methods of `Number` over the global functions, as they keep number helpers in one namespace.",
		Recommendation: "call `Number.parseInt` or `Number.parseFloat` instead of the globals. `Number.isNaN` and `Number.isFinite` do not convert strings to numbers, so `isNaN(\"x\")` is true while `Number.isNaN(\"x\")` is false. convert the value first when it may be a string.",
		check:          checkNumberStaticMethods,
	},
	{
		ID:             "ts-double-type-assertion",
		Langs:          []code.Lang{code.TypeScript},
		Description:    "avoid asserting a value through `unknown` or `any` to another type, as the double assertion turns off the check that the 2 types overlap.",
		Recommendation: "fix the declared type of the value, or narrow it with a type guard. for untyped interop (an Emscripten module, a DOM object without typings) the double assertion is acceptable, and a comment on the line or the line above should then say why the value has that type. the rule skips test files and assertions with such a comment.",
		check:          checkDoubleTypeAssertion,
	},
}

var (
	// guardStarts are the tokens after which the left side of && can start
	// an expression: `x = a && a.b`, `f(a && a.b)`, `if (a && a.b)`.
	guardStarts = setOf("", "(", "[", "{", ",", ";", "=", "&&", "||", "?", ":", "return", "=>")
	// guardEnds are the tokens that can follow the guarded access. If an
	// operator binding tighter than && came next, it would apply to the
	// access alone, and a comparison like `a && a.b !== x` gives a different
	// result for undefined.
	guardEnds = setOf("", ")", "]", "}", ",", ";", "&&", "||", "?", ":")
)

// checkOptionalChain flags `a && a.b` and longer guard chains like
// `a && a.b && a.b.c()` (SonarJS S6582).
func checkOptionalChain(s *source) []finding {
	var out []finding
	for i := 0; i < len(s.code); i++ {
		if before := s.tok(i - 1); !guardStarts[before] && !isAssignOp(before) {
			continue
		}
		prefix, prefixEnd := i, s.chainEnd(i)
		if prefixEnd == i || s.tok(prefixEnd) != "&&" {
			continue
		}
		var rewrite strings.Builder
		rewrite.WriteString(s.codeText(prefix, prefixEnd))
		end, safeEnd, safeRewrite := prefixEnd, -1, ""
		for s.tok(end) == "&&" {
			r := end + 1
			rEnd, n := s.chainEnd(r), prefixEnd-prefix
			if rEnd-r <= n || !s.sameTokens(prefix, r, n) {
				break
			}
			rest := s.codeText(r+n, rEnd)
			switch {
			case strings.HasPrefix(rest, "?."):
			case strings.HasPrefix(rest, "."):
				rest = "?" + rest
			default:
				rest = "?." + rest
			}
			rewrite.WriteString(rest)
			prefix, prefixEnd, end = r, rEnd, rEnd
			if s.guardEnd(end) {
				safeEnd, safeRewrite = end, rewrite.String()
			}
		}
		if safeEnd < 0 {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, safeEnd),
			detail: "`" + s.codeText(i, safeEnd) + "` -> `" + safeRewrite + "`",
		})
		i = safeEnd - 1
	}
	return out
}

// guardEnd reports whether the guarded access that ends before code token i
// is the whole operand of &&: either a guardEnds token follows, or the next
// token starts a new statement on a later line.
func (s *source) guardEnd(i int) bool {
	if guardEnds[s.tok(i)] {
		return true
	}
	return s.code[i].Kind != code.TokPunct && s.line(s.code[i].Span.Start) > s.line(s.code[i-1].Span.End)
}

// isAssignOp is true for compound assignments like +=, ||= or ??=, but not
// for a plain "=".
func isAssignOp(t string) bool {
	switch t {
	case "==", "===", "!=", "!==", "<=", ">=", "=>":
		return false
	}
	return len(t) > 1 && strings.HasSuffix(t, "=")
}

// tsChainStart reports whether a member chain starts at code token i. It
// has to be the start, not a member name after a dot.
func (s *source) tsChainStart(i int) bool {
	if p := s.tok(i - 1); p == "." || p == "?." {
		return false
	}
	return s.chainEnd(i) > i
}

// tsOperandStart walks back from code token i to the first token of the
// operand that ends there. That can be a member chain, a literal or a
// parenthesized expression, with a typeof in front. If there's no such
// operand we return i.
func (s *source) tsOperandStart(i int) int {
	j := i - 1
scan:
	for j >= 0 {
		t := s.code[j]
		switch text := s.text(t); {
		case text == ")" || text == "]":
			k := s.matchOpen(s.code, j)
			if k <= 0 {
				return i
			}
			switch p := s.code[k-1]; {
			case s.text(p) == "?.":
				j = k - 2
				continue
			case p.Kind == code.TokIdent || s.isKeyword(p, "this", "super") || s.text(p) == ")" || s.text(p) == "]":
				j = k - 1
				continue
			}
			j = k
		case t.Kind == code.TokIdent || t.Kind == code.TokString || t.Kind == code.TokNumber ||
			s.isKeyword(t, "this", "super", "null", "true", "false"):
			if p := s.tok(j - 1); p == "." || p == "?." {
				j -= 2
				continue
			}
		default:
			return i
		}
		break scan
	}
	if j < 0 {
		return i
	}
	if s.tok(j-1) == "typeof" {
		j--
	}
	return j
}

// tsOperandEnd is the forward version of tsOperandStart. It returns the
// index after the member chain, literal, negative number, typeof operand or
// parenthesized expression starting at code token i, or i if there's none.
func (s *source) tsOperandEnd(i int) int {
	if i >= len(s.code) {
		return i
	}
	t := s.code[i]
	switch {
	case s.tok(i) == "typeof":
		if e := s.chainEnd(i + 1); e > i+1 {
			return e
		}
		return i
	case s.tok(i) == "-" && i+1 < len(s.code) && s.code[i+1].Kind == code.TokNumber:
		return i + 2
	case s.tok(i) == "(" || s.tok(i) == "[":
		if k := s.matchClose(s.code, i); k < len(s.code) {
			return k + 1
		}
		return i
	case t.Kind == code.TokString || t.Kind == code.TokNumber || s.isKeyword(t, "null", "true", "false"):
		return i + 1
	}
	return s.chainEnd(i)
}

// tsStartsOperand reports whether a comparison operand can start at i, so
// we know nothing that binds tighter than the comparison comes before it.
func (s *source) tsStartsOperand(i int) bool {
	before := s.tok(i - 1)
	return guardStarts[before] || isAssignOp(before)
}

func (s *source) tsMemberCall(j int, name string) bool {
	p := s.tok(j - 1)
	return (p == "." || p == "?.") && s.tok(j) == name && s.tok(j+1) == "("
}

func (s *source) tsTopLevel(j int) int {
	if t := s.tok(j); t == "(" || t == "[" {
		return s.matchClose(s.code, j) + 1
	}
	return j + 1
}

func (s *source) rewriteDetail(i, j int, replacement string) string {
	return "`" + s.codeText(i, j) + "` -> `" + replacement + "`"
}

var (
	// anyBefore are the tokens after which `any` has to be a type:
	// annotations, assertions, generic arguments, unions and function types.
	anyBefore = setOf(":", "as", "<", "|", "&", "extends", "keyof", "readonly", "=>")
	// anyAfterComma are the tokens that can follow `any` if it's a later
	// argument of a generic type, as in `Map<string, any>`.
	anyAfterComma = setOf(">", ">>", ">>>", "[", "|", "&")
)

// checkExplicitAny flags `any` used as a type (SonarJS S4204).
func checkExplicitAny(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokKeyword || s.text(t) != "any" {
			continue
		}
		before, after := s.tok(i-1), s.tok(i+1)
		switch {
		case anyBefore[before]:
		case before == "," && anyAfterComma[after]:
		case before == "=" && (after == ">" || after == "," || s.tok(i-3) == "type"):
		default:
			continue
		}
		out = append(out, finding{span: t.Span})
	}
	return out
}

// nonNullAfter are the tokens that can follow a non-null assertion. We need
// the list because text in JSX, like `Hi!</b>`, has other tokens after the !.
var nonNullAfter = setOf("", ".", "?.", "[", "(", ")", "]", ",", ";", "}", "=", "&&", "||", "??", "===", "!==", "+")

// checkNonNullAssertion flags postfix non-null assertions like `x!.y` and
// `f()!` (SonarJS S2966). We don't flag definite assignments (`let x!: T`).
func checkNonNullAssertion(s *source) []finding {
	var out []finding
	for i := 1; i < len(s.code); i++ {
		if s.tok(i) != "!" || s.code[i-1].Span.End != s.code[i].Span.Start || !nonNullAfter[s.tok(i+1)] {
			continue
		}
		p := s.code[i-1]
		if p.Kind != code.TokIdent && !s.isKeyword(p, "this") && s.text(p) != ")" && s.text(p) != "]" {
			continue
		}
		start := s.tsOperandStart(i)
		if start == i || s.tok(start) == "typeof" {
			start = i - 1
		}
		out = append(out, finding{span: s.tokSpan(start, i+1)})
	}
	return out
}

// checkVarDeclaration flags `var` declarations (SonarJS S3504). Ambient
// declarations have to use `var`, so we skip `declare var`, members of
// `declare global` blocks and .d.ts files.
func checkVarDeclaration(s *source) []finding {
	if strings.HasSuffix(s.f.Path, ".d.ts") {
		return nil
	}
	// An ambient block needs a declare keyword somewhere in the file. If
	// there isn't one we can skip inAmbientBlock, which scans backwards from
	// each var.
	ambient := slices.ContainsFunc(s.code, func(t code.Token) bool { return s.text(t) == "declare" })
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokKeyword || s.text(t) != "var" {
			continue
		}
		if p := s.tok(i - 1); p == "." || p == "?." || p == "declare" || s.tok(i+1) == ":" || ambient && s.inAmbientBlock(i) {
			continue
		}
		end, detail := i+1, "`var` -> `let`"
		if i+1 < len(s.code) && s.code[i+1].Kind == code.TokIdent {
			end = i + 2
			repl, note := s.tsVarReplacement(i)
			detail = s.rewriteDetail(i, end, repl) + note
		}
		out = append(out, finding{span: s.tokSpan(i, end), detail: detail})
	}
	return out
}

// tsVarReplacement returns the code to put in place of `var name` at code
// token i so the program still behaves the same, plus a note on where it
// goes. If the function already declared the name with var, that's just the
// bare name. Otherwise it's a let declaration, which has to move if the
// function uses the name before the declaration or outside its block.
func (s *source) tsVarReplacement(i int) (string, string) {
	name := s.tok(i + 1)
	fn := s.tsFuncBody(i)
	lo, hi := fn+1, len(s.code)
	if fn >= 0 {
		hi = s.matchClose(s.code, fn)
	}
	var decls []tsVarDecl
	var uses []int
	var self tsVarDecl
	for k := lo; k < hi; k++ {
		if !s.tsUses(k, name) {
			continue
		}
		// Uses in nested functions count too, because a closure outside the
		// block would lose the variable. Their own declarations of the name
		// don't.
		if s.tok(k-1) != "var" {
			uses = append(uses, k)
			continue
		}
		if s.tsFuncBody(k) != fn {
			continue
		}
		d := s.tsVarBlock(k-1, fn, hi)
		if d.at == i {
			self = d
		}
		decls = append(decls, d)
	}
	for _, d := range decls {
		if d.at < i && d.start <= self.start && self.end <= d.end {
			return name, ", as the function already declares it"
		}
	}
	for _, u := range uses {
		covered := false
		for _, d := range decls {
			if d.at < u && d.start < u && u < d.end {
				covered = true
				break
			}
		}
		switch {
		case covered:
		case u < i:
			return "let " + name, " before its first use"
		default:
			return "let " + name, " before the enclosing block, as later code uses it"
		}
	}
	return "let " + name, ""
}

// tsVarDecl is a var declaration at code token at, with the code tokens
// [start, end] a let in its place would cover.
type tsVarDecl struct{ at, start, end int }

// tsVarBlock returns the block a let replacing the var at code token i
// would be scoped to. That is the enclosing brace block, the whole for
// statement for a loop variable, or else the function body [fn, hi].
func (s *source) tsVarBlock(i, fn, hi int) tsVarDecl {
	o := s.tsEnclosingOpener(i)
	if o <= fn {
		return tsVarDecl{i, fn, hi}
	}
	c := s.matchClose(s.code, o)
	if s.tok(o) == "(" && s.tok(o-1) == "for" && s.tok(c+1) == "{" {
		c = s.matchClose(s.code, c+1)
	}
	return tsVarDecl{i, o, c}
}

// tsUses reports whether code token k reads or writes the variable name. A
// member name or an object literal key with the same text doesn't count.
func (s *source) tsUses(k int, name string) bool {
	if s.code[k].Kind != code.TokIdent || s.tok(k) != name {
		return false
	}
	p := s.tok(k - 1)
	return p != "." && p != "?." && (s.tok(k+1) != ":" || p != "{" && p != ",")
}

func (s *source) tsEnclosingOpener(i int) int {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		switch s.tok(j) {
		case ")", "]", "}":
			depth++
		case "(", "[", "{":
			if depth == 0 {
				return j
			}
			depth--
		}
	}
	return -1
}

// tsFuncBody returns the brace that opens the innermost function body
// around code token i, or -1 at the top level. Declared functions, arrow
// functions and anonymous function expressions all count.
func (s *source) tsFuncBody(i int) int {
	for j := range s.enclosingBraces(i) {
		if s.funcBodies()[s.code[j].Span.Start] || s.tok(j-1) == "=>" {
			return j
		}
		if s.tok(j-1) == ")" {
			if k := s.matchOpen(s.code, j-1); s.tok(k-1) == "function" || s.tok(k-2) == "function" {
				return j
			}
		}
	}
	return -1
}

// inAmbientBlock reports whether code token i lies inside a `declare
// global`, `declare module` or `declare namespace` block.
func (s *source) inAmbientBlock(i int) bool {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		switch s.tok(j) {
		case "}":
			depth++
		case "{":
			if depth > 0 {
				depth--
				continue
			}
			for k := j - 1; k >= j-3; k-- {
				if s.tok(k) == "declare" {
					return true
				}
			}
		}
	}
	return false
}

// checkLooseEquality flags `==` and `!=` (SonarJS S1440). Like the "smart"
// option of eslint's eqeqeq, we allow comparing to null, comparing two
// literals and comparing a typeof result.
func checkLooseEquality(s *source) []finding {
	var out []finding
	for i := 1; i < len(s.code); i++ {
		op := s.tok(i)
		if op != "==" && op != "!=" {
			continue
		}
		start, end := s.tsOperandStart(i), s.tsOperandEnd(i+1)
		if s.tok(i-1) == "null" || s.tok(i+1) == "null" || s.tok(start) == "typeof" || s.tok(i+1) == "typeof" ||
			start == i-1 && end == i+2 && s.tsLiteral(i-1) && s.tsLiteral(i+1) {
			continue
		}
		if start == i || end == i+1 {
			out = append(out, finding{span: s.code[i].Span, detail: "`" + op + "` -> `" + op + "=`"})
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(start, end),
			detail: s.rewriteDetail(start, end, s.codeText(start, i)+" "+op+"= "+s.codeText(i+1, end)),
		})
	}
	return out
}

func (s *source) tsLiteral(i int) bool {
	t := s.code[i]
	return t.Kind == code.TokString || t.Kind == code.TokNumber || s.isKeyword(t, "true", "false")
}

// tsArrayFilter reports whether code token j is a `.filter(` call on an
// array, so the argument can't be a string. jQuery's `.filter(".selector")`
// has no `.find()` or `.some()` we could suggest instead.
func (s *source) tsArrayFilter(j int) bool {
	if !s.tsMemberCall(j, "filter") || s.tok(j+2) == ")" {
		return false
	}
	return j+2 < len(s.code) && s.code[j+2].Kind != code.TokString
}

// checkPreferArrayFind flags `.filter(f)[0]`, `.filter(f).shift()` and
// `.filter(f).at(0)` (SonarJS S7750).
func checkPreferArrayFind(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.tsChainStart(i) {
			continue
		}
		end := s.chainEnd(i)
		for j := i + 1; j < end; j = s.tsTopLevel(j) {
			if !s.tsArrayFilter(j) {
				continue
			}
			k := s.matchClose(s.code, j+1)
			n := 0
			switch {
			case s.seq(k+1, "[", "0", "]"):
				n = 3
			case s.seq(k+1, ".", "shift", "(", ")"):
				n = 4
			case s.seq(k+1, ".", "at", "(", "0", ")"):
				n = 5
			default:
				continue
			}
			e := k + 1 + n
			if a := s.tok(e); a == "=" || isAssignOp(a) || a == "++" || a == "--" {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(i, e),
				detail: s.rewriteDetail(i, e, s.codeText(i, j)+"find"+s.codeText(j+1, k+1)),
			})
		}
	}
	return out
}

// checkPreferArraySome flags `.filter(f).length` compared to 0 or 1
// (SonarJS S7754).
func checkPreferArraySome(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.tsChainStart(i) || !s.tsStartsOperand(i) {
			continue
		}
		end := s.chainEnd(i)
		if end-i < 6 || !s.seq(end-2, ".", "length") || s.tok(end-3) != ")" {
			continue
		}
		j := s.matchOpen(s.code, end-3) - 1
		if j <= i || !s.tsArrayFilter(j) {
			continue
		}
		negate := ""
		switch {
		case s.seq(end, ">", "0"), s.seq(end, "!==", "0"), s.seq(end, "!=", "0"), s.seq(end, ">=", "1"):
		case s.seq(end, "===", "0"), s.seq(end, "==", "0"), s.seq(end, "<", "1"):
			negate = "!"
		default:
			continue
		}
		if !s.guardEnd(end + 2) {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, end+2),
			detail: s.rewriteDetail(i, end+2, negate+s.codeText(i, j)+"some"+s.codeText(j+1, end-2)),
		})
	}
	return out
}

// checkPreferIncludes flags `.indexOf(x)` compared to -1, `>= 0` or `< 0`
// (SonarJS S7765). We leave comparisons with other positions, like
// `=== 0`, alone since they care where the value is.
func checkPreferIncludes(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.tsChainStart(i) || !s.tsStartsOperand(i) {
			continue
		}
		end := s.chainEnd(i)
		if s.tok(end-1) != ")" {
			continue
		}
		open := s.matchOpen(s.code, end-1)
		j := open - 1
		if j <= i || open+1 == end-1 || !s.tsMemberCall(j, "indexOf") {
			continue
		}
		negate, n := "", 3
		switch {
		case s.seq(end, "!==", "-", "1"), s.seq(end, "!=", "-", "1"), s.seq(end, ">", "-", "1"):
		case s.seq(end, "===", "-", "1"), s.seq(end, "==", "-", "1"):
			negate = "!"
		case s.seq(end, ">=", "0"):
			n = 2
		case s.seq(end, "<", "0"):
			negate, n = "!", 2
		default:
			continue
		}
		if !s.guardEnd(end + n) {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, end+n),
			detail: s.rewriteDetail(i, end+n, negate+s.codeText(i, j)+"includes"+s.codeText(open, end)),
		})
	}
	return out
}

// checkPreferAt flags `xs[xs.length - 1]` (SonarJS S7755). We skip
// receivers with calls, since repeating them could run side effects twice,
// assignment targets, which `.at()` can't replace, and the `arguments`
// object, which has no `.at()`.
func checkPreferAt(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.tsChainStart(i) || s.tok(i) == "arguments" {
			continue
		}
		if p := s.tok(i - 1); p == "delete" || p == "++" || p == "--" {
			continue
		}
		end := s.chainEnd(i)
		for b := i + 1; b < end; b = s.tsTopLevel(b) {
			if s.tok(b) == "(" {
				break
			}
			if s.tok(b) != "[" {
				continue
			}
			n, e := b-i, s.matchClose(s.code, b)
			num := b + 1 + n + 3
			if num+1 != e || !s.sameTokens(i, b+1, n) || !s.seq(b+1+n, ".", "length", "-") ||
				s.code[num].Kind != code.TokNumber || strings.Trim(s.tok(num), "0123456789") != "" {
				continue
			}
			if a := s.tok(e + 1); a == "=" || isAssignOp(a) || a == "++" || a == "--" {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(i, e+1),
				detail: s.rewriteDetail(i, e+1, s.codeText(i, b)+".at(-"+s.tok(num)+")"),
			})
		}
	}
	return out
}

// checkPreferObjectHasOwn flags `Object.prototype.hasOwnProperty.call(o, k)`
// and `{}.hasOwnProperty.call(o, k)` (SonarJS S6653).
func checkPreferObjectHasOwn(s *source) []finding {
	var out []finding
	for i := range s.code {
		n := 0
		switch {
		case s.seq(i, "Object", ".", "prototype", ".", "hasOwnProperty", ".", "call", "("):
			if p := s.tok(i - 1); p == "." || p == "?." {
				continue
			}
			n = 8
		case s.seq(i, "{", "}", ".", "hasOwnProperty", ".", "call", "("):
			n = 7
		default:
			continue
		}
		k := s.matchClose(s.code, i+n-1)
		if k >= len(s.code) || k == i+n {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, k+1),
			detail: s.rewriteDetail(i, k+1, "Object.hasOwn("+s.codeText(i+n, k)+")"),
		})
	}
	return out
}

var (
	// nodeBuiltins are the Node.js builtin modules that no common npm
	// package shadows.
	nodeBuiltins = setOf("async_hooks", "child_process", "cluster", "crypto", "dgram", "diagnostics_channel",
		"dns", "dns/promises", "fs", "fs/promises", "http", "http2", "https", "inspector", "inspector/promises",
		"module", "net", "os", "path/posix", "path/win32", "perf_hooks", "readline", "readline/promises", "repl",
		"stream/consumers", "stream/promises", "stream/web", "timers/promises", "tls", "trace_events", "tty",
		"util/types", "v8", "vm", "wasi", "worker_threads", "zlib", "assert/strict")
	// nodeShadowedBuiltins are builtins that browser bundlers resolve to an
	// npm polyfill with the same name.
	nodeShadowedBuiltins = setOf("assert", "buffer", "console", "constants", "events", "path", "process",
		"punycode", "querystring", "stream", "string_decoder", "timers", "url", "util")
)

// checkPreferNodeProtocol flags imports, re-exports, require and import()
// calls of Node.js builtins that are missing the `node:` prefix (SonarJS
// S7772). A module name that browser bundlers resolve to an npm polyfill
// only counts if the file also imports a builtin that can't be a polyfill.
func checkPreferNodeProtocol(s *source) []finding {
	var found []int
	node := false
	for j, t := range s.code {
		if t.Kind != code.TokString {
			continue
		}
		switch p := s.tok(j - 1); {
		case p == "from", p == "import" && s.code[j-1].Kind == code.TokKeyword:
		case p == "(" && s.tok(j+1) == ")" && (s.tok(j-2) == "import" || s.tok(j-2) == "require" && s.tok(j-3) != "." && s.tok(j-3) != "?."):
		default:
			continue
		}
		name := s.tok(j)
		if len(name) < 2 || name[0] != '"' && name[0] != '\'' {
			continue
		}
		name = name[1 : len(name)-1]
		switch {
		case strings.HasPrefix(name, "node:"):
			node = true
		case nodeBuiltins[name]:
			node = true
			found = append(found, j)
		case nodeShadowedBuiltins[name]:
			found = append(found, j)
		}
	}
	var out []finding
	for _, j := range found {
		lit := s.tok(j)
		if !node && nodeShadowedBuiltins[lit[1:len(lit)-1]] {
			continue
		}
		out = append(out, finding{
			span:   s.code[j].Span,
			detail: "`" + lit + "` -> `" + lit[:1] + "node:" + lit[1:] + "`",
		})
	}
	return out
}

// checkDebuggerStatement flags `debugger` statements (SonarJS S1525).
func checkDebuggerStatement(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokKeyword || s.text(t) != "debugger" {
			continue
		}
		end := i + 1
		if s.tok(end) == ";" {
			end++
		}
		out = append(out, finding{span: s.tokSpan(i, end)})
	}
	return out
}

// typeEnds are the tokens that can end a type after `as`, outside brackets.
var typeEnds = setOf("", ";", ",", ")", "]", "}", "=", "?", ":", "&&", "||", "??", "=>")

// checkDoubleTypeAssertion flags `x as unknown as T` and `x as any as T`
// outside test files. If a comment on the line or the line above gives the
// reason, we let it be.
func checkDoubleTypeAssertion(s *source) []finding {
	if s.test {
		return nil
	}
	var out []finding
	for i := range s.code {
		if s.tok(i) != "as" || s.tok(i+1) != "unknown" && s.tok(i+1) != "any" || s.tok(i+2) != "as" {
			continue
		}
		if s.commentJustifies(s.line(s.code[i].Span.Start)) {
			continue
		}
		end, angles, line := i+3, 0, s.line(s.code[i].Span.Start)
		for end < len(s.code) && (angles > 0 || !typeEnds[s.tok(end)]) && s.line(s.code[end].Span.Start) == line {
			switch s.tok(end) {
			case "(", "[", "{":
				end = s.matchClose(s.code, end)
			case "<":
				angles++
			case ">":
				angles--
			case ">>":
				angles -= 2
			}
			end++
		}
		end = min(end, len(s.code))
		out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "`"})
	}
	return out
}

var eqOps = setOf("===", "!==", "==", "!=")

// checkTypeofUndefined flags `typeof x === "undefined"` with any equality
// operator, on either side (SonarJS S7741). Like the SonarJS rule we skip
// bare identifiers the file doesn't declare, because `typeof window` is
// there to guard a global that might not exist.
func checkTypeofUndefined(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokKeyword || s.text(t) != "typeof" {
			continue
		}
		x, xe := i+1, s.chainEnd(i+1)
		if xe == x {
			continue
		}
		start, end, op := i, xe+2, s.tok(xe)
		if !eqOps[op] || !isUndefinedString(s.tok(xe+1)) {
			start, end, op = i-2, xe, s.tok(i-1)
			if !eqOps[op] || !isUndefinedString(s.tok(i-2)) {
				continue
			}
		}
		if xe-x == 1 && !s.tsDeclared(s.tok(x), t.Span.Start) {
			continue
		}
		cmp := "==="
		if op[0] == '!' {
			cmp = "!=="
		}
		out = append(out, finding{
			span:   s.tokSpan(start, end),
			detail: s.rewriteDetail(start, end, s.codeText(x, xe)+" "+cmp+" undefined"),
		})
	}
	return out
}

func isUndefinedString(lit string) bool { return lit == `"undefined"` || lit == `'undefined'` }

// tsDeclared reports whether the file declares a variable, parameter,
// function, class or import called name in a scope that contains offset
// off. Ambient `declare` declarations don't count, since they might not
// exist at run time.
func (s *source) tsDeclared(name string, off int) bool {
	for _, d := range s.f.Decls {
		if d.Name != name || d.Kind == code.KindField || d.Kind == code.KindProperty ||
			d.Kind == code.KindMethod || d.Kind == code.KindEnumMember {
			continue
		}
		if d.Parent != nil && d.Parent != s.f.Module && !d.Parent.Span.Contains(off) {
			continue
		}
		lineStart := bytes.LastIndexByte(s.f.Src[:d.NameSpan.Start], '\n') + 1
		if !bytes.Contains(s.f.Src[lineStart:d.NameSpan.Start], []byte("declare ")) {
			return true
		}
	}
	return false
}

const regexMeta = `\^$.*+?()[]{}|/"'`

// checkPreferReplaceAll flags `.replace()` with a /g regex literal (SonarJS
// S7781).
func checkPreferReplaceAll(s *source) []finding {
	var out []finding
	for j := 1; j+3 < len(s.code); j++ {
		re := s.tok(j + 2)
		if !s.tsMemberCall(j, "replace") || s.code[j+2].Kind != code.TokString || !strings.HasPrefix(re, "/") || s.tok(j+3) != "," {
			continue
		}
		slash := strings.LastIndexByte(re, '/')
		flags := re[slash+1:]
		if slash == 0 || !strings.Contains(flags, "g") {
			continue
		}
		k := s.matchClose(s.code, j+1)
		if k >= len(s.code) {
			continue
		}
		pattern := re
		if body := re[1:slash]; flags == "g" && !strings.ContainsAny(body, regexMeta) {
			pattern = `"` + body + `"`
		}
		dot, rest := s.tok(j-1), s.codeText(j+3, k+1)
		detail := s.rewriteDetail(j-1, k+1, dot+"replaceAll("+pattern+rest)
		if len(rest) > 40 {
			// A long callback would bury the change, so we elide the arguments.
			detail = "`" + dot + "replace(" + re + ", ...)` -> `" + dot + "replaceAll(" + pattern + ", ...)`"
		}
		out = append(out, finding{span: s.tokSpan(j-1, k+1), detail: detail})
	}
	return out
}

var globalNumberFuncs = setOf("parseInt", "parseFloat", "isNaN", "isFinite")

// checkNumberStaticMethods flags calls to the global parseInt, parseFloat,
// isNaN and isFinite (SonarJS S7773). Member calls, method definitions and
// functions the file declares itself are fine.
func checkNumberStaticMethods(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		name := s.text(t)
		if t.Kind != code.TokIdent || !globalNumberFuncs[name] || s.tok(i+1) != "(" {
			continue
		}
		if p := s.tok(i - 1); p == "." || p == "?." || p == "function" || p == "new" {
			continue
		}
		k := s.matchClose(s.code, i+1)
		if k >= len(s.code) || s.tok(k+1) == "{" || s.tok(k+1) == ":" || s.tsDeclared(name, t.Span.Start) {
			continue
		}
		out = append(out, finding{span: s.tokSpan(i, k+1), detail: s.rewriteDetail(i, k+1, "Number."+s.codeText(i, k+1))})
	}
	return out
}
