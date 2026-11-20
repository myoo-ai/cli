package codelint

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

var langCPP = []code.Lang{code.CPP}

var cRules = []*Rule{
	{
		ID:             "c-insecure-function",
		Langs:          langsC,
		Description:    "prefer a bounded alternative to this function, as it writes past the end of the buffer when the input is longer than expected.",
		Recommendation: "pass the buffer size, for example `sprintf(buf, fmt, x)` becomes `snprintf(buf, sizeof buf, fmt, x)` and `gets(buf)` becomes `fgets(buf, sizeof buf, stdin)`. `sizeof` gives the buffer size only for arrays, not for pointers. in C++ prefer `std::string`.",
		check:          checkInsecureFunction,
	},
	{
		ID:             "c-atoi",
		Langs:          langsC,
		Description:    "prefer strtol or strtod instead, as atoi cannot report invalid input or overflow.",
		Recommendation: "use `strtol` or `strtod` and check the end pointer and `errno`. in C++ `std::stoi` and its siblings throw on invalid input, and `std::from_chars` reports errors without exceptions.",
		check:          checkAtoi,
	},
	{
		ID:             "c-void-parameter-list",
		Langs:          []code.Lang{code.C},
		Description:    "prefer declaring an empty parameter list as `(void)` instead, as `()` declares a function without a prototype that accepts any arguments.",
		Recommendation: "write `int f(void)` instead of `int f()`, so the compiler rejects calls that pass arguments.",
		check:          checkVoidParameterList,
	},
	{
		ID:             "cpp-nullptr",
		Langs:          langCPP,
		Description:    "prefer using `nullptr` instead of `NULL`, as `NULL` is an integer constant that can select the wrong overload.",
		Recommendation: "replace `NULL` with `nullptr`. code that must also compile as C can keep `NULL`.",
		check:          checkNullptr,
	},
	{
		ID:             "cpp-typedef",
		Langs:          langCPP,
		Description:    "prefer a `using` alias instead of `typedef`, as it reads left to right and also works for templates.",
		Recommendation: "rewrite `typedef T Name;` as `using Name = T;`. for a struct, `typedef struct S {...} S;` becomes `struct S {...};`.",
		check:          checkTypedef,
	},
	{
		ID:             "cpp-macro-constant",
		Langs:          langCPP,
		Description:    "prefer a `constexpr` variable instead of a macro to define a constant, as it has a type and a scope.",
		Recommendation: "replace `#define NAME value` with `constexpr auto NAME = value;` in the namespace that uses it. keep the macro when the preprocessor or C code needs it.",
		check:          checkMacroConstant,
	},
	{
		ID:             "cpp-std-endl",
		Langs:          langCPP,
		Description:    "prefer writing `'\\n'` instead of `std::endl`, as `std::endl` also flushes the stream on every line.",
		Recommendation: "write `'\\n'`, or append it to the preceding string literal. where the output must appear at once, flush explicitly with `std::flush`.",
		check:          checkStdEndl,
	},
	{
		ID:             "cpp-make-unique",
		Langs:          langCPP,
		Description:    "prefer `std::make_unique` or `std::make_shared` instead of passing `new` to a smart pointer, as it is shorter and cannot leak when another argument throws.",
		Recommendation: "write `auto p = std::make_unique<T>(args)` instead of `std::unique_ptr<T> p(new T(args))`. `std::make_shared` also allocates the object and the reference count in one block. `std::make_unique` needs C++14, and a constructor that is not public needs `new`.",
		check:          checkMakeUnique,
	},
	{
		ID:             "cpp-manual-memory",
		Langs:          langCPP,
		Description:    "prefer containers and smart pointers instead of managing memory by hand, as a missed or doubled release leaks or corrupts memory.",
		Recommendation: "own the memory with `std::vector`, `std::string`, `std::unique_ptr` or `std::shared_ptr`, which release it in their destructors. keep `malloc` and `free` only for buffers that C code allocates or releases.",
		check:          checkManualMemory,
	},
	{
		ID:             "cpp-c-style-cast",
		Langs:          langCPP,
		Description:    "prefer a named cast instead of a C-style cast, as it states the conversion and the compiler rejects the unsafe ones.",
		Recommendation: "use `static_cast` for numeric conversions and conversions from `void*`, `const_cast` to remove `const` and `reinterpret_cast` between unrelated pointer types. `(void)expr` that discards a value is fine.",
		check:          checkCStyleCast,
	},
	{
		ID:             "cpp-size-empty",
		Langs:          langCPP,
		Description:    "prefer using `empty()` instead of comparing `size()` with zero, as it states the intent and is constant time for every container.",
		Recommendation: "write `v.empty()` for `v.size() == 0` and `!v.empty()` for `v.size() != 0`. types without an `empty()` member keep the comparison.",
		check:          checkSizeEmpty,
	},
	{
		ID:             "cpp-using-namespace-in-header",
		Langs:          langCPP,
		Description:    "avoid `using namespace` at namespace scope in a header, as it leaks the names into every file that includes the header.",
		Recommendation: "qualify the names (`std::string`), or move the directive into the source file or a function body. `using namespace` of a literals namespace is fine.",
		check:          checkUsingNamespaceInHeader,
	},
}

var (
	insecureFunctions = map[string]string{
		"gets": "fgets", "strcpy": "snprintf", "strcat": "snprintf", "sprintf": "snprintf", "vsprintf": "vsnprintf",
	}
	atoiC   = map[string]string{"atoi": "strtol", "atol": "strtol", "atoll": "strtoll", "atof": "strtod"}
	atoiCPP = map[string]string{"atoi": "std::stoi", "atol": "std::stol", "atoll": "std::stoll", "atof": "std::stod"}
)

// checkInsecureFunction flags calls to gets, strcpy, strcat, sprintf and
// vsprintf (Sonar CFamily S1081).
func checkInsecureFunction(s *source) []finding {
	return s.libraryCalls(func(name string) string {
		if r, ok := insecureFunctions[name]; ok {
			return "`" + name + "` -> `" + r + "`"
		}
		return ""
	})
}

// checkAtoi flags calls to atoi, atol, atoll and atof (Sonar CFamily S989).
func checkAtoi(s *source) []finding {
	repl := atoiC
	if s.f.Lang == code.CPP {
		repl = atoiCPP
	}
	return s.libraryCalls(func(name string) string {
		if r, ok := repl[name]; ok {
			return "`" + name + "` -> `" + r + "`"
		}
		return ""
	})
}

// libraryCalls returns a finding for each call to a C library function in
// detail, plain or qualified with `::` or `std::`. We skip member calls and
// calls to functions or macros the file declares itself.
func (s *source) libraryCalls(detail func(name string) string) []finding {
	var out []finding
	var own map[string]bool
	for _, r := range s.f.Refs {
		if !r.Call {
			continue
		}
		d := detail(r.Name)
		if d == "" {
			continue
		}
		if own == nil {
			own = s.ownFunctions()
		}
		if own[r.Name] {
			continue
		}
		i := s.codeIndex(r.Span.Start)
		start, ok := s.libraryCallee(i)
		if !ok || s.tok(i+1) != "(" {
			continue
		}
		end := s.matchClose(s.code, i+1)
		if end >= len(s.code) {
			continue
		}
		out = append(out, finding{span: s.tokSpan(start, end+1), detail: d})
	}
	return out
}

// ownFunctions returns the functions and macros the file declares. These
// shadow library functions with the same name.
func (s *source) ownFunctions() map[string]bool {
	own := map[string]bool{}
	for _, d := range s.f.Decls {
		if d.Callable() || d.Kind == code.KindMacro {
			own[d.Name] = true
		}
	}
	return own
}

// libraryCallee reports whether the name at code token i is a free function,
// global or in std. If so, it also returns the index where the qualified
// name starts.
func (s *source) libraryCallee(i int) (int, bool) {
	switch s.tok(i - 1) {
	case ".", "->":
		return i, false
	case "::":
		if s.tok(i-2) == "std" && s.tok(i-3) != "::" {
			return i - 2, true
		}
		if k := i - 2; k < 0 || s.code[k].Kind != code.TokIdent {
			return i - 1, true
		}
		return i, false
	}
	return i, true
}

// checkVoidParameterList flags C function declarations and definitions that
// have an empty parameter list `f()` (Sonar CFamily S929).
func checkVoidParameterList(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if d.Kind != code.KindFunction {
			continue
		}
		i := s.codeIndex(d.NameSpan.Start)
		if s.tok(i) != d.Name || !s.seq(i+1, "(", ")") {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, i+3),
			detail: "`" + d.Name + "()` -> `" + d.Name + "(void)`",
		})
	}
	return out
}

// sharedWithC reports whether the file tests __cplusplus. That means the code
// has to compile as C too, so we can't suggest C++-only replacements.
func (s *source) sharedWithC() bool {
	for _, t := range s.code {
		if t.Kind == code.TokIdent && s.text(t) == "__cplusplus" {
			return true
		}
	}
	return false
}

// directiveEnd returns the index past the preprocessor directive that starts
// at code token i (the `#`). A directive runs to the end of the line, or
// further if the line ends in a backslash.
func (s *source) directiveEnd(i int) int {
	j := i + 1
	for j < len(s.code) {
		prev, next := s.code[j-1].Span.End, s.code[j].Span.Start
		if s.line(prev) != s.line(next) && bytes.IndexByte(s.f.Src[prev:next], '\\') < 0 {
			break
		}
		j++
	}
	return j
}

func (s *source) directive(i int) (string, bool) {
	if s.tok(i) != "#" || !s.firstOnLine(i) {
		return "", false
	}
	return s.tok(i + 1), true
}

// checkNullptr flags NULL in C++ code (Sonar CFamily S4962).
func checkNullptr(s *source) []finding {
	if s.sharedWithC() {
		return nil
	}
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokIdent || s.text(t) != "NULL" {
			continue
		}
		switch s.tok(i - 1) {
		case ".", "->", "::", "define", "ifdef", "ifndef", "undef", "defined":
			continue
		}
		out = append(out, finding{span: t.Span, detail: "`NULL` -> `nullptr`"})
	}
	return out
}

// A declaration can start after these tokens.
var typedefStarts = setOf("", ";", "{", "}", ":")

// checkTypedef flags typedef declarations in C++ (Sonar CFamily S5416).
func checkTypedef(s *source) []finding {
	if s.sharedWithC() {
		return nil
	}
	var out []finding
	for i := 0; i < len(s.code); i++ {
		if s.tok(i) != "typedef" || s.code[i].Kind != code.TokKeyword || !typedefStarts[s.tok(i-1)] && !s.firstOnLine(i) {
			continue
		}
		end, simple := i+1, true
		for end < len(s.code) && s.tok(end) != ";" {
			switch s.tok(end) {
			case "(", "[", "{":
				simple = false
				end = s.matchClose(s.code, end)
			case ",", "<":
				simple = false
			}
			end++
		}
		if end >= len(s.code) {
			break
		}
		name := end - 1
		detail := "`typedef` -> `using`"
		if simple && name > i+1 && s.code[name].Kind == code.TokIdent {
			detail = "`" + s.codeText(i, end+1) + "` -> `using " + s.tok(name) + " = " + s.codeText(i+1, name) + ";`"
		}
		out = append(out, finding{span: s.tokSpan(i, end+1), detail: detail})
		i = end
	}
	return out
}

// macroUses returns the macro names that a directive other than #define
// mentions, plus the ones defined inside a conditional block or more than
// once. Those configure the build, so they have to stay macros.
func (s *source) macroUses() map[string]bool {
	uses, defined := map[string]bool{}, map[string]bool{}
	depth := 0
	for i := 0; i < len(s.code); i++ {
		name, ok := s.directive(i)
		if !ok {
			continue
		}
		end := s.directiveEnd(i)
		switch name {
		case "define":
			if n := s.tok(i + 2); depth > 0 || defined[n] {
				uses[n] = true
			} else {
				defined[n] = true
			}
		case "if", "ifdef", "ifndef":
			depth++
			fallthrough
		default:
			if name == "endif" {
				depth--
			}
			for k := i + 2; k < end; k++ {
				if s.code[k].Kind == code.TokIdent {
					uses[s.tok(k)] = true
				}
			}
		}
		i = end - 1
	}
	return uses
}

// checkMacroConstant flags object-like macros whose value is a literal or an
// expression of literals (Sonar CFamily S5028). We leave a macro alone if
// another directive mentions it, if it's a config macro defined in a
// conditional block, or if it's pasted next to a string literal, since a
// constant can't replace it there.
func checkMacroConstant(s *source) []finding {
	if s.sharedWithC() {
		return nil
	}
	var uses map[string]bool
	var out []finding
	for i := 0; i < len(s.code); i++ {
		name, ok := s.directive(i)
		if !ok {
			continue
		}
		end := s.directiveEnd(i)
		n := i + 2
		if name != "define" || n+1 >= end || s.code[n].Kind != code.TokIdent ||
			s.code[n+1].Span.Start == s.code[n].Span.End && s.tok(n+1) == "(" || !s.literalExpr(n+1, end) {
			i = end - 1
			continue
		}
		if uses == nil {
			uses = s.macroUses()
		}
		if !uses[s.tok(n)] && !s.pastedToString(s.tok(n)) {
			out = append(out, finding{
				span:   s.tokSpan(i, end),
				detail: "`" + s.codeText(i, end) + "` -> `constexpr auto " + s.tok(n) + " = " + s.codeText(n+1, end) + ";`",
			})
		}
		i = end - 1
	}
	return out
}

var literalOps = setOf("+", "-", "*", "/", "%", "<<", ">>", "|", "&", "^", "~", "(", ")")

// literalExpr reports whether the code tokens [i, end) form an expression
// made only of number, string, character or boolean literals.
func (s *source) literalExpr(i, end int) bool {
	lit := false
	for k := i; k < end; k++ {
		t := s.code[k]
		switch {
		case t.Kind == code.TokNumber || t.Kind == code.TokString || s.isKeyword(t, "true", "false"):
			lit = true
		case t.Kind == code.TokPunct && literalOps[s.text(t)]:
		default:
			return false
		}
	}
	return lit
}

// pastedToString reports whether the macro name appears next to a string
// literal, as in `"v" VERSION`. Only the preprocessor can join those.
func (s *source) pastedToString(name string) bool {
	for i, t := range s.code {
		if t.Kind == code.TokIdent && s.text(t) == name && s.tok(i-1) != "define" &&
			(i > 0 && s.code[i-1].Kind == code.TokString || i+1 < len(s.code) && s.code[i+1].Kind == code.TokString) {
			return true
		}
	}
	return false
}

// checkStdEndl flags `<< std::endl`, and `<< endl` if a using directive or
// declaration brings it in (Sonar CFamily S5946).
func checkStdEndl(s *source) []finding {
	var out []finding
	bare := false
	for i := range s.code {
		if s.seq(i, "using", "namespace", "std", ";") || s.seq(i, "using", "std", "::", "endl", ";") {
			bare = true
		}
		if s.tok(i) != "<<" {
			continue
		}
		switch {
		case s.seq(i+1, "std", "::", "endl"):
			out = append(out, finding{span: s.tokSpan(i+1, i+4), detail: "`std::endl` -> `'\\n'`"})
		case bare && s.tok(i+1) == "endl" && s.tok(i+2) != "::" && s.tok(i+2) != "(":
			out = append(out, finding{span: s.code[i+1].Span, detail: "`endl` -> `'\\n'`"})
		}
	}
	return out
}

// checkMakeUnique flags `std::unique_ptr<T>(new T(...))`, the declaration
// `std::unique_ptr<T> p(new T(...))`, the shared_ptr forms of both, and
// `p.reset(new T(...))` in files that use std smart pointers (Sonar CFamily
// S5950). Like clang-tidy's modernize-make-unique, we skip `new T` without
// parentheses (it default-initializes, make_unique value-initializes),
// brace and array new, and placement new or custom deleters.
func checkMakeUnique(s *source) []finding {
	var out []finding
	smart := false
	for _, t := range s.code {
		if text := s.text(t); text == "unique_ptr" || text == "shared_ptr" {
			smart = true
			break
		}
	}
	for i := 0; i < len(s.code); i++ {
		ptr := s.tok(i)
		if ptr != "unique_ptr" && ptr != "shared_ptr" && ptr != "reset" {
			continue
		}
		start := i
		if s.seq(i-2, "std", "::") {
			start = i - 2
		}
		if s.tok(start-1) == "::" {
			continue
		}
		open, varName := -1, ""
		if ptr == "reset" {
			// If the file has no std smart pointer, reset might belong to
			// some other owner type like QScopedPointer.
			if p := s.tok(i - 1); smart && (p == "." || p == "->") && s.tok(i+1) == "(" {
				open = i + 1
			}
		} else if s.tok(i+1) == "<" {
			gt := s.angleClose(i + 1)
			switch {
			case gt < 0:
			case s.tok(gt+1) == "(":
				open = gt + 1
			case s.code[gt+1].Kind == code.TokIdent && s.tok(gt+2) == "(":
				open, varName = gt+2, s.tok(gt+1)
			}
		}
		if open < 0 || s.tok(open+1) != "new" {
			continue
		}
		typ, args, ok := s.newExpr(open + 1)
		if !ok {
			continue
		}
		maker := "std::make_unique"
		if ptr == "shared_ptr" {
			maker = "std::make_shared"
		}
		cl := s.matchClose(s.code, open)
		made := maker + "<" + typ + ">(" + args + ")"
		var detail string
		switch {
		case ptr == "reset":
			if start = s.chainStart(i - 1); start == i-1 {
				continue
			}
			detail = "`" + s.codeText(start, cl+1) + "` -> `" + s.codeText(start, i-1) + " = " + made + "` (`std::make_shared` for a shared_ptr)"
		case varName != "":
			detail = "`" + s.codeText(start, cl+1) + "` -> `auto " + varName + " = " + made + "`"
		default:
			detail = "`" + s.codeText(start, cl+1) + "` -> `" + made + "`"
		}
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: detail})
		i = cl
	}
	return out
}

func (s *source) angleClose(i int) int {
	depth := 0
	for j := i; j < len(s.code); j++ {
		switch s.tok(j) {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		case ";", "{", "}", "(", ")":
			return -1
		}
		if depth == 0 {
			return j
		}
		if depth < 0 {
			return -1
		}
	}
	return -1
}

// newExpr reads `new T(args)` at code token i if it's the only argument of
// the surrounding call, and returns T and the argument text.
func (s *source) newExpr(i int) (typ, args string, ok bool) {
	j := i + 1
	for j < len(s.code) && (s.code[j].Kind == code.TokIdent || s.code[j].Kind == code.TokKeyword || s.tok(j) == "::") {
		j++
	}
	if s.tok(j) == "<" {
		if j = s.angleClose(j); j < 0 {
			return "", "", false
		}
		j++
	}
	if j == i+1 || s.tok(j) != "(" {
		return "", "", false
	}
	cl := s.matchClose(s.code, j)
	if cl >= len(s.code) || s.tok(cl+1) != ")" {
		return "", "", false
	}
	if cl > j+1 {
		args = s.codeText(j+1, cl)
	}
	return s.codeText(i+1, j), args, true
}

var cMemoryFunctions = map[string]string{
	"malloc": "`std::vector` or `std::make_unique`", "calloc": "`std::vector`", "realloc": "`std::vector`", "free": "an owning container",
}

// checkManualMemory flags delete expressions and calls to malloc, calloc,
// realloc and free in C++ (Sonar CFamily S5025 and S1231). We skip deleted
// functions (`= delete`) and operator delete declarations. We also skip the
// C allocation calls in files with an `extern "C"` API, because their
// callers release the memory with free.
func checkManualMemory(s *source) []finding {
	if s.sharedWithC() {
		return nil
	}
	var out []finding
	if !s.externC() {
		out = s.libraryCalls(func(name string) string {
			if r, ok := cMemoryFunctions[name]; ok {
				return "`" + name + "` -> " + r
			}
			return ""
		})
	}
	for i, t := range s.code {
		if t.Kind != code.TokKeyword || s.text(t) != "delete" {
			continue
		}
		if p := s.tok(i - 1); p == "=" || p == "operator" || p == "::" && s.tok(i-2) == "operator" {
			continue
		}
		end := i + 1
		if s.seq(i+1, "[", "]") {
			end = i + 3
		}
		out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "` -> a smart pointer or container"})
	}
	return out
}

// externC reports whether the file implements a C API in an `extern "C"`
// block. A single `extern "C"` declaration only imports a C function, so it
// doesn't count.
func (s *source) externC() bool {
	for i, t := range s.code {
		if t.Kind == code.TokString && s.text(t) == `"C"` && s.tok(i-1) == "extern" && s.tok(i+1) == "{" {
			return true
		}
	}
	return false
}

var (
	builtinTypes = setOf("void", "bool", "char", "short", "int", "long", "float", "double", "signed", "unsigned",
		"wchar_t", "char8_t", "char16_t", "char32_t", "const", "volatile", "auto")
	// Tokens that can start a cast operand.
	castOperandStarts = setOf("(", "-", "+", "!", "~", "*", "&", "++", "--")
	// After these keywords, `(` starts an expression.
	castPrev = setOf("return", "throw", "case", "co_return", "co_yield")
)

// checkCStyleCast flags C-style casts `(T)x` to a builtin type, a `_t` type
// or a pointer type (Sonar CFamily S871). We skip `(void)x`, sizeof,
// alignof, decltype and parameter lists.
func checkCStyleCast(s *source) []finding {
	if s.sharedWithC() {
		return nil
	}
	var out []finding
	for i := range s.code {
		if s.tok(i) != "(" || !s.castContext(i) {
			continue
		}
		cl, pointer, typeName := s.castType(i)
		if cl < 0 || s.codeText(i+1, cl) == "void" {
			continue
		}
		next := cl + 1
		if next >= len(s.code) {
			continue
		}
		nt := s.code[next]
		switch {
		case nt.Kind == code.TokIdent || nt.Kind == code.TokNumber || nt.Kind == code.TokString:
		case s.isKeyword(nt, "this", "true", "false", "nullptr", "sizeof"):
		case nt.Kind == code.TokPunct && castOperandStarts[s.text(nt)]:
			// `(n_t) - 1` is a subtraction from a variable named like a type.
			if !pointer && !typeName && s.text(nt) != "(" {
				continue
			}
		default:
			continue
		}
		end := s.castOperandEnd(next)
		typ := s.codeText(i+1, cl)
		detail := "`(" + typ + ")` -> `static_cast<" + typ + ">`"
		if end > next {
			detail = "`" + s.codeText(i, end) + "` -> `static_cast<" + typ + ">(" + s.codeText(next, end) + ")`"
		} else {
			end = cl + 1
		}
		if pointer {
			detail += ", or `reinterpret_cast` between unrelated types"
		}
		out = append(out, finding{span: s.tokSpan(i, end), detail: detail})
	}
	return out
}

// castContext reports whether the `(` at code token i starts an expression.
// That's the case if it follows a punctuator other than a closing bracket, or
// a keyword like return. `operator++(int)` doesn't count, since it declares
// a postfix operator.
func (s *source) castContext(i int) bool {
	if i == 0 {
		return false
	}
	p := s.code[i-1]
	if p.Kind == code.TokPunct {
		switch s.text(p) {
		case ")", "]", ">", "}", "#", ">>":
			return false
		}
		return s.tok(i-2) != "operator"
	}
	return p.Kind == code.TokKeyword && castPrev[s.text(p)]
}

// castType reads the type in the parentheses that open at code token i:
// builtin keywords, a name ending in `_t`, or any name followed by `*`. It
// returns the index of the closing parenthesis (-1 if the parentheses don't
// hold such a type), whether the type is a pointer, and whether it's all
// keywords.
func (s *source) castType(i int) (int, bool, bool) {
	j := i + 1
	keywords, named, last := true, false, ""
scan:
	for j < len(s.code) {
		t := s.code[j]
		text := s.text(t)
		switch {
		case t.Kind == code.TokKeyword && builtinTypes[text]:
		case t.Kind == code.TokIdent && !named:
			named, keywords, last = true, false, text
		case text == "::" && named && s.code[j+1].Kind == code.TokIdent:
			j++
			last = s.tok(j)
		default:
			break scan
		}
		j++
	}
	if j == i+1 || j >= len(s.code) {
		return -1, false, false
	}
	pointer := false
	for s.tok(j) == "*" || s.tok(j) == "const" && pointer {
		pointer = true
		j++
	}
	if s.tok(j) != ")" {
		return -1, false, false
	}
	if named && !pointer && !strings.HasSuffix(last, "_t") {
		return -1, false, false
	}
	return j, pointer, keywords
}

// castOperandEnd returns the index after the unary operand that starts at
// code token i, or i if the operand isn't a single name chain or literal
// or parenthesized expression.
func (s *source) castOperandEnd(i int) int {
	t := s.code[i]
	switch {
	case t.Kind == code.TokIdent || s.isKeyword(t, "this"):
		return s.chainEnd(i)
	case t.Kind == code.TokNumber || t.Kind == code.TokString:
		return i + 1
	case s.text(t) == "(":
		if k := s.matchClose(s.code, i); k < len(s.code) {
			return k + 1
		}
	}
	return i
}

// A comparison operand can start after these tokens.
var sizeEmptyStarts = setOf("", "(", "[", "{", ",", ";", "=", "&&", "||", "?", ":", "return")

// These tokens can follow a comparison operand.
var sizeEmptyEnds = setOf("", ")", "]", "}", ",", ";", "&&", "||", "?", ":")

// sizeComparisons maps a comparison with size() on the left and a literal on
// the right to true if it tests for emptiness, false if it tests for content.
var sizeComparisons = map[string]bool{"== 0": true, "< 1": true, "!= 0": false, "> 0": false, ">= 1": false}

// sizeComparisonsRight is the same with the literal first.
var sizeComparisonsRight = map[string]bool{"0 ==": true, "1 >": true, "0 !=": false, "0 <": false, "1 <=": false}

// checkSizeEmpty flags `x.size() == 0` and the other comparisons of size()
// with zero (Sonar CFamily S1155). We only count receivers the file declares
// with a standard container type, because other types like fmt::string_view
// might not have empty(). We also skip comparisons inside a definition of
// empty().
func checkSizeEmpty(s *source) []finding {
	var out []finding
	for i := 0; i < len(s.code); i++ {
		if !sizeEmptyStarts[s.tok(i-1)] {
			continue
		}
		var start, end, chain, chainEnd int
		var empty, ok bool
		if t := s.code[i].Kind; t == code.TokNumber {
			empty, ok = sizeComparisonsRight[s.tok(i)+" "+s.tok(i+1)]
			start, chain = i, i+2
			chainEnd = s.chainEnd(chain)
			end = chainEnd
		} else {
			start, chain = i, i
			chainEnd = s.chainEnd(i)
			empty, ok = sizeComparisons[s.tok(chainEnd)+" "+s.tok(chainEnd+1)]
			end = chainEnd + 2
		}
		if !ok || chainEnd-chain < 5 || !sizeEmptyEnds[s.tok(end)] {
			continue
		}
		if acc := s.tok(chainEnd - 4); (acc != "." && acc != "->") || !s.seq(chainEnd-3, "size", "(", ")") {
			continue
		}
		if !s.stdContainer(s.tok(chainEnd-5), s.code[i].Span.Start, chainEnd-5 > chain) {
			continue
		}
		if d := s.scopeOf(s.code[i].Span.Start); d.Name == "empty" {
			continue
		}
		repl := s.codeText(chain, chainEnd-3) + "empty()"
		if !empty {
			repl = "!" + repl
		}
		out = append(out, finding{
			span:   s.tokSpan(start, end),
			detail: "`" + s.codeText(start, end) + "` -> `" + repl + "`",
		})
		i = end - 1
	}
	return out
}

var stdContainers = []string{
	"std::vector<", "std::string", "std::wstring", "std::u8string", "std::u16string", "std::u32string", "std::basic_string",
	"std::map<", "std::set<", "std::multimap<", "std::multiset<", "std::unordered_", "std::deque<", "std::list<",
	"std::forward_list<", "std::array<", "std::span<",
}

// stdContainer reports whether name, used at offset off, is declared with a
// standard container or string type. We go by the innermost local or
// parameter of that name whose function contains off. If there isn't one,
// all non-local declarations of the name have to match, and there has to be
// at least one. A member name (member set) only matches non-local
// declarations.
func (s *source) stdContainer(name string, off int, member bool) bool {
	var local *code.Decl
	var others []*code.Decl
	for _, d := range s.f.Decls {
		if d.Name != name {
			continue
		}
		if !d.Local {
			others = append(others, d)
		} else if !member && d.Parent != nil && d.Parent.Span.Contains(off) && (local == nil || d.Parent.Span.Start >= local.Parent.Span.Start) {
			local = d
		}
	}
	if local != nil {
		return stdContainerType(local.Type)
	}
	for _, d := range others {
		if !stdContainerType(d.Type) {
			return false
		}
	}
	return len(others) > 0
}

func stdContainerType(typ string) bool {
	typ = strings.TrimPrefix(strings.TrimPrefix(typ, "const "), "::")
	for _, c := range stdContainers {
		if strings.HasPrefix(typ, c) {
			return true
		}
	}
	return false
}

var headerExts = setOf(".h", ".hh", ".hpp", ".hxx")

// checkUsingNamespaceInHeader flags `using namespace` at file or namespace
// scope in a C++ header (Sonar CFamily S1003). Like clang-tidy's
// google-build-using-namespace, we allow literals namespaces such as
// std::literals::chrono_literals.
func checkUsingNamespaceInHeader(s *source) []finding {
	if !headerExts[strings.ToLower(filepath.Ext(s.f.Path))] {
		return nil
	}
	var out []finding
	for i := range s.code {
		if !s.seq(i, "using", "namespace") || !s.namespaceScope(i) {
			continue
		}
		end := i + 2
		for end < len(s.code) && s.tok(end) != ";" {
			end++
		}
		if end >= len(s.code) || strings.HasSuffix(s.tok(end-1), "literals") {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, end+1),
			detail: "`" + s.codeText(i, end+1) + "` -> qualified names",
		})
	}
	return out
}

// namespaceScope reports whether code token i isn't inside any braces except
// namespace and extern "C" bodies.
func (s *source) namespaceScope(i int) bool {
	for j := range s.enclosingBraces(i) {
		k := j - 1
		if s.code[k].Kind == code.TokString && s.tok(k-1) == "extern" {
			continue
		}
		for k >= 0 && (s.code[k].Kind == code.TokIdent || s.tok(k) == "::") {
			k--
		}
		if k < 0 || s.tok(k) != "namespace" {
			return false
		}
	}
	return true
}
