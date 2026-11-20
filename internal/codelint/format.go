package codelint

import (
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// formatCall describes a function that formats its argument number arg. If
// arg is -1, the format is the first of the first two arguments that is a
// string literal, as in `String.format(Locale.ROOT, "...")`. brace marks {}
// placeholders instead of % conversions.
type formatCall struct {
	arg   int
	brace bool
}

// cFormatCalls is shared by C and C++. That works because C code has no
// std:: or fmt:: calls.
var cFormatCalls = map[string]formatCall{
	"printf": {}, "fprintf": {arg: 1}, "sprintf": {arg: 1}, "snprintf": {arg: 2}, "dprintf": {arg: 1},
	"std.printf": {}, "std.fprintf": {arg: 1}, "std.sprintf": {arg: 1}, "std.snprintf": {arg: 2},
	"std.format": {brace: true}, "fmt.format": {brace: true},
	"std.print": {arg: -1, brace: true}, "std.println": {arg: -1, brace: true},
	"fmt.print": {arg: -1, brace: true}, "fmt.println": {arg: -1, brace: true},
}

// formatCalls maps each language to its formatting calls. The keys are
// "Qual.Name" for a call through a package, type or namespace, ".Name" for a
// method on any receiver and "Name" for a plain call.
var formatCalls = map[code.Lang]map[string]formatCall{
	code.Go: {
		".Sprintf": {}, ".Printf": {}, ".Errorf": {}, ".Fatalf": {}, ".Panicf": {}, ".Logf": {}, ".Skipf": {},
		".Infof": {}, ".Warnf": {}, ".Warningf": {}, ".Debugf": {}, ".Tracef": {},
		".Fprintf": {arg: 1}, ".Appendf": {arg: 1},
	},
	code.Java: {
		"String.format": {arg: -1}, ".printf": {arg: -1}, "out.format": {arg: -1}, "err.format": {arg: -1},
		"MessageFormat.format": {brace: true},
	},
	code.CSharp: {
		"string.Format": {arg: -1, brace: true}, "String.Format": {arg: -1, brace: true}, ".AppendFormat": {arg: -1, brace: true},
	},
	code.C:   cFormatCalls,
	code.CPP: cFormatCalls,
	code.PHP: {
		"sprintf": {}, "printf": {}, "vsprintf": {}, "vprintf": {}, "fprintf": {arg: 1}, "vfprintf": {arg: 1},
	},
}

// checkFormatPlaceholders flags format strings without a placeholder:
// Python f-strings and literals formatted with `.format()` or `%`,
// JavaScript template literals, C# interpolated strings, Java `formatted()`
// and the format argument of printf-style and {}-style calls. If there's no
// placeholder, the formatting does nothing, or it drops the arguments passed
// for it.
func checkFormatPlaceholders(s *source) []finding {
	var out []finding
	switch s.f.Lang {
	case code.Python:
		out = s.pythonFormatStrings()
	case code.JavaScript, code.TypeScript:
		out = s.templateLiterals()
	case code.CSharp:
		out = s.interpolatedStrings()
	case code.Rust:
		out = s.rustFormatMacros()
	case code.Java:
		for i, t := range s.code {
			if t.Kind == code.TokString && s.seq(i+1, ".", "formatted", "(") {
				if fd, ok := s.literalMethodFormat(i, i+1, false); ok {
					out = append(out, fd)
				}
			}
		}
	}
	if calls := formatCalls[s.f.Lang]; calls != nil {
		out = append(out, s.formatCallFindings(calls)...)
	}
	return out
}

// formatCallFindings flags the calls listed in calls if their format
// argument is a string literal without a placeholder.
func (s *source) formatCallFindings(calls map[string]formatCall) []finding {
	var out []finding
	for i := range s.code {
		if s.code[i].Kind != code.TokIdent || s.tok(i+1) != "(" {
			continue
		}
		fc, start, ok := s.formatCallAt(i, calls)
		if !ok {
			continue
		}
		args, cl := s.callArgs(i + 1)
		if cl >= len(s.code) {
			continue // unbalanced parentheses, for example inside a macro
		}
		arg := fc.arg
		if arg < 0 {
			arg = 0
			if len(args) > 1 && !s.stringArg(args[0]) {
				arg = 1
			}
		}
		if arg >= len(args) || !s.stringArg(args[arg]) || s.hasPlaceholder(args[arg][0], args[arg][1], fc.brace) {
			continue
		}
		out = append(out, finding{
			span:   s.tokSpan(start, cl+1),
			detail: s.formatCallRewrite(start, i, cl, args, arg, fc.brace),
		})
	}
	return out
}

// formatCallAt returns the formatting call named by code token i and the
// index of the first token of the callee: `fmt` of `fmt.Sprintf`.
func (s *source) formatCallAt(i int, calls map[string]formatCall) (formatCall, int, bool) {
	name := s.tok(i)
	switch s.tok(i - 1) {
	case ".", "::", "->":
		// The qualifier can be a keyword, like in C#'s string.Format.
		if q := s.code[i-2]; q.Kind == code.TokIdent || q.Kind == code.TokKeyword {
			if fc, ok := calls[s.tok(i-2)+"."+name]; ok {
				return fc, min(s.chainStart(i+1), i-2), true
			}
		}
		fc, ok := calls["."+name]
		return fc, s.chainStart(i + 1), ok
	}
	fc, ok := calls[name]
	return fc, i, ok
}

// callArgs takes the code token of a call's opening parenthesis and returns
// the token ranges of its arguments, plus the index of the closing one.
func (s *source) callArgs(open int) ([][2]int, int) {
	cl := s.matchClose(s.code, open)
	var args [][2]int
	start, depth := open+1, 0
	for k := open + 1; k < cl; k++ {
		switch s.tok(k) {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ",":
			if depth == 0 {
				args = append(args, [2]int{start, k})
				start = k + 1
			}
		}
	}
	if start < cl {
		args = append(args, [2]int{start, cl})
	}
	return args, cl
}

// stringArg reports whether an argument is only string literals. C and
// Python concatenate adjacent ones.
func (s *source) stringArg(arg [2]int) bool {
	for k := arg[0]; k < arg[1]; k++ {
		if s.code[k].Kind != code.TokString {
			return false
		}
	}
	return arg[1] > arg[0]
}

// hasPlaceholder reports whether the string literals [i, j) hold a
// placeholder. With brace set that's an unescaped `{`, otherwise a %
// conversion other than `%%`. A PHP string that interpolates a variable can
// also hold one.
func (s *source) hasPlaceholder(i, j int, brace bool) bool {
	for k := i; k < j; k++ {
		lit := stringContent(s.tok(k))
		if brace && braceHole(lit, 1) || !brace && percentHole(lit) || s.f.Lang == code.PHP && phpInterpolates(s.tok(k)) {
			return true
		}
	}
	return false
}

// percentHole reports whether lit holds a printf-style conversion.
func percentHole(lit string) bool {
	for k := 0; k < len(lit); k++ {
		if lit[k] != '%' {
			continue
		}
		if k+1 < len(lit) && lit[k+1] == '%' {
			k++
			continue
		}
		return true
	}
	return false
}

// braceHole reports whether lit has an interpolation hole, which is a run of
// at least n opening braces. For n = 1 a doubled brace is an escape.
func braceHole(lit string, n int) bool {
	for k := 0; k < len(lit); {
		if lit[k] != '{' {
			k++
			continue
		}
		run := 0
		for k < len(lit) && lit[k] == '{' {
			run++
			k++
		}
		if n == 1 && run%2 == 1 || n > 1 && run >= n {
			return true
		}
	}
	return false
}

// unescapeFormat returns a format string literal as a plain literal with
// the same text: `%%` or doubled braces become single characters.
func unescapeFormat(lit string, brace bool) string {
	if brace {
		return strings.NewReplacer("{{", "{", "}}", "}").Replace(lit)
	}
	return strings.ReplaceAll(lit, "%%", "%")
}

// formatCallRewrite describes the fix for the call [start, cl] named by code
// token name. Argument number arg is the format string without placeholders.
func (s *source) formatCallRewrite(start, name, cl int, args [][2]int, arg int, brace bool) string {
	call := s.codeText(start, cl+1)
	if arg < len(args)-1 {
		return "`" + call + "` passes arguments that no placeholder uses"
	}
	lit := unescapeFormat(s.codeText(args[arg][0], args[arg][1]), brace)
	recv, fn := s.f.Text(code.Span{Start: s.code[start].Span.Start, End: s.code[name].Span.Start}), s.tok(name)
	var repl string
	switch s.f.Lang {
	case code.Go:
		switch {
		case fn == "Sprintf":
			repl = lit
		case fn == "Errorf" && recv == "fmt.":
			repl = "errors.New(" + lit + ")"
		default:
			lead := ""
			if arg > 0 {
				lead = s.codeText(args[0][0], args[arg-1][1]) + ", "
			}
			repl = recv + strings.TrimSuffix(fn, "f") + "(" + lead + lit + ")"
		}
	case code.Java:
		switch {
		case recv == "String." || recv == "MessageFormat.":
			repl = lit
		case arg == 0:
			repl = recv + "print(" + lit + ")"
		}
	case code.CSharp:
		repl = lit
		if fn == "AppendFormat" {
			repl = recv + "Append(" + lit + ")"
		}
	case code.PHP:
		switch fn {
		case "sprintf":
			repl = lit
		case "printf":
			repl = "echo " + lit
		case "fprintf":
			repl = "fwrite(" + s.codeText(args[0][0], args[0][1]) + ", " + lit + ")"
		}
	default:
		switch fn {
		case "printf":
			repl = "fputs(" + lit + ", stdout)"
		case "fprintf":
			repl = "fputs(" + lit + ", " + s.codeText(args[0][0], args[0][1]) + ")"
		case "format":
			repl = "std::string(" + lit + ")"
		}
	}
	if repl == "" {
		return "`" + call + "` has no placeholder"
	}
	return "`" + call + "` -> `" + repl + "`"
}

// literalMethodFormat checks the string literals [i, j) formatted by the
// method call that follows them: `"...".format(x)`, `"...".formatted(x)`.
func (s *source) literalMethodFormat(i, j int, brace bool) (finding, bool) {
	if s.hasPlaceholder(i, j, brace) {
		return finding{}, false
	}
	args, cl := s.callArgs(j + 2)
	call := s.codeText(i, cl+1)
	detail := "`" + call + "` -> `" + unescapeFormat(s.codeText(i, j), brace) + "`"
	if len(args) > 0 {
		detail = "`" + call + "` passes arguments that no placeholder uses"
	}
	return finding{span: s.tokSpan(i, cl+1), detail: detail}, true
}

// pythonFormatStrings flags f-strings without a replacement field and
// literals formatted with `.format()` or `%` without a placeholder.
// Adjacent literals form a single string, so `f"{a}" "b"` has a field.
func (s *source) pythonFormatStrings() []finding {
	var out []finding
	for i := 0; i < len(s.code); i++ {
		if s.code[i].Kind != code.TokString {
			continue
		}
		j := i
		fstring, field := false, false
		for j < len(s.code) && s.code[j].Kind == code.TokString {
			if lit := s.tok(j); pythonFString(lit) {
				fstring = true
				field = field || braceHole(stringContent(lit), 1)
			}
			j++
		}
		switch {
		case fstring && !field:
			var plain []string
			for k := i; k < j; k++ {
				lit := s.tok(k)
				if pythonFString(lit) {
					lit = unescapeFormat(pythonDropF(lit), true)
				}
				plain = append(plain, lit)
			}
			out = append(out, finding{span: s.tokSpan(i, j), detail: "`" + s.codeText(i, j) + "` -> `" + strings.Join(plain, " ") + "`"})
		case !fstring && s.seq(j, ".", "format", "("):
			if fd, ok := s.literalMethodFormat(i, j, true); ok {
				out = append(out, fd)
			}
		case !fstring && s.tok(j) == "%" && !s.hasPlaceholder(i, j, false):
			out = append(out, finding{span: s.tokSpan(i, j+1), detail: "`" + s.codeText(i, j) + "` has no conversion for the `%` operator"})
		}
		i = j - 1
	}
	return out
}

func pythonFString(lit string) bool {
	q := strings.IndexAny(lit, `"'`)
	return q > 0 && strings.ContainsAny(lit[:q], "fF")
}

func pythonDropF(lit string) string {
	q := strings.IndexAny(lit, `"'`)
	return strings.NewReplacer("f", "", "F", "").Replace(lit[:q]) + lit[q:]
}

// templateLiterals flags untagged template literals without a `${}`
// substitution. We leave multi-line templates and those with both kinds of
// quotes alone, since a quoted string would need escapes.
func (s *source) templateLiterals() []finding {
	var out []finding
	for i, t := range s.code {
		lit := s.text(t)
		if t.Kind != code.TokString || !strings.HasPrefix(lit, "`") || strings.Contains(lit, "${") || strings.Contains(lit, "\n") {
			continue
		}
		// A tagged template such as html`...` calls its tag.
		if i > 0 && (s.code[i-1].Kind == code.TokIdent || s.tok(i-1) == ")" || s.tok(i-1) == "]") {
			continue
		}
		body := lit[1 : len(lit)-1]
		q := `"`
		switch {
		case strings.Contains(body, `"`) && strings.Contains(body, "'"):
			continue
		case strings.Contains(body, `"`):
			q = "'"
		}
		out = append(out, finding{span: t.Span, detail: "`" + lit + "` -> `" + q + body + q + "`"})
	}
	return out
}

// interpolatedStrings flags C# interpolated strings without a hole. `$"..."`
// needs an unescaped `{`, and a raw `$$"""..."""` needs a run of as many
// braces as it has dollar signs.
func (s *source) interpolatedStrings() []finding {
	var out []finding
	for _, t := range s.code {
		lit := s.text(t)
		q := strings.IndexByte(lit, '"')
		if t.Kind != code.TokString || q <= 0 || !strings.Contains(lit[:q], "$") {
			continue
		}
		n := strings.Count(lit[:q], "$")
		if braceHole(stringContent(lit), n) {
			continue
		}
		plain := strings.ReplaceAll(lit[:q], "$", "") + lit[q:]
		if n == 1 {
			plain = unescapeFormat(plain, true)
		}
		out = append(out, finding{span: t.Span, detail: "`" + lit + "` -> `" + plain + "`"})
	}
	return out
}
