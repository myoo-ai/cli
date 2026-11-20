package codelint

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/myoo-ai/cli/internal/code"
)

var langGo = []code.Lang{code.Go}

var goRules = []*Rule{
	{
		ID:             "go-deprecated-ioutil",
		Langs:          langGo,
		Description:    "prefer the io and os functions instead of the deprecated io/ioutil package, which only wraps them since Go 1.16.",
		Recommendation: "call the replacement shown in the detail. `os.ReadDir` returns `[]fs.DirEntry` sorted by name instead of `[]fs.FileInfo`, so call `Info()` on an entry where the size or mode is needed.",
		check:          checkGoIoutil,
	},
	{
		ID:             "go-interface-any",
		Langs:          langGo,
		Description:    "prefer using `any` instead of `interface{}`, as it's shorter and easier to read.",
		Recommendation: "replace `interface{}` with `any`, the alias predeclared since Go 1.18. both name the same type, so nothing else changes.",
		check:          checkGoInterfaceAny,
	},
	{
		ID:             "go-errors-new-sprintf",
		Langs:          langGo,
		Description:    "prefer using `fmt.Errorf` instead of `errors.New(fmt.Sprintf(...))`, as it formats the message in one call.",
		Recommendation: "pass the format and its arguments to `fmt.Errorf` directly. it also lets `%w` wrap an error that callers can match with `errors.Is`.",
		check:          checkGoErrorsNewSprintf,
	},
	{
		ID:             "go-error-string-format",
		Langs:          langGo,
		Description:    "error strings should start lowercase and end without punctuation, as callers usually print them inside other messages.",
		Recommendation: "lowercase the first word and drop the trailing punctuation or newline, for example `errors.New(\"Bad input.\")` becomes `errors.New(\"bad input\")`. keep the capital of a proper noun.",
		check:          checkGoErrorString,
	},
	{
		ID:             "go-time-since",
		Langs:          langGo,
		Description:    "prefer using `time.Since` or `time.Until` instead of subtracting `time.Now()`, as they state the intent directly.",
		Recommendation: "write `time.Since(t)` for `time.Now().Sub(t)` and `time.Until(t)` for `t.Sub(time.Now())`. the results are the same.",
		check:          checkGoTimeSince,
	},
	{
		ID:             "go-strings-contains",
		Langs:          langGo,
		Description:    "prefer using `strings.Contains` instead of comparing the result of `strings.Index`, as it says what is tested.",
		Recommendation: "call the `Contains` function of the same package that matches the `Index` function, negated where the code tests for a missing match.",
		check:          checkGoContains,
	},
	{
		ID:             "go-bytes-equal",
		Langs:          langGo,
		Description:    "prefer using `bytes.Equal` instead of `bytes.Compare` for equality checks, as it is faster and easier to read.",
		Recommendation: "replace `bytes.Compare(a, b) == 0` with `bytes.Equal(a, b)` and `!= 0` with `!bytes.Equal(a, b)`. both treat a nil slice as equal to an empty one.",
		check:          checkGoBytesEqual,
	},
	{
		ID:             "go-strings-equal-fold",
		Langs:          langGo,
		Description:    "prefer using `strings.EqualFold` instead of comparing case-converted strings, as it compares without allocating 2 new strings.",
		Recommendation: "replace `strings.ToLower(a) == strings.ToLower(b)` with `strings.EqualFold(a, b)`. it applies Unicode case folding, which can match a few characters that the converted strings keep apart.",
		check:          checkGoEqualFold,
	},
	{
		ID:             "go-defer-in-loop",
		Langs:          langGo,
		Description:    "deferred calls in a loop should be avoided, as they run when the function returns instead of at the end of each iteration.",
		Recommendation: "move the loop body into a function that defers the call, or release the resource explicitly at the end of the iteration. otherwise every iteration holds its resource until the whole loop finishes.",
		check:          checkGoDeferInLoop,
	},
	{
		ID:             "go-range-over-int",
		Langs:          langGo,
		Description:    "prefer using `for i := range n` instead of a three-clause loop counting from zero, as it's more concise and easier to read.",
		Recommendation: "range over the integer limit, available since Go 1.22, and drop the index when the body does not use it. `i` takes the type of the limit, so write `range int(n)` where the limit is not an int and the body needs one.",
		check:          checkGoRangeInt,
	},
	{
		ID:             "go-redundant-blank-range",
		Langs:          langGo,
		Description:    "prefer omitting blank identifiers at the end of a range clause, as they add nothing.",
		Recommendation: "write `for x := range s` instead of `for x, _ := range s` and `for range s` instead of `for _ = range s`. `gofmt -s` applies this rewrite.",
		check:          checkGoBlankRange,
	},
	{
		ID:             "go-context-cancel-not-deferred",
		Langs:          langGo,
		Description:    "context cancellation functions should be deferred, as the context and its timer leak until the parent context ends.",
		Recommendation: "add `defer cancel()` right after creating the context. a single `cancel()` call at the end misses early returns and panics, and calling `cancel` twice is safe.",
		check:          checkGoContextCancel,
	},
}

var (
	// If a comparison operand follows one of these tokens, it starts an
	// expression, so the comparison applies to the call alone.
	goExprStarts = setOf("", "(", "[", "{", ",", ";", "=", ":=", "&&", "||", "return", "if", "for", "case", ":")
	// These tokens can follow a comparison without an operator that binds
	// tighter than it.
	goExprEnds = setOf("", ")", "]", "}", ",", ";", "{", "&&", "||", ":")
)

// goImports maps the local names of the file's imports to their paths, like
// "ioutil" or an alias to "io/ioutil". We leave out blank and dot imports.
func (s *source) goImports() map[string]string {
	out := map[string]string{}
	for i := 0; i < len(s.code); i++ {
		if !s.isKeyword(s.code[i], "import") {
			continue
		}
		end := i + 2
		if s.tok(i+1) == "(" {
			end = s.matchClose(s.code, i+1)
		}
		for j := i + 1; j < end && j < len(s.code); j++ {
			if s.code[j].Kind != code.TokString {
				continue
			}
			p, err := strconv.Unquote(s.tok(j))
			if err != nil {
				continue
			}
			name := path.Base(p)
			if strings.HasPrefix(name, "v") && strings.Contains(p, "/") {
				if _, err := strconv.Atoi(name[1:]); err == nil {
					name = path.Base(path.Dir(p))
				}
			}
			if s.code[j-1].Kind == code.TokIdent && j-1 > i {
				name = s.tok(j - 1)
			}
			if name != "_" && name != "." {
				out[name] = p
			}
		}
		i = end
	}
	return out
}

// goPkg reports whether code token i names the package imported from pth
// and starts a selector, like the `strings` in `strings.Index`.
func (s *source) goPkg(imports map[string]string, i int, pth string) bool {
	return s.tok(i+1) == "." && s.tok(i-1) != "." && s.code[i].Kind == code.TokIdent && imports[s.tok(i)] == pth
}

var goModMinor sync.Map // directory -> minor Go version of its go.mod, 0 when unknown

// goMinor returns the minor version of the `go` directive in the go.mod that
// governs the file, or 0 if we can't find a go.mod with a go directive.
func (s *source) goMinor() int {
	dir := filepath.Dir(s.f.Abs)
	if v, ok := goModMinor.Load(dir); ok {
		return v.(int)
	}
	minor := 0
	for d := dir; ; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for line := range strings.SplitSeq(string(data), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "go 1."); ok {
					v, _, _ = strings.Cut(v, ".")
					minor, _ = strconv.Atoi(v)
				}
			}
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	goModMinor.Store(dir, minor)
	return minor
}

// goSupports reports whether the file's module can use a feature added in
// Go 1.minor. For files outside a module we assume a recent Go.
func (s *source) goSupports(minor int) bool {
	v := s.goMinor()
	return v == 0 || v >= minor
}

var ioutilReplacements = map[string]string{
	"ReadAll":   "io.ReadAll",
	"ReadFile":  "os.ReadFile",
	"WriteFile": "os.WriteFile",
	"ReadDir":   "os.ReadDir",
	"TempFile":  "os.CreateTemp",
	"TempDir":   "os.MkdirTemp",
	"NopCloser": "io.NopCloser",
	"Discard":   "io.Discard",
}

// checkGoIoutil flags uses of the io/ioutil package, deprecated since Go
// 1.16 (staticcheck SA1019).
func checkGoIoutil(s *source) []finding {
	if !s.goSupports(16) {
		return nil
	}
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if !s.goPkg(imports, i, "io/ioutil") {
			continue
		}
		repl, ok := ioutilReplacements[s.tok(i+2)]
		if !ok {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, i+3),
			detail: "`" + s.codeText(i, i+3) + "` -> `" + repl + "`",
		})
	}
	return out
}

// checkGoInterfaceAny flags `interface{}`, which gopls' efaceany modernizer
// rewrites to `any`.
func checkGoInterfaceAny(s *source) []finding {
	if !s.goSupports(18) {
		return nil
	}
	for _, d := range s.f.Decls {
		if d.Name == "any" {
			return nil
		}
	}
	var out []finding
	for i := range s.code {
		if s.isKeyword(s.code[i], "interface") && s.seq(i+1, "{", "}") {
			out = append(out, finding{span: s.tokSpan(i, i+3), detail: "`interface{}` -> `any`"})
		}
	}
	return out
}

// checkGoErrorsNewSprintf flags `errors.New(fmt.Sprintf(...))` (staticcheck
// S1028).
func checkGoErrorsNewSprintf(s *source) []finding {
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if !s.goPkg(imports, i, "errors") || !s.seq(i+1, ".", "New", "(") || !s.goPkg(imports, i+4, "fmt") || !s.seq(i+5, ".", "Sprintf", "(") {
			continue
		}
		inner := s.matchClose(s.code, i+7)
		if inner >= len(s.code) || s.tok(inner+1) != ")" {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, inner+2),
			detail: "`" + s.codeText(i, inner+2) + "` -> `" + s.codeText(i+4, i+5) + ".Errorf(" + s.codeText(i+8, inner) + ")`",
		})
	}
	return out
}

// checkGoErrorString flags error strings that start with a capitalized word
// or end with punctuation or a newline (staticcheck ST1005). Like ST1005, we
// accept a first word with more upper-case letters or digits (an initialism
// or a Go name) or with no lower-case letters (`C`), a call like `Open()`,
// and a capital in a single-word message. ST1005 accepts the package's
// names. We go further and accept any identifier in the file, since
// `Get: %v` names the call that failed.
func checkGoErrorString(s *source) []finding {
	imports := s.goImports()
	idents := map[string]bool{}
	for _, t := range s.code {
		if t.Kind == code.TokIdent {
			idents[s.text(t)] = true
		}
	}
	var out []finding
	for i := range s.code {
		if !(s.goPkg(imports, i, "errors") && s.seq(i+1, ".", "New", "(")) && !(s.goPkg(imports, i, "fmt") && s.seq(i+1, ".", "Errorf", "(")) {
			continue
		}
		lit := i + 4
		if lit >= len(s.code) || s.code[lit].Kind != code.TokString || (s.tok(lit+1) != ")" && s.tok(lit+1) != ",") {
			continue
		}
		raw := s.tok(lit)
		text, err := strconv.Unquote(raw)
		if err != nil || text == "" {
			continue
		}
		fixed := strings.TrimRight(text, ".:!\n")
		if word, _, ok := strings.Cut(fixed, " "); ok && capitalized(word) && !idents[strings.TrimRightFunc(word, unicode.IsPunct)] {
			r, n := utf8.DecodeRuneInString(fixed)
			fixed = string(unicode.ToLower(r)) + fixed[n:]
		}
		if fixed == text {
			continue
		}
		quoted := strconv.Quote(fixed)
		if raw[0] == '`' {
			quoted = "`" + fixed + "`"
		}
		out = append(out, finding{span: s.tokSpan(lit, lit+1), detail: "`" + raw + "` -> `" + quoted + "`"})
	}
	return out
}

// capitalized reports whether word is an upper-case letter followed by
// lower-case letters, with no other upper-case letter, digit or parenthesis.
func capitalized(word string) bool {
	r, n := utf8.DecodeRuneInString(word)
	if !unicode.IsUpper(r) {
		return false
	}
	lower := false
	for _, c := range word[n:] {
		if unicode.IsUpper(c) || unicode.IsDigit(c) || c == '(' {
			return false
		}
		lower = lower || unicode.IsLower(c)
	}
	return lower
}

// checkGoTimeSince flags `time.Now().Sub(t)` and `t.Sub(time.Now())`
// (staticcheck S1012 and S1024).
func checkGoTimeSince(s *source) []finding {
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if !s.goPkg(imports, i, "time") || !s.seq(i+1, ".", "Now", "(", ")") {
			continue
		}
		if s.seq(i+5, ".", "Sub", "(") {
			end := s.matchClose(s.code, i+7)
			if end >= len(s.code) || end == i+8 {
				continue
			}
			out = append(out, finding{
				span:   s.tokSpan(i, end+1),
				detail: "`" + s.codeText(i, end+1) + "` -> `" + s.tok(i) + ".Since(" + s.codeText(i+8, end) + ")`",
			})
			continue
		}
		if !s.seq(i-3, ".", "Sub", "(") || s.tok(i+5) != ")" {
			continue
		}
		start := s.chainStart(i - 3)
		if start == i-3 || s.tok(start-1) == "*" || s.tok(start-1) == "&" {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(start, i+6),
			detail: "`" + s.codeText(start, i+6) + "` -> `" + s.tok(i) + ".Until(" + s.codeText(start, i-3) + ")`",
		})
	}
	return out
}

var containsOf = map[string]string{"Index": "Contains", "IndexAny": "ContainsAny", "IndexRune": "ContainsRune"}

// checkGoContains flags `strings.Index(s, x) != -1` and the like
// (staticcheck S1003).
func checkGoContains(s *source) []finding {
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if !s.goPkg(imports, i, "strings") && !s.goPkg(imports, i, "bytes") {
			continue
		}
		fn, ok := containsOf[s.tok(i+2)]
		if !ok || s.tok(i+3) != "(" || !s.goExprStart(i) {
			continue
		}
		cl := s.matchClose(s.code, i+3)
		if cl >= len(s.code) {
			continue
		}
		var negate bool
		end := cl + 3
		switch {
		case s.seq(cl+1, "!=", "-", "1"), s.seq(cl+1, ">", "-", "1"):
			end++
		case s.seq(cl+1, "==", "-", "1"):
			negate, end = true, end+1
		case s.seq(cl+1, ">=", "0"):
		case s.seq(cl+1, "<", "0"):
			negate = true
		default:
			continue
		}
		if !s.goExprEnd(end) {
			continue
		}
		repl := s.tok(i) + "." + fn + "(" + s.codeText(i+4, cl) + ")"
		if negate {
			repl = "!" + repl
		}
		out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "` -> `" + repl + "`"})
	}
	return out
}

// goExprStart reports whether an operand that starts at code token i is the
// whole left operand of the comparison after it.
func (s *source) goExprStart(i int) bool {
	return goExprStarts[s.tok(i-1)] || isAssignOp(s.tok(i-1)) || s.firstOnLine(i)
}

// goExprEnd reports whether the comparison ending before code token i ends
// its operand, i.e. a goExprEnds token or a new line comes next.
func (s *source) goExprEnd(i int) bool {
	return i >= len(s.code) || goExprEnds[s.tok(i)] || s.line(s.code[i].Span.Start) > s.line(s.code[i-1].Span.End)
}

// checkGoBytesEqual flags `bytes.Compare(a, b) == 0` and `!= 0` (SonarGo
// S8197, staticcheck S1004).
func checkGoBytesEqual(s *source) []finding {
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if !s.goPkg(imports, i, "bytes") || !s.seq(i+1, ".", "Compare", "(") || !s.goExprStart(i) {
			continue
		}
		cl := s.matchClose(s.code, i+3)
		if cl >= len(s.code) || s.tok(cl+2) != "0" || (s.tok(cl+1) != "==" && s.tok(cl+1) != "!=") || !s.goExprEnd(cl+3) {
			continue
		}
		repl := s.tok(i) + ".Equal(" + s.codeText(i+4, cl) + ")"
		if s.tok(cl+1) == "!=" {
			repl = "!" + repl
		}
		out = append(out, finding{span: s.tokSpan(i, cl+3), detail: "`" + s.codeText(i, cl+3) + "` -> `" + repl + "`"})
	}
	return out
}

// checkGoEqualFold flags `strings.ToLower(a) == strings.ToLower(b)` plus the
// ToUpper and `!=` variants (staticcheck SA6005).
func checkGoEqualFold(s *source) []finding {
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if !s.goPkg(imports, i, "strings") || (s.tok(i+2) != "ToLower" && s.tok(i+2) != "ToUpper") || s.tok(i+3) != "(" || !s.goExprStart(i) {
			continue
		}
		cl := s.matchClose(s.code, i+3)
		op := s.tok(cl + 1)
		if cl >= len(s.code) || (op != "==" && op != "!=") || !s.goPkg(imports, cl+2, "strings") || !s.sameTokens(i+1, cl+3, 3) {
			continue
		}
		cl2 := s.matchClose(s.code, cl+5)
		if cl2 >= len(s.code) || !s.goExprEnd(cl2+1) {
			continue
		}
		repl := s.tok(i) + ".EqualFold(" + s.codeText(i+4, cl) + ", " + s.codeText(cl+6, cl2) + ")"
		if op == "!=" {
			repl = "!" + repl
		}
		out = append(out, finding{span: s.tokSpan(i, cl2+1), detail: "`" + s.codeText(i, cl2+1) + "` -> `" + repl + "`"})
	}
	return out
}

// checkGoDeferInLoop flags `defer` statements in the body of a for loop
// (revive's defer rule reports these as `loop`). We let a few cases through.
// A defer inside a function literal called in the loop runs once per call.
// A defer in a select case is fine because event loops usually take each
// case once. A defer followed in its block by a return or break can only run
// once. And a deferred function literal is usually there to restore state
// on purpose.
func checkGoDeferInLoop(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.isKeyword(s.code[i], "defer") || s.isKeyword(s.code[i+1], "func") || !s.goDeferLoops(i) {
			continue
		}
		end := s.lineEnd(i) + 1
		out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "` runs when the function returns"})
	}
	return out
}

// goDeferLoops reports whether the defer at code token i can run on each
// iteration of an enclosing for loop in the same function.
func (s *source) goDeferLoops(i int) bool {
	first := true
	for j := range s.enclosingBraces(i) {
		if s.funcBodies()[s.code[j].Span.Start] || s.lambdaBody(j) {
			return false
		}
		kw, ok := s.controlHeader(s.code, j)
		if first && s.goExitsBlock(i, j, ok && s.tok(kw) == "switch") {
			return false
		}
		first = false
		if ok && s.tok(kw) == "select" {
			return false
		}
		if ok && s.tok(kw) == "for" {
			return true
		}
	}
	return false
}

// goExitsBlock reports whether the block opening at code token open leaves
// the loop after code token i. That takes a return, or a break if the block
// isn't a switch body, at the top level of the block with no continue
// before it.
func (s *source) goExitsBlock(i, open int, inSwitch bool) bool {
	end := s.matchClose(s.code, open)
	depth := 0
	for j := i + 1; j < end; j++ {
		switch t := s.tok(j); {
		case t == "{":
			depth++
		case t == "}":
			depth--
		case s.isKeyword(s.code[j], "continue"):
			return false
		case depth == 0 && (s.isKeyword(s.code[j], "return") || !inSwitch && s.isKeyword(s.code[j], "break")):
			return true
		}
	}
	return false
}

// goIntTypes are the integer types a conversion in a loop limit can use.
var goIntTypes = setOf("int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune")

// checkGoRangeInt flags `for i := 0; i < n; i++ {` loops whose body doesn't
// touch i or n. gopls' rangeint modernizer rewrites these to
// `for i := range n` (Go 1.22).
func checkGoRangeInt(s *source) []finding {
	if !s.goSupports(22) {
		return nil
	}
	var out []finding
	for i := range s.code {
		if !s.isKeyword(s.code[i], "for") || s.code[i+1].Kind != code.TokIdent {
			continue
		}
		v := s.tok(i + 1)
		if !s.seq(i+2, ":=", "0", ";", v, "<") {
			continue
		}
		semi := i + 7
		for semi < len(s.code) && s.tok(semi) != ";" && s.tok(semi) != "{" {
			semi++
		}
		if !s.seq(semi, ";", v, "++", "{") {
			continue
		}
		open := semi + 3
		limit, ok := s.goLoopLimit(i+7, semi)
		if !ok {
			continue
		}
		cl := s.matchClose(s.code, open)
		if cl >= len(s.code) {
			continue
		}
		used := false
		for j := open + 1; j < cl; j++ {
			if s.tok(j) != v || s.tok(j-1) == "." {
				continue
			}
			used = true
			if s.goMutated(j, open) {
				ok = false
				break
			}
		}
		for _, name := range limit {
			for j := open + 1; ok && j < cl; j++ {
				if s.tok(j) == name && s.tok(j-1) != "." && s.goMutated(j, open) {
					ok = false
				}
			}
		}
		if !ok {
			continue
		}
		repl := "for " + v + " := range " + s.codeText(i+7, semi)
		if !used {
			repl = "for range " + s.codeText(i+7, semi)
		}
		out = append(out, finding{span: s.tokSpan(i, open), detail: "`" + s.codeText(i, open) + "` -> `" + repl + "`"})
	}
	return out
}

// goLoopLimit checks that the code tokens [i, end) form a loop limit that
// only depends on its identifiers: a number, an identifier or field
// selector, `len` of one, or an integer conversion of one. It returns the
// root identifiers of the limit.
func (s *source) goLoopLimit(i, end int) ([]string, bool) {
	if end == i+1 && s.code[i].Kind == code.TokNumber {
		return nil, true
	}
	if s.tok(i) == "len" || goIntTypes[s.tok(i)] {
		if s.tok(i+1) != "(" || s.matchClose(s.code, i+1) != end-1 {
			return nil, false
		}
		i, end = i+2, end-1
	}
	if end <= i || s.code[i].Kind != code.TokIdent {
		return nil, false
	}
	for j := i + 1; j < end; j += 2 {
		if s.tok(j) != "." || s.code[j+1].Kind != code.TokIdent {
			return nil, false
		}
	}
	return []string{s.tok(i)}, true
}

// goMutated reports whether the identifier at code token j might change in
// the loop body opening at code token open. That's the case if it's
// assigned, incremented, has its address taken, is deleted from or cleared,
// receives a method call or is passed to a call. Reads like `x[i]` or `x.f`
// are fine.
func (s *source) goMutated(j, open int) bool {
	next, prev := s.tok(j+1), s.tok(j-1)
	switch {
	case next == "=" || next == "++" || next == "--" || isAssignOp(next):
		return true
	case prev == "&":
		return true
	case next == "," && s.goAssignAhead(j):
		return true
	case next == "." && s.tok(j+3) == "(":
		return true
	case (prev == "(" || prev == ",") && (next == ")" || next == ","):
		return true
	}
	// x.f = v, x.f++ and x[k] = v change a field or element the limit reads.
	k := j
	for s.tok(k+1) == "." || s.tok(k+1) == "[" {
		if s.tok(k+1) == "[" {
			k = s.matchClose(s.code, k+1)
		} else {
			k += 2
		}
		if t := s.tok(k + 1); t == "=" || t == "++" || t == "--" || isAssignOp(t) {
			return true
		}
	}
	return false
}

// goAssignAhead reports whether the identifier list holding code token j is
// the left side of an assignment, as in `a, i = f()`.
func (s *source) goAssignAhead(j int) bool {
	line := s.line(s.code[j].Span.Start)
	for k := j + 1; k < len(s.code) && s.line(s.code[k].Span.Start) == line; k++ {
		t := s.tok(k)
		if t == "=" || t == ":=" {
			return true
		}
		if t != "," && s.code[k].Kind != code.TokIdent {
			return false
		}
	}
	return false
}

// checkGoBlankRange flags blank identifiers at the end of a range clause
// (staticcheck S1005, `gofmt -s`).
func checkGoBlankRange(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.isKeyword(s.code[i], "for") {
			continue
		}
		var repl string
		var end int
		switch {
		case s.seq(i+1, "_", ",", "_", "=", "range"):
			repl, end = "for range", i+6
		case s.seq(i+1, "_", "=", "range"):
			repl, end = "for range", i+4
		case s.code[i+1].Kind == code.TokIdent && s.seq(i+2, ",", "_") && (s.seq(i+4, ":=", "range") || s.seq(i+4, "=", "range")):
			repl, end = "for "+s.tok(i+1)+" "+s.tok(i+4)+" range", i+6
		default:
			continue
		}
		out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "` -> `" + repl + "`"})
	}
	return out
}

var cancelCtors = setOf("WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause", "WithTimeoutCause", "WithDeadlineCause")

// checkGoContextCancel flags `ctx, cancel := context.WithTimeout(...)` in a
// function that never defers cancel (SonarGo S8188). We accept
// `defer func() { ... cancel() ... }()`. If cancel is returned, stored,
// passed on or used in a function literal, we leave it alone, because
// whoever receives it owns it now.
func checkGoContextCancel(s *source) []finding {
	imports := s.goImports()
	var out []finding
	for i := range s.code {
		if s.code[i].Kind != code.TokIdent || s.tok(i+1) != "," || s.code[i+2].Kind != code.TokIdent || s.tok(i+3) != ":=" {
			continue
		}
		if !s.goPkg(imports, i+4, "context") || !cancelCtors[s.tok(i+6)] || s.tok(i+7) != "(" {
			continue
		}
		cl := s.matchClose(s.code, i+7)
		fn := s.goFuncBody(i)
		if cl >= len(s.code) || fn < 0 {
			continue
		}
		name := s.tok(i + 2)
		if name != "_" && s.goCancelHandled(name, cl+1, fn) {
			continue
		}
		detail := "`defer " + name + "()` is missing"
		if name == "_" {
			detail = "the cancel function is discarded"
		}
		out = append(out, finding{span: s.tokSpan(i, cl+1), detail: detail})
	}
	return out
}

// goCancelHandled reports whether the cancel function name, assigned before
// code token from in the function body opening at code token fn, is
// deferred, handed to other code, or called before any return statement
// could skip the call. `ctx, cancel := ...` followed by `cancel()` makes a
// context that's already canceled.
func (s *source) goCancelHandled(name string, from, fn int) bool {
	end := s.matchClose(s.code, fn)
	returned := false
	for j := from; j < end; j++ {
		if s.isKeyword(s.code[j], "return") && !returned {
			returned = s.goFuncBody(j) == fn
			continue
		}
		if s.tok(j) != name || s.tok(j-1) == "." {
			continue
		}
		if s.isKeyword(s.code[j-1], "defer") || s.goFuncBody(j) != fn || s.tok(j+1) != "(" || !returned {
			return true
		}
	}
	return false
}

// goFuncBody returns the index of the brace opening the innermost function
// or function literal body around code token i, or -1 if there isn't one.
func (s *source) goFuncBody(i int) int {
	for j := range s.enclosingBraces(i) {
		if s.funcBodies()[s.code[j].Span.Start] || s.lambdaBody(j) {
			return j
		}
	}
	return -1
}
