package codelint

import (
	"slices"
	"strings"

	"github.com/myoo-ai/cli/internal/code"
)

// Rule is a single code_lint check.
type Rule struct {
	ID             string
	Description    string
	Recommendation string
	// Langs limits the rule to these languages. If it's nil, the rule runs
	// on all of them.
	Langs []code.Lang
	order int
	// If several rules that judge comment wording match the same comment, we
	// only report the first in catalogue order.
	commentWording bool
	// Findings are comments, reported only if their aiScore is above
	// aiScoreThreshold.
	scored   bool
	check    func(s *source) []finding
	checkAll func(srcs []*source) []finding // replaces check for rules that compare files
}

// finding is a rule hit. If detail is set, we append it to the description.
// unscored findings are reported but don't count towards the likelihood.
type finding struct {
	src      *source
	span     code.Span
	detail   string
	unscored bool
}

type violation struct {
	finding
	rule *Rule
}

func (v violation) description() string {
	if v.detail == "" {
		return v.rule.Description
	}
	return strings.TrimSuffix(v.rule.Description, ".") + " (" + v.detail + ")."
}

func (r *Rule) appliesTo(l code.Lang) bool {
	if r.Langs == nil {
		return true
	}
	return slices.Contains(r.Langs, l)
}

func (r *Rule) langList() string {
	if r.Langs == nil {
		return "all"
	}
	names := make([]string, len(r.Langs))
	for i, l := range r.Langs {
		names[i] = string(l)
	}
	return strings.Join(names, ", ")
}

func ruleByID(id string) *Rule {
	for _, r := range Rules {
		if r.ID == id {
			return r
		}
	}
	return nil
}

const ignoreHint = "if the pattern is intended, add a comment containing `myoo-ignore: %s` on the line before (or `myoo-ignore-file: %s` anywhere in the file)."

func withIgnoreHint(id, text string) string {
	return text + " " + strings.ReplaceAll(ignoreHint, "%s", id)
}

// Rules is the catalogue in the order --list-rules prints it, with the rules
// for all languages first and the language rules from lang_*.go after them.
var Rules = slices.Concat(coreRules, tsRules, pyRules, goRules, javaRules, csRules, cRules, phpRules, rustRules)

var coreRules = []*Rule{
	{
		ID:             "comments-in-file-header",
		Description:    "use of docstring/explanatory comments in file header.",
		Recommendation: withIgnoreHint("comments-in-file-header", "in general, explanations should be part of the documentation outside of the code itself (external applications like Notion or wikis, or markdown files), however there are sometimes cases where this is warranted (eg. for purposes of generating an automated documentation), in that case it is acceptable to ignore this."),
		check:          checkFileHeader,
	},
	{
		ID:             "decorative-comment",
		Description:    "use of banner or separator comments made of repeated characters.",
		Recommendation: "remove the decoration. if the file needs sections, split it into smaller files or let the declarations speak for themselves.",
		check:          checkDecorative,
	},
	{
		ID:             "step-comment",
		Description:    "use of numbered or sequenced comments that narrate the steps of the code.",
		Recommendation: "remove the step comments. if a step needs a name, extract it into a well-named function. keep comments only for why the code does something non-obvious.",
		commentWording: true,
		scored:         true,
		check:          checkStepComments,
	},
	{
		ID:             "narrating-comment",
		Description:    "comment narrates what the code does instead of explaining why.",
		Recommendation: "remove the comment, or explain the reason or constraint behind the code.",
		commentWording: true,
		scored:         true,
		check:          checkNarrating,
	},
	{
		ID:             "obvious-comment",
		Description:    "comment repeats the code it is attached to.",
		Recommendation: "remove the comment, the code already says this.",
		commentWording: true,
		scored:         true,
		check:          checkObvious,
	},
	{
		ID:             "comment-prose",
		Description:    "comment contains a prose pattern that reads as AI generated.",
		Recommendation: "rewrite the comment as plain sentences: split clauses joined by a semicolon or colon, drop stock phrases, and use ASCII punctuation. `myoo prose_lint --list-rules --include-recommendations` explains the named prose rule.",
		scored:         true,
		check:          checkCommentProse,
	},
	{
		ID:             "string-prose",
		Description:    "string literal contains a prose pattern that reads as AI generated.",
		Recommendation: "rewrite the text as plain sentences. `myoo prose_lint --list-rules --include-recommendations` explains the named prose rule and how to fix it. the rule reads string literals outside tests that are sentences of 8 or more words of running prose, such as a message or UI copy.",
		check:          checkStringProse,
	},
	{
		ID:             "change-narration-comment",
		Description:    "comment describes the edit history instead of the code.",
		Recommendation: "remove the comment, the commit message is the place for history. if the reader needs to know something, say what the code does now without comparing it with an earlier version.",
		check:          checkChangeNarration,
	},
	{
		ID:             "reviewer-addressed-comment",
		Description:    "comment speaks to the reviewer or the person who asked for the change.",
		Recommendation: "remove the comment, or move it to the pull request description. a value the reader is expected to change belongs in configuration.",
		check:          checkReviewerAddressed,
	},
	{
		ID:             "demo-grade-comment",
		Description:    "comment admits that the code is a simplification or a stub.",
		Recommendation: "implement the real behavior, or state the actual limitation and why it is acceptable here (for example `// single process, tokens die with it`). the rule skips test files.",
		check:          checkDemoGrade,
	},
	{
		ID:             "placeholder-code",
		Description:    "placeholder code left in the source (an empty body marked TODO, or a not implemented error).",
		Recommendation: "write the actual implementation, or remove the stub. for an abstract method use the language's abstract/interface mechanism instead.",
		check:          checkPlaceholderCode,
	},
	{
		ID:             "placeholder-data",
		Description:    "sample data left in a string literal (`lorem ipsum`, `John Doe`, an API key placeholder).",
		Recommendation: "use real configuration, or fail with a clear error when the value is missing. the rule skips tests, fixtures, examples and values meant as placeholders, such as `placeholder: \"John Doe\"`.",
		check:          checkPlaceholderData,
	},
	{
		ID:             "redundant-docstring",
		Description:    "documentation only restates the name of the declaration.",
		Recommendation: "remove the doc comment, or describe what a reader cannot infer from the name (behavior, units, side effects, errors).",
		scored:         true,
		check:          checkRedundantDoc,
	},
	{
		ID:             "definition-comment",
		Description:    "comments define the members of a type by their own names, one after another (`ctx is the innermost declaration`, `local is the parameter the name binds to`).",
		Recommendation: "delete the comments that the name and type already explain. where a member needs one, say only what the declaration can't (a unit, an invariant, why it exists), without the `<name> is` opener. comments on exported Go names are left alone, since godoc expects them to start with the name.",
		commentWording: true,
		check:          checkDefinitionComments,
	},
	{
		ID:             "excessive-comments",
		Description:    "function body is dominated by comments.",
		Recommendation: "remove comments that restate the code. prefer descriptive names and small functions. keep only comments that explain why.",
		check:          checkExcessiveComments,
	},
	{
		ID:             "overblanking",
		Description:    "function body has a blank line for every 2 lines of code or more.",
		Recommendation: "keep blank lines only between groups of statements that belong together, and remove the rest.",
		check:          checkOverblanking,
	},
	{
		ID:             "emoji-in-code",
		Description:    "use of emoji in comments or string literals.",
		Recommendation: "remove the emoji. use words or an output status indicator, such as an exit code or log level.",
		check:          checkEmoji,
	},
	{
		ID:             "utf8-icons-in-code",
		Description:    "use of utf-8 characters (arrows, check marks, bullets, box drawing) as icons in comments or string literals.",
		Recommendation: "use plain ASCII (-> instead of an arrow, - or * for bullets) or words.",
		check:          checkIcons,
	},
	{
		ID:             "emdash-in-code",
		Description:    "use of emdash in comments or string literals.",
		Recommendation: "split the sentence, or join the parts with a comma, parentheses or a regular dash.",
		check:          checkEmdash,
	},
	{
		ID:             "ascii-banner-output",
		Description:    "printing separator lines or banners made of repeated characters.",
		Recommendation: "drop the separator. structure output with blank lines, headings or a proper table/log format.",
		check:          checkBannerOutput,
	},
	{
		ID:             "cheerful-message",
		Description:    "chatty or celebratory string literal (\"Oops\", \"Yay\", \"Great!\", \"successfully!\").",
		Recommendation: "state the result plainly (`Saved 3 files`), and on an error say what failed and what the user can do. the rule skips test files.",
		check:          checkCheerfulMessage,
	},
	{
		ID:             "generic-error-message",
		Description:    "error raised with a generic message that names neither what failed nor the value.",
		Recommendation: "say what failed and include the value or the cause, for example `port must be 1-65535, got 70000` instead of `Invalid input`. the rule skips test files and Go sentinel errors (`var errInvalid = errors.New(\"invalid input\")`), which get their context from the callers that wrap them.",
		check:          checkGenericErrorMessage,
	},
	{
		ID:             "no-formatted-string-placeholders",
		Description:    "formatted string without any placeholders.",
		Recommendation: "use a plain string literal, or the call that does not format (for example `errors.New` instead of `fmt.Errorf`). where arguments follow the format string, add the placeholders that use them.",
		check:          checkFormatPlaceholders,
	},
	{
		ID:             "swallowed-exception",
		Langs:          []code.Lang{code.Go, code.Python, code.JavaScript, code.TypeScript, code.Java, code.CSharp, code.CPP, code.PHP, code.Rust},
		Description:    "broad exception handler that ignores the error (empty, pass/continue, or only logs).",
		Recommendation: withIgnoreHint("swallowed-exception", "catch the specific exception you expect and handle it, or let it propagate (re-raise/return the error). if ignoring is deliberate, say why in a comment inside the handler. in Rust, handle the `Result` or propagate it with `?` instead of `let _ =`, `.ok();` or an empty `Err(_)` arm."),
		check:          checkSwallowed,
	},
	{
		ID:             "suppressed-diagnostic",
		Description:    "diagnostic suppressed without a justification.",
		Recommendation: "fix the underlying issue. if the suppression is necessary, narrow it to the specific code and add the reason after it (eg. `# noqa: E501 - long URL`, `//nolint:errcheck // best-effort cleanup`).",
		check:          checkSuppressed,
	},
	{
		ID:             "tautological-assertion",
		Description:    "assertion compares literals or asserts a literal true, so it cannot fail.",
		Recommendation: "assert the behavior under test, comparing the value the code returns with the expected literal. remove the assertion when there is nothing to check.",
		check:          checkTautologicalAssertion,
	},
	{
		ID:             "import-inside-function",
		Description:    "import statement inside a function body.",
		Langs:          []code.Lang{code.Python},
		Recommendation: withIgnoreHint("import-inside-function", "move the import to the top of the module, unless it breaks an import cycle or defers a heavy optional dependency. explain such exceptions in a nearby comment."),
		check:          checkImportInFunction,
	},
	{
		ID:             "long-function",
		Description:    "function has more than 80 lines of code.",
		Recommendation: "split it into smaller functions with descriptive names, or move data tables out of the function.",
		check:          checkLongFunction,
	},
	{
		ID:             "deep-nesting",
		Description:    "control flow nested more than 4 levels deep.",
		Recommendation: "flatten it with early returns/continue, or extract the inner blocks into functions.",
		check:          checkDeepNesting,
	},
	{
		ID:             "duplicated-block",
		Description:    "block of code duplicates another block.",
		Recommendation: "extract the shared code into a function (or a table-driven loop) and call it from both places.",
		checkAll:       checkDuplicates,
	},
}

func init() {
	for i, r := range Rules {
		r.order = i
	}
}
