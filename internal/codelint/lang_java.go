package codelint

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/myoo-ai/cli/internal/code"
)

var langJava = []code.Lang{code.Java}

var javaRules = []*Rule{
	{
		ID:             "java-collection-is-empty",
		Langs:          langJava,
		Description:    "prefer `isEmpty()` over comparing the size or length to zero, as it states the intent and can be cheaper.",
		Recommendation: "replace the comparison with `isEmpty()`, for example `list.size() == 0` becomes `list.isEmpty()` and `list.size() > 0` becomes `!list.isEmpty()`. arrays have no `isEmpty()`, so `array.length == 0` stays as it is.",
		check:          checkJavaIsEmpty,
	},
	{
		ID:             "java-string-reference-equality",
		Langs:          langJava,
		Description:    "strings should be compared with `equals()` instead of `==`, as `==` compares references and fails for equal strings built at runtime.",
		Recommendation: "call `equals()` on the literal, for example `s == \"a\"` becomes `\"a\".equals(s)`, which also returns false for a null `s`.",
		check:          checkJavaStringEquality,
	},
	{
		ID:             "java-print-stack-trace",
		Langs:          langJava,
		Description:    "prefer logging the exception over `printStackTrace()`, as the trace goes to standard error without a level or context.",
		Recommendation: "pass the exception to the logger, for example `log.error(\"loading config failed\", e)`, or rethrow it when the caller should handle it.",
		check:          checkJavaPrintStackTrace,
	},
	{
		ID:             "java-synchronized-legacy-class",
		Langs:          langJava,
		Description:    "prefer the unsynchronized collections over `Vector`, `Hashtable`, `Stack` and `StringBuffer`, as their locking costs time and rarely makes compound actions thread safe.",
		Recommendation: "use `ArrayList` for `Vector`, `HashMap` for `Hashtable`, `ArrayDeque` behind `Deque` for `Stack` and `StringBuilder` for `StringBuffer`. where threads share the object, pick a concurrent collection or guard it explicitly.",
		check:          checkJavaLegacyClass,
	},
	{
		ID:             "java-wrapper-constructor",
		Langs:          langJava,
		Description:    "prefer `valueOf()` or a literal over constructing a wrapper or string, as the constructors are deprecated and always allocate a new object.",
		Recommendation: "replace `new Integer(n)` with `Integer.valueOf(n)` or autoboxing and `new String(\"a\")` with `\"a\"`. code that relies on a distinct identity for `==` or locking must change first.",
		check:          checkJavaWrapperConstructor,
	},
	{
		ID:             "java-try-with-resources",
		Langs:          langJava,
		Description:    "prefer try-with-resources over closing a resource in `finally`, as it closes the resource in every case and keeps suppressed exceptions.",
		Recommendation: "declare the resource in the try header, for example `try (Reader r = new FileReader(f)) { ... }`, and drop the null check and `close()` from the finally block.",
		check:          checkJavaTryWithResources,
	},
	{
		ID:             "java-replace-all-literal",
		Langs:          langJava,
		Description:    "prefer `replace()` over `replaceAll()` when the pattern has no regular expression syntax, as it skips compiling a pattern.",
		Recommendation: "call `replace()` with the same arguments. `replace()` takes the replacement literally, so a `$` or `\\` in a computed replacement no longer refers to groups.",
		check:          checkJavaReplaceAll,
	},
	{
		ID:             "java-string-concat-in-loop",
		Langs:          langJava,
		Description:    "prefer a `StringBuilder` over concatenating strings in a loop, as each `+=` copies the whole string built so far.",
		Recommendation: "create a `StringBuilder` before the loop, call `append()` inside it and `toString()` after it.",
		check:          checkJavaConcatInLoop,
	},
	{
		ID:             "java-entry-set-iteration",
		Langs:          langJava,
		Description:    "prefer iterating `entrySet()` over looking up each key of `keySet()`, as it saves a map lookup per entry.",
		Recommendation: "loop over `map.entrySet()` and use `getKey()` and `getValue()` of each entry.",
		check:          checkJavaEntrySet,
	},
	{
		ID:             "java-instanceof-pattern",
		Langs:          langJava,
		Description:    "prefer a pattern variable in `instanceof` over casting after the test, as it is more concise and cannot cast to the wrong type.",
		Recommendation: "bind a variable in the test, for example `if (o instanceof Rect r)`, and use it instead of the cast. this needs Java 16 or later.",
		check:          checkJavaInstanceofPattern,
	},
	{
		ID:             "java-empty-collection-constant",
		Langs:          langJava,
		Description:    "prefer `Collections.emptyList()` and its siblings over the raw `EMPTY_` constants, as the methods are type safe.",
		Recommendation: "replace `EMPTY_LIST` with `emptyList()`, `EMPTY_MAP` with `emptyMap()` or `EMPTY_SET` with `emptySet()`.",
		check:          checkJavaEmptyCollectionConstant,
	},
}

var (
	// javaOperandStarts and javaOperandEnds are the tokens that can surround
	// an equality if no tighter operator binds its operands. In
	// `x + a.size() == 0`, the left side is a sum.
	javaOperandStarts = setOf("", "(", "{", ",", ";", "=", "&&", "||", "?", ":", "return", "->")
	javaOperandEnds   = setOf("", ")", "}", ",", ";", "&&", "||", "?", ":")
)

// checkJavaIsEmpty flags `.size()` and `.length()` compared with zero or one
// (SonarJava S1155 for collections, S7158 for strings). We skip the
// implementation of isEmpty() itself.
func checkJavaIsEmpty(s *source) []finding {
	var out []finding
	for i := 1; i+3 < len(s.code); i++ {
		if !s.seq(i, ".", "size", "(", ")") && !s.seq(i, ".", "length", "(", ")") {
			continue
		}
		start, end := s.chainStart(i), i+4
		if start > i-1 || s.scopeOf(s.code[i].Span.Start).Name == "isEmpty" || !s.javaEmptyReceiver(start, i) {
			continue
		}
		recv := s.codeText(start, i)
		var from, to int
		var empty, ok bool
		if op, n := s.tok(end), s.tok(end+1); javaOperandStarts[s.tok(start-1)] && javaOperandEnds[s.tok(end+2)] {
			empty, ok = javaEmptyTest(op, n)
			from, to = start, end+2
		}
		if op, n := s.tok(start-1), s.tok(start-2); !ok && javaOperandStarts[s.tok(start-3)] && javaOperandEnds[s.tok(end)] {
			empty, ok = javaEmptyTest(javaFlip(op), n)
			from, to = start-2, end
		}
		if !ok {
			continue
		}
		rewrite := recv + ".isEmpty()"
		if !empty {
			rewrite = "!" + rewrite
		}
		out = append(out, finding{span: s.tokSpan(from, to), detail: "`" + s.codeText(from, to) + "` -> `" + rewrite + "`"})
	}
	return out
}

// javaEmptyTest reports whether `size op n` tests for emptiness, and whether
// the test is true if the collection is empty.
func javaEmptyTest(op, n string) (empty, ok bool) {
	switch op + n {
	case "==0", "<1", "<=0":
		return true, true
	case "!=0", ">0", ">=1":
		return false, true
	}
	return false, false
}

// javaFlip mirrors a comparison operator, so `0 < n` reads as `n > 0`.
func javaFlip(op string) string {
	switch op {
	case "<":
		return ">"
	case ">":
		return "<"
	case "<=":
		return ">="
	case ">=":
		return "<="
	}
	return op
}

// checkJavaStringEquality flags `==` and `!=` with a string literal operand
// (SonarJava S4973).
func checkJavaStringEquality(s *source) []finding {
	var out []finding
	for i := 1; i+1 < len(s.code); i++ {
		op := s.tok(i)
		if op != "==" && op != "!=" {
			continue
		}
		lit, from, to, other := -1, 0, 0, ""
		switch {
		case javaStringLit(s.tok(i + 1)):
			if start := s.chainStart(i); start < i {
				lit, from, to, other = i+1, start, i+2, s.codeText(start, i)
			}
		case javaStringLit(s.tok(i - 1)):
			if end := s.chainEnd(i + 1); end > i+1 {
				lit, from, to, other = i-1, i-1, end, s.codeText(i+1, end)
			}
		}
		if lit < 0 {
			continue
		}
		rewrite := s.tok(lit) + ".equals(" + other + ")"
		if op == "!=" {
			rewrite = "!" + rewrite
		}
		out = append(out, finding{span: s.tokSpan(from, to), detail: "`" + s.codeText(from, to) + "` -> `" + rewrite + "`"})
	}
	return out
}

// javaStringLit reports whether t is a string literal or text block. Char
// literals don't count.
func javaStringLit(t string) bool { return strings.HasPrefix(t, `"`) }

// checkJavaPrintStackTrace flags `e.printStackTrace()` without a target
// stream (PMD AvoidPrintStackTrace, formerly SonarJava S1148).
func checkJavaPrintStackTrace(s *source) []finding {
	var out []finding
	for i := 1; i+3 < len(s.code); i++ {
		if !s.seq(i, ".", "printStackTrace", "(", ")") {
			continue
		}
		start := s.chainStart(i)
		if start >= i {
			continue
		}
		e := s.codeText(start, i)
		out = append(out, finding{span: s.tokSpan(start, i+4), detail: "`" + s.codeText(start, i+4) + "` -> `log.error(\"...\", " + e + ")`"})
	}
	return out
}

var javaLegacyClasses = map[string]string{
	"Vector":       "ArrayList",
	"Hashtable":    "HashMap",
	"Stack":        "ArrayDeque",
	"StringBuffer": "StringBuilder",
}

// checkJavaLegacyClass flags the creation of the classes in javaLegacyClasses
// (SonarJava S1149). We skip names that are qualified by another package,
// imported from one or declared in the file.
func checkJavaLegacyClass(s *source) []finding {
	own := map[string]bool{}
	for _, d := range s.f.Decls {
		if _, ok := javaLegacyClasses[d.Name]; ok && !d.Local && d.Kind != code.KindVariable && d.Kind != code.KindField && d.Kind != code.KindParameter {
			own[d.Name] = true
		}
	}
	var out []finding
	for i := 0; i < len(s.code); i++ {
		if s.tok(i) == "import" {
			j := i + 1
			for j < len(s.code) && s.tok(j) != ";" {
				j++
			}
			name := s.tok(j - 1)
			if _, ok := javaLegacyClasses[name]; ok && s.codeText(i+1, j) != "java.util."+name && s.codeText(i+1, j) != "java.lang."+name {
				own[name] = true
			}
			i = j
			continue
		}
		repl, ok := javaLegacyClasses[s.tok(i)]
		if !ok || s.code[i].Kind != code.TokIdent || own[s.tok(i)] {
			continue
		}
		from := i - 1
		if s.seq(i-4, "java", ".", "util", ".") || s.seq(i-4, "java", ".", "lang", ".") {
			from = i - 5
		}
		// We only flag the creation, since that's where the class gets
		// picked. Parameter and return types follow an API that might require
		// it, and so do instanceof tests. An object passed straight to a call,
		// returned from a method declared to return the class, or stored in a
		// field (getters might expose it) has to keep the class too.
		if s.tok(from) != "new" || javaArgStarts[s.tok(from-1)] || s.scopeOf(s.code[i].Span.Start).Type == s.tok(i) || s.javaFieldTarget(from) {
			continue
		}
		out = append(out, finding{span: s.tokSpan(from, i+1), detail: "`" + s.codeText(from, i+1) + "` -> `new " + repl + "`"})
	}
	return out
}

var javaArgStarts = setOf("(", ",", "return")

func (s *source) javaFieldTarget(i int) bool {
	depth := 0
	for j := i - 1; j > 0; j-- {
		switch s.tok(j) {
		case ")", "]":
			depth++
		case "(", "[":
			depth--
		case ";", "{", "}":
			return false
		case "=":
			if depth != 0 || s.code[j-1].Kind != code.TokIdent {
				return false
			}
			d := s.visibleDecl(s.tok(j-1), s.code[j-1].Span.Start)
			return d != nil && d.Kind == code.KindField
		}
		if depth < 0 {
			return false
		}
	}
	return false
}

// javaEmptyReceiver reports whether the code tokens [start, dot) name a
// variable whose declared type has isEmpty(), meaning a String for length()
// or a JDK collection or map for size(). Builders only got isEmpty() in Java
// 15, and other classes with size() might not have it.
func (s *source) javaEmptyReceiver(start, dot int) bool {
	if s.seq(start, "this", ".") {
		start += 2
	}
	if dot != start+1 || s.code[start].Kind != code.TokIdent {
		return false
	}
	d := s.visibleDecl(s.tok(start), s.code[start].Span.Start)
	if d == nil {
		return false
	}
	typ, _, _ := strings.Cut(d.Type, "<")
	if _, last, ok := strings.CutLast(typ, "."); ok {
		typ = last
	}
	if s.tok(dot+1) == "length" {
		return typ == "String"
	}
	return javaCollectionTypes[typ]
}

var javaCollectionTypes = setOf(
	"Collection", "List", "Set", "Map", "Queue", "Deque", "SortedSet", "SortedMap", "NavigableSet", "NavigableMap",
	"ArrayList", "LinkedList", "HashSet", "LinkedHashSet", "TreeSet", "HashMap", "LinkedHashMap", "TreeMap",
	"ArrayDeque", "PriorityQueue", "EnumSet", "EnumMap", "IdentityHashMap", "WeakHashMap",
	"ConcurrentHashMap", "CopyOnWriteArrayList", "Vector", "Stack", "Hashtable",
)

var javaWrappers = setOf("Integer", "Long", "Short", "Byte", "Double", "Float", "Boolean", "Character")

// checkJavaWrapperConstructor flags `new Integer(n)` and the other boxed
// primitive constructors, plus `new String()` and `new String("a")`
// (SonarJava S2129).
func checkJavaWrapperConstructor(s *source) []finding {
	var out []finding
	for i := 0; i+2 < len(s.code); i++ {
		if s.tok(i) != "new" || s.tok(i+2) != "(" {
			continue
		}
		typ := s.tok(i + 1)
		end := s.matchClose(s.code, i+2)
		if end >= len(s.code) {
			continue
		}
		var rewrite string
		switch {
		case javaWrappers[typ] && end > i+3:
			rewrite = typ + ".valueOf(" + s.codeText(i+3, end) + ")"
		case typ == "String" && end == i+3:
			rewrite = `""`
		case typ == "String" && end == i+4 && javaStringLit(s.tok(i+3)):
			rewrite = s.tok(i + 3)
		default:
			continue
		}
		out = append(out, finding{span: s.tokSpan(i, end+1), detail: "`" + s.codeText(i, end+1) + "` -> `" + rewrite + "`"})
	}
	return out
}

// checkJavaTryWithResources flags a try statement whose finally block calls
// `x.close()` on a local variable that was declared with `new` before the
// finally block (SonarJava S2093).
func checkJavaTryWithResources(s *source) []finding {
	var out []finding
	for i := 0; i+1 < len(s.code); i++ {
		if s.tok(i) != "try" || s.tok(i+1) != "{" {
			continue
		}
		j := s.matchClose(s.code, i+1)
		for j+1 < len(s.code) && s.tok(j+1) == "catch" {
			k := s.matchClose(s.code, j+2)
			if k+1 >= len(s.code) || s.tok(k+1) != "{" {
				break
			}
			j = s.matchClose(s.code, k+1)
		}
		if j+2 >= len(s.code) || !s.seq(j+1, "finally", "{") {
			continue
		}
		body := s.f.DeclAt(s.code[i].Span.Start)
		for body != nil && !body.Callable() {
			body = body.Parent
		}
		if body == nil || body.Body.Empty() {
			continue
		}
		first := s.codeIndex(body.Body.Start)
		end := s.matchClose(s.code, j+2)
		for k := j + 3; k+4 < end; k++ {
			if !s.seq(k+1, ".", "close", "(", ")", ";") || s.code[k].Kind != code.TokIdent || s.tok(k-1) == "." {
				continue
			}
			if !s.javaCreatedLocal(s.tok(k), first, j) || s.guardedClose(k) {
				continue
			}
			out = append(out, finding{span: s.tokSpan(k, k+5), detail: "`" + s.codeText(k, k+5) + "` in finally -> `try (... " + s.tok(k) + " = new ...)`"})
		}
	}
	return out
}

// guardedClose reports whether the close at code token k runs under an if
// whose condition is more than a null check of the resource, as in
// `if (!handed) tee.close();`. In the other branch the code hands the
// resource on, so try-with-resources, which always closes it, would be wrong.
func (s *source) guardedClose(k int) bool {
	p := k - 1
	if s.tok(p) == "{" {
		p--
	}
	if s.tok(p) != ")" {
		return false
	}
	open := s.matchOpen(s.code, p)
	if open < 1 || s.tok(open-1) != "if" {
		return false
	}
	cond, name := s.codeText(open+1, p), s.tok(k)
	return cond != name+" != null" && cond != "null != "+name
}

// javaCreatedLocal reports whether the code tokens [from, to) declare the
// local variable name and assign it a new object, either as `T name = new`
// or as `T name = ...` with a later `name = new`.
func (s *source) javaCreatedLocal(name string, from, to int) bool {
	declared, created := false, false
	for k := from; k+2 < to; k++ {
		if s.tok(k) != name || s.code[k].Kind != code.TokIdent || s.tok(k+1) != "=" {
			continue
		}
		if p := s.code[k-1]; p.Kind == code.TokIdent || s.tok(k-1) == ">" {
			declared = true
		}
		if s.tok(k+2) == "new" && s.tok(k-1) != "." {
			created = true
		}
	}
	return declared && created
}

// checkJavaReplaceAll flags `replaceAll("lit", ...)` if the pattern has no
// regular expression metacharacter (SonarJava S5361).
func checkJavaReplaceAll(s *source) []finding {
	var out []finding
	for i := 0; i+4 < len(s.code); i++ {
		if !s.seq(i, ".", "replaceAll", "(") || s.tok(i+4) != "," {
			continue
		}
		lit := s.tok(i + 3)
		if !javaStringLit(lit) || strings.HasPrefix(lit, `"""`) || len(lit) < 3 || strings.ContainsAny(lit[1:len(lit)-1], `\.[]{}()<>*+-=!?^$|`) {
			continue
		}
		end := s.matchClose(s.code, i+2)
		if end >= len(s.code) {
			continue
		}
		args := s.codeText(i+3, end)
		out = append(out, finding{span: s.tokSpan(i+1, end+1), detail: "`replaceAll(" + args + ")` -> `replace(" + args + ")`"})
	}
	return out
}

var javaStringTypes = setOf("String")

// checkJavaConcatInLoop flags string concatenation in loops (SonarJava
// S1643).
func checkJavaConcatInLoop(s *source) []finding {
	return s.stringConcatInLoop(javaStringTypes, "append")
}

// checkJavaEntrySet flags `for (K k : m.keySet())` if the body calls
// `m.get(k)` (SonarJava S2864).
func checkJavaEntrySet(s *source) []finding {
	var out []finding
	for i := 0; i+1 < len(s.code); i++ {
		if s.tok(i) != "for" || s.tok(i+1) != "(" {
			continue
		}
		head := s.matchClose(s.code, i+1)
		if head+1 >= len(s.code) || !s.seq(head-4, ".", "keySet", "(", ")") {
			continue
		}
		colon := i + 2
		for colon < head && s.tok(colon) != ":" {
			colon++
		}
		m := colon + 1
		if colon >= head || s.chainEnd(m) != head || s.tok(colon-1) == "" {
			continue
		}
		key, n := s.tok(colon-1), head-4-m
		bodyEnd := head + 1
		if s.tok(head+1) == "{" {
			bodyEnd = s.matchClose(s.code, head+1)
		} else {
			for bodyEnd < len(s.code) && s.tok(bodyEnd) != ";" {
				bodyEnd++
			}
		}
		for k := head + 1; k+n+4 < bodyEnd; k++ {
			if s.sameTokens(m, k, n) && s.tok(k-1) != "." && s.seq(k+n, ".", "get", "(", key, ")") {
				out = append(out, finding{span: s.tokSpan(m, head), detail: "`" + s.codeText(m, head) + "` with `" + s.codeText(k, k+n+5) + "` -> `" + s.codeText(m, head-4) + ".entrySet()`"})
				break
			}
		}
	}
	return out
}

// checkJavaInstanceofPattern flags `x instanceof T` in an if condition made
// of && terms only, where the guarded block then casts `(T) x` (SonarJava
// S6201). Like Sonar, we stay quiet if the build targets a Java release
// before 16.
func checkJavaInstanceofPattern(s *source) []finding {
	if r := javaRelease(filepath.Dir(s.f.Abs)); r > 0 && r < 16 {
		return nil
	}
	var out []finding
	for i := 0; i+1 < len(s.code); i++ {
		if !s.seq(i, "if", "(") {
			continue
		}
		cond := s.matchClose(s.code, i+1)
		if cond+1 >= len(s.code) || s.tok(cond+1) != "{" {
			continue
		}
		block := s.matchClose(s.code, cond+1)
		for k := i + 2; k < cond; k++ {
			if s.tok(k) == "||" {
				break
			}
			if s.tok(k) != "instanceof" {
				continue
			}
			x := s.chainStart(k)
			t := k + 1
			if x >= k || (s.tok(x-1) != "(" && s.tok(x-1) != "&&") || x-1 < i+1 || s.code[t].Kind != code.TokIdent {
				continue
			}
			if after := s.tok(t + 1); after != ")" && after != "&&" || (after == ")" && t+1 != cond) {
				continue
			}
			if s.hasTopLevel(i+2, cond, "||") || (s.tok(x-1) == "(" && x-1 != i+1) {
				continue
			}
			n := k - x
			for c := cond + 2; c+n+2 < block; c++ {
				if s.seq(c, "(", s.tok(t), ")") && s.sameTokens(x, c+3, n) && s.chainEnd(c+3) >= c+3+n {
					name := strings.ToLower(s.tok(t)[:1]) + s.tok(t)[1:]
					out = append(out, finding{span: s.tokSpan(x, t+1), detail: "`" + s.codeText(x, t+1) + "` -> `" + s.codeText(x, t+1) + " " + name + "`"})
					break
				}
			}
		}
	}
	return out
}

var (
	javaReleases sync.Map // directory -> int
	// javaVersionPattern matches the release a Maven or Gradle build
	// compiles for: <maven.compiler.release>8<, <source>1.8<,
	// JavaVersion.VERSION_11, JavaLanguageVersion.of(17), release = 8.
	javaVersionPattern = regexp.MustCompile(`(?:<(?:maven\.compiler\.)?(?:release|source|target)>|JavaVersion\.VERSION_|JavaLanguageVersion\.of\(|(?:sourceCompatibility|targetCompatibility|release)\s*=\s*['"]?)(?:1[._])?(\d+)`)
)

// javaRelease returns the lowest Java release that the nearest pom.xml,
// build.gradle or build.gradle.kts at or above dir names, or 0 if we can't
// find a release in any of them.
func javaRelease(dir string) int {
	if r, ok := javaReleases.Load(dir); ok {
		return r.(int)
	}
	r := 0
	for _, name := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, m := range javaVersionPattern.FindAllSubmatch(b, -1) {
			if v, err := strconv.Atoi(string(m[1])); err == nil && v > 0 && (r == 0 || v < r) {
				r = v
			}
		}
	}
	if parent := filepath.Dir(dir); r == 0 && parent != dir {
		r = javaRelease(parent)
	}
	javaReleases.Store(dir, r)
	return r
}

var javaEmptyConstants = map[string]string{
	"EMPTY_LIST": "emptyList()",
	"EMPTY_MAP":  "emptyMap()",
	"EMPTY_SET":  "emptySet()",
}

// checkJavaEmptyCollectionConstant flags Collections.EMPTY_LIST, EMPTY_MAP
// and EMPTY_SET (SonarJava S1596).
func checkJavaEmptyCollectionConstant(s *source) []finding {
	var out []finding
	for i := 0; i+2 < len(s.code); i++ {
		repl, ok := javaEmptyConstants[s.tok(i+2)]
		if !ok || !s.seq(i, "Collections", ".") || s.tok(i-1) == "." {
			continue
		}
		out = append(out, finding{span: s.tokSpan(i, i+3), detail: "`" + s.codeText(i, i+3) + "` -> `Collections." + repl + "`"})
	}
	return out
}
