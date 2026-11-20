package javascript

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/myoo-ai/cli/internal/code"
)

type frontend struct{}

// New returns the JavaScript/TypeScript frontend.
func New() code.Frontend { return frontend{} }

func (frontend) Lang(path string) (code.Lang, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs", ".jsx":
		return code.JavaScript, true
	case ".ts", ".mts", ".cts", ".tsx", ".vue", ".svelte":
		return code.TypeScript, true
	}
	return "", false
}

func (frontend) Parse(f *code.File) error {
	newParser(f, true).run()
	return nil
}

func (frontend) Resolve(ix *code.Index, files []*code.File) {
	infos := make([]*fileInfo, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, f := range files {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			infos[i] = reparse(f)
		})
	}
	wg.Wait()
	newResolver(ix.Root, infos).run()
}

// reparse rebuilds the scopes of a parsed file. If the parser panics, we
// leave the file unresolved.
func reparse(f *code.File) (fi *fileInfo) {
	defer func() {
		if recover() != nil {
			fi = nil
		}
	}()
	if f.Module == nil {
		return nil
	}
	p := newParser(f, false)
	p.run()
	return &fileInfo{f: f, pr: p.pr}
}
