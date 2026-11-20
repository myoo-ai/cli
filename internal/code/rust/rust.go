// Package rust is the Rust frontend. A hand-written lexer and a tolerant
// parser pull out items, bindings and references. The resolver then
// rebuilds the crate module trees from Cargo.toml and the file layout, so it
// can follow paths, imports, impl blocks and method calls.
package rust

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/myoo-ai/cli/internal/code"
)

type frontend struct {
	code.FileParser[fileInfo]
}

// New returns the Rust frontend.
func New() code.Frontend {
	return &frontend{FileParser: code.NewFileParser(parseFile)}
}

func (*frontend) Lang(p string) (code.Lang, bool) {
	if strings.HasSuffix(p, ".rs") {
		return code.Rust, true
	}
	return "", false
}

// Resolve uses the scopes recorded by Parse and drops them afterwards.
func (fe *frontend) Resolve(_ *code.Index, files []*code.File) {
	infos := make([]*fileInfo, 0, len(files))
	fe.Drain(files, func(_ *code.File, fi *fileInfo) { infos = append(infos, fi) })
	resolve(infos)
}

// manifest holds the fields of a Cargo.toml that the frontend uses.
type manifest struct {
	dir     string // absolute directory of the Cargo.toml
	name    string // package name with dashes turned into underscores
	edition string
}

var (
	manifestMu    sync.Mutex
	manifestCache = map[string]*manifest{}
	tomlKeyRe     = regexp.MustCompile(`^\s*(name|edition)\s*=\s*"([^"]*)"`)
	tomlTableRe   = regexp.MustCompile(`^\s*\[\s*([A-Za-z0-9_.-]+)\s*\]`)
)

// findManifest returns the nearest Cargo.toml at or above dir that declares
// a package, or nil. If stop is an ancestor of dir, we don't look above it.
func findManifest(dir, stop string) *manifest {
	for d := dir; ; {
		if m := readManifest(d); m != nil {
			return m
		}
		parent := filepath.Dir(d)
		if d == stop || parent == d {
			return nil
		}
		d = parent
	}
}

func readManifest(dir string) *manifest {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if m, ok := manifestCache[dir]; ok {
		return m
	}
	var m *manifest
	if fh, err := os.Open(filepath.Join(dir, "Cargo.toml")); err == nil {
		m = &manifest{dir: dir, name: filepath.Base(dir)}
		table := ""
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := sc.Text()
			if t := tomlTableRe.FindStringSubmatch(line); t != nil {
				table = t[1]
				continue
			}
			if table != "package" {
				continue
			}
			if kv := tomlKeyRe.FindStringSubmatch(line); kv != nil {
				switch kv[1] {
				case "name":
					m.name = kv[2]
				case "edition":
					m.edition = kv[2]
				}
			}
		}
		fh.Close()
		m.name = strings.ReplaceAll(m.name, "-", "_")
	}
	manifestCache[dir] = m
	return m
}

// Edition returns the edition set in the Cargo.toml of the package that
// holds the file at abs. It returns "" if there's no Cargo.toml or it
// doesn't set an edition, which means 2015.
func Edition(abs string) string {
	if m := findManifest(filepath.Dir(abs), ""); m != nil {
		return m.edition
	}
	return ""
}

// layout is where a file sits in its crate, judged by its path.
type layout struct {
	module string // dotted module path, the crate name first
	root   bool   // crate root like lib.rs, main.rs, src/bin/x.rs, tests/x.rs or build.rs
	modRS  bool   // child modules live in the file's directory, as for mod.rs and crate roots
}

// fileLayout works out the module path of a file from Cargo conventions.
// src/lib.rs and src/main.rs are the package crate. src/bin, tests, examples
// and benches have a crate per file or directory. src/a/b.rs or
// src/a/b/mod.rs is module a::b. If there's no Cargo.toml, a src directory
// marks the crate, and if there's no src either, the workspace root does.
func fileLayout(rel, abs string) layout {
	rel = filepath.ToSlash(rel)
	if filepath.IsAbs(rel) {
		rel = path.Base(rel)
	}
	absDir := filepath.Dir(abs)
	rootDir := strings.TrimSuffix(filepath.ToSlash(abs), "/"+rel)
	var crateRel, name string
	if m := findManifest(absDir, filepath.FromSlash(rootDir)); m != nil {
		r, err := filepath.Rel(m.dir, abs)
		if err == nil && !strings.HasPrefix(r, "..") {
			crateRel = filepath.ToSlash(r)
			name = m.name
		}
	}
	if name == "" {
		crateRel = rel
		name = identName(path.Base(rootDir))
		if i := strings.Index("/"+rel, "/src/"); i >= 0 {
			if i > 0 {
				name = identName(path.Base(rel[:i-1]))
			}
			crateRel = rel[i:]
		}
	}
	p := strings.TrimSuffix(crateRel, ".rs")
	segs := strings.Split(p, "/")
	switch {
	case p == "build":
		return layout{module: "build", root: true, modRS: true}
	case len(segs) >= 3 && segs[0] == "src" && segs[1] == "bin":
		segs, name = segs[2:], ""
	case len(segs) >= 2 && (segs[0] == "tests" || segs[0] == "examples" || segs[0] == "benches"):
		segs, name = segs[1:], ""
	case len(segs) >= 2 && segs[0] == "src":
		segs = segs[1:]
	}
	var ly layout
	last := segs[len(segs)-1]
	switch {
	case name == "" && (len(segs) == 1 || len(segs) == 2 && last == "main"):
		// One crate per file or per directory with a main.rs.
		segs = segs[:1]
		ly.root, ly.modRS = true, true
	case name != "" && len(segs) == 1 && (last == "lib" || last == "main"):
		segs = nil
		ly.root, ly.modRS = true, true
	case last == "mod":
		segs = segs[:len(segs)-1]
		ly.modRS = true
	}
	if name != "" {
		segs = append([]string{name}, segs...)
	}
	for i, s := range segs {
		segs[i] = identName(s)
	}
	ly.module = strings.Join(segs, ".")
	return ly
}

// identName turns a file or directory name into a Rust identifier.
func identName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '.' || r == ' ' {
			return '_'
		}
		return r
	}, s)
}
