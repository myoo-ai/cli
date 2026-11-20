package codelint

import (
	"strings"
	"unicode"

	"github.com/myoo-ai/cli/internal/code"
)

var csRules = []*Rule{
	{
		ID:             "cs-any-for-emptiness",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer `Any()` to test a sequence for emptiness, as `Count()` may enumerate the whole sequence.",
		Recommendation: "replace the comparison with `Any()`, for example `xs.Count() > 0` becomes `xs.Any()` and `xs.Count() == 0` becomes `!xs.Any()`. for a collection with a `Count` property, comparing that property is just as cheap.",
		check:          checkCsAnyForEmptiness,
	},
	{
		ID:             "cs-simplify-linq",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer passing the predicate to the method directly instead of calling `Where` first, as it's more concise.",
		Recommendation: "move the predicate of `Where` into the next call, for example `xs.Where(p).First()` becomes `xs.First(p)`.",
		check:          checkCsSimplifyLinq,
	},
	{
		ID:             "cs-string-is-null-or-empty",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer `string.IsNullOrEmpty` to comparing with an empty string, as it states the intent and also covers null.",
		Recommendation: "call `string.IsNullOrEmpty(x)` instead, or `!string.IsNullOrEmpty(x)` for an inequality. it returns true for null where `x == \"\"` returns false, so check that a null value should count as empty. where null must stay distinct and `x` is never null, compare `x.Length == 0`.",
		check:          checkCsStringIsNullOrEmpty,
	},
	{
		ID:             "cs-explicit-rethrow",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer `throw;` to rethrowing the caught exception by name, as `throw e;` resets its stack trace.",
		Recommendation: "replace `throw e;` with `throw;` to keep the original stack trace. to add context, throw a new exception that wraps the caught one instead.",
		check:          checkCsExplicitRethrow,
	},
	{
		ID:             "cs-blocking-async-call",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer `await` to blocking on a task in an async method, as blocking wastes a thread and can deadlock.",
		Recommendation: "await the task instead of blocking on it, for example `t.Result` becomes `await t` and `t.Wait()` becomes `await t`. under a synchronization context such as a UI thread, a blocking wait on a task that resumes on that context never returns.",
		check:          checkCsBlockingAsyncCall,
	},
	{
		ID:             "cs-string-concat-in-loop",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer a `StringBuilder` to concatenating strings in a loop, as each concatenation copies the whole string.",
		Recommendation: "append to a `StringBuilder` declared before the loop and call `ToString()` once after it. a loop with a small fixed number of iterations may keep the concatenation.",
		check:          checkCsStringConcatInLoop,
	},
	{
		ID:             "cs-async-void",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer returning `Task` from an async method, as callers cannot await an `async void` method and its exceptions crash the process.",
		Recommendation: "change the return type to `Task` and await the call. event handlers must stay `async void`, so catch their exceptions inside the handler instead.",
		check:          checkCsAsyncVoid,
	},
	{
		ID:             "cs-duplicate-cast",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer a declaration pattern to a type test followed by a cast, as the pattern tests and casts once.",
		Recommendation: "declare a variable in the type test and use it instead of the cast, for example `if (o is Foo) { ((Foo)o).Run(); }` becomes `if (o is Foo foo) { foo.Run(); }`.",
		check:          checkCsDuplicateCast,
	},
	{
		ID:             "cs-nameof",
		Langs:          []code.Lang{code.CSharp},
		Description:    "prefer `nameof` to a string literal holding a parameter name, as it stays correct when the parameter is renamed.",
		Recommendation: "replace the literal with `nameof(param)`, which the compiler checks against the parameter. on .NET 6 or later `ArgumentNullException.ThrowIfNull(param)` fills in the name itself.",
		check:          checkCsNameof,
	},
}

var (
	// If a comparison operand follows one of these tokens, it starts an
	// expression, so no tighter operator can bind to it.
	csExprStarts = setOf("", "(", "[", "{", ",", ";", "=", "&&", "||", "?", ":", "??", "return", "=>")
	// These tokens can follow a comparison operand.
	csExprEnds = setOf("", ")", "]", "}", ",", ";", "&&", "||", "?", ":")
)

// csParams returns the parameters of the callable d. The parser also records
// lambda parameters and catch variables as parameters of d, so we leave
// those out.
func csParams(d *code.Decl) []*code.Decl {
	var out []*code.Decl
	for _, c := range d.Children {
		if c.Kind == code.KindParameter && (d.Body.Empty() || c.NameSpan.Start < d.Body.Start) {
			out = append(out, c)
		}
	}
	return out
}

func (s *source) csModifier(d *code.Decl, kw string) int {
	for i := s.codeIndex(d.Span.Start); i < len(s.code) && s.code[i].Span.Start < d.NameSpan.Start; i++ {
		if s.tok(i) == kw {
			return i
		}
	}
	return -1
}

// checkCsAnyForEmptiness flags `xs.Count() > 0`, `xs.Count() == 0` and the
// mirrored comparisons (SonarC# S1155). We don't touch the `Count` property
// of collections.
func checkCsAnyForEmptiness(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, "Count", "(", ")") || s.tok(i-1) != "." {
			continue
		}
		r := s.chainStart(i - 1)
		if r == i-1 || strings.Contains(s.codeText(r, i), "?.") {
			continue
		}
		recv := s.codeText(r, i-1)
		var any bool
		var start, end int
		switch {
		case csExprStarts[s.tok(r-1)] && csExprEnds[s.tok(i+5)]:
			switch s.tok(i+3) + " " + s.tok(i+4) {
			case "> 0", "!= 0", ">= 1":
				any = true
			case "== 0", "< 1", "<= 0":
			default:
				continue
			}
			start, end = r, i+5
		case csExprStarts[s.tok(r-3)] && csExprEnds[s.tok(i+3)]:
			switch s.tok(r-2) + " " + s.tok(r-1) {
			case "0 <", "0 !=", "1 <=":
				any = true
			case "0 ==", "1 >", "0 >=":
			default:
				continue
			}
			start, end = r-2, i+3
		default:
			continue
		}
		repl := recv + ".Any()"
		if !any {
			repl = "!" + repl
		}
		out = append(out, finding{
			span:   s.tokSpan(start, end),
			detail: "`" + s.codeText(start, end) + "` -> `" + repl + "`",
		})
	}
	return out
}

// csPredicateMethods are the LINQ methods that have an overload taking the
// predicate of a preceding Where.
var csPredicateMethods = setOf("First", "FirstOrDefault", "Last", "LastOrDefault", "Single", "SingleOrDefault", "Any", "Count", "LongCount")

// checkCsSimplifyLinq flags `.Where(p).First()` and the like (SonarC#
// S2971). We skip an indexed predicate `(x, i) => ...`, since there's no
// matching overload for it.
func checkCsSimplifyLinq(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, ".", "Where", "(") {
			continue
		}
		k := s.matchClose(s.code, i+2)
		if k >= len(s.code) || k == i+3 || s.tok(k+1) != "." || !csPredicateMethods[s.tok(k+2)] || !s.seq(k+3, "(", ")") {
			continue
		}
		if s.hasTopLevel(i+3, k, ",") {
			continue
		}
		if s.tok(i+3) == "(" {
			if c := s.matchClose(s.code, i+3); s.tok(c+1) == "=>" && s.hasTopLevel(i+4, c, ",") {
				continue
			}
		}
		end := k + 5
		pred := s.codeText(i+3, k)
		if s.line(s.code[i+3].Span.Start) != s.line(s.code[k-1].Span.End) {
			// A block lambda squashed onto one line would swamp the message.
			pred = "..."
		}
		m := s.tok(k + 2)
		out = append(out, finding{
			span:   s.tokSpan(i, end),
			detail: "`.Where(" + pred + ")." + m + "()` -> `." + m + "(" + pred + ")`",
		})
	}
	return out
}

// csEmptyStringAt returns the end of an empty string operand (`""`,
// `string.Empty` or `String.Empty`) that starts at code token i.
func (s *source) csEmptyStringAt(i int) (int, bool) {
	if s.tok(i) == `""` {
		return i + 1, true
	}
	if (s.tok(i) == "string" || s.tok(i) == "String") && s.seq(i+1, ".", "Empty") {
		return i + 3, true
	}
	return i, false
}

// checkCsStringIsNullOrEmpty flags `x == ""`, `x != string.Empty` and
// `x.Equals("")` (SonarC# S3256).
func checkCsStringIsNullOrEmpty(s *source) []finding {
	var out []finding
	for i := range s.code {
		op := s.tok(i)
		var start, end, x, xEnd int
		switch op {
		case "==", "!=":
			if e, ok := s.csEmptyStringAt(i + 1); ok {
				x, xEnd = s.chainStart(i), i
				if x == xEnd || !csExprStarts[s.tok(x-1)] || !csExprEnds[s.tok(e)] {
					continue
				}
				start, end = x, e
			} else {
				st := i - 1
				if s.tok(st) == "Empty" {
					st = i - 3
				}
				if _, ok := s.csEmptyStringAt(st); !ok || st < 0 || !csExprStarts[s.tok(st-1)] {
					continue
				}
				x, xEnd = i+1, s.chainEnd(i+1)
				if xEnd == x || !csExprEnds[s.tok(xEnd)] {
					continue
				}
				start, end = st, xEnd
			}
		case "Equals":
			if s.tok(i-1) != "." || s.tok(i+1) != "(" {
				continue
			}
			e, ok := s.csEmptyStringAt(i + 2)
			if !ok || s.tok(e) != ")" {
				continue
			}
			x, xEnd = s.chainStart(i-1), i-1
			if x == xEnd || s.tok(x-1) == "new" {
				continue
			}
			start, end = x, e+1
			op = "=="
		default:
			continue
		}
		if t := s.tok(x); t == "string" || t == "String" {
			continue
		}
		repl := "string.IsNullOrEmpty(" + s.codeText(x, xEnd) + ")"
		if op == "!=" {
			repl = "!" + repl
		}
		out = append(out, finding{
			span:   s.tokSpan(start, end),
			detail: "`" + s.codeText(start, end) + "` -> `" + repl + "`",
		})
	}
	return out
}

// checkCsExplicitRethrow flags `throw e;` where e is the variable of the
// enclosing catch clause (SonarC# S3445). We skip a throw in a nested catch,
// finally, lambda or local function, because there `throw;` would rethrow a
// different exception or not compile. We also skip a catch that assigns e.
func checkCsExplicitRethrow(s *source) []finding {
	var out []finding
	for i := range s.code {
		if s.tok(i) != "catch" || s.tok(i+1) != "(" {
			continue
		}
		k := s.matchClose(s.code, i+1)
		if k >= len(s.code) || k-i < 4 || s.code[k-1].Kind != code.TokIdent {
			continue
		}
		v := s.tok(k - 1)
		b := k + 1
		if s.tok(b) == "when" && s.tok(b+1) == "(" {
			b = s.matchClose(s.code, b+1) + 1
		}
		if s.tok(b) != "{" {
			continue
		}
		be := s.matchClose(s.code, b)
		var throws []int
		assigned := false
		for j := b + 1; j < be; j++ {
			if s.tok(j) == v && (isAssignOp(s.tok(j+1)) || s.tok(j+1) == "=" || s.tok(j-1) == "ref" || s.tok(j-1) == "out") {
				assigned = true
			}
			if s.tok(j) == "throw" && s.seq(j+1, v, ";") && stmtStarts[s.tok(j-1)] && !s.csNestedHandler(b, j) {
				throws = append(throws, j)
			}
		}
		if assigned {
			continue
		}
		for _, j := range throws {
			out = append(out, finding{
				span:   s.tokSpan(j, j+3),
				detail: "`" + s.codeText(j, j+3) + "` -> `throw;`",
			})
		}
	}
	return out
}

// csNestedHandler reports whether code token j sits in a block, between the
// catch block opening at b and j, that changes what `throw;` means: a nested
// catch or finally, a lambda, an anonymous method or a local function.
func (s *source) csNestedHandler(b, j int) bool {
	for m := range s.enclosingBraces(j) {
		if m <= b {
			return false
		}
		if s.csHandlerBrace(m) || s.csCallbackBrace(m) {
			return true
		}
	}
	return false
}

func (s *source) csHandlerBrace(m int) bool {
	switch s.tok(m - 1) {
	case "finally", "catch":
		return true
	case ")":
		o := s.matchOpen(s.code, m-1)
		return o > 0 && (s.tok(o-1) == "catch" || s.tok(o-1) == "when")
	}
	return false
}

// csCallbackBrace reports whether the brace at code token k opens the body
// of a lambda, an anonymous method or a local function.
func (s *source) csCallbackBrace(k int) bool {
	switch s.tok(k - 1) {
	case "=>":
		return true
	case ")":
		o := s.matchOpen(s.code, k-1)
		return o > 0 && s.tok(o-1) == "delegate" || s.funcBodies()[s.code[k].Span.Start]
	}
	return false
}

// checkCsBlockingAsyncCall flags `t.Result`, `t.Wait()` and
// `t.GetAwaiter().GetResult()` in the body of an async method (SonarC#
// S4462). We don't have type information, so `.Result` and `.Wait()` only
// count on a variable or field declared as Task or ValueTask, or on
// a call to a method whose name ends in Async. We skip code inside lambdas.
func checkCsBlockingAsyncCall(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() || d.Body.Empty() || s.csModifier(d, "async") < 0 {
			continue
		}
		bs := s.codeIndex(d.Body.Start)
		be := s.matchClose(s.code, bs)
		for j := bs + 1; j < be; j++ {
			if s.tok(j) != "." {
				continue
			}
			var end int
			anyRecv := false
			switch {
			case s.tok(j+1) == "Result" && s.tok(j+2) != "(" && s.tok(j+2) != "=":
				end = j + 2
			case s.seq(j+1, "Wait", "(", ")"):
				end = j + 4
			case s.seq(j+1, "GetAwaiter", "(", ")", ".", "GetResult", "(", ")"):
				end, anyRecv = j+8, true
			default:
				continue
			}
			r := s.chainStart(j)
			if r == j || s.tok(r-1) == "await" || (!anyRecv && !s.csTaskExpr(r, j)) || s.csInLambda(bs, r) {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(r, end),
				detail: "`" + s.codeText(r, end) + "` -> `await " + s.codeText(r, j) + "`",
			})
		}
	}
	return out
}

// csTaskExpr reports whether the code tokens [r, end) clearly produce a
// task. That's a variable declared as Task or ValueTask, a call to a method
// named *Async, or a call to Task.Run, Task.WhenAll or Task.WhenAny.
func (s *source) csTaskExpr(r, end int) bool {
	if end-r == 1 {
		d := s.visibleDecl(s.tok(r), s.code[r].Span.Start)
		return d != nil && (d.Type == "Task" || strings.HasPrefix(d.Type, "Task<") || strings.HasPrefix(d.Type, "ValueTask"))
	}
	if s.tok(end-1) != ")" {
		return false
	}
	o := s.matchOpen(s.code, end-1)
	name := s.tok(o - 1)
	if strings.HasSuffix(name, "Async") {
		return true
	}
	return o-3 == r && s.tok(r) == "Task" && (name == "Run" || name == "WhenAll" || name == "WhenAny")
}

// csInLambda reports whether code token j is in a lambda, anonymous method
// or local function body that starts after the method body opening at bs.
func (s *source) csInLambda(bs, j int) bool {
	depth := 0
	sep := false
	for k := j - 1; k > bs; k-- {
		switch s.tok(k) {
		case ")", "]", "}":
			depth++
		case "(", "[", "{":
			if depth > 0 {
				depth--
				continue
			}
			if s.tok(k) == "{" && s.csCallbackBrace(k) {
				return true
			}
			sep = false
		case ",", ";":
			if depth == 0 {
				sep = true
			}
		case "=>":
			if depth == 0 && !sep {
				return true
			}
		}
	}
	return false
}

var csStringTypes = setOf("string", "String", "System.String")

// checkCsStringConcatInLoop flags string concatenation in loops (SonarC#
// S1643).
func checkCsStringConcatInLoop(s *source) []finding {
	return s.stringConcatInLoop(csStringTypes, "Append")
}

// checkCsAsyncVoid flags `async void` methods (SonarC# S3168). Like Sonar,
// we skip overrides, event handlers (the second parameter has an *EventArgs
// type), methods named OnX, and methods the enclosing type passes as a
// delegate or subscribes with +=.
func checkCsAsyncVoid(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if (d.Kind != code.KindMethod && d.Kind != code.KindFunction) || d.Type != "void" {
			continue
		}
		a := s.csModifier(d, "async")
		if a < 0 || s.csModifier(d, "override") >= 0 {
			continue
		}
		if len(d.Name) > 2 && strings.HasPrefix(d.Name, "On") && unicode.IsUpper(rune(d.Name[2])) {
			continue
		}
		if ps := csParams(d); len(ps) == 2 && strings.HasSuffix(ps[1].Type, "EventArgs") {
			continue
		}
		if s.csUsedAsDelegate(d) {
			continue
		}
		n := s.codeIndex(d.NameSpan.Start)
		out = append(out, finding{
			span:   s.tokSpan(a, n+1),
			detail: "`" + s.codeText(a, n+1) + "` -> `async Task " + d.Name + "`",
		})
	}
	return out
}

// csUsedAsDelegate reports whether the type declaring d uses d's name as a
// method group, as an argument or on the right side of += or an assignment.
func (s *source) csUsedAsDelegate(d *code.Decl) bool {
	scope := d.Parent
	for scope != nil && !scope.IsType() {
		scope = scope.Parent
	}
	if scope == nil {
		return false
	}
	toks := s.tokensIn(scope.Span)
	for k, t := range toks {
		if s.text(t) != d.Name || t.Span.Start == d.NameSpan.Start || k == 0 || k+1 == len(toks) {
			continue
		}
		prev, next := s.text(toks[k-1]), s.text(toks[k+1])
		if (prev == "(" || prev == "," || prev == "+=" || prev == "=") && (next == ")" || next == "," || next == ";") {
			return true
		}
	}
	return false
}

// csTypeKeywords are the built-in type names that can follow `is`.
var csTypeKeywords = setOf("string", "object", "int", "long", "short", "byte", "bool", "char", "double", "float", "decimal")

// checkCsDuplicateCast flags `if (x is T)` without a designation if the
// guarded statement casts x to T again with `(T)x` or `x as T` (SonarC#
// S3247). A type test only counts if && joins it to the condition, and we
// stop scanning where x is reassigned.
func checkCsDuplicateCast(s *source) []finding {
	var out []finding
	for i := range s.code {
		if i < 2 || s.tok(i) != "is" || s.code[i-1].Kind != code.TokIdent || (s.tok(i-2) != "(" && s.tok(i-2) != "&&") {
			continue
		}
		x := s.tok(i - 1)
		te := s.csTypeEnd(i + 1)
		if te == i+1 || (s.tok(te) != ")" && s.tok(te) != "&&") {
			continue
		}
		p := s.csEnclosingParen(i)
		if p <= 0 || s.tok(p-1) != "if" {
			continue
		}
		c := s.matchClose(s.code, p)
		if c >= len(s.code) || strings.Contains(" "+s.codeText(p, c)+" ", " || ") {
			continue
		}
		bs, be := c+1, c+1
		if s.tok(bs) == "{" {
			be = s.matchClose(s.code, bs)
		} else {
			for be < len(s.code) && s.tok(be) != ";" {
				be++
			}
		}
		n := te - (i + 1)
		found := false
		for j := bs; j < be && !found; j++ {
			if s.tok(j) == x && s.tok(j+1) == "=" {
				break
			}
			switch {
			case s.tok(j) == "(" && s.sameTokens(i+1, j+1, n) && s.seq(j+1+n, ")", x) && s.tok(j-1) != "." && s.code[j-1].Kind != code.TokIdent:
				switch s.tok(j + 3 + n) {
				case ".", "?.", "(", "[", "++", "--":
				default:
					found = true
				}
			case s.tok(j) == x && s.tok(j+1) == "as" && s.sameTokens(i+1, j+2, n) && s.tok(j-1) != ".":
				found = true
			}
		}
		if !found {
			continue
		}
		v := s.tok(te - 1)
		if csTypeKeywords[v] {
			v = "value"
		} else if v != "" {
			v = strings.ToLower(v[:1]) + v[1:]
		}
		out = append(out, finding{
			span:   s.tokSpan(i-1, te),
			detail: "`" + s.codeText(i-1, te) + "` -> `" + s.codeText(i-1, te) + " " + v + "`",
		})
	}
	return out
}

// csTypeEnd returns the end of the type name that starts at code token i: a
// dotted name with optional type arguments, or a built-in type keyword. If
// no type starts there, it returns i.
func (s *source) csTypeEnd(i int) int {
	if csTypeKeywords[s.tok(i)] {
		return i + 1
	}
	if i >= len(s.code) || s.code[i].Kind != code.TokIdent || s.tok(i) == "not" || s.tok(i) == "var" {
		return i
	}
	j := i + 1
	for s.tok(j) == "." && j+1 < len(s.code) && s.code[j+1].Kind == code.TokIdent {
		j += 2
	}
	if s.tok(j) == "<" {
		depth := 0
		for k := j; k < len(s.code); k++ {
			switch s.tok(k) {
			case "<":
				depth++
			case ">":
				depth--
			case ">>":
				depth -= 2
			case "(", ")", ";", "{", "}", "&&", "||":
				return i
			}
			if depth <= 0 {
				return k + 1
			}
		}
		return i
	}
	return j
}

func (s *source) csEnclosingParen(i int) int {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		switch s.tok(j) {
		case ")", "]":
			depth++
		case "[":
			depth--
		case "(":
			if depth == 0 {
				return j
			}
			depth--
		case "{", "}", ";":
			return -1
		}
	}
	return -1
}

var csParamNameArg = map[string]int{"ArgumentNullException": 0, "ArgumentOutOfRangeException": 0, "ArgumentException": 1}

// checkCsNameof flags a string literal that names a parameter of the
// enclosing method, passed as the paramName argument of an exception in
// csParamNameArg (SonarC# S2302).
func checkCsNameof(s *source) []finding {
	var out []finding
	for i := range s.code {
		if s.tok(i) != "new" {
			continue
		}
		n := i + 1
		if s.seq(n, "System", ".") {
			n += 2
		}
		pos, ok := csParamNameArg[s.tok(n)]
		if !ok || s.tok(n+1) != "(" {
			continue
		}
		c := s.matchClose(s.code, n+1)
		arg, a := 0, n+2
		for a < c && arg < pos {
			if s.tok(a) == "," {
				arg++
			}
			if t := s.tok(a); t == "(" || t == "[" || t == "{" {
				a = s.matchClose(s.code, a)
			}
			a++
		}
		if arg != pos || a+1 > c || (s.tok(a+1) != ")" && s.tok(a+1) != ",") || s.code[a].Kind != code.TokString {
			continue
		}
		lit := s.tok(a)
		if !strings.HasPrefix(lit, `"`) {
			continue
		}
		fn := s.scopeOf(s.code[i].Span.Start)
		if !fn.Callable() {
			continue
		}
		name := lit[1 : len(lit)-1]
		for _, p := range csParams(fn) {
			if p.Name == name {
				out = append(out, finding{
					span:   s.code[a].Span,
					detail: "`" + lit + "` -> `nameof(" + name + ")`",
				})
				break
			}
		}
	}
	return out
}
