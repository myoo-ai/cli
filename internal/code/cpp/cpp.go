// Package cpp is the C and C++ frontend. The hand-written lexer keeps
// preprocessor directives together, and the tolerant parser is built around
// declarations. The resolver handles block scopes, include files, linkage
// across translation units, class members and namespaces.
package cpp

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/myoo-ai/cli/internal/code"
)

var cExts = map[string]bool{".c": true}

var cppExts = map[string]bool{
	".cc": true, ".cpp": true, ".cxx": true, ".c++": true, ".hpp": true, ".hh": true,
	".hxx": true, ".ipp": true, ".inl": true, ".tpp": true,
}

type frontend struct {
	mu sync.Mutex
	// The language of the sources in each directory, which decides the
	// language of the .h headers there.
	dirs map[string]code.Lang
	code.FileParser[fileInfo]
}

// New returns the C/C++ frontend.
func New() code.Frontend {
	return &frontend{dirs: map[string]code.Lang{}, FileParser: code.NewFileParser(parseFile)}
}

func (fe *frontend) Lang(p string) (code.Lang, bool) {
	ext := strings.ToLower(filepath.Ext(p))
	switch {
	case cppExts[ext] || ext == ".c" && filepath.Ext(p) == ".C":
		return code.CPP, true
	case cExts[ext]:
		return code.C, true
	case ext == ".h":
		switch fe.dirLang(filepath.Dir(p)) {
		case code.CPP:
			return code.CPP, true
		case code.C:
			return code.C, true
		}
		if cppHeader(p) {
			return code.CPP, true
		}
		return code.C, true
	}
	return "", false
}

// dirLang reports code.CPP if there's a C++ file in dir, else code.C if
// there's a C source, else "".
func (fe *frontend) dirLang(dir string) code.Lang {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	if v, ok := fe.dirs[dir]; ok {
		return v
	}
	var v code.Lang
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() {
			continue
		}
		if cppExts[ext] {
			v = code.CPP
			break
		}
		if cExts[ext] {
			v = code.C
		}
	}
	fe.dirs[dir] = v
	return v
}

// cppMarkers are byte sequences that in practice only show up in C++ headers.
var cppMarkers = []string{"namespace ", "template<", "template <", "class ", "public:", "private:", "std::", "nullptr"}

// cppHeader sniffs the start of a header for C++ syntax. We only need this
// for headers with no sources next to them (include/ directories).
func cppHeader(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 16<<10)
	n, _ := io.ReadFull(f, buf)
	head := string(buf[:n])
	for _, m := range cppMarkers {
		if strings.Contains(head, m) {
			return true
		}
	}
	return false
}

// Resolve uses the scopes recorded by Parse and drops them afterwards.
func (fe *frontend) Resolve(_ *code.Index, files []*code.File) {
	infos := make([]*fileInfo, 0, len(files))
	fe.Drain(files, func(_ *code.File, fi *fileInfo) { infos = append(infos, fi) })
	resolve(infos)
}

// moduleName is the path relative to the root, minus the extension.
func moduleName(rel string) string {
	p := filepath.ToSlash(rel)
	if filepath.IsAbs(rel) {
		p = path.Base(p)
	}
	return strings.TrimSuffix(p, path.Ext(p))
}
