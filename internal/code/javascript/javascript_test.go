package javascript

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func TestGolden(t *testing.T) {
	for _, name := range []string{"project", "jsx", "commonjs", "literals", "malformed"} {
		t.Run(name, func(t *testing.T) {
			ix, err := code.Load(code.LoadOptions{
				Root:      filepath.Join("testdata", name),
				Frontends: []code.Frontend{New()},
				Resolve:   true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(ix.Errors) > 0 {
				t.Fatalf("load errors: %v", ix.Errors)
			}
			known := map[*code.Decl]bool{}
			for d := range ix.Decls() {
				known[d] = true
			}
			for _, f := range ix.Files {
				for _, r := range f.Refs {
					if r.Target != nil && !known[r.Target] {
						t.Errorf("%s: ref %s at %d targets a declaration outside the index", f.Path, r.Name, r.Span.Start)
					}
				}
			}
			got := code.Dump(ix)
			golden := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("Dump(%s) differs from %s; run go test -update to regenerate\n%s", name, golden, got)
			}
		})
	}
}

func TestLang(t *testing.T) {
	tests := []struct {
		path string
		want code.Lang
		ok   bool
	}{
		{"a.js", code.JavaScript, true},
		{"a.mjs", code.JavaScript, true},
		{"a.cjs", code.JavaScript, true},
		{"a.jsx", code.JavaScript, true},
		{"a.ts", code.TypeScript, true},
		{"a.d.ts", code.TypeScript, true},
		{"a.mts", code.TypeScript, true},
		{"a.cts", code.TypeScript, true},
		{"a.tsx", code.TypeScript, true},
		{"a.vue", code.TypeScript, true},
		{"a.svelte", code.TypeScript, true},
		{"a.json", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := New().Lang(tt.path)
			if got != tt.want || ok != tt.ok {
				t.Errorf("Lang(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestModuleName(t *testing.T) {
	tests := []struct{ path, want string }{
		{"src/index.ts", "src/index"},
		{"src/types.d.ts", "src/types"},
		{"lib/util.min.js", "lib/util.min"},
		{"Makefile", "Makefile"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := moduleName(tt.path); got != tt.want {
				t.Errorf("moduleName(%q) = %q; want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestStripJSONC(t *testing.T) {
	in := `{
  // line
  "a": "x // not a comment", /* block */
  "b": [1, 2,],
}`
	want := "{\n  \n  \"a\": \"x // not a comment\", \n  \"b\": [1, 2]\n}"
	if got := string(stripJSONC([]byte(in))); got != want {
		t.Errorf("stripJSONC() = %q; want %q", got, want)
	}
}

func TestMatchPaths(t *testing.T) {
	paths := map[string][]string{
		"@/*":       {"src/*"},
		"@lib/*":    {"src/lib/*", "vendor/*"},
		"@exact":    {"src/exact.ts"},
		"*.styles":  {"styles/*"},
		"unrelated": {"x"},
	}
	tests := []struct {
		spec string
		want []string
	}{
		{"@lib/math", []string{"src/lib/math", "vendor/math"}},
		{"@/app", []string{"src/app"}},
		{"@exact", []string{"src/exact.ts"}},
		{"button.styles", []string{"styles/button"}},
		{"react", nil},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			if got := matchPaths(paths, tt.spec); !slices.Equal(got, tt.want) {
				t.Errorf("matchPaths(%q) = %q; want %q", tt.spec, got, tt.want)
			}
		})
	}
}

// TestParseLargeFile is here to catch quadratic behavior and crashes on a
// big synthetic file.
func TestParseLargeFile(t *testing.T) {
	var src []byte
	for range 2000 {
		src = append(src, "export class C { m(a: number) { return this.n(a) / 2 + `${a}`; } n(x) { return [x].map(y => y * 2) } }\nconst f = (x: number): number => x;\n"...)
	}
	f := code.NewFile("big.ts", "/big.ts", code.TypeScript, src)
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	if len(f.Decls) < 2000*5 {
		t.Errorf("Parse(big.ts) found %d declarations; want at least %d", len(f.Decls), 2000*5)
	}
}

// checkReparse parses src, re-parses it in resolve mode and reports the
// references the second pass didn't find again, because resolution would
// silently lose them.
func checkReparse(t *testing.T, path string, src []byte) {
	t.Helper()
	lang, _ := New().Lang(path)
	f := code.NewFile(path, "/"+path, lang, src)
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	p := newParser(f, false)
	p.run()
	for _, r := range f.Refs {
		if p.pr.refs[r] == nil {
			t.Fatalf("%s (%d bytes): ref %s at %d not revisited", path, len(src), r.Name, r.Span.Start)
		}
	}
	if len(p.pr.refs) != len(f.Refs) {
		t.Fatalf("%s (%d bytes): resolve pass saw %d refs; parse created %d", path, len(src), len(p.pr.refs), len(f.Refs))
	}
}

func fixtureFiles(t testing.TB) map[string][]byte {
	files := map[string][]byte{}
	err := filepath.WalkDir("testdata", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := New().Lang(p); ok {
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(p)] = src
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestTruncatedFixtures parses all prefixes of the fixtures. Malformed
// input must not crash or get the two parser passes out of sync.
func TestTruncatedFixtures(t *testing.T) {
	for path, src := range fixtureFiles(t) {
		t.Run(path, func(t *testing.T) {
			for n := range len(src) + 1 {
				checkReparse(t, path, src[:n])
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	for path, src := range fixtureFiles(f) {
		f.Add(filepath.Ext(path), src)
	}
	f.Fuzz(func(t *testing.T, ext string, src []byte) {
		if _, ok := New().Lang("x" + ext); !ok {
			ext = ".tsx"
		}
		checkReparse(t, "x"+ext, src)
	})
}

func TestDelimitedRecovery(t *testing.T) {
	tests := []struct {
		name, src string
		refs      []string
	}{
		{"arguments", `f(a @ skipped); const tail = c;`, []string{"f", "a", "c"}},
		{"array", `[a @ skipped]; const tail = c;`, []string{"a", "c"}},
		{"object", `({a: value @ skipped}); const tail = c;`, []string{"value", "c"}},
		{"parameters", `function f(a @ skipped) { return c; } const tail = d;`, []string{"c", "d"}},
		{"computed binding", `const {[a, skipped]: x} = obj; const tail = c;`, []string{"a", "obj", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := code.NewFile("recovery.ts", "/recovery.ts", code.TypeScript, []byte(tt.src))
			if err := New().Parse(f); err != nil {
				t.Fatal(err)
			}
			var refs []string
			for _, r := range f.Refs {
				refs = append(refs, r.Name)
			}
			if !slices.Equal(refs, tt.refs) {
				t.Errorf("Parse(%q) references = %v; want %v", tt.src, refs, tt.refs)
			}
			found := false
			for _, d := range f.Decls {
				found = found || d.Name == "tail"
			}
			if !found {
				t.Errorf("Parse(%q) lost the declaration after recovery", tt.src)
			}
			checkReparse(t, "recovery.ts", []byte(tt.src))
		})
	}
}

func BenchmarkParse(b *testing.B) {
	var src []byte
	for range 1000 {
		src = append(src, `import { a, b as c } from './mod';
export class Service extends Base<Item> implements Api {
  private items: Map<string, Item> = new Map();
  constructor(private readonly repo: Repo) { super(); }
  async find(id: string): Promise<Item | undefined> {
    const cached = this.items.get(id);
    if (cached) return cached;
    return (await this.repo.load(id)) ?? c(a, /x+/g.test(id) ? id.length / 2 : 0);
  }
}
`...)
	}
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		f := code.NewFile("bench.ts", "/bench.ts", code.TypeScript, src)
		if err := New().Parse(f); err != nil {
			b.Fatal(err)
		}
	}
}

// TestComponent checks that a Vue component gives us the tokens of its
// script at their offsets in the file, plus the comments of its markup.
// Styles and markup text should give us nothing.
func TestComponent(t *testing.T) {
	src := "<template>\n  <!-- the list header -->\n  <h1 :class=\"a < b\">{{ title }}</h1>\n</template>\n\n" +
		"<script setup lang=\"ts\">\n// keeps the title\nconst title = ref<string>('x')\n</script>\n\n" +
		"<style scoped>\n/* not code */ h1 { color: red }\n</style>\n"
	f := code.NewFile("c.vue", "/c.vue", code.TypeScript, []byte(src))
	if err := New().Parse(f); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, tok := range f.Tokens {
		if tok.Kind != code.TokComment {
			texts = append(texts, f.TokenText(tok))
		}
	}
	want := []string{"const", "title", "=", "ref", "<", "string", ">", "(", "'x'", ")"}
	if !slices.Equal(texts, want) {
		t.Errorf("tokens = %q, want %q", texts, want)
	}
	var comments []string
	for _, c := range f.Comments {
		comments = append(comments, f.Text(c.Span))
	}
	if want := []string{"<!-- the list header -->", "// keeps the title"}; !slices.Equal(comments, want) {
		t.Errorf("comments = %q, want %q", comments, want)
	}
	if len(f.Decls) == 0 || f.Decls[len(f.Decls)-1].Name != "title" {
		t.Error("the script declaration title was not parsed")
	}
}
