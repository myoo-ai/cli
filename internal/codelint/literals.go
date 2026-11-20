package codelint

import (
	"regexp"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// plainString returns the content of string literal token t unless it has a
// `${}` template, is an f-string or C# `$` string, or holds a PHP variable.
// Regex literals don't count as strings.
func (s *source) plainString(t code.Token) (string, bool) {
	if t.Kind != code.TokString {
		return "", false
	}
	lit := s.text(t)
	switch s.f.Lang {
	case code.JavaScript, code.TypeScript:
		if lit[0] == '/' || lit[0] == '`' && strings.Contains(lit, "${") {
			return "", false
		}
	case code.Python:
		if pythonFString(lit) {
			return "", false
		}
	case code.CSharp:
		if strings.HasPrefix(lit, "$") || strings.HasPrefix(lit, "@$") {
			return "", false
		}
	case code.PHP:
		if phpInterpolates(lit) {
			return "", false
		}
	}
	return stringContent(lit), true
}

// literalKeys maps constant keywords to keys. Two keywords with the same key
// have the same value.
var literalKeys = map[string]string{
	"true": "true", "True": "true", "false": "false", "False": "false",
	"null": "null", "None": "null", "nil": "null", "undefined": "undefined",
}

// literalKey returns a key for the code tokens [i, j) if they form a signed
// or unsigned number, a string with no interpolation, or a constant like
// true, None or undefined. If two literals have the same key, they're equal.
func (s *source) literalKey(i, j int) (string, bool) {
	if j-i == 2 && (s.tok(i) == "-" || s.tok(i) == "+") && s.code[i+1].Kind == code.TokNumber {
		return "n:" + s.tok(i) + s.tok(i+1), true
	}
	if j-i != 1 {
		return "", false
	}
	t := s.code[i]
	switch t.Kind {
	case code.TokNumber:
		return "n:" + s.text(t), true
	case code.TokString:
		if c, ok := s.plainString(t); ok {
			return "s:" + c, true
		}
	case code.TokKeyword, code.TokIdent:
		if k, ok := literalKeys[s.text(t)]; ok {
			return k, true
		}
	}
	return "", false
}

// assertKind says whether an assertion call checks that argument number arg
// is true, false, or equal to the next argument.
type assertKind int

const (
	assertsTrue assertKind = iota
	assertsFalse
	assertsEqual
)

type assertCall struct {
	kind assertKind
	arg  int
}

// assertCalls maps each language to its assertion calls, keyed like
// formatCalls. For Rust the keys are macro names.
var assertCalls = map[code.Lang]map[string]assertCall{
	code.JavaScript: jsAssertCalls,
	code.TypeScript: jsAssertCalls,
	code.Python: {
		".assertTrue": {assertsTrue, 0}, ".assertFalse": {assertsFalse, 0},
		".assertEqual": {assertsEqual, 0}, ".assertEquals": {assertsEqual, 0},
	},
	code.Java: {
		"assertTrue": {assertsTrue, 0}, ".assertTrue": {assertsTrue, 0},
		"assertFalse": {assertsFalse, 0}, ".assertFalse": {assertsFalse, 0},
		"assertEquals": {assertsEqual, 0}, ".assertEquals": {assertsEqual, 0},
	},
	code.CSharp: {
		"Assert.True": {assertsTrue, 0}, "Assert.IsTrue": {assertsTrue, 0},
		"Assert.False": {assertsFalse, 0}, "Assert.IsFalse": {assertsFalse, 0},
		"Assert.Equal": {assertsEqual, 0}, "Assert.AreEqual": {assertsEqual, 0},
	},
	code.PHP: {
		".assertTrue": {assertsTrue, 0}, ".assertFalse": {assertsFalse, 0},
		".assertEquals": {assertsEqual, 0}, ".assertSame": {assertsEqual, 0},
	},
	code.Go: {
		"assert.True": {assertsTrue, 1}, "require.True": {assertsTrue, 1},
		"assert.False": {assertsFalse, 1}, "require.False": {assertsFalse, 1},
		"assert.Equal": {assertsEqual, 1}, "require.Equal": {assertsEqual, 1},
		"assert.EqualValues": {assertsEqual, 1}, "require.EqualValues": {assertsEqual, 1},
	},
	code.Rust: {
		"assert": {assertsTrue, 0}, "debug_assert": {assertsTrue, 0},
		"assert_eq": {assertsEqual, 0}, "debug_assert_eq": {assertsEqual, 0},
	},
	code.C:   cAssertCalls,
	code.CPP: cAssertCalls,
}

var (
	jsAssertCalls = map[string]assertCall{
		"assert": {assertsTrue, 0}, "assert.ok": {assertsTrue, 0}, "assert.isTrue": {assertsTrue, 0},
		"assert.isFalse": {assertsFalse, 0}, "assert.equal": {assertsEqual, 0}, "assert.strictEqual": {assertsEqual, 0},
		"assert.deepEqual": {assertsEqual, 0}, "assert.deepStrictEqual": {assertsEqual, 0},
	}
	cAssertCalls = map[string]assertCall{
		"assert": {assertsTrue, 0}, "EXPECT_TRUE": {assertsTrue, 0}, "ASSERT_TRUE": {assertsTrue, 0},
		"EXPECT_FALSE": {assertsFalse, 0}, "ASSERT_FALSE": {assertsFalse, 0},
		"EXPECT_EQ": {assertsEqual, 0}, "ASSERT_EQ": {assertsEqual, 0},
	}
	// literalMatchers maps the no-argument matchers of `expect(x)` and
	// AssertJ's `assertThat(x)` to the literal they accept.
	literalMatchers = map[string]string{
		"toBeTruthy": "true", "toBeFalsy": "false", "toBeNull": "null", "toBeUndefined": "undefined",
		"isTrue": "true", "isFalse": "false", "isNull": "null",
	}
	equalMatchers = setOf("toBe", "toEqual", "toStrictEqual", "isEqualTo", "isSameAs")
)

// callKey returns the lookup keys for the call named by code token i, most
// specific first. A qualified call gets "Qual.Name" and ".Name", a plain one
// "Name".
func (s *source) callKey(i int) []string {
	name := s.tok(i)
	switch s.tok(i - 1) {
	case ".", "::", "->", "?.":
		return []string{s.tok(i-2) + "." + name, "." + name}
	}
	return []string{name}
}

// checkTautologicalAssertion flags assertions on literals that pass whatever
// the code does, like `assert True`, `expect(1).toBe(1)`,
// `assertEquals("a", "a")` or `assert!(true)`.
func checkTautologicalAssertion(s *source) []finding {
	calls := assertCalls[s.f.Lang]
	var out []finding
	for i, t := range s.code {
		if s.f.Lang == code.Python && t.Kind == code.TokKeyword && s.text(t) == "assert" {
			if end, ok := s.pythonTautology(i); ok {
				out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "`"})
			}
			continue
		}
		if t.Kind != code.TokIdent && t.Kind != code.TokKeyword {
			continue
		}
		open := i + 1
		if s.f.Lang == code.Rust {
			if s.tok(i+1) != "!" || s.tok(i-1) == "." {
				continue
			}
			open = i + 2
		}
		if s.tok(open) != "(" {
			continue
		}
		name := s.tok(i)
		expectation := name == "expect" || name == "assertThat"
		var call assertCall
		known := false
		for _, k := range s.callKey(i) {
			if call, known = calls[k]; known {
				break
			}
		}
		if !expectation && !known {
			continue
		}
		args, cl := s.callArgs(open)
		if cl >= len(s.code) {
			continue
		}
		start := i
		if p := s.tok(i - 1); p == "." || p == "::" || p == "->" || p == "?." {
			start = i - 2
		}
		var hit bool
		if expectation {
			hit, cl = s.literalExpectation(args, cl)
		} else {
			hit = s.tautology(call, args)
		}
		if hit {
			out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "`"})
		}
	}
	return out
}

// tautology reports whether the arguments of an assertion call are literals
// that make it pass.
func (s *source) tautology(c assertCall, args [][2]int) bool {
	if c.arg >= len(args) {
		return false
	}
	key, ok := s.literalKey(args[c.arg][0], args[c.arg][1])
	if !ok {
		return false
	}
	switch c.kind {
	case assertsTrue:
		return key == "true"
	case assertsFalse:
		return key == "false"
	}
	if c.arg+1 >= len(args) {
		return false
	}
	other, ok := s.literalKey(args[c.arg+1][0], args[c.arg+1][1])
	return ok && other == key
}

// literalExpectation checks the matcher chained to `expect(x)` or
// `assertThat(x)`, where the argument list closes at code token cl. It
// reports whether x is a literal the matcher accepts, and returns the index
// of the `)` that closes the matcher call.
func (s *source) literalExpectation(args [][2]int, cl int) (bool, int) {
	if len(args) != 1 || s.tok(cl+1) != "." || s.tok(cl+3) != "(" {
		return false, cl
	}
	key, ok := s.literalKey(args[0][0], args[0][1])
	if !ok {
		return false, cl
	}
	margs, mcl := s.callArgs(cl + 3)
	if mcl >= len(s.code) {
		return false, cl
	}
	matcher := s.tok(cl + 2)
	if want, ok := literalMatchers[matcher]; ok {
		return len(margs) == 0 && key == want, mcl
	}
	if matcher == "toBeDefined" {
		return len(margs) == 0 && key != "undefined", mcl
	}
	if !equalMatchers[matcher] || len(margs) != 1 {
		return false, cl
	}
	other, ok := s.literalKey(margs[0][0], margs[0][1])
	return ok && other == key, mcl
}

// pythonTautology reports whether the assert statement at code token i
// checks `True` or compares two equal literals with ==. It also returns the
// index after the condition.
func (s *source) pythonTautology(i int) (int, bool) {
	end := i + 1
	for end < len(s.code) && !s.firstOnLine(end) && s.tok(end) != "," && s.tok(end) != ";" {
		end++
	}
	switch end - i - 1 {
	case 1:
		k, ok := s.literalKey(i+1, end)
		return end, ok && k == "true"
	case 3:
		if s.tok(i+2) != "==" {
			return end, false
		}
		a, ok1 := s.literalKey(i+1, i+2)
		b, ok2 := s.literalKey(i+3, i+4)
		return end, ok1 && ok2 && a == b
	}
	return end, false
}

// genericErrorRe matches error messages that don't say what failed.
var genericErrorRe = regexp.MustCompile(`(?i)^(?:something went wrong|an? (?:unexpected |unknown )?error (?:has )?occurred|unexpected error|unknown error|error|invalid (?:input|value|data|argument|request)|operation failed|failed|internal error)[.!]?$`)

var (
	errorTypeRe = regexp.MustCompile(`^[A-Z]\w*(?:Error|Exception)$|^\w+_error$`)
	// errorMacros are the Rust macros that build or raise an error from a
	// message.
	errorMacros = setOf("anyhow", "bail", "panic", "eyre", "format_err")
	// rustStringConversions are the methods that can turn a string literal
	// into an error value.
	rustStringConversions = setOf("into", "to_string", "to_owned")
)

// checkGenericErrorMessage flags errors built from a single string literal
// with a generic message, like `throw new Error("Invalid input")`,
// `raise ValueError("failed")`, `errors.New("unknown error")` or
// `bail!("Something went wrong")`. We skip tests, since a stub that returns
// `errors.New("failed")` only needs to fail.
func checkGenericErrorMessage(s *source) []finding {
	var out []finding
	for _, k := range s.productStrings() {
		msg, ok := s.plainString(s.code[k])
		if !ok || !genericErrorRe.MatchString(strings.TrimSpace(msg)) || s.tok(k-1) != "(" {
			continue
		}
		end := k + 1
		if s.f.Lang == code.Rust && s.tok(k-2) == "Err" && s.tok(k+1) == "." && rustStringConversions[s.tok(k+2)] && s.seq(k+3, "(", ")") {
			end = k + 5
		}
		if s.tok(end) != ")" {
			continue
		}
		start, ok := s.errorCall(k - 1)
		// A Go sentinel (`var errInvalid = errors.New("invalid input")`)
		// gets its context from the callers that wrap it, so we leave it.
		if !ok || s.f.Lang == code.Go && s.tok(start-1) == "=" {
			continue
		}
		out = append(out, finding{span: s.tokSpan(start, end+1), detail: "`" + s.codeText(start, end+1) + "`"})
	}
	return out
}

// errorCall reports whether the call with its parenthesis at code token
// open builds or raises an error. It also returns the index of the call's
// first token.
func (s *source) errorCall(open int) (int, bool) {
	n := open - 1
	if n < 0 {
		return 0, false
	}
	if s.f.Lang == code.Rust {
		switch {
		case s.tok(n) == "Err":
			return n, true
		case s.tok(n) == "!" && errorMacros[s.tok(n-1)]:
			return n - 1, true
		}
		return 0, false
	}
	if s.code[n].Kind != code.TokIdent && s.code[n].Kind != code.TokKeyword {
		return 0, false
	}
	name := s.tok(n)
	if s.f.Lang == code.Go {
		switch {
		case name == "panic" && s.tok(n-1) != ".":
			return n, true
		case s.seq(n-2, "errors", ".", "New"), s.seq(n-2, "fmt", ".", "Errorf"):
			return n - 2, true
		}
		return 0, false
	}
	start := s.chainStart(open)
	if start == open {
		return 0, false
	}
	switch before := s.tok(start - 1); {
	case before == "new" && s.tok(start-2) == "throw":
		return start - 2, true
	case before == "new" && errorTypeRe.MatchString(name):
		return start - 1, true
	case before == "throw" || before == "raise":
		return start - 1, true
	}
	// Python classes and JavaScript's Error can be called without new.
	py := s.f.Lang == code.Python || s.f.Lang == code.JavaScript || s.f.Lang == code.TypeScript
	return start, py && start == n && errorTypeRe.MatchString(name)
}

// cheerfulRe matches chatty or celebratory openers and "successfully!".
// Programmers do write "oops: conversion to untyped" in panics, and "yay" or
// "WOOHOO" tend to be test data or a custom HTTP method, so an opener only
// counts if it has an exclamation mark, or a comma with a phrase after it.
var cheerfulRe = regexp.MustCompile(`(?i)^(?:oops|whoops|uh[ -]?oh)(?:!|,\s*(?:something|it\s+looks|looks|there))|^(?:yay|hooray|woo-?hoo)[!,]|^congrat(?:s|ulations)\b[!,]?\s+\w|^(?:great|awesome|success)!|successfully!`)

// checkCheerfulMessage flags string literals outside test code that start
// with a cheer or an "oops", or that say "successfully!".
func checkCheerfulMessage(s *source) []finding {
	var out []finding
	for _, i := range s.productStrings() {
		t := s.code[i]
		lit := strings.TrimSpace(stringContent(s.text(t)))
		if _, ok := usedPhrase(cheerfulRe, lit); ok {
			out = append(out, finding{span: t.Span, detail: "`" + excerpt(lit, 60) + "`"})
		}
	}
	return out
}

// regexLikeRe matches regex groups and escapes, so we can tell if a string
// is a regular expression.
var regexLikeRe = regexp.MustCompile(`\(\?|\\[bswdBSWD]`)

// usedPhrase returns the first match of re in the string literal content
// lit, if the string uses the phrase as text. We don't count it if lit is a
// regular expression that matches the phrase, or if the phrase is quoted in
// double quotes or backticks, since then it's only being mentioned.
func usedPhrase(re *regexp.Regexp, lit string) (string, bool) {
	if regexLikeRe.MatchString(lit) {
		return "", false
	}
	for _, m := range re.FindAllStringIndex(lit, -1) {
		before := lit[:m[0]]
		if strings.Count(before, `"`)%2 == 0 && strings.Count(before, "`")%2 == 0 {
			return lit[m[0]:m[1]], true
		}
	}
	return "", false
}

var (
	placeholderDataRe = regexp.MustCompile(`(?i)\blorem\s+ipsum\b|\b(?:john|jane)\s+doe\b|\byour[_ -]api[_ -]key`)
	// sampleWordRe matches names of sample values and placeholder fields.
	sampleWordRe = regexp.MustCompile(`(?i)example|sample|demo|placeholder`)
	// fixturePathRe matches paths of fixtures, mocks, seed data, examples,
	// component stories, integration and conformance tests and lorem ipsum
	// generators. Those are supposed to hold sample data.
	fixturePathRe = regexp.MustCompile(`(?i)fixture|mock|seed|sample|example|demo|stories|storybook|integration|conformance|e2e|lorem|(?:^|/)_?tests?(?:[./]|$)`)
	wordLikeRe    = regexp.MustCompile(`^[\w.:-]+$`)
)

// checkPlaceholderData flags sample values like lorem ipsum, John Doe or an
// API key placeholder in string literals outside tests and fixtures. If
// something on the same line says example, sample, demo or placeholder, as
// in `placeholder: "John Doe"`, the value is meant to be a sample and we
// leave it.
func checkPlaceholderData(s *source) []finding {
	if fixturePathRe.MatchString(s.f.Path) {
		return nil
	}
	var out []finding
	for _, i := range s.productStrings() {
		t := s.code[i]
		phrase, ok := usedPhrase(placeholderDataRe, stringContent(s.text(t)))
		if !ok || s.namesSample(t) {
			continue
		}
		out = append(out, finding{span: t.Span, detail: "`" + phrase + "`"})
	}
	return out
}

// namesSample reports whether an identifier or a one-word string on the
// lines of literal t says example, sample, demo or placeholder.
func (s *source) namesSample(t code.Token) bool {
	for l := s.line(t.Span.Start); l <= s.line(t.Span.End); l++ {
		for _, o := range s.lineCodeTokens(l) {
			if o.Span == t.Span {
				continue
			}
			text := s.text(o)
			if o.Kind == code.TokString {
				text = stringContent(text)
				if !wordLikeRe.MatchString(text) {
					continue
				}
			} else if o.Kind != code.TokIdent && o.Kind != code.TokKeyword {
				continue
			}
			if sampleWordRe.MatchString(text) {
				return true
			}
		}
	}
	return false
}

// productStrings returns the indices into s.code of the string literals
// outside test code. JavaScript regex literals are left out.
func (s *source) productStrings() []int {
	if s.test || s.f.Lang == code.Rust && s.rustTestCode() {
		return nil
	}
	var tests []code.Span
	if s.f.Lang == code.Rust {
		tests = s.rustTestSpans()
	}
	var out []int
	for i, t := range s.code {
		if t.Kind != code.TokString || inSpans(tests, t.Span.Start) {
			continue
		}
		if (s.f.Lang == code.JavaScript || s.f.Lang == code.TypeScript) && s.text(t)[0] == '/' {
			continue
		}
		out = append(out, i)
	}
	return out
}

// excerpt cuts s to n runes and adds "..." if it had to cut.
func excerpt(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}
