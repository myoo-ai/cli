package codelint

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/myoo-ai/cli/internal/code"
)

var pyRules = []*Rule{
	{
		ID:             "py-none-equality",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `is None` to `== None`, as None is a singleton and `==` can be overridden.",
		Recommendation: "compare with `is None` or `is not None`. keep `==` where an operator overload is intended, as in SQLAlchemy filters or pandas masks.",
		check:          checkNoneEquality,
	},
	{
		ID:             "py-identity-with-literal",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `==` to `is` for literals and new objects, as their identity is not guaranteed.",
		Recommendation: "compare values with `==` or `!=`. a new list or dict is never identical to an existing object, and the interpreter caches only some strings and numbers.",
		check:          checkIdentityWithLiteral,
	},
	{
		ID:             "py-mutable-default-argument",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `None` as the default of this parameter, as a mutable default is created once and shared by every call.",
		Recommendation: "default to `None` and create the object in the body, for example `if items is None: items = []`. widen a type annotation to allow None.",
		check:          checkMutableDefault,
	},
	{
		ID:             "py-range-len-loop",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `enumerate()` to `range(len())`, as it yields the index and the element together.",
		Recommendation: "loop over `enumerate(x)` and use the element instead of indexing `x[i]` in the body.",
		check:          checkRangeLenLoop,
	},
	{
		ID:             "py-type-comparison",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `isinstance()` to comparing `type()` results, as it also accepts subclasses.",
		Recommendation: "use `isinstance(x, T)` or `not isinstance(x, T)`. where subclasses must be rejected on purpose, compare with `type(x) is T`. note that bool is a subclass of int.",
		check:          checkTypeComparison,
	},
	{
		ID:             "py-redundant-keys-call",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer testing membership on the dict itself, as `.keys()` adds a call and a view for nothing.",
		Recommendation: "drop the `.keys()` call, `k in d` and `for k in d` already work on the keys.",
		check:          checkRedundantKeys,
	},
	{
		ID:             "py-lambda-assignment",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer a `def` statement to assigning a lambda, as a named function shows its name in tracebacks and can have a docstring.",
		Recommendation: "define the function with `def` under the same name and return the lambda body.",
		check:          checkLambdaAssignment,
	},
	{
		ID:             "py-wildcard-import",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer importing the needed names, as a wildcard import hides where names come from and can shadow local ones.",
		Recommendation: "import the names the module uses, or import the module and qualify its members. wildcard imports in `__init__.py` and pure re-export modules are fine.",
		check:          checkWildcardImport,
	},
	{
		ID:             "py-sorted-for-min-max",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `min()` or `max()` to indexing a `sorted()` result, as they find the value without sorting the whole input.",
		Recommendation: "call `min()` or `max()` with the same iterable and `key`. on ties `max()` returns the first largest element where `sorted()[-1]` returns the last one.",
		check:          checkSortedMinMax,
	},
	{
		ID:             "py-list-first-element",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer `next(iter(x))` to `list(x)[0]`, as it reads one element instead of copying the whole iterable.",
		Recommendation: "use `next(iter(x))`, or `next(iter(x), default)` when x may be empty. an empty input raises StopIteration instead of IndexError.",
		check:          checkListFirstElement,
	},
	{
		ID:             "py-startswith-tuple",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer one `startswith()` or `endswith()` call with a tuple, as it is shorter and scans the string once.",
		Recommendation: "pass the alternatives as a tuple, for example `s.startswith((\"a\", \"b\"))`.",
		check:          checkStartswithTuple,
	},
	{
		ID:             "py-open-without-with",
		Langs:          []code.Lang{code.Python},
		Description:    "prefer opening files in a `with` statement, as it closes the file even when an exception is raised.",
		Recommendation: "open the file with `with open(...) as f:` and use f inside the block. a handle kept beyond the block belongs in an attribute or `ExitStack`.",
		check:          checkOpenWithoutWith,
	},
}

var (
	// pyOperandEnds are the tokens that end a comparison operand, because
	// they bind looser than comparisons.
	pyOperandEnds = setOf(")", "]", "}", ",", ":", ";", "and", "or", "if", "else", "for")
	// An expression can start after these tokens.
	pyExprStarts = setOf("", "(", "[", "{", ",", "=", ":", ";", "and", "or", "not", "if", "elif", "while", "return", "assert", "yield", "else", "lambda", "in")
	pyAssignOps  = setOf("=", "+=", "-=", "*=", "/=", "//=", "%=", "**=", "@=", "&=", "|=", "^=", ">>=", "<<=")
	// pyTypeNames are the builtin classes people compare type() results to.
	pyTypeNames = setOf("int", "float", "complex", "bool", "str", "bytes", "bytearray", "list", "tuple", "dict", "set", "frozenset")
	// pyDictTypes are the annotations and constructors of mappings whose
	// keys() view tests membership the same way the mapping does.
	pyDictTypes = setOf("dict", "Dict", "Mapping", "MutableMapping", "defaultdict", "OrderedDict", "Counter")
	// pyLoaders are functions that read or write a whole file object and
	// then drop it: json.load(f), pickle.dump(x, f), yaml.safe_load(f).
	pyLoaders = setOf("load", "dump", "safe_load", "safe_dump")
	// pyMutators are the methods that change a list, dict or set in place.
	pyMutators = setOf("append", "extend", "insert", "pop", "remove", "clear", "sort", "reverse", "popitem", "update", "setdefault", "add", "discard")
	// A wildcard import from these modules replaces the builtin open(),
	// like `from posix import *` does in os.py.
	pyOpenModules = setOf("os", "posix", "nt", "io", "_io", "codecs", "gzip", "bz2", "lzma", "tarfile", "shelve", "dbm", "webbrowser", "wave", "tokenize")
)

// pyLines returns the logical lines of a Python file as [start, end)
// index ranges into s.code.
func (s *source) pyLines() [][2]int {
	lls := s.splitLogicalLines(s.code)
	out := make([][2]int, len(lls))
	for k, ll := range lls {
		a := s.codeIndex(ll[0].Span.Start)
		out[k] = [2]int{a, a + len(ll)}
	}
	return out
}

// pyDepth returns the bracket depth of code token i, counting from token a.
func (s *source) pyDepth(a, i int) int {
	depth := 0
	for k := a; k < i; k++ {
		switch s.tok(k) {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
	}
	return depth
}

// pyOperandEnd reports whether code token i ends an operand of a comparison
// in the logical line that ends before token end.
func (s *source) pyOperandEnd(i, end int) bool {
	return i >= end || pyOperandEnds[s.tok(i)]
}

// pyDottedEnd returns the index after the dotted name (a, a.b.c) that starts
// at code token i, or i if none starts there.
func (s *source) pyDottedEnd(i int) int {
	if i >= len(s.code) || s.code[i].Kind != code.TokIdent && !s.isKeyword(s.code[i], "self") {
		return i
	}
	j := i + 1
	for s.tok(j) == "." && j+1 < len(s.code) && s.code[j+1].Kind == code.TokIdent {
		j += 2
	}
	return j
}

func (s *source) pyHasTok(i, j int, t string) bool {
	for k := i; k < j; k++ {
		if s.tok(k) == t {
			return true
		}
	}
	return false
}

// pyShadowed reports whether the file declares or imports name, or imports
// open() through a wildcard. If so, a call to it might not reach the
// builtin.
func (s *source) pyShadowed(name string) bool {
	for _, d := range s.f.Decls {
		if d.Name == name {
			return true
		}
	}
	if name != "open" {
		return false
	}
	for k := 1; k+1 < len(s.code); k++ {
		if s.seq(k, "import", "*") && pyOpenModules[s.tok(k-1)] {
			return true
		}
	}
	return false
}

// pyBuiltinCall reports whether code token i calls the builtin name, i.e. a
// bare name followed by "(" that the file doesn't redefine.
func (s *source) pyBuiltinCall(i int, name string) bool {
	return s.tok(i) == name && s.code[i].Kind == code.TokIdent && s.tok(i+1) == "(" &&
		s.tok(i-1) != "." && s.tok(i-1) != "def" && !s.pyShadowed(name)
}

// checkNoneEquality flags `x == None` and `None != x` (pycodestyle E711). We
// skip comparisons inside brackets and on subscripts, because SQLAlchemy
// filters (`where(T.c == None)`) and pandas masks (`df[df.a == None]`)
// overload == on purpose.
func checkNoneEquality(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		for i := a + 1; i < b-1; i++ {
			op := s.tok(i)
			if op != "==" && op != "!=" || s.pyDepth(a, i) != 0 {
				continue
			}
			is := "is"
			if op == "!=" {
				is = "is not"
			}
			if s.tok(i+1) == "None" && s.pyOperandEnd(i+2, b) && s.tok(i-1) != "None" {
				start := s.chainStart(i)
				if start == i || start < a || s.pyHasTok(start, i, "[") {
					continue
				}
				out = append(out, finding{
					span:   s.tokSpan(start, i+2),
					detail: "`" + s.codeText(start, i+2) + "` -> `" + s.codeText(start, i) + " " + is + " None`",
				})
				continue
			}
			if s.tok(i-1) == "None" && pyExprStarts[s.tok(i-2)] {
				end := s.chainEnd(i + 1)
				if end == i+1 || !s.pyOperandEnd(end, b) || s.pyHasTok(i+1, end, "[") {
					continue
				}
				out = append(out, finding{
					span:   s.tokSpan(i-1, end),
					detail: "`" + s.codeText(i-1, end) + "` -> `" + s.codeText(i+1, end) + " " + is + " None`",
				})
			}
		}
	}
	return out
}

// pyLiteralEnd returns the index after the literal that starts at code token
// i if it's a string, a number, a negative number, a [...] or {...} display
// or a tuple, since you can't rely on the identity of those.
// Otherwise it returns -1.
func (s *source) pyLiteralEnd(i int) int {
	if i >= len(s.code) {
		return -1
	}
	switch t := s.code[i]; {
	case t.Kind == code.TokString || t.Kind == code.TokNumber:
		return i + 1
	case s.tok(i) == "-" && i+1 < len(s.code) && s.code[i+1].Kind == code.TokNumber:
		return i + 2
	}
	switch s.tok(i) {
	case "[", "{":
		if c := s.matchClose(s.code, i); c < len(s.code) {
			return c + 1
		}
	case "(":
		c := s.matchClose(s.code, i)
		if c < len(s.code) && (c == i+1 || len(s.pyCommas(i+1, c)) > 0) {
			return c + 1
		}
	}
	return -1
}

// checkIdentityWithLiteral flags `x is "a"`, `x is not 1` and `x is []`
// (SonarPython S5795 and S5796).
func checkIdentityWithLiteral(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		for i := a + 1; i < b-1; i++ {
			if s.tok(i) != "is" {
				continue
			}
			k, eq := i+1, "=="
			if s.tok(k) == "not" {
				k, eq = k+1, "!="
			}
			if end := s.pyLiteralEnd(k); end > 0 && end <= b && s.tok(end) != "." && s.tok(end) != "(" && s.tok(end) != "[" {
				start := s.chainStart(i)
				if start == i || start < a {
					out = append(out, finding{
						span:   s.tokSpan(i, end),
						detail: "`" + s.codeText(i, end) + "` -> `" + eq + " " + s.codeText(k, end) + "`",
					})
					continue
				}
				out = append(out, finding{
					span:   s.tokSpan(start, end),
					detail: "`" + s.codeText(start, end) + "` -> `" + s.codeText(start, i) + " " + eq + " " + s.codeText(k, end) + "`",
				})
				continue
			}
			lit := s.code[i-1]
			if lit.Kind != code.TokString && lit.Kind != code.TokNumber || !pyExprStarts[s.tok(i-2)] && i-1 != a {
				continue
			}
			end := s.chainEnd(k)
			if end == k || !s.pyOperandEnd(end, b) {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(i-1, end),
				detail: "`" + s.codeText(i-1, end) + "` -> `" + s.codeText(i-1, i) + " " + eq + " " + s.codeText(k, end) + "`",
			})
		}
	}
	return out
}

// checkMutableDefault flags parameters that default to `[]`, `{}`, a list or
// dict display, or a `list()`, `dict()` or `set()` call (ruff B006, pylint
// W0102). We skip idioms that rely on the shared object: parameters starting
// with "_" (`_cache={}`), parameters whose items the body assigns
// (`cache[key] = v`, `calls[0] += 1`), and signatures that bind globals as
// locals (`len=len`).
func checkMutableDefault(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() {
			continue
		}
		i := s.codeIndex(d.NameSpan.Start)
		if s.tok(i-1) != "def" || s.tok(i+1) != "(" {
			continue
		}
		open, close := i+1, s.matchClose(s.code, i+1)
		if close >= len(s.code) {
			continue
		}
		bounds := append([]int{open}, s.pyCommas(open+1, close)...)
		bounds = append(bounds, close)
		if s.pyBindsLocals(bounds) {
			continue
		}
		for p := 0; p+1 < len(bounds); p++ {
			from, to := bounds[p]+1, bounds[p+1]
			eq := -1
			for k := from; k < to; k++ {
				if s.tok(k) == "=" && s.pyDepth(from, k) == 0 {
					eq = k
					break
				}
			}
			if eq < 0 || strings.HasPrefix(s.tok(from), "_") || !s.pyMutable(eq+1, to) || s.pyStoresInto(d, s.tok(from)) {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(from, to),
				detail: "`" + s.codeText(from, to) + "` -> `" + s.pyParamNone(from, eq) + "`",
			})
		}
	}
	return out
}

// pyParamNone returns the parameter in the code tokens [from, eq) with a None
// default, spaced the way PEP 8 wants: `b=None`, `b: list | None = None`.
func (s *source) pyParamNone(from, eq int) string {
	if eq == from+1 {
		return s.tok(from) + "=None"
	}
	return s.codeText(from, eq) + " | None = None"
}

// pyMutable reports whether the code tokens [i, j) are exactly a [...] or
// {...} display, or a list(), dict() or set() call.
func (s *source) pyMutable(i, j int) bool {
	switch s.tok(i) {
	case "[", "{":
		return s.matchClose(s.code, i) == j-1
	case "list", "dict", "set":
		return s.pyBuiltinCall(i, s.tok(i)) && s.matchClose(s.code, i+1) == j-1
	}
	return false
}

// checkRangeLenLoop flags `for i in range(len(x)):` loops whose body reads
// `x[i]` (pylint C0200, and SonarPython S8518 for the index lookups). We
// skip loops that assign or delete `x[i]`, since enumerate() doesn't give
// you an element to write to.
func checkRangeLenLoop(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		if !s.seq(a, "for") || s.code[a+1].Kind != code.TokIdent || !s.seq(a+2, "in", "range", "(", "len", "(") ||
			!s.pyBuiltinCall(a+3, "range") || !s.pyBuiltinCall(a+5, "len") {
			continue
		}
		x := a + 7
		xEnd := s.pyDottedEnd(x)
		colon := xEnd + 2
		if xEnd == x || !s.seq(xEnd, ")", ")", ":") || colon >= b {
			continue
		}
		bodyEnd := b
		if s.line(s.code[colon+1].Span.Start) > s.line(s.code[colon].Span.Start) {
			base := s.indent(s.line(s.code[a].Span.Start))
			for bodyEnd = colon + 1; bodyEnd < len(s.code); bodyEnd++ {
				if s.firstOnLine(bodyEnd) && s.indent(s.line(s.code[bodyEnd].Span.Start)) <= base {
					break
				}
			}
		}
		if !s.pyReadsIndex(colon+1, bodyEnd, x, xEnd, s.tok(a+1)) {
			continue
		}
		out = append(out, finding{
			span: s.tokSpan(a, colon+1),
			detail: "`" + s.codeText(a, colon+1) + "` -> `for " + s.tok(a+1) + ", item in enumerate(" +
				s.codeText(x, xEnd) + "):`",
		})
	}
	return out
}

// pyReadsIndex reports whether the code tokens [from, to) read `x[idx]`,
// with x the tokens [x, xEnd), and never change x. We need that because
// range(len(x)) is fixed before the loop, but enumerate(x) sees items the
// body deletes or inserts, and if x gets rebound the loop keeps going over
// the old object.
func (s *source) pyReadsIndex(from, to, x, xEnd int, idx string) bool {
	n, reads := xEnd-x, false
	for _, ll := range s.splitLogicalLines(s.code[from:to]) {
		a := s.codeIndex(ll[0].Span.Start)
		b := a + len(ll)
		for k := a; k+n < b; k++ {
			if !s.sameTokens(x, k, n) || s.tok(k-1) == "." {
				continue
			}
			switch after := s.tok(k + n); {
			case after == "[":
				// A subscript before the line's assignment operator is assigned
				// to.
				if s.tok(k-1) == "del" || s.pyAssignBefore(a, b) && !s.pyAssignBefore(a, k) {
					return false
				}
				reads = reads || s.seq(k+n, "[", idx, "]")
			case after == "." && pyMutators[s.tok(k+n+1)] && s.tok(k+n+2) == "(":
				return false
			case pyAssignOps[after] && k == a:
				return false
			}
		}
	}
	return reads
}

// pyBindsLocals reports whether the parameters between bounds (the
// parenthesis and comma indexes of a signature) include `len=len`. That
// idiom binds globals as fast locals, so each default is a constant that's
// meant to be shared across calls.
func (s *source) pyBindsLocals(bounds []int) bool {
	for p := 0; p+1 < len(bounds); p++ {
		from, to := bounds[p]+1, bounds[p+1]
		if to == from+3 && s.tok(from+1) == "=" && s.tok(from) == s.tok(from+2) {
			return true
		}
	}
	return false
}

// pyStoresInto reports whether the body of d assigns to an item of the
// parameter name, like `cache[key] = value` or `counts[0] += 1`. Those are
// the memoization and call counter idioms, which rely on a shared default.
func (s *source) pyStoresInto(d *code.Decl, name string) bool {
	end := s.codeIndex(d.Body.End)
	for k := s.codeIndex(d.Body.Start); k < end; k++ {
		if s.tok(k) != name || s.tok(k-1) == "." || s.tok(k+1) != "[" {
			continue
		}
		if c := s.matchClose(s.code, k+1); c < end && pyAssignOps[s.tok(c+1)] {
			return true
		}
	}
	return false
}

func (s *source) pyAssignBefore(a, k int) bool {
	for j := a; j < k; j++ {
		if pyAssignOps[s.tok(j)] && s.pyDepth(a, j) == 0 {
			return true
		}
	}
	return false
}

// checkTypeComparison flags `type(x) == T` and `T != type(x)` where T is a
// class name (SonarPython S6660). We leave `type(a) == type(b)` alone.
func checkTypeComparison(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		for i := a; i < b; i++ {
			if !s.pyBuiltinCall(i, "type") {
				continue
			}
			c := s.matchClose(s.code, i+1)
			if c >= b || c == i+2 || len(s.pyCommas(i+2, c)) > 0 {
				continue
			}
			arg := s.codeText(i+2, c)
			if op := s.tok(c + 1); op == "==" || op == "!=" {
				t := c + 2
				tEnd := s.pyDottedEnd(t)
				if tEnd > t && s.pyOperandEnd(tEnd, b) && s.pyClassName(tEnd-1) && pyExprStarts[s.tok(i-1)] {
					out = append(out, s.pyTypeFinding(i, tEnd, op, arg, s.codeText(t, tEnd)))
				}
				continue
			}
			if op := s.tok(i - 1); (op == "==" || op == "!=") && s.pyOperandEnd(c+1, b) {
				t := i - 2
				for t-2 >= a && s.tok(t-1) == "." && s.code[t-2].Kind == code.TokIdent {
					t -= 2
				}
				if t >= a && s.pyClassName(i-2) && pyExprStarts[s.tok(t-1)] && s.pyDottedEnd(t) == i-1 {
					out = append(out, s.pyTypeFinding(t, c+1, op, arg, s.codeText(t, i-1)))
				}
			}
		}
	}
	return out
}

// pyClassName reports whether code token i names a class, i.e. a builtin
// type or a capitalized identifier.
func (s *source) pyClassName(i int) bool {
	name := s.tok(i)
	if s.code[i].Kind != code.TokIdent || name == "" {
		return false
	}
	return pyTypeNames[name] || unicode.IsUpper([]rune(name)[0])
}

func (s *source) pyTypeFinding(i, j int, op, arg, typ string) finding {
	repl := "isinstance(" + arg + ", " + typ + ")"
	if op == "!=" {
		repl = "not " + repl
	}
	return finding{span: s.tokSpan(i, j), detail: "`" + s.codeText(i, j) + "` -> `" + repl + "`"}
}

// checkRedundantKeys flags `k in d.keys()`, `k not in d.keys()` and
// `for k in d.keys()` (SonarPython S8521, pylint C0201). Like those, we need
// evidence that d is a mapping, because other keys() methods (sqlite3.Row,
// Redis clients) don't match the object's own membership test.
func checkRedundantKeys(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		for i := a + 1; i < b; i++ {
			if s.tok(i) != "in" {
				continue
			}
			r := i + 1
			end := s.chainEnd(r)
			if end-r < 5 || !s.seq(end-4, ".", "keys", "(", ")") || !s.pyOperandEnd(end, b) && !s.seq(end, "]") || !s.pyMapping(r, end-4) {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(r, end),
				detail: "`" + s.codeText(r, end) + "` -> `" + s.codeText(r, end-4) + "`",
			})
		}
	}
	return out
}

// pyMapping reports whether we know the code tokens [i, j) hold a dict:
// os.environ, a `**kwargs` parameter, or a name that's annotated with a
// mapping type or assigned a dict display or constructor call.
func (s *source) pyMapping(i, j int) bool {
	if j == i+3 && s.seq(i, "os", ".", "environ") {
		return true
	}
	if j != i+1 {
		return false
	}
	name := s.tok(i)
	for scope := s.f.DeclAt(s.code[i].Span.Start); scope != nil; scope = scope.Parent {
		for _, d := range scope.Children {
			if d.Name != name || d.Kind != code.KindVariable && d.Kind != code.KindConstant && d.Kind != code.KindParameter {
				continue
			}
			if typ, _, _ := strings.Cut(d.Type, "["); pyDictTypes[strings.TrimPrefix(strings.TrimPrefix(typ, "typing."), "collections.")] {
				return true
			}
			k := s.codeIndex(d.NameSpan.Start)
			if s.tok(k-1) == "**" {
				return true
			}
			if s.tok(k+1) != "=" {
				return false
			}
			v := k + 2
			if s.tok(v) == "{" {
				c := s.matchClose(s.code, v)
				return c == v+1 || s.pyHasTok(v+1, c, ":") && s.pyDepth(v+1, c) == 0
			}
			return pyDictTypes[s.tok(v)] && s.tok(v+1) == "("
		}
	}
	return false
}

// checkLambdaAssignment flags `f = lambda x: ...` with plain name targets
// (SonarPython S6661, pycodestyle E731). We skip tuple targets and
// annotated class attributes (dataclass fields).
func checkLambdaAssignment(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		if s.code[a].Kind != code.TokIdent {
			continue
		}
		k := a
		switch s.tok(a + 1) {
		case "=":
			for s.code[k].Kind == code.TokIdent && s.tok(k+1) == "=" {
				k += 2
			}
		case ":":
			if s.pyInClassBody(a) {
				continue
			}
			for k = a + 2; k < b && (s.tok(k) != "=" || s.pyDepth(a, k) != 0); k++ {
			}
			k++
		default:
			continue
		}
		if k >= b || s.tok(k) != "lambda" {
			continue
		}
		colon := k + 1
		for colon < b && (s.tok(colon) != ":" || s.pyDepth(k, colon) != 0) {
			colon++
		}
		if colon+1 >= b {
			continue
		}
		if len(s.pyCommas(colon+1, b)) > 0 {
			continue
		}
		out = append(out, finding{
			span: s.tokSpan(a, b),
			detail: "`" + s.codeText(a, b) + "` -> `def " + s.tok(a) + "(" + s.pyParams(k+1, colon) + "): return " +
				s.codeText(colon+1, b) + "`",
		})
	}
	return out
}

func (s *source) pyParams(i, j int) string {
	if i >= j {
		return ""
	}
	return s.codeText(i, j)
}

func (s *source) pyCommas(i, j int) []int {
	var out []int
	depth := 0
	for k := i; k < j; k++ {
		switch s.tok(k) {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ",":
			if depth == 0 {
				out = append(out, k)
			}
		}
	}
	return out
}

func (s *source) pyInClassBody(i int) bool {
	d := s.f.DeclAt(s.code[i].Span.Start)
	for d != nil && d.Kind != code.KindClass && !d.Callable() && d != s.f.Module {
		d = d.Parent
	}
	return d != nil && d.Kind == code.KindClass
}

// checkWildcardImport flags `from m import *` at the top of a module
// (SonarPython S2208). Like Sonar, we skip `__init__.py` and modules that
// only re-export names with imports, dunder assignments, del and
// warnings.warn(), and no functions, classes, loops or other assignments
// and calls. We also skip imports nested in try or if blocks (optional C
// accelerators, platform modules), imports of a private module the file
// wraps (`from _socket import *`), and modules that re-export `m.__all__`.
func checkWildcardImport(s *source) []finding {
	if filepath.Base(s.f.Path) == "__init__.py" {
		return nil
	}
	var out []finding
	logic := false
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		if s.tok(a) != "from" || !s.seq(b-2, "import", "*") {
			logic = logic || s.pyLogic(a, b)
			continue
		}
		mod := s.tok(b - 3)
		if s.indent(s.line(s.code[a].Span.Start)) > 0 || strings.HasPrefix(mod, "_") || s.pyReexports(mod) {
			continue
		}
		out = append(out, finding{span: s.tokSpan(a, b)})
	}
	if !logic {
		return nil
	}
	return out
}

// pyReexports reports whether the file reads `mod.__all__`, like tokenize
// does with `__all__ = token.__all__ + [...]`.
func (s *source) pyReexports(mod string) bool {
	for k := range s.code {
		if s.seq(k, mod, ".", "__all__") {
			return true
		}
	}
	return false
}

// pyLogic reports whether the logical line [a, b) might hold application
// logic, as checkWildcardImport sees it. Registering submodules in
// sys.modules, like xml.parsers.expat does, counts as re-exporting.
func (s *source) pyLogic(a, b int) bool {
	if s.tok(a) == "import" || s.tok(a) == "from" || s.tok(a) == "del" || s.seq(a, "sys", ".", "modules", "[") {
		return false
	}
	first := s.tok(a)
	dunder := len(first) > 4 && strings.HasPrefix(first, "__") && strings.HasSuffix(first, "__")
	for k := a; k < b; k++ {
		t := s.tok(k)
		switch {
		case s.isKeyword(s.code[k], "def", "class", "while", "for", "with", "lambda"):
			return true
		case pyAssignOps[t] && !(dunder && t == "=" && k == a+1):
			return true
		case t == "(" && k > a && (s.code[k-1].Kind == code.TokIdent || s.tok(k-1) == ")" || s.tok(k-1) == "]"):
			if !(k-3 >= a && s.seq(k-3, "warnings", ".", "warn")) {
				return true
			}
		}
	}
	return false
}

// checkSortedMinMax flags `sorted(x)[0]` and `sorted(x)[-1]` (SonarPython
// S8517). If there's a `reverse=` argument that isn't a True, False, 1 or 0
// literal, we can't tell what the result is, so we skip the call.
func checkSortedMinMax(s *source) []finding {
	var out []finding
	for i := range len(s.code) {
		if !s.pyBuiltinCall(i, "sorted") {
			continue
		}
		c := s.matchClose(s.code, i+1)
		if c >= len(s.code) || s.tok(c+1) != "[" {
			continue
		}
		last := false
		switch {
		case s.seq(c+1, "[", "0", "]"):
		case s.seq(c+1, "[", "-", "1", "]"):
			last = true
		default:
			continue
		}
		end := s.matchClose(s.code, c+1) + 1
		args := append([]int{i + 1}, s.pyCommas(i+2, c)...)
		args = append(args, c)
		first := args[0] + 1
		if first >= args[1] || s.tok(first) == "*" || s.tok(first) == "**" || s.tok(first+1) == "=" {
			continue
		}
		key, reverse, ok := "", false, true
		for p := 1; p+1 < len(args) && ok; p++ {
			from, to := args[p]+1, args[p+1]
			if from >= to {
				continue
			}
			switch {
			case s.seq(from, "key", "="):
				key = ", " + s.codeText(from, to)
			case s.seq(from, "reverse", "=") && to == from+3 && (s.tok(from+2) == "True" || s.tok(from+2) == "1"):
				reverse = true
			case s.seq(from, "reverse", "=") && to == from+3 && (s.tok(from+2) == "False" || s.tok(from+2) == "0"):
			default:
				ok = false
			}
		}
		if !ok {
			continue
		}
		fn := "min"
		if last != reverse {
			fn = "max"
		}
		out = append(out, finding{
			span:   s.tokSpan(i, end),
			detail: "`" + s.codeText(i, end) + "` -> `" + fn + "(" + s.codeText(first, args[1]) + key + ")`",
		})
	}
	return out
}

// checkListFirstElement flags `list(x)[0]` (SonarPython S8519).
func checkListFirstElement(s *source) []finding {
	var out []finding
	for i := range len(s.code) {
		if !s.pyBuiltinCall(i, "list") {
			continue
		}
		c := s.matchClose(s.code, i+1)
		if c >= len(s.code) || c == i+2 || !s.seq(c+1, "[", "0", "]") || len(s.pyCommas(i+2, c)) > 0 ||
			s.tok(i+2) == "*" || s.tok(i+2) == "**" {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, c+4),
			detail: "`" + s.codeText(i, c+4) + "` -> `next(iter(" + s.codeText(i+2, c) + "))`",
		})
	}
	return out
}

// checkStartswithTuple flags `s.startswith(a) or s.startswith(b)` on the
// same receiver, and the same for endswith (SonarPython S8513). Like Sonar,
// we need string literal arguments, receivers without calls, and the same
// method in all operands.
func checkStartswithTuple(s *source) []finding {
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		for i := a; i < b; i++ {
			if !pyExprStarts[s.tok(i-1)] || s.tok(i-1) == "not" || s.tok(i-1) == "and" || s.tok(i-1) == "in" {
				continue
			}
			recv, method, next := s.pyPrefixCall(i, b)
			if next < 0 {
				continue
			}
			n := recv - i
			args := []string{s.tok(recv + 3)}
			end, safeEnd, safeArgs := next, -1, 0
			for s.tok(end) == "or" {
				r, m, nx := s.pyPrefixCall(end+1, b)
				if nx < 0 || r-(end+1) != n || !s.sameTokens(i, end+1, n) || m != method {
					break
				}
				args = append(args, s.tok(r+3))
				end = nx
				if s.tok(end) == "or" || s.pyOperandEnd(end, b) && s.tok(end) != "and" {
					safeEnd, safeArgs = end, len(args)
				}
			}
			if safeEnd < 0 {
				continue
			}
			out = append(out, finding{
				span: s.tokSpan(i, safeEnd),
				detail: "`" + s.codeText(i, safeEnd) + "` -> `" + s.codeText(i, recv) + "." + method + "((" +
					strings.Join(args[:safeArgs], ", ") + "))`",
			})
			i = safeEnd - 1
		}
	}
	return out
}

// pyPrefixCall matches `recv.startswith("lit")` or endswith at code token i,
// where recv is a dotted name. It returns the index of the "." before the
// method, the method, and the index after the call. If there's no match,
// next is -1.
func (s *source) pyPrefixCall(i, b int) (recv int, method string, next int) {
	// pyDottedEnd takes the method name too, so we step back to the ".".
	recv = s.pyDottedEnd(i) - 2
	if recv <= i || s.tok(recv) != "." {
		return 0, "", -1
	}
	method = s.tok(recv + 1)
	if method != "startswith" && method != "endswith" || !s.seq(recv+2, "(") || recv+5 > b ||
		s.code[recv+3].Kind != code.TokString || s.tok(recv+4) != ")" {
		return 0, "", -1
	}
	return recv, method, recv + 5
}

// checkOpenWithoutWith flags calls to the builtin open() outside a `with`
// item (pylint R1732, ruff SIM115), in the shapes where a `with` block keeps
// the behavior: `open(p).read()`, `json.load(open(p))`, and `f = open(p)` in
// a function that only uses f through attributes. We skip handles that
// escape, such as returned, passed on, stored or assigned on another branch,
// and module globals and __enter__.
func checkOpenWithoutWith(s *source) []finding {
	if s.pyShadowed("open") {
		return nil
	}
	var out []finding
	for _, ln := range s.pyLines() {
		a, b := ln[0], ln[1]
		header := -1
		if s.tok(a) == "with" || s.seq(a, "async", "with") {
			for header = a; header < b && (s.tok(header) != ":" || s.pyDepth(a, header) != 0); header++ {
			}
		}
		for i := a; i < b; i++ {
			if i < header || !s.pyBuiltinCall(i, "open") {
				continue
			}
			c := s.matchClose(s.code, i+1)
			scope := s.scopeOf(s.code[i].Span.Start)
			if c >= b || scope == s.f.Module || scope.Name == "__enter__" {
				continue
			}
			call := s.codeText(i, c+1)
			detail := "`" + call + "` -> `with " + call + " as f:`"
			switch {
			case s.tok(c+1) == "." || s.pyLoadArg(a, i, c):
			case i == a+2 && s.tok(a+1) == "=" && s.code[a].Kind == code.TokIdent && c+1 == b:
				if s.pyHandleEscapes(a, scope) {
					continue
				}
				name := s.tok(a)
				detail = "`" + name + " = " + call + "` -> `with " + call + " as " + name + ":`"
			default:
				continue
			}
			out = append(out, finding{span: s.tokSpan(i, c+1), detail: detail})
		}
	}
	return out
}

// pyLoadArg reports whether the open() call [i, c] in the logical line
// starting at code token a is a whole argument of a load() or dump() call,
// like json.load(open(p)), which reads or writes the file and drops it.
func (s *source) pyLoadArg(a, i, c int) bool {
	if s.tok(c+1) != ")" && s.tok(c+1) != "," {
		return false
	}
	depth := 0
	for k := i - 1; k > a; k-- {
		switch s.tok(k) {
		case ")", "]", "}":
			depth++
		case "[", "{":
			depth--
		case "(":
			if depth == 0 {
				return s.tok(k-2) == "." && pyLoaders[s.tok(k-1)]
			}
			depth--
		}
		if depth < 0 {
			return false
		}
	}
	return false
}

// pyHandleEscapes reports whether the name assigned at code token at might
// outlive the call. That's the case if scope uses it other than through
// attribute access or as the iterable of a for loop, as in `return f`,
// `Reader(f)`, `with f:` or `f = sys.stdin` on another branch.
func (s *source) pyHandleEscapes(at int, scope *code.Decl) bool {
	name := s.tok(at)
	end := s.codeIndex(scope.Span.End)
	for k := s.codeIndex(scope.Span.Start); k < end; k++ {
		if k == at || s.tok(k) != name || s.code[k].Kind != code.TokIdent || s.tok(k-1) == "." {
			continue
		}
		if s.tok(k+1) == "." || s.tok(k-1) == "in" && s.tok(k+1) == ":" {
			continue
		}
		return true
	}
	return false
}
