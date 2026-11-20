package cpp

import (
	"strings"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

func lexed(src string, cpp bool) []string {
	l := lex([]byte(src), cpp)
	out := make([]string, len(l.toks))
	for i, t := range l.toks {
		out[i] = t.kind.String() + ":" + src[t.s:t.e]
	}
	return out
}

func TestLex(t *testing.T) {
	tests := []struct {
		name string
		src  string
		cpp  bool
		want []string
	}{
		{
			name: "raw string",
			src:  `auto s = R"json({"a": ")"})json";`,
			cpp:  true,
			want: []string{"keyword:auto", "ident:s", "punct:=", `string:R"json({"a": ")"})json"`, "punct:;"},
		},
		{
			name: "prefixed raw string with udl",
			src:  `u8R"(x)"_sv`,
			cpp:  true,
			want: []string{`string:u8R"(x)"_sv`},
		},
		{
			name: "unterminated raw string",
			src:  "R\"x(open\nstill",
			cpp:  true,
			want: []string{"string:R\"x(open\nstill"},
		},
		{
			name: "string prefixes and chars",
			src:  `L"w" u"16" U'c' u8"s" '\'' "a\"b" 'x'`,
			want: []string{`string:L"w"`, `string:u"16"`, `string:U'c'`, `string:u8"s"`, `string:'\''`, `string:"a\"b"`, `string:'x'`},
		},
		{
			name: "digit separators and literals",
			src:  "1'000'000 0x1F'FFu 1.5e-3f 0b1010 12_km .5",
			cpp:  true,
			want: []string{"number:1'000'000", "number:0x1F'FFu", "number:1.5e-3f", "number:0b1010", "number:12_km", "number:.5"},
		},
		{
			name: "template closing brackets",
			src:  "std::vector<std::vector<int>> v; a >> b;",
			cpp:  true,
			want: []string{"ident:std", "punct:::", "ident:vector", "punct:<", "ident:std", "punct:::", "ident:vector",
				"punct:<", "keyword:int", "punct:>>", "ident:v", "punct:;", "ident:a", "punct:>>", "ident:b", "punct:;"},
		},
		{
			name: "operators",
			src:  "p->x <=> y ...",
			cpp:  true,
			want: []string{"ident:p", "punct:->", "ident:x", "punct:<=>", "ident:y", "punct:..."},
		},
		{
			name: "attributes",
			src:  "[[nodiscard]] int f();",
			cpp:  true,
			want: []string{"punct:[", "punct:[", "ident:nodiscard", "punct:]", "punct:]", "keyword:int", "ident:f", "punct:(", "punct:)", "punct:;"},
		},
		{
			name: "c keywords are not c++ keywords",
			src:  "class new",
			want: []string{"ident:class", "ident:new"},
		},
		{
			name: "directive with continuation",
			src:  "#define ADD(a, b) \\\n  ((a) + (b))\nint x;",
			want: []string{"punct:#", "keyword:define", "ident:ADD", "punct:(", "ident:a", "punct:,", "ident:b", "punct:)",
				"punct:(", "punct:(", "ident:a", "punct:)", "punct:+", "punct:(", "ident:b", "punct:)", "punct:)",
				"keyword:int", "ident:x", "punct:;"},
		},
		{
			name: "system include",
			src:  "#include <sys/types.h>\n#  include \"a.h\" // c\n",
			want: []string{"punct:#", "keyword:include", "string:<sys/types.h>", "punct:#", "keyword:include", `string:"a.h"`, "comment:// c"},
		},
		{
			name: "hash inside a line is punctuation",
			src:  "x # y",
			want: []string{"ident:x", "punct:#", "ident:y"},
		},
		{
			name: "unterminated string ends at line end",
			src:  "\"abc\nint",
			want: []string{`string:"abc`, "keyword:int"},
		},
		{
			name: "unterminated block comment",
			src:  "a /* b",
			want: []string{"ident:a", "comment:/* b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lexed(tt.src, tt.cpp)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lex(%q)\n got %q\nwant %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestLexComments(t *testing.T) {
	src := "/** doc */\n/// doc\n//! doc\n// line\n/* block */\n/**/\n//// rule\n/*! qt */\n"
	l := lex([]byte(src), true)
	names := map[code.CommentKind]string{code.LineComment: "line", code.BlockComment: "block", code.DocComment: "doc"}
	var got []string
	for _, c := range l.comments {
		got = append(got, names[c.Kind]+" "+src[c.Span.Start:c.Span.End])
	}
	doc, line, block := "doc", "line", "block"
	want := []string{
		doc + " /** doc */", doc + " /// doc", doc + " //! doc", line + " // line",
		block + " /* block */", block + " /**/", line + " //// rule", doc + " /*! qt */",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("comments\n got %q\nwant %q", got, want)
	}
}

func TestLexDirectives(t *testing.T) {
	src := "#define A 1 \\\n  + 2\n#if 0\nint dead;\n#else\nint live;\n#endif\n#ifdef X\nint first;\n#elif Y\nint second;\n#endif\n"
	l := lex([]byte(src), false)
	if len(l.dirs) != 7 {
		t.Fatalf("got %d directives; want 7", len(l.dirs))
	}
	if got := src[l.dirs[0].span.Start:l.dirs[0].span.End]; got != "#define A 1 \\\n  + 2" {
		t.Errorf("define span = %q", got)
	}
	var active []string
	for _, tk := range activeTokens(l) {
		active = append(active, src[tk.s:tk.e])
	}
	if got, want := strings.Join(active, " "), "int live ; int first ;"; got != want {
		t.Errorf("active tokens = %q; want %q", got, want)
	}
}
