package clike

import (
	"slices"
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

// render lists the tokens of f as "kind:text", with the interpolation
// holes of parser tokens appended as "{hole}".
func render(f *code.File, toks []tok) []string {
	holes := map[int][]code.Span{}
	for _, t := range toks {
		holes[t.s] = t.holes
	}
	var out []string
	for _, t := range f.Tokens {
		s := t.Kind.String() + ":" + f.TokenText(t)
		for _, h := range holes[t.Span.Start] {
			s += " {" + f.Text(h) + "}"
		}
		out = append(out, s)
	}
	return out
}

func TestLex(t *testing.T) {
	tests := []struct {
		name string
		lang code.Lang
		src  string
		want []string
	}{
		{
			name: "java text block with quotes and escapes",
			lang: code.Java,
			src:  "s = \"\"\"\n  a \"quoted\" \\\"\"\" b\n  \"\"\";",
			want: []string{"ident:s", "punct:=", "string:\"\"\"\n  a \"quoted\" \\\"\"\" b\n  \"\"\"", "punct:;"},
		},
		{
			name: "java char literals",
			lang: code.Java,
			src:  `'a' '\'' '\u0041' '"'`,
			want: []string{"string:'a'", `string:'\''`, `string:'\u0041'`, `string:'"'`},
		},
		{
			name: "java unicode escape in identifier",
			lang: code.Java,
			src:  `int caf\u00e9 = 1;`,
			want: []string{"keyword:int", `ident:caf\u00e9`, "punct:=", "number:1", "punct:;"},
		},
		{
			name: "java annotations and nested generics",
			lang: code.Java,
			src:  `@Override List<List<String>> x;`,
			want: []string{"punct:@", "ident:Override", "ident:List", "punct:<", "ident:List", "punct:<", "ident:String", "punct:>", "punct:>", "ident:x", "punct:;"},
		},
		{
			name: "numbers",
			lang: code.Java,
			src:  `0x1F_FFL 1.5e-3f .5 1_000 2.toString`,
			want: []string{"number:0x1F_FFL", "number:1.5e-3f", "number:.5", "number:1_000", "number:2", "punct:.", "ident:toString"},
		},
		{
			name: "comment kinds",
			lang: code.Java,
			src:  "/** doc */ /**/ /* block */ /// doc\n//// line\n// line",
			want: []string{"comment:/** doc */", "comment:/**/", "comment:/* block */", "comment:/// doc", "comment://// line", "comment:// line"},
		},
		{
			name: "unterminated java string stops at the line end",
			lang: code.Java,
			src:  "a = \"open;\nb = 1;",
			want: []string{"ident:a", "punct:=", "string:\"open;", "ident:b", "punct:=", "number:1", "punct:;"},
		},
		{
			name: "java lambda and method reference",
			lang: code.Java,
			src:  `x -> Foo::bar`,
			want: []string{"ident:x", "punct:->", "ident:Foo", "punct:::", "ident:bar"},
		},
		{
			name: "csharp verbatim string",
			lang: code.CSharp,
			src:  `@"a ""b"" c\d" x`,
			want: []string{`string:@"a ""b"" c\d"`, "ident:x"},
		},
		{
			name: "csharp interpolated string holes",
			lang: code.CSharp,
			src:  `$"a {b} {{c}} {d,5:F2} {e:yyyy-MM-dd}"`,
			want: []string{`string:$"a {b} {{c}} {d,5:F2} {e:yyyy-MM-dd}" {b} {d} {e}`},
		},
		{
			name: "csharp nested interpolation",
			lang: code.CSharp,
			src:  `$"{(x ? $"{y}" : "}")}" z`,
			want: []string{`string:$"{(x ? $"{y}" : "}")}" {(x ? $"{y}" : "}")}`, "ident:z"},
		},
		{
			name: "csharp raw strings",
			lang: code.CSharp,
			src:  `"""a "" b""" $$"""{{x}} {y} {{{z}}}"""`,
			want: []string{`string:"""a "" b"""`, `string:$$"""{{x}} {y} {{{z}}}""" {x} {z}`},
		},
		{
			name: "csharp verbatim interpolated in both orders",
			lang: code.CSharp,
			src:  `$@"{p}\n" @$"""{q}"""`,
			want: []string{`string:$@"{p}\n" {p}`, `string:@$"""{q}""" {q}`},
		},
		{
			name: "csharp verbatim identifier",
			lang: code.CSharp,
			src:  `@class.@event`,
			want: []string{"ident:@class", "punct:.", "ident:@event"},
		},
		{
			name: "csharp preprocessor lines",
			lang: code.CSharp,
			src:  "#if DEBUG // why\nx();\n  #region Don't parse\n#endregion",
			want: []string{"punct:#", "keyword:if", "ident:DEBUG", "comment:// why", "ident:x", "punct:(", "punct:)", "punct:;", "punct:#", "keyword:region", "ident:Don", "string:'t parse", "punct:#", "keyword:endregion"},
		},
		{
			name: "csharp null conditional and ternary with fraction",
			lang: code.CSharp,
			src:  `a?.b ? .5 : c ?? d`,
			want: []string{"ident:a", "punct:?.", "ident:b", "punct:?", "number:.5", "punct::", "ident:c", "punct:??", "ident:d"},
		},
		{
			name: "csharp unterminated interpolation",
			lang: code.CSharp,
			src:  `$"x {y`,
			want: []string{`string:$"x {y {y}`},
		},
		{
			name: "non-ascii punctuation",
			lang: code.CSharp,
			src:  "a \u00b7 b",
			want: []string{"ident:a", "punct:\u00b7", "ident:b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := code.NewFile("x", "x", tc.lang, []byte(tc.src))
			toks := lex(f, tc.src, tc.lang == code.CSharp)
			if got := render(f, toks); !slices.Equal(got, tc.want) {
				t.Errorf("tokens\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestLexPreprocessorStaysOutOfParse(t *testing.T) {
	src := "#if DEBUG\nint x;\n#endif\n"
	f := code.NewFile("x.cs", "x.cs", code.CSharp, []byte(src))
	var texts []string
	for _, tk := range lex(f, src, true) {
		texts = append(texts, tk.text)
	}
	if want := []string{"int", "x", ";"}; !slices.Equal(texts, want) {
		t.Errorf("parser tokens = %q, want %q", texts, want)
	}
}

func TestCommentsAndContextualKeywords(t *testing.T) {
	src := `/// <summary>Doc.</summary>
class C
{
    /** Also doc. */
    public int P { get; set; }
    // plain
    void M() { var q = from x in xs where x > 1 select x; }
}
`
	f := code.NewFile("c.cs", "c.cs", code.CSharp, []byte(src))
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	var kinds []code.CommentKind
	for _, c := range f.Comments {
		kinds = append(kinds, c.Kind)
	}
	if want := []code.CommentKind{code.DocComment, code.DocComment, code.LineComment}; !slices.Equal(kinds, want) {
		t.Errorf("comment kinds = %v, want %v", kinds, want)
	}
	var keywords []string
	for _, tk := range f.Tokens {
		if tk.Kind == code.TokKeyword {
			keywords = append(keywords, f.TokenText(tk))
		}
	}
	want := "class public int get set void var from in where select"
	if got := strings.Join(keywords, " "); got != want {
		t.Errorf("keywords = %q, want %q", got, want)
	}
}
