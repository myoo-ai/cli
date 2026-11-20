package javascript

import (
	"slices"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

func describe(t token, src string) string {
	name := t.kind.String()
	switch t.sub {
	case subRegex:
		name = "regex"
	case subTmplFull:
		name = "tmpl"
	case subTmplHead:
		name = "tmpl-head"
	case subTmplMiddle:
		name = "tmpl-mid"
	case subTmplTail:
		name = "tmpl-tail"
	case subPrivate:
		name = "private"
	case subJSXOpen:
		name = "jsx-open"
	case subJSXClose:
		name = "jsx-close"
	case subJSXEnd:
		name = "jsx-end"
	case subJSXSelf:
		name = "jsx-self"
	case subJSXName:
		name = "jsx-name"
	case subJSXText:
		name = "jsx-text"
	case subJSXString:
		name = "jsx-string"
	}
	return name + " " + src[t.start:t.end]
}

func TestLex(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		jsx, ts bool
		want    []string
	}{
		{
			name: "regex after assignment",
			src:  `x = /ab+c/i.test(s)`,
			want: []string{"ident x", "punct =", "regex /ab+c/i", "punct .", "ident test", "punct (", "ident s", "punct )"},
		},
		{
			name: "division after identifiers",
			src:  `a / b / c`,
			want: []string{"ident a", "punct /", "ident b", "punct /", "ident c"},
		},
		{
			name: "division after parenthesis and bracket",
			src:  `(a) / 2 + x[0] / 3`,
			want: []string{"punct (", "ident a", "punct )", "punct /", "number 2", "punct +", "ident x", "punct [", "number 0", "punct ]", "punct /", "number 3"},
		},
		{
			name: "regex after keyword",
			src:  `return /x/g`,
			want: []string{"keyword return", "regex /x/g"},
		},
		{
			name: "division after this",
			src:  `this / 2`,
			want: []string{"keyword this", "punct /", "number 2"},
		},
		{
			name: "slash inside class",
			src:  `f(/[/\]]+/, /=/)`,
			want: []string{"ident f", "punct (", `regex /[/\]]+/`, "punct ,", "regex /=/", "punct )"},
		},
		{
			name: "divide assign",
			src:  `a /= 2`,
			want: []string{"ident a", "punct /=", "number 2"},
		},
		{
			name: "regex at statement start after block",
			src:  "}\n/re/.test(x)",
			want: []string{"punct }", "regex /re/", "punct .", "ident test", "punct (", "ident x", "punct )"},
		},
		{
			name: "regex in template substitution",
			src:  "`${/a/.source}`",
			want: []string{"tmpl-head `${", "regex /a/", "punct .", "ident source", "tmpl-tail }`"},
		},
		{
			name: "nested templates",
			src:  "`a${b + `c${d}`}e`",
			want: []string{"tmpl-head `a${", "ident b", "punct +", "tmpl-head `c${", "ident d", "tmpl-tail }`", "tmpl-tail }e`"},
		},
		{
			name: "object literal inside substitution",
			src:  "`${ {a: 1}.a }x${y}`",
			want: []string{"tmpl-head `${", "punct {", "ident a", "punct :", "number 1", "punct }", "punct .", "ident a", "tmpl-mid }x${", "ident y", "tmpl-tail }`"},
		},
		{
			name: "division after template",
			src:  "`x` / 2",
			want: []string{"tmpl `x`", "punct /", "number 2"},
		},
		{
			name: "escaped backtick and dollar",
			src:  "`a\\`b$c\\${d}`",
			want: []string{"tmpl `a\\`b$c\\${d}`"},
		},
		{
			name: "numbers",
			src:  `1_000n 0xFF_FFn .5e-3 0b1010 0o17 1e+10 017 1.5`,
			want: []string{"number 1_000n", "number 0xFF_FFn", "number .5e-3", "number 0b1010", "number 0o17", "number 1e+10", "number 017", "number 1.5"},
		},
		{
			name: "strings with escapes",
			src:  `'it\'s' "a\"b" 'line\` + "\n" + `cont'`,
			want: []string{`string 'it\'s'`, `string "a\"b"`, "string 'line\\\ncont'"},
		},
		{
			name: "unterminated string stops at newline",
			src:  "'abc\nx",
			want: []string{"string 'abc", "ident x"},
		},
		{
			name: "private names and optional chaining",
			src:  `this.#x?.y ?? a?.[0] ? .5 : 1`,
			want: []string{"keyword this", "punct .", "private #x", "punct ?.", "ident y", "punct ??", "ident a", "punct ?.", "punct [", "number 0", "punct ]", "punct ?", "number .5", "punct :", "number 1"},
		},
		{
			name: "jsx element with text and expressions",
			src:  `x = <div className="a b">hi {name} isn't</div>;`,
			jsx:  true,
			want: []string{"ident x", "punct =", "jsx-open <", "jsx-name div", "jsx-name className", "punct =", `jsx-string "a b"`, "jsx-end >", "jsx-text hi", "punct {", "ident name", "punct }", "jsx-text isn't", "jsx-close </", "jsx-name div", "jsx-end >", "punct ;"},
		},
		{
			name: "jsx nesting, fragments and self closing",
			src:  `f(<><Foo.Bar a={<b/>} {...p} /></>) / 2`,
			jsx:  true,
			want: []string{"ident f", "punct (", "jsx-open <", "jsx-end >", "jsx-open <", "jsx-name Foo", "punct .", "jsx-name Bar", "jsx-name a", "punct =", "punct {", "jsx-open <", "jsx-name b", "jsx-self />", "punct }", "punct {", "punct ...", "ident p", "punct }", "jsx-self />", "jsx-close </", "jsx-end >", "punct )", "punct /", "number 2"},
		},
		{
			name: "less than after identifier is not jsx",
			src:  `a < b && c > d`,
			jsx:  true,
			want: []string{"ident a", "punct <", "ident b", "punct &&", "ident c", "punct >", "ident d"},
		},
		{
			name: "type assertion in ts is not jsx",
			src:  `x = <T>y`,
			ts:   true,
			want: []string{"ident x", "punct =", "punct <", "ident T", "punct >", "ident y"},
		},
		{
			name: "generic arrow in tsx is not jsx",
			src:  `f = <T,>(x: T) => x`,
			jsx:  true,
			ts:   true,
			want: []string{"ident f", "punct =", "punct <", "ident T", "punct ,", "punct >", "punct (", "ident x", "punct :", "ident T", "punct )", "punct =>", "ident x"},
		},
		{
			name: "comments",
			src:  "#!/usr/bin/env node\n/** doc */ /**/ /* b */ // line\n<!-- html\n--> also\nx",
			want: []string{"comment #!/usr/bin/env node", "comment /** doc */", "comment /**/", "comment /* b */", "comment // line", "comment <!-- html", "comment --> also", "ident x"},
		},
		{
			name: "unterminated template and comment",
			src:  "`abc ${x\n/* open",
			want: []string{"tmpl-head `abc ${", "ident x", "comment /* open"},
		},
		{
			name: "keywords and contextual words",
			src:  `let async = typeof yield`,
			want: []string{"ident let", "ident async", "punct =", "keyword typeof", "ident yield"},
		},
		{
			name: "unicode identifiers and escapes",
			src:  `const café = \u0061b; ünï`,
			want: []string{"keyword const", "ident café", "punct =", `ident \u0061b`, "punct ;", "ident ünï"},
		},
		{
			name: "braced identifier escape",
			src:  `\u{61}b`,
			want: []string{`ident \u{61}b`},
		},
		{
			name: "unterminated braced identifier escape",
			src:  `\u{61`,
			want: []string{`ident \u{61`},
		},
		{
			name: "truncated fixed identifier escape",
			src:  `\u0`,
			want: []string{`ident \u0`},
		},
		{
			name: "non unicode identifier escape",
			src:  `\x`,
			want: []string{`ident \x`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lex([]byte(tt.src), tt.jsx, tt.ts)
			var got []string
			for _, tok := range l.toks {
				got = append(got, describe(tok, tt.src))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("lex(%q) =\n  %s\nwant\n  %s", tt.src, strings.Join(got, "\n  "), strings.Join(tt.want, "\n  "))
			}
		})
	}
}

func TestParseTokensAndComments(t *testing.T) {
	src := "/** Doc. */\n// line\nlet s = `a${b}c` + obj.default; /* block */\ntype T = string;\nconst f = async () => await g();\n"
	f := code.NewFile("a.ts", "/a.ts", code.TypeScript, []byte(src))
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tok := range f.Tokens {
		got = append(got, tok.Kind.String()+" "+f.TokenText(tok))
	}
	want := []string{
		"comment /** Doc. */", "comment // line",
		"keyword let", "ident s", "punct =", "string `a${b}c`", "punct +", "ident obj", "punct .", "ident default", "punct ;", "comment /* block */",
		"keyword type", "ident T", "punct =", "keyword string", "punct ;",
		"keyword const", "ident f", "punct =", "keyword async", "punct (", "punct )", "punct =>", "keyword await", "ident g", "punct (", "punct )", "punct ;",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Parse tokens =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	kinds := []code.CommentKind{code.DocComment, code.LineComment, code.BlockComment}
	if len(f.Comments) != len(kinds) {
		t.Fatalf("Parse found %d comments; want %d", len(f.Comments), len(kinds))
	}
	for i, c := range f.Comments {
		if c.Kind != kinds[i] {
			t.Errorf("comment %d %q kind = %d; want %d", i, f.Text(c.Span), c.Kind, kinds[i])
		}
	}
}

func TestJSXCommentKinds(t *testing.T) {
	src := "/// code doc\nconst view = <Widget /// attribute comment\n/** attribute doc */ />"
	l := lex([]byte(src), true, false)
	want := []code.CommentKind{code.DocComment, code.LineComment, code.DocComment}
	var got []code.CommentKind
	for _, c := range l.comments {
		got = append(got, c.Kind)
	}
	if !slices.Equal(got, want) {
		t.Errorf("lex(%q) comment kinds = %v; want %v", src, got, want)
	}
}
