package javascript

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// probeExts are the extensions we try, in this order, if a module specifier
// has no known file extension, and after `<dir>/index`.
var probeExts = []string{".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"}

// sourceExts maps the emitted extension written in a specifier to the
// TypeScript sources that compile to it.
var sourceExts = map[string][]string{
	".js":  {".ts", ".tsx", ".d.ts"},
	".jsx": {".tsx"},
	".mjs": {".mts", ".d.mts"},
	".cjs": {".cts", ".d.cts"},
}

type modKey struct{ dir, spec string }

// modules resolves module specifiers to workspace files.
type modules struct {
	root    string
	byPath  map[string]*fileInfo
	cache   map[modKey]*fileInfo
	configs map[string]*tsconfig // nearest tsconfig.json by directory
}

func newModules(root string) modules {
	return modules{
		root:    root,
		byPath:  map[string]*fileInfo{},
		cache:   map[modKey]*fileInfo{},
		configs: map[string]*tsconfig{},
	}
}

// module resolves spec as imported from fi. A bare package name stays nil
// unless tsconfig paths or baseUrl map it into the workspace.
func (m *modules) module(fi *fileInfo, spec string) *fileInfo {
	if fi == nil || spec == "" {
		return nil
	}
	dir := path.Dir(fi.f.Path)
	key := modKey{dir, spec}
	if v, ok := m.cache[key]; ok {
		return v
	}
	var res *fileInfo
	switch {
	case spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
		res = m.probe(path.Join(dir, spec))
	case !strings.HasPrefix(spec, "/") && !filepath.IsAbs(fi.f.Path):
		res = m.viaConfig(dir, spec)
	}
	m.cache[key] = res
	return res
}

func (m *modules) probe(p string) *fileInfo {
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return nil
	}
	if fi := m.byPath[p]; fi != nil {
		return fi
	}
	ext := path.Ext(p)
	for _, e := range sourceExts[ext] {
		if fi := m.byPath[strings.TrimSuffix(p, ext)+e]; fi != nil {
			return fi
		}
	}
	for _, e := range probeExts {
		if fi := m.byPath[p+e]; fi != nil {
			return fi
		}
	}
	for _, e := range probeExts {
		if fi := m.byPath[path.Join(p, "index"+e)]; fi != nil {
			return fi
		}
	}
	return nil
}

// tsconfig holds the module resolution options of a tsconfig.json or
// jsconfig.json, after applying `extends`. Directories are relative to the
// workspace root.
type tsconfig struct {
	baseURL  string
	hasBase  bool
	paths    map[string][]string
	pathsDir string
}

func (m *modules) viaConfig(dir, spec string) *fileInfo {
	cfg := m.config(dir)
	if cfg == nil {
		return nil
	}
	if len(cfg.paths) > 0 {
		base := cfg.pathsDir
		if cfg.hasBase {
			base = cfg.baseURL
		}
		for _, target := range matchPaths(cfg.paths, spec) {
			if fi := m.probe(path.Join(base, target)); fi != nil {
				return fi
			}
		}
	}
	if cfg.hasBase {
		return m.probe(path.Join(cfg.baseURL, spec))
	}
	return nil
}

// matchPaths returns the substituted targets of the paths pattern that
// matches spec. An exact pattern wins. If there is none, we take the
// wildcard pattern with the longest prefix.
func matchPaths(paths map[string][]string, spec string) []string {
	if targets, ok := paths[spec]; ok {
		return targets
	}
	best, bestLen, captured := "", -1, ""
	for pattern := range paths {
		prefix, suffix, ok := strings.Cut(pattern, "*")
		if !ok || !strings.HasPrefix(spec, prefix) || !strings.HasSuffix(spec, suffix) || len(spec) < len(prefix)+len(suffix) {
			continue
		}
		if len(prefix) > bestLen || len(prefix) == bestLen && pattern < best {
			best, bestLen, captured = pattern, len(prefix), spec[len(prefix):len(spec)-len(suffix)]
		}
	}
	if bestLen < 0 {
		return nil
	}
	targets := make([]string, 0, len(paths[best]))
	for _, t := range paths[best] {
		targets = append(targets, strings.Replace(t, "*", captured, 1))
	}
	return targets
}

// config returns the nearest tsconfig.json (or jsconfig.json) at or above
// dir, as long as it's inside the workspace.
func (m *modules) config(dir string) *tsconfig {
	if cfg, ok := m.configs[dir]; ok {
		return cfg
	}
	var cfg *tsconfig
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		if c, ok := loadConfig(m.root, path.Join(dir, name), 0); ok {
			cfg = c
			break
		}
	}
	if cfg == nil && dir != "." && dir != "/" && dir != "" {
		cfg = m.config(path.Dir(dir))
	}
	m.configs[dir] = cfg
	return cfg
}

// loadConfig reads the config file at rel (relative to root) and any
// relative configs it extends.
func loadConfig(root, rel string, depth int) (*tsconfig, bool) {
	if depth > 8 {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, false
	}
	var raw struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions struct {
			BaseURL *string             `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &raw); err != nil {
		return &tsconfig{}, true
	}
	dir := path.Dir(rel)
	cfg := &tsconfig{}
	var parents []string
	if len(raw.Extends) > 0 {
		var one string
		if json.Unmarshal(raw.Extends, &one) == nil {
			parents = []string{one}
		} else {
			_ = json.Unmarshal(raw.Extends, &parents)
		}
	}
	for _, ext := range parents {
		if !strings.HasPrefix(ext, ".") {
			continue
		}
		p := path.Join(dir, ext)
		if path.Ext(p) != ".json" {
			p += ".json"
		}
		if parent, ok := loadConfig(root, p, depth+1); ok {
			if parent.hasBase {
				cfg.baseURL, cfg.hasBase = parent.baseURL, true
			}
			if parent.paths != nil {
				cfg.paths, cfg.pathsDir = parent.paths, parent.pathsDir
			}
		}
	}
	if b := raw.CompilerOptions.BaseURL; b != nil {
		cfg.baseURL, cfg.hasBase = path.Join(dir, *b), true
	}
	if raw.CompilerOptions.Paths != nil {
		cfg.paths, cfg.pathsDir = raw.CompilerOptions.Paths, dir
	}
	return cfg, true
}

// stripJSONC removes comments and trailing commas so encoding/json will
// accept tsconfig files.
func stripJSONC(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(b) && b[j] != '"' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(b))
			out = append(out, b[i:j]...)
			i = j - 1
		case c == '/' && i+1 < len(b) && (b[i+1] == '/' || b[i+1] == '*'):
			i = skipJSONComment(b, i) - 1
		case c == ',':
			j := skipJSONSpace(b, i+1)
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

func skipJSONComment(b []byte, i int) int {
	if b[i+1] == '/' {
		for i < len(b) && b[i] != '\n' {
			i++
		}
		return i
	}
	for i += 2; i+1 < len(b); i++ {
		if b[i] == '*' && b[i+1] == '/' {
			return i + 2
		}
	}
	return len(b)
}

// skipJSONSpace returns the offset of the first byte at or after i that
// isn't white space or part of a comment.
func skipJSONSpace(b []byte, i int) int {
	for i < len(b) {
		switch {
		case b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r':
			i++
		case b[i] == '/' && i+1 < len(b) && (b[i+1] == '/' || b[i+1] == '*'):
			i = skipJSONComment(b, i)
		default:
			return i
		}
	}
	return i
}
