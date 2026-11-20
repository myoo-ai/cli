package codelint

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/myoo-ai/cli/internal/code"
)

var langsPHP = []code.Lang{code.PHP}

var phpRules = []*Rule{
	{
		ID:             "php-long-array-syntax",
		Langs:          langsPHP,
		Description:    "prefer the short array syntax `[...]` over `array(...)`, as it is shorter and the style every current PHP standard uses.",
		Recommendation: "replace `array(` with `[` and its closing parenthesis with `]`. both forms build the same array.",
		check:          checkPhpLongArray,
	},
	{
		ID:             "php-loose-equality",
		Langs:          langsPHP,
		Description:    "use `===` and `!==` instead of `==` and `!=`, as loose comparison juggles types in surprising ways.",
		Recommendation: "compare with `===` or `!==`. `\"abc\" == 0` was true before PHP 8 and `\"1e3\" == \"1000\"` still is. when one side may hold a number or a numeric string, convert it explicitly first. comparisons with null and of 2 literals are allowed.",
		check:          checkPhpLooseEquality,
	},
	{
		ID:             "php-str-contains",
		Langs:          langsPHP,
		Description:    "prefer `str_contains()` over comparing the result of `strpos()` with false, as it states the intent.",
		Recommendation: "call `str_contains($haystack, $needle)`, negated for `=== false`. it needs PHP 8.0, or symfony/polyfill-php80. calls with an offset argument are not reported.",
		check:          checkPhpStrContains,
	},
	{
		ID:             "php-str-starts-with",
		Langs:          langsPHP,
		Description:    "prefer `str_starts_with()` over `strpos(...) === 0` or comparing a `substr()` prefix, as it states the intent and does not scan the whole string.",
		Recommendation: "call `str_starts_with($haystack, $prefix)`, negated for `!==`. it needs PHP 8.0. `substr()` comparisons are only reported when the length matches the literal.",
		check:          func(s *source) []finding { return s.phpStrAffix(false) },
	},
	{
		ID:             "php-str-ends-with",
		Langs:          langsPHP,
		Description:    "prefer `str_ends_with()` over comparing a `substr()` suffix, as it states the intent.",
		Recommendation: "call `str_ends_with($haystack, $suffix)`, negated for `!==`. it needs PHP 8.0. comparisons are only reported when the length matches the literal.",
		check:          func(s *source) []finding { return s.phpStrAffix(true) },
	},
	{
		ID:             "php-null-coalescing",
		Langs:          langsPHP,
		Description:    "prefer the null coalescing operator `??` over `isset($a) ? $a : $b`, as it repeats nothing.",
		Recommendation: "write `$a ?? $b`. it behaves like the isset() ternary and gives `$b` for undefined variables and array keys too.",
		check:          checkPhpNullCoalescing,
	},
	{
		ID:             "php-null-coalescing-assignment",
		Langs:          langsPHP,
		Description:    "prefer `??=` over `$a = $a ?? $b`, as it names the target once.",
		Recommendation: "write `$a ??= $b`. it needs PHP 7.4 and assigns only when `$a` is null or undefined.",
		check:          checkPhpNullCoalescingAssignment,
	},
	{
		ID:             "php-dir-constant",
		Langs:          langsPHP,
		Description:    "prefer `__DIR__` over `dirname(__FILE__)`, as the constant is resolved at compile time.",
		Recommendation: "replace the call with `__DIR__`, which holds the same directory.",
		check:          checkPhpDirConstant,
	},
	{
		ID:             "php-get-class",
		Langs:          langsPHP,
		Description:    "prefer `::class` over `get_class()`, as it is shorter and `get_class()` without arguments is deprecated.",
		Recommendation: "write `static::class` for `get_class($this)`, `self::class` for `get_class()` and `$object::class` for `get_class($object)`. `$object::class` needs PHP 8.0, and `get_class()` without arguments is deprecated since PHP 8.3.",
		check:          checkPhpGetClass,
	},
	{
		ID:             "php-alias-function",
		Langs:          langsPHP,
		Description:    "prefer the canonical function over its alias, as aliases such as `sizeof` and `join` hide the function readers know.",
		Recommendation: "call the canonical function with the same arguments, for example `count` instead of `sizeof` and `implode` instead of `join`. the alias is the same function under another name.",
		check:          checkPhpAliasFunction,
	},
	{
		ID:             "php-error-suppression",
		Langs:          langsPHP,
		Description:    "avoid the `@` error control operator, as it hides warnings that explain later failures.",
		Recommendation: "remove the `@` and handle the failure. check the return value, test the condition first (for example `is_file()`), or convert warnings to exceptions with an error handler. errors and exceptions thrown inside are not suppressed anyway.",
		check:          checkPhpErrorSuppression,
	},
	{
		ID:             "php-debug-leftover",
		Langs:          langsPHP,
		Description:    "remove this debug output, as `var_dump`, `print_r`, `dump` and `dd` calls are leftovers from debugging.",
		Recommendation: "delete the call, or log the value through the application logger. `print_r` and `var_export` with `true` as the second argument return a string and are not reported.",
		check:          checkPhpDebugLeftover,
	},
	{
		ID:             "php-closing-tag",
		Langs:          langsPHP,
		Description:    "omit the closing `?>` tag at the end of a file that only holds PHP code, as any white space after it is sent as output.",
		Recommendation: "delete the closing tag. PER Coding Style and PSR-12 require omitting it, and a stray newline after it can break headers and binary output.",
		check:          checkPhpClosingTag,
	},
	{
		ID:             "php-strict-types",
		Langs:          langsPHP,
		Description:    "declare `strict_types=1` in a file that declares functions or classes, as without it scalar arguments are silently coerced.",
		Recommendation: "add `declare(strict_types=1);` as the first statement. it applies to the calls made from this file, which then throw a TypeError instead of converting `\"5 apples\"` to an int. run the tests after adding it.",
		check:          checkPhpStrictTypes,
	},
	{
		ID:             "php-nullsafe-operator",
		Langs:          langsPHP,
		Description:    "prefer the nullsafe operator `?->` over a null check in a ternary, as it is shorter and evaluates the object once.",
		Recommendation: "write `$a?->b`. it needs PHP 8.0 and short-circuits the rest of the chain when `$a` is null. checks of an expression with a call are not reported, since the ternary evaluates the call twice.",
		check:          checkPhpNullsafe,
	},
	{
		ID:             "php-switch-to-match",
		Langs:          langsPHP,
		Description:    "prefer a `match` expression to a `switch` whose cases only return or assign a value, as `match` has no fallthrough and yields the value directly.",
		Recommendation: "return or assign a `match` expression with one arm per case, for example `case 1: return 'a';` becomes `1 => 'a',`. `match` compares with `===` where `switch` uses `==`, so check that the subject and the case values have the same type. only switches with a default case are reported, because `match` throws an `UnhandledMatchError` when no arm matches.",
		check:          checkPhpSwitchToMatch,
	},
	{
		ID:             "php-call-user-func",
		Langs:          langsPHP,
		Description:    "prefer calling a callable directly over `call_user_func()`, as `$f(...)` is shorter and faster.",
		Recommendation: "write `$f($a, $b)` for `call_user_func($f, $a, $b)` and `$f(...$args)` for `call_user_func_array($f, $args)`. string keys in `$args` become named arguments in both forms on PHP 8.",
		check:          checkPhpCallUserFunc,
	},
	{
		ID:             "php-list-syntax",
		Langs:          langsPHP,
		Description:    "prefer the short destructuring syntax `[...]` over `list(...)`, as it matches the array syntax.",
		Recommendation: "replace `list(` with `[` and its closing parenthesis with `]`. it needs PHP 7.1, also for keyed destructuring.",
		check:          checkPhpListSyntax,
	},
	{
		ID:             "php-extract",
		Langs:          langsPHP,
		Description:    "avoid `extract()`, as it creates variables that neither readers nor static analysis can see and may overwrite existing ones.",
		Recommendation: "read the values from the array explicitly, for example `['name' => $name] = $data;` or `$name = $data['name'];`. for templates, pass the array and access its keys.",
		check:          checkPhpExtract,
	},
	{
		ID:             "php-removed-function",
		Langs:          langsPHP,
		Description:    "replace this function, as current PHP versions removed it.",
		Recommendation: "use the replacement the detail names, for example a closure for `create_function`, `foreach` for `each`, `preg_*` for the `ereg` family, PDO or mysqli for `mysql_*` and `NumberFormatter` for `money_format`.",
		check:          checkPhpRemovedFunction,
	},
	{
		ID:             "php-cast-function",
		Langs:          langsPHP,
		Description:    "prefer a cast over `intval()`, `floatval()`, `strval()` and `boolval()` with one argument, as the cast is shorter and avoids a function call.",
		Recommendation: "write `(int) $x`, `(float) $x`, `(string) $x` or `(bool) $x`. `intval()` with a base argument has no cast and is not reported.",
		check:          checkPhpCastFunction,
	},
	{
		ID:             "php-is-null",
		Langs:          langsPHP,
		Description:    "prefer `=== null` over `is_null()`, as the comparison is shorter and needs no function call.",
		Recommendation: "write `$x === null`, or `$x !== null` for `!is_null($x)`.",
		check:          checkPhpIsNull,
	},
	{
		ID:             "php-array-push",
		Langs:          langsPHP,
		Description:    "prefer `$a[] = $x` over `array_push($a, $x)` for a single element, as it avoids a function call.",
		Recommendation: "write `$array[] = $value;`. keep `array_push` for several values or where its return value is used.",
		check:          checkPhpArrayPush,
	},
	{
		ID:             "php-pow-function",
		Langs:          langsPHP,
		Description:    "prefer the `**` operator over `pow()`, as it reads like the math it does.",
		Recommendation: "write `$a ** $b`, with parentheses around operands that are not a single value: `pow(-2, 2)` becomes `(-2) ** 2`.",
		check:          checkPhpPow,
	},
}

var (
	// If a comparison operand follows one of these tokens, it starts an
	// expression, so no tighter operator can bind to it.
	phpExprStarts = setOf("", "(", "[", "{", "}", ",", ";", "=", "&&", "||", "?", ":", "??", "=>", "return", "and", "or", "xor",
		"echo", "print", "<?php", "<?=", "?>", "yield", "throw")
	// These tokens can follow a comparison operand.
	phpExprEnds = setOf("", ")", "]", "}", ",", ";", "&&", "||", "?", ":", "??", "and", "or", "xor", "?>", "=>")
	// A statement can start after these tokens.
	phpStmtStarts = setOf("", ";", "{", "}", ":", "<?php", "?>", "else")
)

// phpPreambleEnd returns the index of the first code token in a PHP file
// that isn't an open tag or part of a declare statement. For other
// languages it returns 0.
func (s *source) phpPreambleEnd() int {
	if s.f.Lang != code.PHP {
		return 0
	}
	i := 0
	for i < len(s.code) {
		switch {
		case s.code[i].Kind == code.TokPunct && strings.HasPrefix(s.tok(i), "<?"):
			i++
		case s.isPhpKeyword(i, "declare") && s.tok(i+1) == "(":
			i = s.matchClose(s.code, i+1) + 1
			if s.tok(i) == ";" {
				i++
			}
		default:
			return i
		}
	}
	return i
}

// phpNamespaceAt reports whether the PHP code token at off is a namespace
// statement. The docblock on that statement describes the file.
func (s *source) phpNamespaceAt(off int) bool {
	return s.f.Lang == code.PHP && s.isPhpKeyword(s.codeIndex(off), "namespace")
}

func (s *source) isPhpKeyword(i int, w string) bool {
	return i >= 0 && i < len(s.code) && s.code[i].Kind == code.TokKeyword && strings.EqualFold(s.tok(i), w)
}

func (s *source) phpLower(i int) string { return strings.ToLower(s.tok(i)) }

func (s *source) phpStartsExpr(i int) bool {
	before := s.phpLower(i - 1)
	return phpExprStarts[before] || isAssignOp(before)
}

func (s *source) phpEndsExpr(i int) bool { return phpExprEnds[s.phpLower(i)] }

func (s *source) phpVar(i int) bool {
	return i >= 0 && i < len(s.code) && s.code[i].Kind == code.TokIdent && strings.HasPrefix(s.tok(i), "$")
}

func (s *source) phpNull(i int) bool { return s.isPhpKeyword(i, "null") }

func (s *source) phpLiteral(i int) bool {
	t := s.code[i]
	return t.Kind == code.TokString || t.Kind == code.TokNumber || s.isPhpKeyword(i, "true") || s.isPhpKeyword(i, "false")
}

// phpFunc reports whether code token i names a global function in a call,
// `foo(` or `\foo(`. If so, it also returns the lower-case name and the
// index of the callee's first token.
func (s *source) phpFunc(i int) (string, int, bool) {
	if s.code[i].Kind != code.TokIdent || s.phpVar(i) || s.tok(i+1) != "(" {
		return "", 0, false
	}
	start := i
	switch s.phpLower(i - 1) {
	case "->", "?->", "::", "function", "fn", "new", "const":
		return "", 0, false
	case "&":
		if s.isPhpKeyword(i-2, "function") {
			return "", 0, false
		}
	case "\\":
		if i >= 2 && (s.code[i-2].Kind == code.TokIdent || s.isPhpKeyword(i-2, "namespace")) && s.code[i-2].Span.End == s.code[i-1].Span.Start {
			return "", 0, false // a namespaced function like App\count
		}
		start = i - 1
	}
	return s.phpLower(i), start, true
}

// phpChainEnd returns the index after the operand that starts at code token
// i, or i if no operand starts there. An operand is a literal, or a variable
// or name or parenthesized expression followed by any member accesses or
// calls or index expressions.
func (s *source) phpChainEnd(i int) int {
	if i >= len(s.code) {
		return i
	}
	t := s.code[i]
	var j int
	switch {
	case t.Kind == code.TokString || t.Kind == code.TokNumber || s.isPhpKeyword(i, "true") || s.isPhpKeyword(i, "false") || s.phpNull(i):
		return i + 1
	case s.tok(i) == "(":
		k := s.matchClose(s.code, i)
		if k >= len(s.code) {
			return i
		}
		j = k + 1
	case t.Kind == code.TokIdent || s.isPhpKeyword(i, "self") || s.isPhpKeyword(i, "static") || s.isPhpKeyword(i, "parent"):
		j = i + 1
		for s.tok(j) == "\\" && j+1 < len(s.code) && s.code[j+1].Kind == code.TokIdent {
			j += 2
		}
	case s.tok(i) == "\\" && i+1 < len(s.code) && s.code[i+1].Kind == code.TokIdent:
		j = i + 2
		for s.tok(j) == "\\" && j+1 < len(s.code) && s.code[j+1].Kind == code.TokIdent {
			j += 2
		}
	default:
		return i
	}
	for j < len(s.code) {
		switch s.tok(j) {
		case "->", "?->", "::":
			if j+1 < len(s.code) && (s.code[j+1].Kind == code.TokIdent || s.code[j+1].Kind == code.TokKeyword) {
				j += 2
				continue
			}
			return j
		case "(", "[":
			k := s.matchClose(s.code, j)
			if k >= len(s.code) {
				return j
			}
			j = k + 1
		default:
			return j
		}
	}
	return j
}

// phpChainStart is the mirror of phpChainEnd. It returns the index of the
// first token of the operand that ends before code token end, or end if no
// operand ends there.
func (s *source) phpChainStart(end int) int {
	j := end - 1
	for j >= 0 {
		t := s.code[j]
		switch {
		case s.tok(j) == ")" || s.tok(j) == "]":
			k := s.matchOpen(s.code, j)
			if k < 0 {
				return end
			}
			if p := s.code[max(k-1, 0)]; k > 0 && (p.Kind == code.TokIdent || s.tok(k-1) == ")" || s.tok(k-1) == "]" || s.isPhpKeyword(k-1, "self") || s.isPhpKeyword(k-1, "static")) {
				j = k - 1
				continue
			}
			if s.tok(j) == "]" {
				return end
			}
			return k
		case t.Kind == code.TokIdent || t.Kind == code.TokKeyword && s.isPhpKeywordAny(j, "self", "static", "parent"):
			switch s.tok(j - 1) {
			case "->", "?->", "::":
				j -= 2
				continue
			case "\\":
				if j >= 2 && s.code[j-2].Kind == code.TokIdent {
					j -= 2
					continue
				}
				return j - 1
			}
			return j
		case t.Kind == code.TokString || t.Kind == code.TokNumber || s.phpLiteral(j) || s.phpNull(j):
			return j
		default:
			return end
		}
	}
	return end
}

func (s *source) isPhpKeywordAny(i int, ws ...string) bool {
	for _, w := range ws {
		if s.isPhpKeyword(i, w) {
			return true
		}
	}
	return false
}

// phpInterpolates reports whether the PHP string literal lit interpolates
// a variable, i.e. it's double-quoted or a heredoc and holds `$name`, `{$`
// or `${`. Nowdocs and single-quoted literals can't interpolate.
func phpInterpolates(lit string) bool {
	switch {
	case strings.HasPrefix(lit, "<<<"):
		if strings.HasPrefix(strings.TrimLeft(lit[3:], " \t"), "'") {
			return false
		}
	case !strings.HasPrefix(lit, `"`) && !strings.HasPrefix(lit, `b"`) && !strings.HasPrefix(lit, `B"`):
		return false
	}
	for k := 0; k+1 < len(lit); k++ {
		switch c, next := lit[k], lit[k+1]; {
		case c == '\\':
			k++
		case c == '$' && (next == '{' || next == '_' || next >= 0x80 || unicode.IsLetter(rune(next))):
			return true
		case c == '{' && next == '$':
			return true
		}
	}
	return false
}

// checkPhpLongArray flags `array(...)` (the
// Generic.Arrays.DisallowLongArraySyntax sniff).
func checkPhpLongArray(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.isPhpKeyword(i, "array") || s.tok(i+1) != "(" {
			continue
		}
		cl := s.matchClose(s.code, i+1)
		if cl >= len(s.code) {
			continue
		}
		detail := "`array(...)` -> `[...]`"
		if text := s.codeText(i, cl+1); len(text) <= 60 {
			inner := ""
			if cl > i+2 {
				inner = s.codeText(i+2, cl)
			}
			detail = "`" + text + "` -> `[" + inner + "]`"
		}
		out = append(out, finding{span: s.tokSpan(i, cl+1), detail: detail})
	}
	return out
}

// checkPhpLooseEquality flags `==`, `!=` and `<>`. We leave comparisons
// with null and between two literals alone.
func checkPhpLooseEquality(s *source) []finding {
	var out []finding
	for i := 1; i < len(s.code); i++ {
		op := s.tok(i)
		if op != "==" && op != "!=" && op != "<>" {
			continue
		}
		strict := "!=="
		if op == "==" {
			strict = "==="
		}
		start, end := s.phpChainStart(i), s.phpChainEnd(i+1)
		if s.phpNull(i-1) || s.phpNull(i+1) || start == i-1 && end == i+2 && s.phpLiteral(i-1) && s.phpLiteral(i+1) {
			continue
		}
		if start == i || end == i+1 || !s.phpStartsExpr(start) || !s.phpEndsExpr(end) {
			out = append(out, finding{span: s.code[i].Span, detail: "`" + op + "` -> `" + strict + "`"})
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(start, end),
			detail: "`" + s.codeText(start, end) + "` -> `" + s.codeText(start, i) + " " + strict + " " + s.codeText(i+1, end) + "`",
		})
	}
	return out
}

// phpCompared finds a comparison of the call [start, cl] with a token that
// satisfies value, in either order (`call === v` or `v === call`). It
// returns the operator, the index range of the whole comparison and whether
// there was one.
func (s *source) phpCompared(start, cl int, value func(int) bool) (string, int, int, bool) {
	if op := s.tok(cl + 1); (op == "===" || op == "!==") && value(cl+2) && s.phpStartsExpr(start) && s.phpEndsExpr(cl+3) {
		return op, start, cl + 3, true
	}
	if op := s.tok(start - 1); (op == "===" || op == "!==") && value(start-2) && s.phpStartsExpr(start-2) && s.phpEndsExpr(cl+1) {
		return op, start - 2, cl + 1, true
	}
	return "", 0, 0, false
}

// checkPhpStrContains flags `strpos($h, $n) !== false` (Rector's
// StrContainsRector).
func checkPhpStrContains(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "strpos" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) != 2 {
			continue
		}
		op, from, to, ok := s.phpCompared(start, cl, func(k int) bool { return s.isPhpKeyword(k, "false") })
		if !ok {
			continue
		}
		repl := "str_contains(" + s.codeText(args[0][0], args[1][1]) + ")"
		if op == "===" {
			repl = "!" + repl
		}
		out = append(out, finding{span: s.tokSpan(from, to), detail: "`" + s.codeText(from, to) + "` -> `" + repl + "`"})
	}
	return out
}

// phpLiteralLen returns the byte length of a string literal's value. The
// literal can't interpolate or use escapes other than `\\` and `\'`.
func phpLiteralLen(lit string) (int, bool) {
	if len(lit) < 2 || lit[len(lit)-1] != lit[0] {
		return 0, false
	}
	body := lit[1 : len(lit)-1]
	switch lit[0] {
	case '\'':
		body = strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(body)
	case '"':
		if strings.ContainsAny(body, `\$`) {
			return 0, false
		}
	default:
		return 0, false
	}
	return len(body), true
}

// phpStrAffix flags the prefix tests (or suffix tests, with ends) that
// str_starts_with and str_ends_with replace: `strpos($h, $n) === 0`,
// `substr($h, 0, 3) === 'abc'` and `substr($h, -3) === 'abc'` (Rector's
// StrStartsWithRector and StrEndsWithRector).
func (s *source) phpStrAffix(ends bool) []finding {
	var out []finding
	fn := "str_starts_with"
	if ends {
		fn = "str_ends_with"
	}
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "strpos" && name != "substr" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) {
			continue
		}
		var op string
		var from, to, lit int
		switch {
		case name == "strpos" && !ends && len(args) == 2:
			op, from, to, ok = s.phpCompared(start, cl, func(k int) bool { return s.tok(k) == "0" })
			lit = -1
		case name == "substr" && !ends && len(args) == 3 && s.tok(args[1][0]) == "0" && args[1][1] == args[1][0]+1 && args[2][1] == args[2][0]+1:
			op, from, to, ok = s.phpCompared(start, cl, func(k int) bool { return s.phpLengthLiteral(k, s.tok(args[2][0])) })
		case name == "substr" && ends && len(args) == 2 && s.tok(args[1][0]) == "-" && args[1][1] == args[1][0]+2:
			op, from, to, ok = s.phpCompared(start, cl, func(k int) bool { return s.phpLengthLiteral(k, s.tok(args[1][0]+1)) })
		default:
			continue
		}
		if !ok {
			continue
		}
		subject := s.codeText(args[0][0], args[0][1])
		var repl string
		if lit < 0 {
			repl = fn + "(" + s.codeText(args[0][0], args[1][1]) + ")"
		} else {
			k := cl + 2
			if from < start {
				k = from
			}
			repl = fn + "(" + subject + ", " + s.tok(k) + ")"
		}
		if op == "!==" {
			repl = "!" + repl
		}
		out = append(out, finding{span: s.tokSpan(from, to), detail: "`" + s.codeText(from, to) + "` -> `" + repl + "`"})
	}
	return out
}

// phpLengthLiteral reports whether code token k is a string literal whose
// value is n bytes long, with n given as decimal text.
func (s *source) phpLengthLiteral(k int, n string) bool {
	if k < 0 || k >= len(s.code) || s.code[k].Kind != code.TokString {
		return false
	}
	l, ok := phpLiteralLen(s.tok(k))
	return ok && n == strconv.Itoa(l)
}

// checkPhpNullCoalescing flags `isset($a) ? $a : $b` (Rector's
// TernaryToNullCoalescingRector).
func checkPhpNullCoalescing(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.isPhpKeyword(i, "isset") || s.tok(i+1) != "(" || !s.phpStartsExpr(i) {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) != 1 {
			continue
		}
		n := args[0][1] - args[0][0]
		if s.tok(cl+1) != "?" || !s.sameTokens(args[0][0], cl+2, n) || s.tok(cl+2+n) != ":" {
			continue
		}
		x := s.codeText(args[0][0], args[0][1])
		colon := cl + 2 + n
		if e := s.phpChainEnd(colon + 1); e > colon+1 && s.phpEndsExpr(e) {
			out = append(out, finding{span: s.tokSpan(i, e), detail: "`" + s.codeText(i, e) + "` -> `" + x + " ?? " + s.codeText(colon+1, e) + "`"})
			continue
		}
		out = append(out, finding{span: s.tokSpan(i, colon+1), detail: "`" + s.codeText(i, colon+1) + "` -> `" + x + " ??`"})
	}
	return out
}

// checkPhpNullCoalescingAssignment flags `$a = $a ?? $b;` (Rector's
// NullCoalescingOperatorRector).
func checkPhpNullCoalescingAssignment(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.phpVar(i) || !phpStmtStarts[s.phpLower(i-1)] {
			continue
		}
		end := s.phpChainEnd(i)
		n := end - i
		if s.tok(end) != "=" || !s.sameTokens(i, end+1, n) || s.tok(end+1+n) != "??" {
			continue
		}
		rest := end + 2 + n
		stop := rest
		ambiguous := false
		for depth := 0; stop < len(s.code); stop++ {
			switch t := s.phpLower(stop); {
			case t == "(" || t == "[" || t == "{":
				depth++
			case t == ")" || t == "]" || t == "}":
				depth--
			case depth == 0 && (t == "?" || t == "and" || t == "or" || t == "xor"):
				ambiguous = true
			}
			if depth < 0 || depth == 0 && (s.tok(stop) == ";" || s.tok(stop) == "?>") {
				break
			}
		}
		if ambiguous || stop == rest {
			continue
		}
		x := s.codeText(i, end)
		out = append(out, finding{span: s.tokSpan(i, stop), detail: "`" + s.codeText(i, stop) + "` -> `" + x + " ??= " + s.codeText(rest, stop) + "`"})
	}
	return out
}

// checkPhpDirConstant flags `dirname(__FILE__)` (PHP-CS-Fixer's
// dir_constant).
func checkPhpDirConstant(s *source) []finding {
	var out []finding
	for i := range s.code {
		if name, start, ok := s.phpFunc(i); ok && name == "dirname" && s.isPhpKeyword(i+2, "__FILE__") && s.tok(i+3) == ")" {
			out = append(out, finding{span: s.tokSpan(start, i+4), detail: "`" + s.codeText(start, i+4) + "` -> `__DIR__`"})
		}
	}
	return out
}

// checkPhpGetClass flags `get_class()` with no argument, `$this` or a
// variable (Rector's ClassOnObjectRector and
// ClassOnThisVariableObjectRector).
func checkPhpGetClass(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "get_class" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		var repl string
		switch {
		case cl >= len(s.code):
			continue
		case len(args) == 0:
			repl = "self::class"
		case len(args) == 1 && args[0][1] == args[0][0]+1 && s.tok(args[0][0]) == "$this":
			repl = "static::class"
		case len(args) == 1 && args[0][1] == args[0][0]+1 && s.phpVar(args[0][0]):
			repl = s.tok(args[0][0]) + "::class"
		default:
			continue
		}
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "` -> `" + repl + "`"})
	}
	return out
}

// phpAliases maps function aliases to their canonical functions. The list
// comes from PHP-CS-Fixer's no_alias_functions.
var phpAliases = map[string]string{
	"sizeof": "count", "join": "implode", "key_exists": "array_key_exists", "chop": "rtrim", "is_integer": "is_int",
	"is_long": "is_int", "is_double": "is_float", "is_real": "is_float", "strchr": "strstr", "pos": "current",
	"fputs": "fwrite", "doubleval": "floatval", "is_writeable": "is_writable", "ini_alter": "ini_set",
	"show_source": "highlight_file", "diskfreespace": "disk_free_space",
}

func checkPhpAliasFunction(s *source) []finding {
	var out []finding
	for i := range s.code {
		if name, _, ok := s.phpFunc(i); ok && phpAliases[name] != "" {
			out = append(out, finding{span: s.code[i].Span, detail: "`" + s.tok(i) + "` -> `" + phpAliases[name] + "`"})
		}
	}
	return out
}

// checkPhpErrorSuppression flags the `@` operator (the
// Generic.PHP.NoSilencedErrors sniff).
func checkPhpErrorSuppression(s *source) []finding {
	var out []finding
	for i, t := range s.code {
		if t.Kind != code.TokPunct || s.tok(i) != "@" {
			continue
		}
		if e := s.phpChainEnd(i + 1); e > i+1 {
			out = append(out, finding{span: s.tokSpan(i, e), detail: "`" + s.codeText(i, e) + "` -> `" + s.codeText(i+1, e) + "`"})
			continue
		}
		out = append(out, finding{span: t.Span})
	}
	return out
}

var phpDebugFuncs = setOf("var_dump", "print_r", "var_export", "dd", "dump", "debug_zval_dump", "debug_print_backtrace", "xdebug_var_dump")

// checkPhpDebugLeftover flags the debug output calls that
// phpstan-disallowed-calls forbids. If a `die(...)` or `exit(...)` wraps
// the call, we include it in the finding.
func checkPhpDebugLeftover(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || !phpDebugFuncs[name] {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || (name == "print_r" || name == "var_export") && len(args) > 1 {
			continue
		}
		if s.tok(start-1) == "(" && s.isPhpKeywordAny(start-2, "die", "exit") && s.tok(cl+1) == ")" {
			start, cl = start-2, cl+1
		}
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "`"})
	}
	return out
}

// phpPureCode reports whether a PHP file is all code, with a single open tag
// at the start and at most a closing tag at the end.
func (s *source) phpPureCode() bool {
	if len(s.code) == 0 || !strings.EqualFold(s.tok(0), "<?php") || strings.TrimSpace(string(s.f.Src[:s.code[0].Span.Start])) != "" {
		return false
	}
	last := len(s.code) - 1
	for i := 1; i < len(s.code); i++ {
		t := s.code[i]
		if t.Kind != code.TokPunct {
			continue
		}
		if x := s.tok(i); strings.HasPrefix(x, "<?") || x == "?>" && (i != last || strings.TrimSpace(string(s.f.Src[t.Span.End:])) != "") {
			return false
		}
	}
	return true
}

// checkPhpClosingTag flags the `?>` that ends a file of PHP code (the
// PSR2.Files.ClosingTag sniff).
func checkPhpClosingTag(s *source) []finding {
	if len(s.code) < 2 || s.tok(len(s.code)-1) != "?>" || !s.phpPureCode() {
		return nil
	}
	return []finding{{span: s.code[len(s.code)-1].Span}}
}

// checkPhpStrictTypes flags a PHP file that declares functions or types
// but not strict_types
// (SlevomatCodingStandard.TypeHints.DeclareStrictTypes).
func checkPhpStrictTypes(s *source) []finding {
	if !s.phpPureCode() {
		return nil
	}
	declares := false
	for _, d := range s.f.Decls {
		if !d.Local && (d.Callable() || d.IsType()) {
			declares = true
			break
		}
	}
	if !declares {
		return nil
	}
	for i := range s.code {
		if s.isPhpKeyword(i, "declare") && s.tok(i+1) == "(" && strings.EqualFold(s.tok(i+2), "strict_types") {
			return nil
		}
	}
	return []finding{{span: s.code[0].Span, detail: "`" + s.tok(0) + "` -> `" + s.tok(0) + " declare(strict_types=1);`"}}
}

// checkPhpNullsafe flags `$a !== null ? $a->b : null` and
// `$a === null ? null : $a->b` (Rector's NullsafeOperatorRector).
func checkPhpNullsafe(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.phpVar(i) || !s.phpStartsExpr(i) {
			continue
		}
		xEnd := s.phpChainEnd(i)
		n := xEnd - i
		if n == 0 || strings.Contains(s.codeText(i, xEnd), "(") {
			continue
		}
		var then, end int
		switch {
		case s.tok(xEnd) == "!==" && s.phpNull(xEnd+1) && s.tok(xEnd+2) == "?":
			then = xEnd + 3
			end = s.phpChainEnd(then)
			if s.tok(end) != ":" || !s.phpNull(end+1) {
				continue
			}
			end += 2
		case s.tok(xEnd) == "===" && s.phpNull(xEnd+1) && s.tok(xEnd+2) == "?" && s.phpNull(xEnd+3) && s.tok(xEnd+4) == ":":
			then = xEnd + 5
			end = s.phpChainEnd(then)
		default:
			continue
		}
		if !s.sameTokens(i, then, n) || s.tok(then+n) != "->" || !s.phpEndsExpr(end) {
			continue
		}
		chainEnd := end
		if s.tok(xEnd) == "!==" {
			chainEnd = end - 2
		}
		repl := s.codeText(i, xEnd) + "?->" + s.codeText(then+n+1, chainEnd)
		out = append(out, finding{span: s.tokSpan(i, end), detail: "`" + s.codeText(i, end) + "` -> `" + repl + "`"})
	}
	return out
}

// checkPhpSwitchToMatch flags a switch with a default case if all the case
// bodies return a value, or all assign the same variable and break
// (Rector's ChangeSwitchToMatchRector).
func checkPhpSwitchToMatch(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.isPhpKeyword(i, "switch") || s.tok(i+1) != "(" {
			continue
		}
		cl := s.matchClose(s.code, i+1)
		if cl >= len(s.code) || s.tok(cl+1) != "{" {
			continue
		}
		end := s.matchClose(s.code, cl+1)
		if end >= len(s.code) || !s.phpMatchableCases(cl+2, end) {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(i, end+1),
			detail: "`switch " + s.codeText(i+1, cl+1) + "` -> `match " + s.codeText(i+1, cl+1) + "`",
		})
	}
	return out
}

// phpMatchableCases reports whether the case groups in code tokens [k, end)
// all return a value or all assign the same variable and break, with a
// default case among them.
func (s *source) phpMatchableCases(k, end int) bool {
	form, target := "", ""
	labels, groups := 0, 0
	hasDefault := false
	for k < end {
		switch {
		case s.isPhpKeyword(k, "case"):
			colon := k + 1
			for ; colon < end; colon++ {
				if t := s.tok(colon); t == ":" || t == ";" {
					break
				} else if t == "?" {
					return false
				} else if t == "(" || t == "[" || t == "{" {
					colon = s.matchClose(s.code, colon)
				}
			}
			if colon >= end || colon == k+1 {
				return false
			}
			labels++
			k = colon + 1
		case s.isPhpKeyword(k, "default") && (s.tok(k+1) == ":" || s.tok(k+1) == ";"):
			hasDefault = true
			labels++
			k += 2
		default:
			if labels == 0 {
				return false
			}
			stmt := s.phpStatementEnd(k, end)
			if stmt >= end {
				return false
			}
			switch {
			case s.isPhpKeyword(k, "return") && stmt > k+1 && (form == "" || form == "return"):
				form = "return"
				k = stmt + 1
			case s.phpVar(k) && s.tok(k+1) == "=" && stmt > k+2 && (form == "" || form == "assign" && target == s.tok(k)) &&
				s.isPhpKeyword(stmt+1, "break") && s.tok(stmt+2) == ";":
				form, target = "assign", s.tok(k)
				k = stmt + 3
			default:
				return false
			}
			groups++
			labels = 0
		}
	}
	return hasDefault && labels == 0 && groups >= 2
}

func (s *source) phpStatementEnd(k, end int) int {
	for ; k < end; k++ {
		switch s.tok(k) {
		case ";":
			return k
		case "(", "[", "{":
			k = s.matchClose(s.code, k)
		}
	}
	return end
}

// checkPhpCallUserFunc flags `call_user_func($f, ...)` and
// `call_user_func_array($f, $args)` if the callable is a variable.
func checkPhpCallUserFunc(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "call_user_func" && name != "call_user_func_array" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) == 0 || args[0][1] != args[0][0]+1 || !s.phpVar(args[0][0]) {
			continue
		}
		f := s.tok(args[0][0])
		var repl string
		switch {
		case name == "call_user_func" && len(args) == 1:
			repl = f + "()"
		case name == "call_user_func":
			repl = f + "(" + s.codeText(args[1][0], args[len(args)-1][1]) + ")"
		case len(args) == 2:
			repl = f + "(..." + s.codeText(args[1][0], args[1][1]) + ")"
		default:
			continue
		}
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "` -> `" + repl + "`"})
	}
	return out
}

// checkPhpListSyntax flags `list(...)` (PHP-CS-Fixer's list_syntax).
func checkPhpListSyntax(s *source) []finding {
	var out []finding
	for i := range s.code {
		if !s.isPhpKeyword(i, "list") || s.tok(i+1) != "(" {
			continue
		}
		cl := s.matchClose(s.code, i+1)
		if cl >= len(s.code) {
			continue
		}
		inner := ""
		if cl > i+2 {
			inner = s.codeText(i+2, cl)
		}
		out = append(out, finding{span: s.tokSpan(i, cl+1), detail: "`" + s.codeText(i, cl+1) + "` -> `[" + inner + "]`"})
	}
	return out
}

func checkPhpExtract(s *source) []finding {
	var out []finding
	for i := range s.code {
		if name, start, ok := s.phpFunc(i); ok && name == "extract" {
			_, cl := s.callArgs(i + 1)
			if cl < len(s.code) {
				out = append(out, finding{span: s.tokSpan(start, cl+1)})
			}
		}
	}
	return out
}

// phpRemoved maps functions that PHP 7 and 8 removed to their replacement.
var phpRemoved = map[string]string{
	"create_function": "a closure", "each": "foreach", "money_format": "NumberFormatter",
	"ereg": "preg_match", "eregi": "preg_match", "ereg_replace": "preg_replace", "eregi_replace": "preg_replace",
	"split": "explode or preg_split", "spliti": "preg_split", "sql_regcase": "preg_quote",
	"mysql_connect": "PDO or mysqli", "mysql_query": "PDO or mysqli", "mysql_fetch_assoc": "PDO or mysqli",
	"mysql_fetch_array": "PDO or mysqli", "mysql_real_escape_string": "prepared statements", "mysql_close": "PDO or mysqli",
	"mysql_select_db": "PDO or mysqli", "mysql_num_rows": "PDO or mysqli", "mysql_error": "PDO or mysqli",
}

func checkPhpRemovedFunction(s *source) []finding {
	var out []finding
	for i := range s.code {
		if name, _, ok := s.phpFunc(i); ok && phpRemoved[name] != "" {
			out = append(out, finding{span: s.code[i].Span, detail: "`" + s.tok(i) + "` -> " + phpRemoved[name]})
		}
	}
	return out
}

// phpOperand returns the source of the argument [a, b) for use as the operand
// of a unary or binary operator, wrapped in parentheses if it's more than a
// single value.
func (s *source) phpOperand(a, b int) string {
	text := s.codeText(a, b)
	if s.phpChainEnd(a) == b {
		return text
	}
	return "(" + text + ")"
}

var phpCastFuncs = map[string]string{"intval": "(int)", "floatval": "(float)", "strval": "(string)", "boolval": "(bool)"}

// checkPhpCastFunction flags conversion functions called with a single
// argument (PHP-CS-Fixer's modernize_types_casting).
func checkPhpCastFunction(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || phpCastFuncs[name] == "" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) != 1 || s.tok(args[0][0]) == "..." {
			continue
		}
		repl := phpCastFuncs[name] + " " + s.phpOperand(args[0][0], args[0][1])
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "` -> `" + repl + "`"})
	}
	return out
}

// checkPhpIsNull flags `is_null($x)` (PHP-CS-Fixer's is_null).
func checkPhpIsNull(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "is_null" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) != 1 {
			continue
		}
		op := " === null"
		if s.tok(start-1) == "!" {
			start--
			op = " !== null"
		}
		if !s.phpStartsExpr(start) || !s.phpEndsExpr(cl+1) {
			continue
		}
		repl := s.phpOperand(args[0][0], args[0][1]) + op
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "` -> `" + repl + "`"})
	}
	return out
}

// checkPhpArrayPush flags `array_push($a, $x);` statements (PHP-CS-Fixer's
// array_push).
func checkPhpArrayPush(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "array_push" || !phpStmtStarts[s.phpLower(start-1)] {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) != 2 || s.tok(cl+1) != ";" || s.tok(args[1][0]) == "..." {
			continue
		}
		repl := s.codeText(args[0][0], args[0][1]) + "[] = " + s.codeText(args[1][0], args[1][1])
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "` -> `" + repl + "`"})
	}
	return out
}

// checkPhpPow flags `pow($a, $b)` (PHP-CS-Fixer's pow_to_exponentiation).
func checkPhpPow(s *source) []finding {
	var out []finding
	for i := range s.code {
		name, start, ok := s.phpFunc(i)
		if !ok || name != "pow" {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) || len(args) != 2 {
			continue
		}
		repl := s.phpOperand(args[0][0], args[0][1]) + " ** " + s.phpOperand(args[1][0], args[1][1])
		out = append(out, finding{span: s.tokSpan(start, cl+1), detail: "`" + s.codeText(start, cl+1) + "` -> `" + repl + "`"})
	}
	return out
}
