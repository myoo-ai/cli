package codelint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/myoo-ai/cli/internal/code"
	"github.com/myoo-ai/cli/internal/code/langs"
)

func zTokens(t *testing.T, root, rel string) (strs, other []string, ok bool) {
	ix, err := code.Load(code.LoadOptions{Root: root, Paths: []string{filepath.Join(root, rel)}, Frontends: langs.All()})
	if err != nil || len(ix.Files) != 1 {
		return nil, nil, false
	}
	f := ix.Files[0]
	for _, tk := range f.Tokens {
		switch tk.Kind {
		case code.TokComment:
		case code.TokString:
			strs = append(strs, f.TokenText(tk))
		default:
			other = append(other, f.TokenText(tk))
		}
	}
	return strs, other, true
}

func TestZZTokens(t *testing.T) {
	base, cur := os.Getenv("BASE"), os.Getenv("CUR")
	if base == "" {
		t.Skip()
	}
	filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		a, _ := os.ReadFile(p)
		b, err := os.ReadFile(filepath.Join(cur, rel))
		if err != nil {
			fmt.Printf("MISSING %s\n", rel)
			return nil
		}
		if bytes.Equal(a, b) {
			return nil
		}
		as, ao, ok1 := zTokens(t, base, rel)
		bs, bo, ok2 := zTokens(t, cur, rel)
		switch {
		case !ok1 || !ok2:
			fmt.Printf("UNPARSED %s\n", rel)
		case fmt.Sprint(ao) != fmt.Sprint(bo):
			fmt.Printf("CODE %s\n", rel)
		case fmt.Sprint(as) != fmt.Sprint(bs):
			fmt.Printf("STRINGS %s\n", rel)
		default:
			fmt.Printf("COMMENTS %s\n", rel)
		}
		return nil
	})
}
