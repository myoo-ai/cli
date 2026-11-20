<!-- markdownlint-disable no-inline-html -->
# myoo-cli

<h1 align="center"><samp>
μ&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;<br>
&nbsp;\&nbsp;&nbsp;&nbsp;&nbsp;<br>
&nbsp;&nbsp;🐄💨
</samp></h1>

Main repository for [μ-cli](https://github.com/myoo-ai/cli) to be primarily used by AI agents.

See <https://github.com/myoo-ai> for the whats and whys.

## Installation

Building from source requires Go 1.27 or newer.

CI/CD creates an automatic release on every tag.

You can download the latest release [on the releases page](https://github.com/myoo-ai/cli/releases/latest).

## Features

Every tool is contained in the CLI binary and can be invoked from the terminal. Most outputs include the "# format: \`...\`" line in the stdout so the caller always knows what the data means. This can be turned off with the `-nf`/`--no-format` flag, to save a couple tokens for frequent runs.

When `code_lint` or `prose_lint` finds no violations, it prints `no violations found` in place of the violations and their format header, including with `-nf`. Both end with `(likely) AI patterns: <n> %`, the estimated likelihood that the input was written by AI (see [How the likelihood works](#how-the-likelihood-works)). `prose_lint --fix` still prints only the resulting text.

Tests run the built-in help and rule listings through `prose_lint`.

If a command failed, the error is in stderr in descriptive format, for example: "err: unable to find symbols matching `hello_world`, try widening the query".

<!-- TODO: examples might not reflect the actual outputs, update once the cli is done -->

### AI prose linter

Best-effort linter for detecting generic AI patterns in prose (written text) in most languages. It detects the language of the text, or takes it from `--lang=<code>`.

```bash
cat > /tmp/prose.txt <<'EOF'
HOW A RELEASE HAPPENS
After all six builds succeed → `gh release create "$GITHUB_REF_NAME" dist/* --verify-tag --generate-notes` — this publishes the release with auto-generated notes. Only this job can write to the repo; everything else is read-only.
It hasn't run on GitHub yet: the package doesn't exist there and this folder isn't a git repo.
EOF
myoo prose_lint /tmp/prose.txt # or: cat /tmp/prose.txt | myoo prose_lint

# attempt to auto-fix all violations:
# myoo prose_lint /tmp/prose.txt --fix > /tmp/prose_fixed.txt

# or just specific nth violations (in this case, 1 and 2):
# myoo prose_lint /tmp/prose.txt --fix=1,2 > /tmp/prose_fixed.txt
```

Sample output. Cmd/ctrl+click a location in the VS Code terminal to open the file at the violation. Locations in text read from stdin have no file part.

```plain
$ myoo prose_lint /tmp/prose.txt
# format: `#<nth violation> [<file>:]<line>:<column>-[<end line>.]<end column> - <rule ID>: <description + proposed fix (if applicable)>`
#1 /tmp/prose.txt:1:1-22 - capitals-as-heading: use of capital letters to signify heading style in non-markdown plaintext prose. write headings in sentence case, or use markdown headings if the output is markdown.
#2 /tmp/prose.txt:2:11-14 - words-instead-of-numbers: use of written numbers in sentences instead of numbers, e.g. `three` -> `3` (`six`). write the number as digits: `three stores` becomes `3 stores`. hyphenated compounds (`two-factor`) and `one` on its own are left alone.
#3 /tmp/prose.txt:2:30-31 - utf8-chars-as-icons: use of utf-8 characters as a replacement for images/icons. in plaintext prose, use ASCII characters instead (for example, `->` for a right arrow). see also skill: no-ai-cliche.
#4 /tmp/prose.txt:2:108-109 - emdash: use of emdash. try to use a different way to join the sentences (for instance, by replacing the emdash with a conjunction or connective such as ", and", ", but", ", so", ", because", ", which" or ", therefore", or by adding a period and starting a new sentence). if the dash must remain, always replace it with a regular dash.
#5 /tmp/prose.txt:2:199-200 - semicolon-inbetween-sentences: use of semicolon as a conjunction in middle of a compound sentence. try to use a different way to join the sentences (for instance, by replacing the semicolon with a conjunction or connective such as ", and", ", but", ", so", ", because", ", which" or ", therefore", or by adding a period and starting a new sentence). alternatively, replace the semicolon with a regular comma if applicable. when one of the sentences is a single word, it is also fine to remove that sentence entirely, for example `Proposed; requires approval` becomes `Requires approval`.
#6 /tmp/prose.txt:3:28-29 - colon-inbetween-sentences: use of colon as a conjunction in middle of a compound sentence. try to use a different way to join the sentences (for instance, by replacing the colon with a conjunction or connective such as ", and", ", but", ", so", ", because", ", which" or ", therefore", or by adding a period and starting a new sentence).
# format: `<log-odds, positive reads as AI> <signal>: <what was counted>`
-2.20 prior: before any evidence
+2.57 prose patterns: 4 scored violations in 48 English words
+0.36 wording: 48 English words, used more than people use them: a 2, how 1, isn't 1, can 1, on 1 (AI-like), this 3, all 1, only 2, there 1, is 1 (human-like)
(likely) AI patterns: 67.4 %
```

Some rules require a skill because the linter cannot detect them. `myoo prose_lint --list-rules --include-recommendations` marks them with `(manual)`:

```plain
unnecessary-context (manual): providing context when not necessary (ex. "What is the capital of France?" -> "The capital of France is Paris. Located on the Seine River in the north-central part of the country, it ...", the second sentence is unnecessary and provides additional information beyond the scope of the original question). avoid polluting outputs with information that is not important or was not requested.
unnecessary-specification (manual): narrowing down assertions with qualifiers nobody asked for (ex. "Does the API support pagination?" -> "Yes, the API supports pagination, specifically for list endpoints, in the current version, when called over HTTPS", where the qualifiers restate scope instead of answering). answer at the precision the reader needs and drop qualifiers that do not change the meaning.
stating-the-obvious (manual): explaining what readers understand intuitively (ex. "Click the copy button to copy the text" next to a copy icon). remove remarks and instructions that do not tell the reader anything new.
```

Besides punctuation and typography, the rules cover chatbot leftovers (`I hope this helps`, `[Your Name]` placeholders, `oaicite` and `utm_source=chatgpt.com` citation artifacts), significance puffery (`stands as a testament`, `plays a pivotal role`), negative parallelism (`it's not X, it's Y`, `the decision is the user's, not the agent's`), list items that open with a bold label (`- **Speed:** ...`), Title Case headings, zero-width and other invisible characters, and English quotes in languages with their own quotation marks (`--lang=sk`, `de`, `cs`, `pl`, `ru` and others). The rules skip code blocks, inline code, HTML `<pre>` and `<code>` elements, URLs and file paths, with one exception. An ellipsis character is never right, so `unicode-ellipsis` reports it inside code and URLs too, and `--fix` replaces it there with 3 periods.

Lists of 3 and negative parallelism are also detected in Czech, Dutch, French, German, Italian, Polish, Portuguese, Russian, Slovak and Spanish, chatbot phrases in most of them. Negative parallelism covers `not only ... but also`, `it's not X, it's Y`, and the `X, not Y` closer where a language marks it clearly (Slovak `VIP je nadstavba, nie prerušenie`, Spanish `una herramienta, no un juguete`, Russian `инструмент, а не игрушка`). `Not X, but Y` (`nicht X, sondern Y`, `no X, sino Y`) is plain grammar in most of these languages, so it isn't reported, as in English. The language comes from the share of each language's function words (articles, pronouns, prepositions, forms of "be") in the text, and the evidence names it (`in 1734 Slovak words`). Text where no language reaches 4 % of the words or several tie, such as Chinese or Hungarian, gets the language-independent rules only, and text of under 20 words counts as English. The words of each language live in one entry of `internal/prose/lang.go`, so adding a language means adding an entry there.

Text in any language is checked for section numbers in headings (`## 1. Overview`, `### 7.1 Setup`, but not a changelog version such as `## 1.2.3`), and for backslash escapes that markdown doesn't need (`## 1\. Goal`, `A \= B`, `\~50 %`), which converters such as a Google Docs export leave behind. `--fix` removes those backslashes. People run converters as often as agents do, so the escapes don't count towards the likelihood. Bold scattered for emphasis is reported too (3 bold spans in a paragraph, or more than one per 40 words). A bold sentence that opens a paragraph counts, a label (`**Speed:**`) doesn't.

English text is also checked for verbs that stand in for `is` or `has` (`serves as the`, `boasts a`), light verbs with an action noun (`performs validation of`), sentences that announce the text (`This guide explains`, `Below you will find`), generic caveats (`make sure to test thoroughly`, `results may vary`) and change summaries that list what was kept (`while preserving existing behavior`). These rules came after the likelihood rates were fit, so prose_lint reports them without counting them in the estimate. Lists of 3 are found when they wrap to the next line too.

A third group of English rules comes from house styles for reports and customer mail. They flag wording that rates, hedges or dramatizes a fact instead of stating it. That covers intensifiers (`very`, `extremely`), adverbs that ask to be believed (`genuinely`, `actually`), `worth noting` and `a notable exception`, false ease (`simply`, `just run`), stacked hedges (`could potentially`), trailing `, making ...` clauses and `verified rather than assuming`. They also flag hollow adjectives (`powerful`, `world-class`), objects that act like people (`the header carries the ID`, `only nine days of logs survive`), gatekeeper imagery and shop talk (`first line of defense`, `blast radius`), and a thing as the subject of what a person needs or can do (`three findings need your attention`, `the CSV lets your team track progress`). The rest cover sales-call verdicts (`is the one`, `your best bet`), slogans (`one command turns it off`), clauses that invent people who failed to act (`logins nobody has accounted for`), instructions about how to regard something (`treat those stores as affected`), corporate words (`utilize`, `obtain`, `regarding`), headings that comment on the document (`, stated up front`) and numbers written as words (`three stores`, fixed to `3 stores`). `one` on its own stays, because it is a pronoun as often as a count. These rules are unscored as well. `code_lint` leaves them out of comments, where people write `just` and `actually` freely.

Skills that clean up AI-generated prose run `prose_lint` in a loop. The agent fixes the reported violations and runs it again until none are left.

`myoo prose_lint --list-rules` prints every rule with its description. Add `--include-recommendations` to also print how to fix violations of each rule. Violation reports always include that advice.

### Word tree

```bash
cat /tmp/prose.txt | myoo word_tree --lang=en
```

Returns an AI-readable tree of synonyms, scores and frequencies for every word in its base form, with the most frequent words being at root. Used for rewriting prose to contain more words that are commonly used by humans.

Sample output:

```plain
$ cat /tmp/prose.txt | myoo word_tree # default lang is en
# format: (roots are input words in base form, most frequent first. children are synonyms ordered by closeness) `<indent>- <word>:L<first line>:<first col>:<count in input>[:<closeness to parent>]:<frequency in the language>`
...
- publish:L2:115:1:0.4862
  - print:L0:0:0:1.0000:0.5528
  - issue:L0:0:0:0.7636:0.5965
  - bring out:L0:0:0:0.6000:0.0000
  - write:L2:182:1:0.5667:0.6673
  ...
...
```

Here this means that "publish" is in the input text 1 time (first at line 2, column 115) and has a frequency score of 0.49 in English (normalized log frequency, 1 is the most common word, 0 means it is not in the frequency list). Its synonyms are ordered by how close they are to it (0 to 1), "write" also appears in the input. The max tree depth is 1 (the synonyms).

Use `myoo word_tree --download-db [--lang=<code>]` to download and build the cached local database of synonyms (LibreOffice thesaurus + OpenSubtitles word frequencies) for quick lookup. This happens automatically on the first run for a language. The cache lives in `$MYOO_CACHE_DIR` or the user cache directory.

Pass `--word="<word1,word2,...>"` instead of text in stdin to display tree for that words only.

#### Synonym rewrite

```bash
cat /tmp/prose.txt | myoo synonym_rewrite --lang=en > /tmp/prose_new.txt
```

Rewrites the prose with synonyms as base words. This is a deterministic algorithm so it might not return the correct form of the words, it needs to be tweaked by human or AI agent afterwards.

Sample output:

```plain
$ cat /tmp/prose.txt
Hello,

We have finished our annual review of assets you sent us. Thank you for your cooperation!

Best regards,
Someone
```

```plain
$ cat /tmp/prose.txt | myoo synonym_rewrite --lang=en
# format: (for every word with a synonym, words without a matching synonym are left as-is) `[<word with matching highest-score synonym>] (<other high-scoring alternative synonyms for the word>)`
[Hi] (howdy, hullo, how-do-you-do),

We have [complete] (end, terminate, stop, ...) our [yearly] (one-year, time period, period of time) [critique] (revue, inspection, recap, ...) of assets you [direct] (ship, station, mail, ...) us. [Give thanks] you for your cooperation!

[Better] (champion, trump, scoop, ...) [respect] (consider, view, see, ...),
[Person] (somebody, individual, mortal, ...)
```

## AI code lint

```bash
# recursively lint all files in current directory:
myoo code_lint .

# or, a specific path:
myoo code_lint calc.py

# skip more paths (comma-separated .gitignore patterns, added to the defaults):
myoo code_lint . -i 'testcases/,*.pb.go'
```

Walking a directory skips `.gitignore`d paths, `testdata` directories, generated files (a `Code generated ... DO NOT EDIT` style header or `.min.` in the name) and paths that match the default patterns. The defaults cover folders of dependencies and build output (`node_modules/`, `vendor/`, `third_party/`, `dist/`, `build/`, `target/`, ...), virtualenvs, test fixtures (`fixture/`, `fixtures/`, `__fixtures__/`), `generated/` folders and `*.min.*` bundles. `-i`/`--ignore-globs` adds patterns, a pattern starting with `!` lints a path the defaults skip (`-i '!vendor/'`), and files named on the command line are always linted. `myoo code_lint --help` lists every default.

Code linter for detecting AI patterns in code. Supports the following programming languages:

- JavaScript and its flavors (TypeScript, Node.js, etc.), including the `<script>` and markup comments of Vue and Svelte components
- Python
- Java
- Go
- C/C++
- C#
- PHP, including templates (`.phtml`) that mix PHP and HTML
- Rust, with crate module trees from `Cargo.toml` and the file layout

Each file is first parsed into an AST of its tokens and comments, and the rules run on that AST.

Duplicate detection compares complete executable statements within their scope, not partial calls or fragments spanning unrelated functions. Literal values and local names may differ, but unrelated nonlocal calls remain distinct. Single registration calls and type-only declarations do not count as duplicated algorithms. Nested callback bodies are still checked. Native keyboard shortcut labels are allowed, and UI error reporters are not assumed to be loggers merely because a method is named `error`. A string literal made only of glyphs (`"▶"`, `"• • •"`, a TeX symbol table mapping `to` to `"→"`) is the glyph the program draws and does not count as an icon. Neither does an arrow between 2 placeholders (`"{from} → {to}"`), which shows how 2 values relate. Check marks stay icons even on their own.

People write terse comments too (`// create temp dirs` above `os.MkdirTemp`), so the rules that judge what a comment says (`step-comment`, `narrating-comment`, `obvious-comment`, `redundant-docstring`) report a comment only when its score is above a threshold. Each `prose_lint` violation in the comment adds 3, spanning several lines adds 3, 30 or more characters add 3, and an upper-case first letter adds 1. A comment needs a score of 4, so no single signal is enough. `placeholder-code` reports only code, such as an empty body marked TODO or a body that only raises a "not implemented" error. A comment such as `// not implemented` above working code is not a placeholder.

`comment-prose` runs the `prose_lint` rules over comments and docstrings, for example a semicolon or colon that joins 2 clauses, an ellipsis character or a stock chatbot phrase. A block of `//` or `#` lines is read as one text, so a sentence can wrap. Lists of 3 (`declarations, references and name resolution`) are reported once a file has 2 or more of them. Comments also get `label-colon`, a colon that attaches an explanation to a short label or a code sample (`// fast path: nothing to do`, `// q.x: member of whatever q resolves to`). Tags such as `TODO:`, `NOTE(kevin):`, `Deprecated:` and `SAFETY:`, issue references (`Issue 22233:`), a URL or a list of single words after the colon are fine, and Python docstrings, which list arguments as `name: description`, don't get it. `label-colon` is house style. People write such labels too, the Go standard library among them. Quoted text, Go doc code blocks, `<pre>` and `<code>` elements of doc comments, commented-out code and license notices are skipped, and like the rules above it reports only comments that score above the threshold. Lists of 3, `label-colon` and `nominalization` came after the likelihood rates were fit and don't count in the estimate.

`definition-comment` flags comments that define the members of a type by their own names, one after another (`// ctx is the innermost declaration`, `// local is the parameter the name binds to`). It reports them when 2 or more members of a type have such a comment and they make up at least half of its documented members. Comments on exported Go names are left alone, since godoc expects them to start with the name. `overblanking` flags function bodies of 8 or more lines with a blank line for every 2 lines of code.

3 phrase rules read the same comment text and report every match, because their wording rarely appears in human comments. `change-narration-comment` flags edit history (`// now uses the shared cache instead of the old loop`, `renamed from`, `NEW:`), but not runtime state such as "the solution that held until now" or deprecation notices. `reviewer-addressed-comment` flags comments that speak to the reviewer (`As requested,`, `adjust as needed`, `Note for reviewers`). `demo-grade-comment` flags admitted stubs (`In a real implementation ...`, `In production, you would ...`, `This is just a placeholder.`) outside tests and example programs. It leaves "for simplicity" alone, which in human code introduces a stated trade-off, and a phrase the sentence goes on to qualify ("This is a stub version which returns v when unsafe is unavailable").

String literals outside tests have 3 more rules. `generic-error-message` flags an error raised or returned with only a generic message, such as `throw new Error("Invalid input")`, `raise ValueError("failed")`, `errors.New("unknown error")` or `bail!("Something went wrong")`. Go sentinel errors (`var errInvalid = errors.New("invalid input")`) are allowed. `cheerful-message` flags copy such as `"Oops! Something went wrong."` or `"Saved successfully!"`, while `"oops: conversion to untyped"` in a panic is left alone. `placeholder-data` flags lorem ipsum, John Doe and API key placeholders, except in fixtures, examples and values assigned to a placeholder. In tests too, `tautological-assertion` flags assertions that compare literals or assert a literal true (`expect(1).toBe(1)`, `assert True`, `assertEquals("a", "a")`, `assert!(true)`, `assert.True(t, true)`), which cannot fail.

Besides AI patterns, language rules adapted from SonarQube, staticcheck, gopls, pylint, clang-tidy, PHPStan, Rector, PHP_CodeSniffer and Clippy flag code that has a more idiomatic replacement, for example `prev && prev.style` in TypeScript (`ts-missing-optional-chain-expression`, use `prev?.style`). Their IDs start with the language: `ts-` (TypeScript and JavaScript), `py-`, `go-`, `java-`, `cs-`, `c-` (C and C++), `cpp-` (C++ only), `php-` and `rust-`. They match token patterns without type information, so each rule skips the cases where the rewrite could change behavior. 2 of them catch ways around a check that agents reach for. `ts-double-type-assertion` flags `x as unknown as T` outside tests unless a comment on the line or above gives the reason, and `rust-expect-empty-message` flags `.expect("")`.

Sample input:

```python
#!/usr/bin/env python3
"""
maze.py - Generate a random maze and solve it, right in your terminal.

Usage:
    python maze.py                 # default 20x10 maze
    python maze.py -W 30 -H 15     # custom size
    python maze.py --seed 42       # reproducible maze
    python maze.py --no-solve      # just the maze, no solution
"""

# ...
```

Sample output:

```plain
$ myoo code_lint maze.py
# format: `#<nth violation> <file>:<line>:<column>-[<end line>.]<end column> - <rule ID>: <description + proposed fix (if applicable)>`
#1 maze.py:2:1-10.4 - comments-in-file-header: use of docstring/explanatory comments in file header. in general, explanations should be part of the documentation outside of the code itself (external applications like Notion or wikis, or markdown files), however there are sometimes cases where this is warranted (eg. for purposes of generating an automated documentation), in that case it is acceptable to ignore this.
# format: `<log-odds, positive reads as AI> <signal>: <what was counted>`
-2.20 prior: before any evidence
-0.19 comment voice: 67 % of 3 function words in English comments are human-style (will, to, if, we, ...), human code has ~80 %, AI code ~35 %
-0.10 comment prose patterns: 0 comment-prose violations in 46 comment words
+0.05 first person, contractions and interjections: 0 first-person words, contractions and interjections in 46 comment words
+0.01 TODO markers: 0 TODO/FIXME/XXX/HACK markers in 46 comment words
+0.01 questions in comments: 0 questions in 46 comment words
(likely) AI patterns: 8.2 %
```

List of all rules: [`internal/codelint/rules.go`](internal/codelint/rules.go) and the language rules in `internal/codelint/lang_*.go`, or run `myoo code_lint --list-rules [--include-recommendations]`. Suppress a finding with a comment containing `myoo-ignore` on the same line or the line above. `myoo-ignore: rule-a, rule-b` limits it to those rules, and `myoo-ignore-file[: rules]` anywhere in a file applies to the whole file.

The builtin backend of the `lsp` commands and `out code_graph` use the same parsers.

## How the likelihood works

The last line of `code_lint` and `prose_lint` estimates how likely the input was written by AI. It is a naive Bayes combination of a few signals, each compared with how it is distributed in human and in AI written input. A signal is a rate (occurrences per 1000 words or lines) or a share. Across the reference projects its logarithm (or logit) is roughly normal in each group, and the signal adds the log-likelihood ratio of the observed value to the log-odds. Small inputs measure a rate imprecisely, so their evidence shrinks towards zero. Every contribution is bounded, and the sum starts from a prior of 10 %. Input without evidence therefore stays near 10 %, and empty input reports 0 %.

The `code_lint` signals were fit on 20 human codebases (projects with anti-AI contribution policies and code from before 2023: chipmunkmod, ZenMod, chezmoi, jsoup, Catima, poezio, borgmatic, yt-dlp, wafrn, Peacock, PouchDB, MCGalaxy, FNA, stb, mGBA, dillo, GoToSocial, Epicyon and 2 mixed projects) and 12 codebases written by AI agents. They were then evaluated on those plus 8 held-out human codebases (Glasgow, Iceshrimp.NET, jqwik, ClassiCube, Telegraf, snac2, BusKill, selfoss) and 3 held-out AI codebases, 28 and 15 in total. The bound of the block label signal was raised after looking at one held-out AI codebase. The `prose_lint` pattern and bold signals were fit on the markdown documentation of 25 human and 14 AI projects, without a held-out set.

The English wording signal of `prose_lint` is a logistic regression on how often a text uses each of about 1300 common words, question marks and exclamation marks. It was trained on 389 pairs of a human text and a text an AI agent (Claude, 2026) wrote on the same topic and in the same genre: news, stories, reviews, abstracts and forum posts from the MAGE dataset, answers from the HC3 dataset and sections of the human projects' documentation. Pairing by topic keeps it from learning what people and agents write about. On 99 held-out pairs it ranks the AI text above the human one 92 % of the time. Its evidence line names the words that pushed the estimate furthest each way. People write `however`, `would`, `I`, `we`, questions and exclamations more often than agents do, and agents write `every`, `its` and `own` more often. People also write more parentheses and semicolons than Claude does in the pairs, but agent-written documentation is full of them, so they don't count. [`internal/prose/train_wording.py`](internal/prose/train_wording.py) regenerates the model.

- `code_lint`: every human codebase scores 0.2 % or less, the held-out ones included. 12 of the 15 AI codebases score above 90 % (1 of the 3 held-out ones). The 3 misses are small (2 to 9 thousand lines of code) and their comments read like a person's.
- `prose_lint`: with all documents of a project pooled, 23 of the 25 human projects score below 10 % (the 459 words of MCGalaxy's docs, with 19 bold spans, score 52 % and the 363 words of jsoup's 24 %) and 13 of the 14 AI projects with documentation above 50 % (8 above 90 %). A single document is a smaller sample: 75 % of AI documents and 2 % of human documents score above 50 % (1 % without Telegraf's plugin READMEs, which a generator decorates with emoji and bold field names).
- Text in other languages has no wording signal. In markdown the bold signal adds to the pattern rules, so a document can score above 90 %. Plain text relies on the pattern rules alone and stays below about 70 %. The rules and the bold signal were checked on 61 Wikipedia articles from 2020 in Slovak, Czech, Polish, German, French and Spanish (52 score below 20 %, 2 above 50 %) and on 25 documents an AI agent wrote in those languages (21 score above 50 %, 10 above 90 %).
- AI detectors such as ZeroGPT, Sapling and QuillBot are trained classifiers and disagree with each other and with `prose_lint`. A low score here doesn't mean a detector will pass the text.

| Signal | `code_lint` | `prose_lint` |
| --- | --- | --- |
| voice: share of human-style function words (`will`, `to`, `if`, `we`, `this`, `should`, ...) among those and AI-style ones (`every`, `its`, `per`, `without`, `once`, `and`, ...), English only | comments | |
| wording: logistic regression on the rates of common words, question and exclamation marks, English only | | text |
| pattern violations per 1000 words | `comment-prose` | scored rules |
| bold spans per 1000 words, labels and table cells included, markdown only | | text |
| TODO/FIXME markers, questions, first person and contractions per 1000 words (people leave them, agents rarely do) | comments | |
| em dashes, utf-8 icons, step comments and block labels (`// Validate input`) per 1000 lines | code | |

Function length, nesting, duplication and other structure metrics did not separate the 2 groups and do not count. The rates live in `internal/codelint/likelihood.go` and `internal/prose/likelihood.go`.

## LSP bridge for AI

Some harnesses can communicate with LSP directly, however this might not be available in every harness. This tool holds several sub-commands that communicate with the matching LSP for the detected or selected programming language in the project.

```bash
myoo lsp --help
```

The commands use the project's language server when one is installed (gopls, basedpyright/pyright, pylsp, jedi-language-server, typescript-language-server, vtsls, clangd, jdtls, csharp-ls, OmniSharp, intelephense, phpactor, rust-analyzer) and the builtin index otherwise. Pick explicitly with `--backend=auto|builtin|lsp` or `--server="<command>"`.

### Find original definition of a symbol

```bash
myoo lsp definition <location>
```

`<location>` here refers to a structured point to the symbol we want to lookup. Format: `[<input_file>:][L<line_from>[-L<line_to>]:]<symbol fuzzy match, start with ! to match exactly>`, `input_file` and the line range (each with its separator) are optional.

Example inputs:

```python
# ./calculator.py
# source for calculator
def calculate(a, b):
    """sums `a` and `b`"""
    return a + b
```

```python
# ./main.py
from calculator import calculate

print(calculate(1, 1))
```

Example outputs:

```plain
# matches `calculator` and `calculate`
$ myoo lsp definition main.py:calc
# format: (for each matching symbol) `<symbol>:<fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)\n\t<source, max. 50 lines counting from top to bottom>\n\n`
calculator:calculator (calculator.py:1:1)
    # source for calculator
    def calculate(a, b):
        """sums `a` and `b`"""
        return a + b

calculate:calculator.calculate (calculator.py:2:5)
    def calculate(a, b):
        """sums `a` and `b`"""
        return a + b
```

```plain
# narrowing down to line range and exact `a` match
$ myoo lsp definition calculator.py:L4:!a
# format: (for each matching symbol) `<symbol>:<fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)\n\t<source, max. 50 lines counting from top to bottom>\n\n`
calculate:calculator.calculate->a (calculator.py:2:15)
    def calculate(a, b):
        """sums `a` and `b`"""
        return a + b
```

### Find all references to a symbol

Working with inputs above:

```plain
$ myoo lsp references main.py:alcu
# format: (for each matching symbol with one or more references) `<fqn>[-><local/temporary symbols>]\n- <reference fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)`
calculator
- main.calculator (main.py:1:6)

calculator.calculate
- main.calculate (main.py:1:24)
- main.calculate (main.py:3:7)
```

### Rename a symbol

<TODO: does this need line number + col to narrow down eg. local symbols that could match multiple? the general assumption is that methods usually do not have duplicate local symbols, are there any edge cases/examples when this isn't true?>

```bash
myoo lsp rename <fqn>[-><local/temporary symbols>] <new name> [-c/--include-comments] [--dry-run]
```

`--include-comments` attempts to find matching params and variable names in docstrings/comments and renames these too.
Note: in general, code shouldn't have much comments, so this is just a small, not-always-reliable helper (could match words/non-actual variables).
`--dry-run` prints the same output without writing any file.

Examples:

```plain
$ myoo lsp rename calculator.calculate->a first_number -c
# format: `<fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)\n\t<new source, max. 50 lines counting from top to bottom>\n\n- <changed file> (<n> edits)`
calculator.calculate->first_number (calculator.py:2:15)
    def calculate(first_number, b):
        """sums `first_number` and `b`"""
        return first_number + b

- calculator.py (3 edits)
```

```plain
$ myoo lsp rename calculator.calculate add
# format: `<fqn>[-><local/temporary symbols>] (<file>:<line>:<col>)\n\t<new source, max. 50 lines counting from top to bottom>\n\n- <changed file> (<n> edits)`
calculator.add (calculator.py:2:5)
    def add(first_number, b):
        """sums `first_number` and `b`"""
        return first_number + b

- calculator.py (1 edit)
- main.py (2 edits)
```

## Outputs readable by humans

Agents can use `myoo out` to render images for people.

### Code graph

Visualizes the (call) tree of all symbols inside a project or file or specific symbol.

```bash
myoo out code_graph <directory or file or fqn of symbol> [-f/--format=<svg or png>] [-o/--out=<path to output file>] [--depth=<n>]
```

A directory with more than 60 functions is drawn as calls between its files. Pass a file or a symbol (with `--depth` levels of callers and callees) to see its functions.

example:

```plain
$ myoo out code_graph .
# format: `<full path to output file>`
/tmp/myoo_code_graph_Users-example-projects-python-calculator.png
```

The renderer uses [`myoo-ai/uml`](https://github.com/myoo-ai/uml) graph network renderer under the hood. SVG and PNG are rendered directly in Go.
