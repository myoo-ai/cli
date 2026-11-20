package codelint

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/rust"
)

var langRust = []code.Lang{code.Rust}

var rustRules = []*Rule{
	{
		ID:             "rust-len-zero",
		Langs:          langRust,
		Description:    "prefer using `is_empty()` instead of comparing `len()` with zero, as it states the intent and works for any collection.",
		Recommendation: "write `x.is_empty()` for `x.len() == 0` and `!x.is_empty()` for `x.len() != 0` or `x.len() > 0`, as Clippy's `len_zero` suggests. every standard collection and `str` have both methods.",
		check:          checkRustLenZero,
	},
	{
		ID:             "rust-needless-return",
		Langs:          langRust,
		Description:    "prefer ending a function with its value instead of a `return` statement, as the last expression of a block is its value.",
		Recommendation: "drop the `return` keyword and the semicolon of the last statement, as Clippy's `needless_return` suggests. keep `return` for early exits.",
		check:          checkRustNeedlessReturn,
	},
	{
		ID:             "rust-bool-comparison",
		Langs:          langRust,
		Description:    "prefer using the boolean itself instead of comparing it with `true` or `false`, as the comparison adds nothing.",
		Recommendation: "write `x` for `x == true` and `!x` for `x == false`, as Clippy's `bool_comparison` suggests.",
		check:          checkRustBoolComparison,
	},
	{
		ID:             "rust-manual-string-new",
		Langs:          langRust,
		Description:    "prefer using `String::new()` instead of converting an empty string literal, as it says what is created without a conversion.",
		Recommendation: "replace `\"\".to_string()`, `\"\".to_owned()` and `String::from(\"\")` with `String::new()`, as Clippy's `manual_string_new` suggests. it does not allocate either.",
		check:          checkRustManualStringNew,
	},
	{
		ID:             "rust-iter-nth-zero",
		Langs:          langRust,
		Description:    "prefer using `next()` instead of `nth(0)` on an iterator, as it says that the first element is taken.",
		Recommendation: "replace `.nth(0)` with `.next()`, as Clippy's `iter_nth_zero` suggests. both advance the iterator by one element.",
		check:          checkRustIterNthZero,
	},
	{
		ID:             "rust-single-char-pattern",
		Langs:          langRust,
		Description:    "prefer using a `char` instead of a one-character string as a string pattern, as a `char` pattern is faster.",
		Recommendation: "pass the character as a `char` literal, for example `s.split(',')` instead of `s.split(\",\")`, as Clippy's `single_char_pattern` suggests. the matches are the same.",
		check:          checkRustSingleCharPattern,
	},
	{
		ID:             "rust-explicit-iter-loop",
		Langs:          langRust,
		Description:    "prefer looping over a reference to the collection instead of calling `iter()`, `iter_mut()` or `into_iter()` in a `for` loop, as the loop does that itself.",
		Recommendation: "write `for x in &v` for `v.iter()`, `for x in &mut v` for `v.iter_mut()` and `for x in v` for `v.into_iter()`, as Clippy's `explicit_iter_loop` and `explicit_into_iter_loop` suggest. the standard collections implement `IntoIterator` for these forms.",
		check:          checkRustExplicitIterLoop,
	},
	{
		ID:             "rust-map-unwrap-or",
		Langs:          langRust,
		Description:    "prefer using `map_or` or `map_or_else` instead of `map(f).unwrap_or(d)`, as it maps and defaults in one call.",
		Recommendation: "write `x.map_or(d, f)` for `x.map(f).unwrap_or(d)` and `x.map_or_else(g, f)` for `x.map(f).unwrap_or_else(g)`, as Clippy's `map_unwrap_or` suggests. the default is evaluated just as eagerly as before.",
		check:          checkRustMapUnwrapOr,
	},
	{
		ID:             "rust-unwrap-or-default",
		Langs:          langRust,
		Description:    "prefer using `unwrap_or_default()` or `or_default()` instead of passing an empty value or `Default::default`, as it states that the default is meant.",
		Recommendation: "replace `.unwrap_or(Vec::new())`, `.unwrap_or_else(String::new)` and `.unwrap_or(Default::default())` with `.unwrap_or_default()`, and `.or_insert(Vec::new())` with `.or_default()`, as Clippy's `unwrap_or_default` suggests.",
		check:          checkRustUnwrapOrDefault,
	},
	{
		ID:             "rust-question-mark",
		Langs:          langRust,
		Description:    "prefer using the `?` operator instead of matching an error or `None` only to return it, as `?` does the same in one character.",
		Recommendation: "replace the match or the `is_none()` check with `?`, as Clippy's `question_mark` suggests. `?` also converts the error with `From`, so `Err(e.into())` is covered.",
		check:          checkRustQuestionMark,
	},
	{
		ID:             "rust-println-empty-string",
		Langs:          langRust,
		Description:    "prefer calling `println!()` without arguments instead of printing an empty string, as both print a newline.",
		Recommendation: "write `println!()`, `eprintln!()` or `writeln!(w)` without the empty string literal, as Clippy's `println_empty_string` suggests.",
		check:          checkRustPrintlnEmpty,
	},
	{
		ID:             "rust-to-string-in-format-args",
		Langs:          langRust,
		Description:    "prefer passing a value to a format macro directly instead of calling `to_string()` on it, as the macro formats it without the extra allocation.",
		Recommendation: "drop the `.to_string()` call of an argument that only `{}` placeholders use, as Clippy's `to_string_in_format_args` suggests. `{}` formats the value with the same `Display` implementation.",
		check:          checkRustToStringInFormatArgs,
	},
	{
		ID:             "rust-format-push-string",
		Langs:          langRust,
		Description:    "prefer using `write!` instead of appending the result of `format!` to a string, as it formats into the string without a temporary allocation.",
		Recommendation: "import `std::fmt::Write` and write `write!(s, ...)`, as Clippy's `format_push_string` suggests. writing to a `String` cannot fail, so ignoring its `fmt::Result` with `let _ =` is safe.",
		check:          checkRustFormatPushString,
	},
	{
		ID:             "rust-chars-next-cmp",
		Langs:          langRust,
		Description:    "prefer using `starts_with` or `ends_with` instead of comparing the first or last of `chars()`, as they say what is tested.",
		Recommendation: "write `s.starts_with(c)` for `s.chars().next() == Some(c)` and `s.ends_with(c)` for `s.chars().last() == Some(c)`, as Clippy's `chars_next_cmp` and `chars_last_cmp` suggest.",
		check:          checkRustCharsNextCmp,
	},
	{
		ID:             "rust-dbg-macro",
		Langs:          langRust,
		Description:    "`dbg!` calls should not be left in the code, as they print to stderr in every build.",
		Recommendation: withIgnoreHint("rust-dbg-macro", "remove the `dbg!` call and keep its argument, which it returns, as Clippy's `dbg_macro` suggests. use a logging crate for output that should stay."),
		check:          checkRustDbgMacro,
	},
	{
		ID:             "rust-undocumented-unsafe",
		Langs:          langRust,
		Description:    "`unsafe` blocks and `unsafe impl` should be preceded by a `// SAFETY:` comment, as readers need to know why the code upholds the invariants.",
		Recommendation: "add a `// SAFETY:` comment directly above that explains why the operation is sound, as Clippy's `undocumented_unsafe_blocks` suggests.",
		check:          checkRustUndocumentedUnsafe,
	},
	{
		ID:             "rust-missing-safety-doc",
		Langs:          langRust,
		Description:    "public `unsafe` functions should document their contract in a `# Safety` section, as callers must know what to uphold.",
		Recommendation: "add a `# Safety` heading to the doc comment that lists the conditions a caller must meet, as Clippy's `missing_safety_doc` and the Rust API Guidelines suggest.",
		check:          checkRustMissingSafetyDoc,
	},
	{
		ID:             "rust-unwrap-in-result",
		Langs:          langRust,
		Description:    "functions returning `Result` should propagate errors with `?` instead of calling `unwrap()`, as a panic takes the choice away from the caller.",
		Recommendation: withIgnoreHint("rust-unwrap-in-result", "replace `.unwrap()` with `?`, and convert an `Option` with `.ok_or(...)?` or `.ok_or_else(...)?`, as Clippy's `unwrap_in_result` suggests. where the value cannot be missing, use `expect(\"why\")` to state the invariant. the rule skips test code and build scripts."),
		check:          checkRustUnwrapInResult,
	},
	{
		ID:             "rust-ptr-arg",
		Langs:          langRust,
		Description:    "prefer borrowing a slice or `str` instead of `&String`, `&Vec<T>`, `&PathBuf` or `&Box<T>` parameters, as the function then accepts more argument types.",
		Recommendation: "take `&str`, `&[T]`, `&Path` or `&T` instead, as Clippy's `ptr_arg` and `borrowed_box` suggest. callers keep passing `&value` because of deref coercion.",
		check:          checkRustPtrArg,
	},
	{
		ID:             "rust-mem-replace-with-default",
		Langs:          langRust,
		Description:    "prefer using `mem::take` or `Option::take` instead of `mem::replace` with a default value, as they state the intent.",
		Recommendation: "write `mem::take(&mut x)` for `mem::replace(&mut x, Default::default())` and `x.take()` for `mem::replace(&mut x, None)`, as Clippy's `mem_replace_with_default` and `mem_replace_option_with_none` suggest.",
		check:          checkRustMemReplace,
	},
	{
		ID:             "rust-extern-crate",
		Langs:          langRust,
		Description:    "`extern crate` declarations are not needed since the 2018 edition, as dependencies are in scope by their name.",
		Recommendation: "remove the declaration, or turn `extern crate a as b;` into `use a as b;`. import macros with `use a::name;` instead of `#[macro_use]`. `alloc` in `no_std` crates still needs the declaration.",
		check:          checkRustExternCrate,
	},
	{
		ID:             "rust-redundant-field-names",
		Langs:          langRust,
		Description:    "prefer the field init shorthand instead of repeating a field name that the variable already has.",
		Recommendation: "write `Point { x, y }` for `Point { x: x, y: y }`, as Clippy's `redundant_field_names` suggests.",
		check:          checkRustRedundantFieldNames,
	},
	{
		ID:             "rust-collapsible-else-if",
		Langs:          langRust,
		Description:    "prefer `else if` instead of an `else` block holding only an `if` expression, as it removes a level of nesting.",
		Recommendation: "merge `else { if c { .. } }` into `else if c { .. }`, as Clippy's `collapsible_else_if` suggests.",
		check:          checkRustCollapsibleElseIf,
	},
	{
		ID:             "rust-single-match",
		Langs:          langRust,
		Description:    "prefer `if let` instead of a `match` with one pattern and an empty wildcard arm, as it states the single case directly.",
		Recommendation: "write `if let Pattern = x { .. }` for `match x { Pattern => .., _ => {} }`, as Clippy's `single_match` suggests.",
		check:          checkRustSingleMatch,
	},
	{
		ID:             "rust-redundant-closure",
		Langs:          langRust,
		Description:    "prefer passing a function directly instead of a closure that only calls it with its argument.",
		Recommendation: "replace `|x| foo(x)` with `foo`, as Clippy's `redundant_closure` suggests. keep the closure where the argument needs a deref coercion, which the compiler then reports.",
		check:          checkRustRedundantClosure,
	},
	{
		ID:             "rust-expect-empty-message",
		Langs:          langRust,
		Description:    "`expect` should say why the value is present, as an empty message panics with no more information than `unwrap()`.",
		Recommendation: "write the invariant that guarantees the value, such as `.expect(\"config was validated in main\")`, or return the error to the caller with `?`.",
		check:          checkRustExpectEmpty,
	},
}

var (
	// If a comparison operand follows one of these tokens, it starts an
	// expression, so the comparison applies to that operand alone.
	rustExprStarts = setOf("", "(", "[", "{", ",", ";", "=", "&&", "||", "return", "if", "while", "=>", "match")
	// These tokens can follow a comparison without an operator that binds
	// tighter than it.
	rustExprEnds = setOf("", ")", "]", "}", ",", ";", "{", "&&", "||", "=>")
	rustStmtEnds = setOf("", ";", "{", "}")
)

// rustTestSpans returns the spans of #[cfg(test)] modules and items, and of
// #[test] functions.
func (s *source) rustTestSpans() []code.Span {
	var out []code.Span
	for i := 0; i < len(s.code); i++ {
		if s.tok(i) != "#" || s.tok(i+1) != "[" {
			continue
		}
		cl := s.matchClose(s.code, i+1)
		if cl >= len(s.code) {
			continue
		}
		attr := s.codeText(i+2, cl)
		test := attr == "test" || strings.HasSuffix(attr, "::test") || strings.HasPrefix(attr, "cfg") && rustTestWordRe.MatchString(attr) && !strings.Contains(attr, "not")
		if !test {
			continue
		}
		j := cl + 1
		for ; j < len(s.code); j++ {
			if t := s.tok(j); t == "{" || t == ";" {
				break
			}
		}
		end := j
		if s.tok(j) == "{" {
			end = min(s.matchClose(s.code, j), len(s.code)-1)
		}
		if end < len(s.code) {
			out = append(out, code.Span{Start: s.code[i].Span.Start, End: s.code[end].Span.End})
		}
		i = cl
	}
	return out
}

// rustTestCode reports whether the whole file is test, example, benchmark
// or build script code.
func (s *source) rustTestCode() bool {
	if s.test || path.Base(s.f.Path) == "build.rs" {
		return true
	}
	for seg := range strings.SplitSeq(path.Dir(s.f.Path), "/") {
		if seg == "examples" || seg == "benches" {
			return true
		}
	}
	return false
}

func inSpans(spans []code.Span, off int) bool {
	for _, sp := range spans {
		if sp.Contains(off) {
			return true
		}
	}
	return false
}

// rustMacroBodies returns the bodies of the macro_rules! definitions. Those
// hold token patterns, not code.
func (s *source) rustMacroBodies() []code.Span {
	var out []code.Span
	for _, d := range s.f.Decls {
		if d.Kind == code.KindMacro {
			out = append(out, d.Body)
		}
	}
	return out
}

func (s *source) rustFinding(i, j int, repl string) finding {
	return finding{span: s.tokSpan(i, j), detail: s.rewriteDetail(i, j, repl)}
}

// rustMacroCall reports whether code token i names a macro invocation with
// parentheses or brackets. If so, it also returns the index of the closing
// bracket.
func (s *source) rustMacroCall(i int, names ...string) (int, bool) {
	if s.code[i].Kind != code.TokIdent || s.tok(i+1) != "!" || s.tok(i-1) == "." {
		return 0, false
	}
	if open := s.tok(i + 2); open != "(" && open != "[" && open != "{" {
		return 0, false
	}
	if len(names) > 0 && !setOf(names...)[s.tok(i)] {
		return 0, false
	}
	cl := s.matchClose(s.code, i+2)
	return cl, cl < len(s.code)
}

func checkRustLenZero(s *source) []finding {
	src := string(s.f.Src)
	if strings.Contains(src, "fn len(") && !strings.Contains(src, "fn is_empty(") {
		// Some type in the file has len() without is_empty().
		return nil
	}
	var out []finding
	for i := range s.code {
		if !s.seq(i, ".", "len", "(", ")") {
			continue
		}
		start := s.chainStart(i)
		if start == i {
			continue
		}
		recv := s.codeText(start, i)
		op, lit := s.tok(i+4), s.tok(i+5)
		neg, ok := false, false
		switch {
		case lit == "0" && op == "==", lit == "1" && op == "<":
			ok = true
		case lit == "0" && (op == "!=" || op == ">"), lit == "1" && op == ">=":
			neg, ok = true, true
		}
		if ok && rustExprStarts[s.tok(start-1)] && rustExprEnds[s.tok(i+6)] {
			out = append(out, s.rustFinding(start, i+6, rustNot(neg)+recv+".is_empty()"))
			continue
		}
		op = s.tok(start - 1)
		if s.tok(start-2) != "0" || !rustExprStarts[s.tok(start-3)] || !rustExprEnds[s.tok(i+4)] {
			continue
		}
		switch op {
		case "==":
			out = append(out, s.rustFinding(start-2, i+4, recv+".is_empty()"))
		case "!=", "<":
			out = append(out, s.rustFinding(start-2, i+4, "!"+recv+".is_empty()"))
		}
	}
	return out
}

func rustNot(neg bool) string {
	if neg {
		return "!"
	}
	return ""
}

func checkRustNeedlessReturn(s *source) []finding {
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() || d.Body.Empty() {
			continue
		}
		toks := s.bodyTokens(d)
		end := len(toks)
		if end > 0 && s.text(toks[end-1]) == ";" {
			end--
		}
		ret, depth := -1, 0
		for k := range end {
			switch t := s.text(toks[k]); t {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			case ";":
				if depth == 0 {
					ret = -1
				}
			case "return":
				if depth == 0 && (k == 0 || rustStmtEnds[s.text(toks[k-1])]) {
					ret = k
				}
			}
		}
		if ret < 0 || ret+1 >= end {
			continue
		}
		i, j := s.codeIndex(toks[ret].Span.Start), s.codeIndex(toks[end-1].Span.Start)+1
		stop := j
		if s.tok(j) == ";" {
			stop = j + 1
		}
		out = append(out, finding{
			span:   s.tokSpan(i, stop),
			detail: "`" + s.codeText(i, stop) + "` -> `" + s.codeText(i+1, j) + "`",
		})
	}
	return out
}

func checkRustBoolComparison(s *source) []finding {
	var out []finding
	for i := range s.code {
		op := s.tok(i)
		if op != "==" && op != "!=" {
			continue
		}
		if v := s.tok(i + 1); v == "true" || v == "false" {
			start := s.chainStart(i)
			if start == i || !rustExprStarts[s.tok(start-1)] || !rustExprEnds[s.tok(i+2)] {
				continue
			}
			out = append(out, s.rustFinding(start, i+2, rustNot((op == "==") != (v == "true"))+s.codeText(start, i)))
			continue
		}
		if v := s.tok(i - 1); v == "true" || v == "false" {
			end := s.chainEnd(i + 1)
			if end == i+1 || !rustExprStarts[s.tok(i-2)] || !rustExprEnds[s.tok(end)] {
				continue
			}
			out = append(out, s.rustFinding(i-1, end, rustNot((op == "==") != (v == "true"))+s.codeText(i+1, end)))
		}
	}
	return out
}

func checkRustManualStringNew(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		switch {
		case t.Kind == code.TokString && s.tok(i) == `""` && (s.seq(i+1, ".", "to_string", "(", ")") || s.seq(i+1, ".", "to_owned", "(", ")")):
			out = append(out, s.rustFinding(i, i+5, "String::new()"))
		case s.seq(i, "String", "::", "from", "(", `""`, ")") && s.tok(i-1) != "::":
			out = append(out, s.rustFinding(i, i+6, "String::new()"))
		}
	}
	return out
}

func checkRustIterNthZero(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, ".", "nth", "(", "0", ")") {
			continue
		}
		start := s.chainStart(i)
		if start == i {
			continue
		}
		out = append(out, s.rustFinding(start, i+5, s.codeText(start, i)+".next()"))
	}
	return out
}

// rustPatternArgs maps the str methods that take a pattern to the index of
// the pattern argument.
var rustPatternArgs = map[string]int{
	"split": 0, "rsplit": 0, "split_terminator": 0, "rsplit_terminator": 0, "splitn": 1, "rsplitn": 1,
	"split_once": 0, "rsplit_once": 0, "starts_with": 0, "ends_with": 0, "find": 0, "rfind": 0,
	"matches": 0, "rmatches": 0, "match_indices": 0, "rmatch_indices": 0, "trim_start_matches": 0,
	"trim_end_matches": 0, "strip_prefix": 0, "strip_suffix": 0, "replace": 0, "replacen": 0, "contains": 0,
}

// rustStringOnly lists pattern methods that other types also have, with a
// different meaning (HashSet::contains, Option::replace). For these we need
// evidence that the receiver is a string.
var rustStringOnly = setOf("contains", "replace")

var rustStringCalls = setOf("to_string", "as_str", "trim", "trim_start", "trim_end", "to_lowercase", "to_uppercase", "to_owned", "to_ascii_lowercase", "to_ascii_uppercase")

func checkRustSingleCharPattern(s *source) []finding {
	var out []finding
	for i := range s.code {
		idx, ok := rustPatternArgs[s.tok(i+1)]
		if s.tok(i) != "." || !ok || s.tok(i+2) != "(" {
			continue
		}
		args, cl := s.callArgs(i + 2)
		if cl >= len(s.code) || idx >= len(args) || args[idx][1]-args[idx][0] != 1 {
			continue
		}
		lit := s.code[args[idx][0]]
		ch, ok := rustCharOf(s.text(lit))
		if lit.Kind != code.TokString || !ok {
			continue
		}
		start := s.chainStart(i)
		if start == i || rustStringOnly[s.tok(i+1)] && !s.rustStringReceiver(start, i) {
			continue
		}
		repl := s.f.Text(code.Span{Start: s.code[start].Span.Start, End: lit.Span.Start}) + ch + s.f.Text(code.Span{Start: lit.Span.End, End: s.code[cl].Span.End})
		out = append(out, s.rustFinding(start, cl+1, strings.Join(strings.Fields(repl), " ")))
	}
	return out
}

// rustCharOf returns the char literal for a plain string literal with a
// single character. If lit isn't that, it returns false.
func rustCharOf(lit string) (string, bool) {
	if len(lit) < 3 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return "", false
	}
	body := lit[1 : len(lit)-1]
	switch {
	case body == `\"`:
		return `'"'`, true
	case body == "'":
		return `'\''`, true
	case len(body) == 2 && body[0] == '\\' && strings.ContainsRune(`nrt0\`, rune(body[1])):
		return "'" + body + "'", true
	case len([]rune(body)) == 1 && body != `\`:
		return "'" + body + "'", true
	}
	return "", false
}

// rustStringReceiver reports whether the chain [start, end) evaluates to a
// string literal, a call that returns a string, or a variable declared as
// a string.
func (s *source) rustStringReceiver(start, end int) bool {
	if end-start == 1 && s.code[start].Kind == code.TokString {
		return true
	}
	if s.tok(end-1) == ")" {
		if open := s.matchOpen(s.code, end-1); open > 0 && rustStringCalls[s.tok(open-1)] {
			return true
		}
		return false
	}
	return s.rustStringVar(s.tok(end-1), s.code[end-1].Span.Start)
}

// rustStringVarRe matches the declared types and initializers of a String
// variable.
var rustStringVarRe = regexp.MustCompile(`:\s*&?(?:mut\s+)?(?:'\w+\s+)?(?:String|str)\b|String::(?:new|from|with_capacity)|format!|\.to_string\(\)|\.to_owned\(\)`)

// rustStringVar reports whether name refers to a binding or field whose
// declaration shows a string type or initializer.
func (s *source) rustStringVar(name string, off int) bool {
	d := s.visibleDecl(name, off)
	if d == nil {
		for _, x := range s.f.Decls {
			if x.Kind == code.KindField && x.Name == name {
				d = x
				break
			}
		}
	}
	if d == nil {
		return false
	}
	if d.Type != "" {
		return rustStringVarRe.MatchString(": " + d.Type)
	}
	return rustStringVarRe.MatchString(s.f.Text(d.Span))
}

var rustCollections = setOf("Vec", "VecDeque", "HashMap", "HashSet", "BTreeMap", "BTreeSet", "BinaryHeap", "LinkedList", "IndexMap", "IndexSet")

func checkRustExplicitIterLoop(s *source) []finding {
	var out []finding
	for i := range s.code {
		if s.tok(i) != "for" || s.code[i].Kind != code.TokKeyword || s.tok(i+1) == "<" {
			continue
		}
		in := i + 1
		for in < len(s.code) && s.tok(in) != "in" && s.tok(in) != "{" && s.tok(in) != ";" {
			in++
		}
		if s.tok(in) != "in" {
			continue
		}
		j := in + 1
		if s.code[min(j, len(s.code)-1)].Kind != code.TokIdent && s.tok(j) != "self" {
			continue
		}
		j++
		for s.tok(j) == "." && s.code[min(j+1, len(s.code)-1)].Kind == code.TokIdent && s.tok(j+2) == "." {
			j += 2
		}
		method := s.tok(j + 1)
		if s.tok(j) != "." || !s.seq(j+2, "(", ")", "{") {
			continue
		}
		var prefix string
		switch method {
		case "iter":
			prefix = "&"
		case "iter_mut":
			prefix = "&mut "
		case "into_iter":
		default:
			continue
		}
		if j == in+2 && !s.rustCollectionVar(s.tok(in+1), s.code[in+1].Span.Start) {
			continue
		}
		out = append(out, s.rustFinding(in+1, j+4, prefix+s.codeText(in+1, j)))
	}
	return out
}

// rustCollectionVar reports whether name is a variable of a standard
// collection, slice or array type, or of a type we can't tell.
func (s *source) rustCollectionVar(name string, off int) bool {
	d := s.visibleDecl(name, off)
	if d == nil || d.Type == "" {
		return true
	}
	t := strings.TrimLeft(d.Type, "&")
	t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "mut "))
	if strings.HasPrefix(t, "'") {
		if _, rest, ok := strings.Cut(t, " "); ok {
			t = strings.TrimSpace(strings.TrimPrefix(rest, "mut "))
		}
	}
	if strings.HasPrefix(t, "[") {
		return true
	}
	head, _, _ := strings.Cut(t, "<")
	if _, last, ok := strings.CutLast(head, "::"); ok {
		head = last
	}
	return rustCollections[head]
}

func checkRustMapUnwrapOr(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, ".", "map", "(") {
			continue
		}
		cl := s.matchClose(s.code, i+2)
		if cl >= len(s.code) || s.tok(cl+1) != "." || s.tok(cl+3) != "(" {
			continue
		}
		var repl string
		switch s.tok(cl + 2) {
		case "unwrap_or":
			repl = "map_or"
		case "unwrap_or_else":
			repl = "map_or_else"
		default:
			continue
		}
		cl2 := s.matchClose(s.code, cl+3)
		start := s.chainStart(i)
		if cl2 >= len(s.code) || start == i || cl2 == cl+4 || cl == i+3 {
			continue
		}
		recv := s.codeText(start, i)
		out = append(out, s.rustFinding(start, cl2+1, recv+"."+repl+"("+s.codeText(cl+4, cl2)+", "+s.codeText(i+3, cl)+")"))
	}
	return out
}

var rustEmptyCtors = setOf("Vec::new()", "String::new()", "HashMap::new()", "HashSet::new()", "BTreeMap::new()",
	"BTreeSet::new()", "VecDeque::new()", "Default::default()", "Vec::new", "String::new", "HashMap::new", "HashSet::new",
	"BTreeMap::new", "BTreeSet::new", "VecDeque::new", "Default::default")

func checkRustUnwrapOrDefault(s *source) []finding {
	var out []finding
	for i := range s.code {
		m := s.tok(i + 1)
		if s.tok(i) != "." || s.tok(i+2) != "(" {
			continue
		}
		var repl string
		paren := true
		switch m {
		case "unwrap_or":
			repl = ".unwrap_or_default()"
		case "unwrap_or_else":
			repl, paren = ".unwrap_or_default()", false
		case "or_insert":
			repl = ".or_default()"
		case "or_insert_with":
			repl, paren = ".or_default()", false
		default:
			continue
		}
		cl := s.matchClose(s.code, i+2)
		if cl >= len(s.code) {
			continue
		}
		arg := strings.ReplaceAll(s.codeText(i+3, cl), " ", "")
		if !rustEmptyCtors[arg] || strings.HasSuffix(arg, "()") != paren {
			continue
		}
		out = append(out, s.rustFinding(i, cl+1, repl))
	}
	return out
}

// rustArm is a match arm with the pattern [pat, arrow) and the body
// [arrow+1, end).
type rustArm struct{ pat, arrow, end int }

// rustArms splits the match body between the braces at open and cl into
// arms. It returns nil if it can't.
func (s *source) rustArms(open, cl int) []rustArm {
	var arms []rustArm
	k := open + 1
	for k < cl {
		arm := rustArm{pat: k, arrow: -1}
		depth := 0
		for ; k < cl; k++ {
			t := s.tok(k)
			if t == "(" || t == "[" || t == "{" {
				depth++
			} else if t == ")" || t == "]" || t == "}" {
				depth--
			}
			if depth == 0 && t == "=>" {
				arm.arrow = k
				break
			}
		}
		if arm.arrow < 0 {
			return nil
		}
		k = arm.arrow + 1
		if s.tok(k) == "{" {
			k = s.matchClose(s.code, k) + 1
			arm.end = k
		} else {
			for depth = 0; k < cl; k++ {
				t := s.tok(k)
				if t == "(" || t == "[" || t == "{" {
					depth++
				} else if t == ")" || t == "]" || t == "}" {
					depth--
				}
				if depth == 0 && t == "," {
					break
				}
			}
			arm.end = k
		}
		if k > cl {
			return nil
		}
		if s.tok(k) == "," {
			k++
		}
		arms = append(arms, arm)
	}
	return arms
}

// rustMatch returns the scrutinee start and the body braces of the match
// whose keyword is code token i. ok is false if there isn't a match there.
func (s *source) rustMatch(i int) (open, cl int, ok bool) {
	depth := 0
	for open = i + 1; open < len(s.code); open++ {
		switch s.tok(open) {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		case ";", "}":
			if depth == 0 {
				return 0, 0, false
			}
		case "{":
			if depth == 0 {
				cl = s.matchClose(s.code, open)
				return open, cl, cl < len(s.code) && open > i+1
			}
		}
	}
	return 0, 0, false
}

// rustArmBody returns the token texts of the arm's body, without the
// enclosing braces or a trailing semicolon.
func (s *source) rustArmBody(a rustArm) []string {
	from, to := a.arrow+1, a.end
	if s.tok(from) == "{" && s.tok(to-1) == "}" {
		from, to = from+1, to-1
	}
	if to > from && s.tok(to-1) == ";" {
		to--
	}
	var out []string
	for k := from; k < to; k++ {
		out = append(out, s.tok(k))
	}
	return out
}

func (s *source) armPattern(a rustArm) string {
	var b []string
	for k := a.pat; k < a.arrow; k++ {
		b = append(b, s.tok(k))
	}
	return strings.Join(b, " ")
}

func checkRustQuestionMark(s *source) []finding {
	var out []finding
	for i := range s.code {
		switch {
		case s.tok(i) == "match" && s.code[i].Kind == code.TokKeyword:
			if fd, ok := s.rustMatchQuestion(i); ok {
				out = append(out, fd)
			}
		case s.tok(i) == "if" && s.code[i].Kind == code.TokKeyword && rustStmtEnds[s.tok(i-1)]:
			if fd, ok := s.rustIfQuestion(i); ok {
				out = append(out, fd)
			}
		}
	}
	return out
}

func (s *source) rustMatchQuestion(i int) (finding, bool) {
	open, cl, ok := s.rustMatch(i)
	if !ok {
		return finding{}, false
	}
	arms := s.rustArms(open, cl)
	if len(arms) != 2 {
		return finding{}, false
	}
	value, fail := arms[0], arms[1]
	if strings.HasPrefix(s.armPattern(value), "Err") || strings.HasPrefix(s.armPattern(value), "None") {
		value, fail = fail, value
	}
	vp, fp := s.armPattern(value), s.armPattern(fail)
	vb, fb := strings.Join(s.rustArmBody(value), " "), strings.Join(s.rustArmBody(fail), " ")
	var good bool
	if m := valueArmRe.FindStringSubmatch(vp); m != nil && vb == m[2] {
		if m[1] == "Some" {
			good = fp == "None" && fb == "return None"
		} else if e := errBindArmRe.FindStringSubmatch(fp); e != nil {
			x := e[1]
			good = fb == "return Err ( "+x+" )" || fb == "return Err ( "+x+" . into ( ) )" || fb == "return Err ( From :: from ( "+x+" ) )"
		}
	}
	if !good {
		return finding{}, false
	}
	scrut := s.codeText(i+1, open)
	if open-i > 2 && s.hasTopLevel(i+1, open, "as") || strings.ContainsAny(scrut, "+-*/|&<>=") && s.chainEnd(i+1) != open {
		scrut = "(" + scrut + ")"
	}
	return finding{span: s.tokSpan(i, cl+1), detail: "`" + s.codeText(i, cl+1) + "` -> `" + scrut + "?`"}, true
}

func (s *source) rustIfQuestion(i int) (finding, bool) {
	end := s.chainEnd(i + 1)
	if end <= i+4 || !s.seq(end-3, "(", ")", "{") && !s.seq(end-2, "(", ")") {
		return finding{}, false
	}
	open := end
	if s.tok(open) != "{" {
		return finding{}, false
	}
	method := s.tok(end - 3)
	recvEnd := end - 4
	if s.tok(recvEnd) != "." {
		return finding{}, false
	}
	cl := s.matchClose(s.code, open)
	if cl >= len(s.code) || s.tok(cl+1) == "else" {
		return finding{}, false
	}
	recv := s.codeText(i+1, recvEnd)
	body := s.codeText(open+1, cl)
	switch {
	case method == "is_none" && (body == "return None;" || body == "return None"):
	case method == "is_err" && (body == "return "+recv+";" || body == "return "+recv):
	default:
		return finding{}, false
	}
	repl := recv + "?;"
	if method == "is_none" && s.tok(recvEnd-1) != ")" {
		repl = recv + ".as_ref()?;"
	}
	return finding{span: s.tokSpan(i, cl+1), detail: "`" + s.codeText(i, cl+1) + "` -> `" + repl + "`"}, true
}

func checkRustPrintlnEmpty(s *source) []finding {
	var out []finding
	for i := range s.code {
		cl, ok := s.rustMacroCall(i, "println", "eprintln", "writeln")
		if !ok {
			continue
		}
		name := s.tok(i)
		switch {
		case name != "writeln" && cl == i+4 && s.tok(i+3) == `""`:
			out = append(out, s.rustFinding(i, cl+1, name+"!()"))
		case name == "writeln" && s.tok(cl-1) == `""` && s.tok(cl-2) == ",":
			out = append(out, s.rustFinding(i, cl+1, name+"!("+s.codeText(i+3, cl-2)+")"))
		}
	}
	return out
}

// checkRustExpectEmpty flags `.expect("")` and messages that are all
// whitespace.
func checkRustExpectEmpty(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, ".", "expect", "(") || s.code[i+3].Kind != code.TokString || s.tok(i+4) != ")" {
			continue
		}
		if strings.TrimSpace(stringContent(s.tok(i+3))) == "" {
			out = append(out, finding{span: s.tokSpan(i, i+5), detail: "`" + s.codeText(i, i+5) + "`"})
		}
	}
	return out
}

// rustFormatArg maps the format macros to the index of their format string
// argument.
var rustFormatArg = map[string]int{
	"format": 0, "print": 0, "println": 0, "eprint": 0, "eprintln": 0, "panic": 0, "format_args": 0,
	"unreachable": 0, "todo": 0, "unimplemented": 0, "write": 1, "writeln": 1, "assert": 1,
	"debug_assert": 1, "assert_eq": 2, "assert_ne": 2, "debug_assert_eq": 2, "debug_assert_ne": 2,
}

// rustHole is a placeholder in a format string. arg is its explicit
// position or name, or "" for the next positional argument.
type rustHole struct{ arg, spec string }

// rustStringContent returns the text between the quotes of a string literal
// (b"", c"", r#""#), and whether it's a string literal at all.
func rustStringContent(lit string) (string, bool) {
	lit = strings.TrimLeft(lit, "bcr")
	hashes := len(lit) - len(strings.TrimLeft(lit, "#"))
	lit = lit[hashes:]
	if len(lit) < 2+hashes || lit[0] != '"' {
		return "", false
	}
	lit = strings.TrimSuffix(lit, strings.Repeat("#", hashes))
	if !strings.HasSuffix(lit, `"`) || len(lit) < 2 {
		return "", false
	}
	return lit[1 : len(lit)-1], true
}

// rustHoles parses the placeholders of a format string: {}, {:?}, {name},
// {0:>8.2}, with {{ and }} as escapes. ok is false if the string is
// malformed.
func rustHoles(content string) (holes []rustHole, ok bool) {
	for k := 0; k < len(content); k++ {
		switch content[k] {
		case '{':
			if k+1 < len(content) && content[k+1] == '{' {
				k++
				continue
			}
			e := strings.IndexByte(content[k:], '}')
			if e < 0 {
				return nil, false
			}
			inner := content[k+1 : k+e]
			arg, spec, _ := strings.Cut(inner, ":")
			holes = append(holes, rustHole{arg: strings.TrimSpace(arg), spec: spec})
			k += e
		case '}':
			if k+1 < len(content) && content[k+1] == '}' {
				k++
			}
		}
	}
	return holes, true
}

// rustFormatCall returns the format string and the arguments after it for
// the format macro named by code token i.
func (s *source) rustFormatCall(i int) (lit int, args [][2]int, cl int, ok bool) {
	idx, known := rustFormatArg[s.tok(i)]
	if !known {
		return 0, nil, 0, false
	}
	cl, ok = s.rustMacroCall(i)
	if !ok || s.tok(i+2) == "{" {
		return 0, nil, 0, false
	}
	all, _ := s.callArgs(i + 2)
	if idx >= len(all) || all[idx][1]-all[idx][0] != 1 || s.code[all[idx][0]].Kind != code.TokString {
		return 0, nil, 0, false
	}
	return all[idx][0], all[idx+1:], cl, true
}

// rustFormatMacros flags format! calls whose format string has no
// placeholder. Those only copy a literal into a String.
func (s *source) rustFormatMacros() []finding {
	var out []finding
	for i := range s.code {
		if s.tok(i) != "format" {
			continue
		}
		lit, args, cl, ok := s.rustFormatCall(i)
		if !ok || len(args) > 0 {
			continue
		}
		content, ok := rustStringContent(s.tok(lit))
		holes, parsed := rustHoles(content)
		if !ok || !parsed || len(holes) > 0 {
			continue
		}
		plain := unescapeFormat(s.tok(lit), true)
		out = append(out, s.rustFinding(i, cl+1, plain+".to_string()"))
	}
	return out
}

func checkRustToStringInFormatArgs(s *source) []finding {
	var out []finding
	for i := range s.code {
		lit, args, _, ok := s.rustFormatCall(i)
		if !ok || len(args) == 0 {
			continue
		}
		content, ok := rustStringContent(s.tok(lit))
		holes, parsed := rustHoles(content)
		if !ok || !parsed || strings.ContainsAny(content, "$*") {
			continue
		}
		// specs collects the format specs of the holes for each argument,
		// keyed by position and name.
		specs := map[string][]string{}
		next := 0
		for _, h := range holes {
			key := h.arg
			if key == "" {
				key = strconv.Itoa(next)
				next++
			}
			specs[key] = append(specs[key], h.spec)
		}
		for n, a := range args {
			key, from := strconv.Itoa(n), a[0]
			if s.code[from].Kind == code.TokIdent && s.tok(from+1) == "=" {
				key, from = s.tok(from), from+2
			}
			to := a[1]
			if to-from < 5 || !s.seq(to-4, ".", "to_string", "(", ")") || s.chainStart(to-4) != from {
				continue
			}
			uses := specs[key]
			plain := len(uses) > 0
			for _, sp := range uses {
				plain = plain && sp == ""
			}
			if plain {
				out = append(out, s.rustFinding(from, to, s.codeText(from, to-4)))
			}
		}
	}
	return out
}

func checkRustFormatPushString(s *source) []finding {
	var out []finding
	for i := range s.code {
		switch {
		case s.seq(i, "+=", "&", "format", "!", "("):
			start := s.chainStart(i)
			cl := s.matchClose(s.code, i+4)
			if start == i || cl >= len(s.code) {
				continue
			}
			out = append(out, s.rustFinding(start, cl+1, "write!("+s.codeText(start, i)+", "+s.codeText(i+5, cl)+")"))
		case s.seq(i, ".", "push_str", "(", "&", "format", "!", "("):
			start := s.chainStart(i)
			inner := s.matchClose(s.code, i+6)
			if start == i || inner+1 >= len(s.code) || s.tok(inner+1) != ")" {
				continue
			}
			out = append(out, s.rustFinding(start, inner+2, "write!("+s.codeText(start, i)+", "+s.codeText(i+7, inner)+")"))
		}
	}
	return out
}

func checkRustCharsNextCmp(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, ".", "chars", "(", ")", ".") {
			continue
		}
		var method string
		switch {
		case s.seq(i+5, "next", "(", ")"):
			method = "starts_with"
		case s.seq(i+5, "last", "(", ")"), s.seq(i+5, "next_back", "(", ")"):
			method = "ends_with"
		default:
			continue
		}
		op := i + 8
		if s.tok(op) != "==" && s.tok(op) != "!=" || !s.seq(op+1, "Some", "(") {
			continue
		}
		cl := s.matchClose(s.code, op+2)
		start := s.chainStart(i)
		if start == i || cl >= len(s.code) || cl != op+4 || !rustExprStarts[s.tok(start-1)] || !rustExprEnds[s.tok(cl+1)] {
			continue
		}
		repl := rustNot(s.tok(op) == "!=") + s.codeText(start, i) + "." + method + "(" + s.tok(op+3) + ")"
		out = append(out, s.rustFinding(start, cl+1, repl))
	}
	return out
}

func checkRustDbgMacro(s *source) []finding {
	if s.rustTestCode() {
		return nil
	}
	tests := s.rustTestSpans()
	var out []finding
	for i := range s.code {
		cl, ok := s.rustMacroCall(i, "dbg")
		if !ok || inSpans(tests, s.code[i].Span.Start) {
			continue
		}
		out = append(out, s.rustFinding(i, cl+1, s.codeText(i+3, cl)))
	}
	return out
}

var (
	safetyRe        = regexp.MustCompile(`(?i)\bsafety\b`)
	safetyHeadingRe = regexp.MustCompile(`(?m)^\s*#+\s*Safety\b`)
	rustTestWordRe  = regexp.MustCompile(`\btest\b`)
	valueArmRe      = regexp.MustCompile(`^(Ok|Some) \( (\w+) \)$`)
	errBindArmRe    = regexp.MustCompile(`^Err \( (\w+) \)$`)
)

// rustCommentAbove reports whether re matches the comment group that ends on
// the line above line, or a comment on line itself before off.
func (s *source) rustCommentAbove(line, off int, re *regexp.Regexp) bool {
	want := line - 1
	for _, c := range slices.Backward(s.comments) {
		switch {
		case c.line == line && c.Span.End <= off:
			if re.MatchString(c.text()) {
				return true
			}
		case c.end == want:
			if re.MatchString(c.text()) {
				return true
			}
			want = c.line - 1
		}
	}
	return false
}

func checkRustUndocumentedUnsafe(s *source) []finding {
	macros := s.rustMacroBodies()
	var out []finding
	for i, t := range s.code {
		if s.tok(i) != "unsafe" || t.Kind != code.TokKeyword || inSpans(macros, t.Span.Start) {
			continue
		}
		next := s.tok(i + 1)
		if next != "{" && next != "impl" {
			continue
		}
		stmt := i
		for depth := 0; stmt > 0; stmt-- {
			p := s.tok(stmt - 1)
			if p == ")" || p == "]" {
				depth++
			} else if p == "(" || p == "[" {
				if depth == 0 {
					break
				}
				depth--
			}
			if depth == 0 && (p == ";" || p == "{" || p == "}") {
				break
			}
		}
		if s.rustCommentAbove(s.line(t.Span.Start), t.Span.Start, safetyRe) || s.rustCommentAbove(s.line(s.code[stmt].Span.Start), s.code[stmt].Span.Start, safetyRe) {
			continue
		}
		end := i + 2
		if next == "{" {
			end = i + 1
		}
		out = append(out, finding{span: s.tokSpan(i, end)})
	}
	return out
}

func checkRustMissingSafetyDoc(s *source) []finding {
	traitImpls := s.rustTraitImpls()
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() || d.Local || inSpans(traitImpls, d.Span.Start) {
			continue
		}
		head := s.tokensIn(code.Span{Start: d.Span.Start, End: d.NameSpan.Start})
		pub, unsafe := false, false
		for k, t := range head {
			switch s.text(t) {
			case "pub":
				pub = k+1 >= len(head) || s.text(head[k+1]) != "("
			case "unsafe":
				unsafe = true
			}
		}
		if !pub || !unsafe {
			continue
		}
		var doc []string
		for _, c := range s.docOf(d) {
			doc = append(doc, c.raw...)
		}
		if safetyHeadingRe.MatchString(strings.Join(doc, "\n")) {
			continue
		}
		out = append(out, finding{span: d.NameSpan})
	}
	return out
}

// rustTraitImpls returns the bodies of `impl Trait for Type` blocks. The
// trait fixes the signatures in there.
func (s *source) rustTraitImpls() []code.Span {
	var out []code.Span
	for i := range s.code {
		if s.tok(i) != "impl" || s.code[i].Kind != code.TokKeyword {
			continue
		}
		trait := false
		for j := i + 1; j < len(s.code); j++ {
			t := s.tok(j)
			if t == "for" && s.tok(j+1) != "<" {
				trait = true
			}
			if t == ";" || t == "}" {
				break
			}
			if t == "{" {
				if trait {
					cl := min(s.matchClose(s.code, j), len(s.code)-1)
					out = append(out, s.tokSpan(j, cl+1))
				}
				break
			}
		}
	}
	return out
}

func rustResultType(t string) bool {
	head, _, _ := strings.Cut(t, "<")
	head = strings.TrimSpace(head)
	return head == "Result" || strings.HasSuffix(head, "::Result")
}

var rustLockCalls = setOf("lock", "read", "write", "join", "try_lock")

func checkRustUnwrapInResult(s *source) []finding {
	if s.rustTestCode() {
		return nil
	}
	tests := s.rustTestSpans()
	var out []finding
	for _, d := range s.f.Decls {
		if !d.Callable() || d.Body.Empty() || !rustResultType(d.Type) || inSpans(tests, d.Span.Start) {
			continue
		}
		body := s.codeIndex(d.Body.Start)
		for i := body; i < len(s.code) && s.code[i].Span.End <= d.Body.End; i++ {
			if !s.seq(i, ".", "unwrap", "(", ")") || s.scopeOf(s.code[i].Span.Start) != d || s.inRustClosure(i, body) {
				continue
			}
			if s.tok(i-1) == ")" {
				if open := s.matchOpen(s.code, i-1); open > 0 && rustLockCalls[s.tok(open-1)] {
					continue
				}
			}
			start := s.chainStart(i)
			out = append(out, finding{span: s.tokSpan(i, i+4), detail: "`" + s.codeText(start, i+4) + "` -> `" + s.codeText(start, i) + "?`"})
		}
	}
	return out
}

var rustClosureStarts = setOf("", "(", ",", "=", "move", "{", ";", "=>", "return", "[")

func (s *source) inRustClosure(i, from int) bool {
	depth := 0
	for j := i - 1; j > from; j-- {
		switch t := s.tok(j); t {
		case ")", "]", "}":
			depth++
		case "(", "[", "{":
			if depth > 0 {
				depth--
				continue
			}
			if t == "{" && (s.tok(j-1) == "|" || s.tok(j-1) == "||") {
				return true
			}
		case "||":
			if depth == 0 && rustClosureStarts[s.tok(j-1)] {
				return true
			}
		case "|":
			if depth > 0 {
				continue
			}
			k := j - 1
			for k > from && s.tok(k) != "|" && s.tok(k) != ";" && s.tok(k) != "{" {
				k--
			}
			if s.tok(k) == "|" && rustClosureStarts[s.tok(k-1)] {
				return true
			}
		}
	}
	return false
}

var ptrArgRe = regexp.MustCompile(`^&(?:'\w+\s*)?(String|Vec<(.+)>|PathBuf|Box<(.+)>)$`)

func checkRustPtrArg(s *source) []finding {
	traitImpls := s.rustTraitImpls()
	var out []finding
	for _, d := range s.f.Decls {
		if d.Kind != code.KindParameter || d.Parent == nil || !d.Parent.Callable() || d.Parent.Body.Empty() {
			continue
		}
		if p := d.Parent.Parent; p != nil && p.Kind == code.KindInterface || inSpans(traitImpls, d.Span.Start) {
			continue
		}
		m := ptrArgRe.FindStringSubmatch(d.Type)
		if m == nil {
			continue
		}
		var repl string
		switch {
		case m[1] == "String":
			repl = "&str"
		case m[1] == "PathBuf":
			repl = "&Path"
		case m[2] != "":
			repl = "&[" + m[2] + "]"
		default:
			repl = "&" + m[3]
		}
		out = append(out, finding{span: d.Span, detail: "`" + d.Name + ": " + d.Type + "` -> `" + d.Name + ": " + repl + "`"})
	}
	return out
}

var rustDefaultCallRe = regexp.MustCompile(`^\w+::default\(\)$`)

func checkRustMemReplace(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, "mem", "::", "replace", "(") {
			continue
		}
		start := i
		if s.tok(i-1) == "::" && (s.tok(i-2) == "std" || s.tok(i-2) == "core") {
			start = i - 2
		}
		args, cl := s.callArgs(i + 3)
		if cl >= len(s.code) || len(args) != 2 {
			continue
		}
		dest := s.codeText(args[0][0], args[0][1])
		val := strings.ReplaceAll(s.codeText(args[1][0], args[1][1]), " ", "")
		switch {
		case val == "None":
			// Method calls dereference, so `&mut *o` and `o` both take.
			recv := strings.TrimPrefix(strings.TrimPrefix(dest, "&mut "), "*")
			out = append(out, s.rustFinding(start, cl+1, recv+".take()"))
		case rustEmptyCtors[val] && strings.HasSuffix(val, "()") || rustDefaultCallRe.MatchString(val):
			out = append(out, s.rustFinding(start, cl+1, strings.TrimSuffix(s.codeText(start, i+3), "replace")+"take("+dest+")"))
		}
	}
	return out
}

var rustNeededCrates = setOf("alloc", "std", "self", "test")

func checkRustExternCrate(s *source) []finding {
	ed := rust.Edition(s.f.Abs)
	if ed == "" || ed == "2015" {
		return nil
	}
	var out []finding
	for i := range s.code {
		if !s.seq(i, "extern", "crate") || rustNeededCrates[s.tok(i+2)] {
			continue
		}
		end := i + 2
		for end < len(s.code) && s.tok(end) != ";" {
			end++
		}
		if end >= len(s.code) {
			continue
		}
		repl := "remove it"
		if s.tok(i+3) == "as" {
			repl = "use " + s.codeText(i+2, end) + ";"
		}
		out = append(out, finding{span: s.tokSpan(i, end+1), detail: "`" + s.codeText(i, end+1) + "` -> " + strings.Replace("`"+repl+"`", "`remove it`", "remove it", 1)})
	}
	return out
}

var rustItemKeywords = setOf("struct", "enum", "union", "trait", "impl", "mod", "fn", "for", "in", "match", "if", "while", "!", "=")

func checkRustRedundantFieldNames(s *source) []finding {
	var out []finding
	for i := range s.code {
		if s.code[i].Kind != code.TokIdent || s.tok(i+1) != ":" || s.tok(i+2) != s.tok(i) || s.tok(i+3) != "," && s.tok(i+3) != "}" {
			continue
		}
		if p := s.tok(i - 1); p != "{" && p != "," {
			continue
		}
		if c := s.tok(i)[0]; c < 'a' || c > 'z' {
			continue
		}
		var open int
		for open = range s.enclosingBraces(i) {
			break
		}
		name := s.tok(open - 1)
		if open == 0 || name != "Self" && (s.code[open-1].Kind != code.TokIdent || name[0] < 'A' || name[0] > 'Z') || rustItemKeywords[s.tok(open-2)] {
			continue
		}
		out = append(out, s.rustFinding(i, i+3, s.tok(i)))
	}
	return out
}

// rustIfEnd returns the index after the if expression whose keyword is code
// token i, including its else branches, or -1 if it can't tell.
func (s *source) rustIfEnd(i int) int {
	depth := 0
	for k := i + 1; k < len(s.code); k++ {
		switch s.tok(k) {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		case "let":
			if depth == 0 {
				return -1
			}
		case ";", "}":
			if depth == 0 {
				return -1
			}
		case "{":
			if depth != 0 {
				continue
			}
			cl := s.matchClose(s.code, k)
			if cl >= len(s.code) {
				return -1
			}
			if s.tok(cl+1) != "else" {
				return cl + 1
			}
			if s.tok(cl+2) == "if" {
				return s.rustIfEnd(cl + 2)
			}
			if s.tok(cl+2) == "{" {
				if e := s.matchClose(s.code, cl+2); e < len(s.code) {
					return e + 1
				}
			}
			return -1
		}
	}
	return -1
}

func checkRustCollapsibleElseIf(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.seq(i, "else", "{", "if") {
			continue
		}
		cl := s.matchClose(s.code, i+1)
		if cl >= len(s.code) || s.rustIfEnd(i+2) != cl || len(s.commentsIn(s.tokSpan(i+1, cl+1))) > 0 {
			continue
		}
		head := s.codeText(i+2, i+3)
		out = append(out, finding{span: s.tokSpan(i, i+3), detail: "`else { " + head + " .. }` -> `else " + head + " ..`"})
	}
	return out
}

func checkRustSingleMatch(s *source) []finding {
	var out []finding
	for i := range s.code {
		if s.tok(i) != "match" || s.code[i].Kind != code.TokKeyword {
			continue
		}
		open, cl, ok := s.rustMatch(i)
		if !ok {
			continue
		}
		arms := s.rustArms(open, cl)
		if len(arms) != 2 || s.armPattern(arms[1]) != "_" || s.hasTopLevel(arms[0].pat, arms[0].arrow, "if") || s.armPattern(arms[0]) == "_" {
			continue
		}
		if b := strings.Join(s.rustArmBody(arms[1]), ""); b != "" && b != "()" {
			continue
		}
		if len(s.commentsIn(s.tokSpan(arms[1].pat, arms[1].end))) > 0 {
			continue
		}
		pat, scrut := s.codeText(arms[0].pat, arms[0].arrow), s.codeText(i+1, open)
		out = append(out, finding{span: s.tokSpan(i, open), detail: "`match " + scrut + " { " + pat + " => .., _ => {} }` -> `if let " + pat + " = " + scrut + " { .. }`"})
	}
	return out
}

func checkRustRedundantClosure(s *source) []finding {
	var out []finding
	for i := range s.code {
		if s.tok(i) != "|" || s.code[min(i+1, len(s.code)-1)].Kind != code.TokIdent || s.tok(i+2) != "|" {
			continue
		}
		if p := s.tok(i - 1); p != "(" && p != "," {
			continue
		}
		param := s.tok(i + 1)
		j := i + 3
		for s.code[min(j, len(s.code)-1)].Kind == code.TokIdent && s.tok(j+1) == "::" {
			j += 2
		}
		if s.code[min(j, len(s.code)-1)].Kind != code.TokIdent || !s.seq(j+1, "(", param, ")") || j == i+3 && s.tok(j) == param {
			continue
		}
		if n := s.tok(j + 4); n != ")" && n != "," {
			continue
		}
		out = append(out, s.rustFinding(i, j+4, s.codeText(i+3, j+1)))
	}
	return out
}

// dropRustModuleDoc removes the inner doc comments (//! and /*! */) from a
// file header. They document the crate or module, which is their job in
// lib.rs, main.rs and mod.rs at any length, and elsewhere up to
// goPackageDocLines lines. Past that limit we return the whole header.
func (s *source) dropRustModuleDoc(header []*comment) []*comment {
	var keep []*comment
	lines := 0
	for _, c := range header {
		text := s.f.Text(c.Span)
		if strings.HasPrefix(text, "//!") || strings.HasPrefix(text, "/*!") {
			lines += c.contentLines()
			continue
		}
		keep = append(keep, c)
	}
	switch path.Base(s.f.Path) {
	case "lib.rs", "main.rs", "mod.rs":
		return keep
	}
	if lines > goPackageDocLines {
		return header
	}
	return keep
}

// rustSuppressed flags an #[allow(...)] or #![allow(...)] attribute at code
// token i if it has no reason = "..." argument.
func (s *source) rustSuppressed(i int) (finding, bool) {
	open := i + 1
	if s.tok(open) == "!" {
		open++
	}
	if s.tok(open) != "[" {
		return finding{}, false
	}
	cl := s.matchClose(s.code, open)
	if cl >= len(s.code) {
		return finding{}, false
	}
	allow, reason := false, false
	for k := open + 1; k < cl; k++ {
		switch s.tok(k) {
		case "allow":
			allow = allow || s.tok(k+1) == "("
		case "reason":
			reason = true
		}
	}
	if !allow || reason || s.commentJustifies(s.line(s.code[i].Span.Start)) {
		return finding{}, false
	}
	return finding{span: s.tokSpan(i, cl+1)}, true
}

// rustLogMacros print or log. Calling them doesn't count as handling an
// error.
var rustLogMacros = setOf("println", "eprintln", "print", "eprint", "dbg", "trace", "debug", "info", "warn", "error", "log")

// rustOnlyLogs reports whether the statements [from, to) are empty, `()` or
// log macro calls.
func (s *source) rustOnlyLogs(from, to int) bool {
	for _, st := range s.splitStatements(s.code[from:to]) {
		if len(st) == 0 || len(st) == 2 && s.text(st[0]) == "(" && s.text(st[1]) == ")" {
			continue
		}
		k := 0
		for k+2 < len(st) && st[k].Kind == code.TokIdent && s.text(st[k+1]) == "::" {
			k += 2
		}
		if k+2 < len(st) && rustLogMacros[s.text(st[k])] && s.text(st[k+1]) == "!" && s.matchClose(st, k+2) == len(st)-1 {
			continue
		}
		return false
	}
	return true
}

// swallowedRust flags code that drops an error: `let _ = call();`, a
// `.ok();` statement, and an `Err(_)` arm or `if let Err(_)` with an empty
// or logging body. Rust has no exceptions, so these patterns stand in for a
// swallowed exception.
func (s *source) swallowedRust() []finding {
	if s.rustTestCode() {
		return nil
	}
	tests := s.rustTestSpans()
	var out []finding
	add := func(i, j int) {
		sp := s.tokSpan(i, j)
		if inSpans(tests, sp.Start) || s.commentJustifies(s.line(sp.Start)) || justified(s.commentsIn(sp)) {
			return
		}
		out = append(out, finding{span: sp})
	}
	for i := range s.code {
		switch {
		case s.seq(i, "let", "_", "=") && rustStmtEnds[s.tok(i-1)]:
			end := i + 3
			for depth := 0; end < len(s.code); end++ {
				t := s.tok(end)
				if t == "(" || t == "[" || t == "{" {
					depth++
				} else if t == ")" || t == "]" || t == "}" {
					depth--
				}
				if depth <= 0 && t == ";" || depth < 0 {
					break
				}
			}
			if s.tok(end) != ";" || s.tok(end-1) != ")" || s.tok(i+3) == "drop" {
				continue
			}
			// The parentheses have to hold the arguments of a call or macro.
			if open := s.matchOpen(s.code, end-1); open <= i+3 || s.code[open-1].Kind != code.TokIdent && s.tok(open-1) != "!" && s.tok(open-1) != ">" {
				continue
			}
			if (s.tok(i+3) == "write" || s.tok(i+3) == "writeln") && s.tok(i+4) == "!" && s.rustStringVar(s.tok(i+6), s.code[i].Span.Start) {
				continue
			}
			add(i, end+1)
		case s.seq(i, ".", "ok", "(", ")", ";"):
			start := s.chainStart(i)
			if start == i || !rustStmtEnds[s.tok(start-1)] {
				continue
			}
			add(start, i+5)
		case s.seq(i, "if", "let", "Err", "(") && s.tok(i+5) == ")" && s.tok(i+6) == "=":
			open := i + 7
			for open < len(s.code) && s.tok(open) != "{" && s.tok(open) != ";" {
				open++
			}
			if s.tok(open) != "{" {
				continue
			}
			cl := s.matchClose(s.code, open)
			if cl >= len(s.code) || s.tok(cl+1) == "else" || !s.rustOnlyLogs(open+1, cl) {
				continue
			}
			add(i, cl+1)
		case s.tok(i) == "match" && s.code[i].Kind == code.TokKeyword:
			open, cl, ok := s.rustMatch(i)
			if !ok {
				continue
			}
			for _, a := range s.rustArms(open, cl) {
				if !errBindArmRe.MatchString(s.armPattern(a)) {
					continue
				}
				from, to := a.arrow+1, a.end
				if s.tok(from) == "{" {
					from, to = from+1, to-1
				}
				if s.rustOnlyLogs(from, to) {
					add(a.pat, a.end)
				}
			}
		}
	}
	return out
}

var rustControl = setOf("if", "while", "for", "match", "loop")

// rustControlHeader reports whether the brace at toks[i] opens the block of
// an if, while, for, match or loop expression, and returns the index of the
// keyword. Rust conditions have no parentheses, so the keyword is the
// nearest one before the brace in the same expression.
func (s *source) rustControlHeader(toks []code.Token, i int) (int, bool) {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		switch s.text(toks[j]) {
		case ")", "]":
			depth++
		case "(", "[":
			depth--
		case "}":
			if depth == 0 {
				return 0, false
			}
			depth++
		case "{":
			if depth == 0 {
				return 0, false
			}
			depth--
		case ";", "=>", "=", "|", "||":
			if depth == 0 {
				return 0, false
			}
		}
		if depth < 0 {
			return 0, false
		}
		if depth == 0 && toks[j].Kind == code.TokKeyword && rustControl[s.text(toks[j])] {
			return j, true
		}
	}
	return 0, false
}
