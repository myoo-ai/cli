package code

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// MaxFileSize is the largest file Load reads. Anything bigger is almost
// always generated or minified.
const MaxFileSize = 2 << 20

var rootMarkers = []string{
	".git", "go.mod", "go.work", "package.json", "tsconfig.json", "pyproject.toml",
	"setup.py", "setup.cfg", "pom.xml", "build.gradle", "build.gradle.kts",
	"settings.gradle", "CMakeLists.txt", "compile_commands.json", "Cargo.toml", "composer.json",
}

var defaultIgnore = []string{
	"node_modules/", "bower_components/", "jspm_packages/", "vendor/", "third_party/",
	"dist/", "build/", "out/", "target/", "bin/", "obj/",
	"__pycache__/", "venv/", "site-packages/", "coverage/",
}

// DefaultIgnore returns a copy of the patterns Load skips if
// LoadOptions.Ignore is nil.
func DefaultIgnore() []string { return slices.Clone(defaultIgnore) }

// FindRoot returns the nearest ancestor of the file or directory start that
// looks like a project root, or start's directory if none does.
func FindRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	dir := abs
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		dir = filepath.Dir(abs)
	}
	for d := dir; ; {
		if hasRootMarker(d) {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func hasRootMarker(dir string) bool {
	for _, m := range rootMarkers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if ext := filepath.Ext(e.Name()); ext == ".sln" || ext == ".csproj" {
			return true
		}
	}
	return false
}

// LoadOptions configures Load.
type LoadOptions struct {
	// Root defaults to FindRoot of the first path, or of the working
	// directory if Paths is empty.
	Root string
	// Paths are files or directories to load, absolute or relative to the
	// working directory. Empty means the whole root. Explicitly named files
	// are loaded even if they're ignored.
	Paths     []string
	Frontends []Frontend
	// Resolve runs the frontends' name resolution after parsing.
	Resolve bool
	// Ignore holds gitignore-style patterns, relative to Root, for files
	// and directories the walk should skip. nil means DefaultIgnore(). They
	// get to decide before the workspace's .gitignore files, and we always
	// skip directories whose name starts with a dot.
	Ignore []string
}

// Index is a set of parsed files.
type Index struct {
	Root  string // absolute workspace root
	Files []*File
	// Errors lists files we couldn't read or parse. They're left out of
	// Files.
	Errors []error

	frontendOf map[*File]Frontend
	once       sync.Once
	byPath     map[string]*File
	byFQN      map[string][]*Decl
	refsTo     map[*Decl][]*Ref
}

type parseJob struct {
	abs string
	fe  Frontend
	lng Lang
}

// Load walks the requested paths, parses all supported files and, if
// opts.Resolve is set, resolves references.
func Load(opts LoadOptions) (*Index, error) {
	root := opts.Root
	if root == "" {
		start := "."
		if len(opts.Paths) > 0 {
			start = opts.Paths[0]
		}
		root = FindRoot(start)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	ix := &Index{Root: root, frontendOf: map[*File]Frontend{}}
	paths := opts.Paths
	if len(paths) == 0 {
		paths = []string{root}
	}
	var jobs []parseJob
	seen := map[string]bool{}
	add := func(abs string, explicit bool) {
		if seen[abs] {
			return
		}
		for _, fe := range opts.Frontends {
			if lang, ok := fe.Lang(abs); ok {
				seen[abs] = true
				jobs = append(jobs, parseJob{abs, fe, lang})
				return
			}
		}
		if explicit {
			ix.Errors = append(ix.Errors, fmt.Errorf("%s: unsupported file type", abs))
		}
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("unable to read `%s`: %w", p, err)
		}
		if !fi.IsDir() {
			add(abs, true)
			continue
		}
		ignore := opts.Ignore
		if ignore == nil {
			ignore = defaultIgnore
		}
		if err := walk(root, abs, ignore, func(file string) { add(file, false) }); err != nil {
			return nil, err
		}
	}

	ix.parse(jobs)

	if opts.Resolve {
		for _, fe := range opts.Frontends {
			var mine []*File
			for _, f := range ix.Files {
				if ix.frontendOf[f] == fe {
					mine = append(mine, f)
				}
			}
			if len(mine) > 0 {
				fe.Resolve(ix, mine)
			}
		}
		// Frontends may have used File/Lookup while resolving, so we rebuild the
		// lookup tables to make RefsTo see the final Targets.
		ix.Invalidate()
	}
	return ix, nil
}

func (ix *Index) parse(jobs []parseJob) {
	files := make([]*File, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, j := range jobs {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			files[i], errs[i] = parseOne(ix.Root, j.abs, j.lng, j.fe)
		})
	}
	wg.Wait()
	for i, f := range files {
		if errs[i] != nil {
			ix.Errors = append(ix.Errors, errs[i])
			continue
		}
		ix.Files = append(ix.Files, f)
		ix.frontendOf[f] = jobs[i].fe
	}
	slices.SortFunc(ix.Files, func(a, b *File) int { return cmp.Compare(a.Path, b.Path) })
}

func parseOne(root, abs string, lang Lang, fe Frontend) (f *File, err error) {
	src, err := readLimited(abs)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(src[:min(len(src), 8000)], 0) >= 0 {
		return nil, fmt.Errorf("%s: binary file, skipped", abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = abs
	}
	f = NewFile(filepath.ToSlash(rel), abs, lang, src)
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("%s: parser crashed: %v", rel, r)
		}
	}()
	if err := fe.Parse(f); err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if f.Module == nil {
		f.SetModule("")
	}
	f.sortDecls()
	return f, nil
}

// readLimited reads a file of up to MaxFileSize bytes. It doesn't load
// anything bigger into memory.
func readLimited(abs string) ([]byte, error) {
	fh, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	src, err := io.ReadAll(io.LimitReader(fh, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(src) > MaxFileSize {
		return nil, fmt.Errorf("%s: file larger than %d bytes, skipped", abs, MaxFileSize)
	}
	return src, nil
}

// walk calls visit for the regular files below dir that aren't hidden or
// ignored. The ignore patterns are relative to root and decide first, so a
// caller's exclusion or re-inclusion wins over the .gitignore files between
// root and the file. Those only decide the paths the patterns leave open.
func walk(root, dir string, ignore []string, visit func(abs string)) error {
	patterns := parseGitignore("", []byte(strings.Join(ignore, "\n")))
	var rules ignoreList
	ignored := func(rel string, isDir bool) bool {
		if skip, decided := patterns.match(rel, isDir); decided {
			return skip
		}
		skip, _ := rules.match(rel, isDir)
		return skip
	}
	loadIgnore := func(abs string) {
		data, err := os.ReadFile(filepath.Join(abs, ".gitignore"))
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(root, abs)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		rules = append(rules, parseGitignore(rel, data)...)
	}
	// .gitignore files of ancestors between root and dir apply as well.
	if rel, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(rel, "..") {
		d := root
		loadIgnore(d)
		if rel != "." {
			for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
				d = filepath.Join(d, part)
				if d != dir {
					loadIgnore(d)
				}
			}
		}
	}
	return filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil
		}
		name := e.Name()
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			if p != dir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if p != dir && ignored(rel, true) {
				return filepath.SkipDir
			}
			loadIgnore(p)
			return nil
		}
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			return nil
		}
		if ignored(rel, false) {
			return nil
		}
		visit(p)
		return nil
	})
}

func (ix *Index) build() {
	ix.once.Do(func() {
		ix.byPath = map[string]*File{}
		ix.byFQN = map[string][]*Decl{}
		ix.refsTo = map[*Decl][]*Ref{}
		for _, f := range ix.Files {
			ix.byPath[f.Path] = f
			ix.byPath[f.Abs] = f
			if f.Module != nil && f.Module.Name != "" {
				ix.byFQN[f.Module.Name] = append(ix.byFQN[f.Module.Name], f.Module)
			}
			for _, d := range f.Decls {
				fqn := d.FQN()
				ix.byFQN[fqn] = append(ix.byFQN[fqn], d)
			}
			for _, r := range f.Refs {
				if r.Target != nil {
					ix.refsTo[r.Target] = append(ix.refsTo[r.Target], r)
				}
			}
		}
	})
}

// File returns the file with the given path, relative to Root or absolute,
// or nil if there's none.
func (ix *Index) File(path string) *File {
	ix.build()
	if f, ok := ix.byPath[filepath.ToSlash(path)]; ok {
		return f
	}
	if abs, err := filepath.Abs(path); err == nil {
		return ix.byPath[abs]
	}
	return nil
}

// Lookup returns the declarations whose FQN equals fqn. Overloads, partial
// classes and redeclarations share an FQN.
func (ix *Index) Lookup(fqn string) []*Decl {
	ix.build()
	return ix.byFQN[fqn]
}

// RefsTo returns the resolved references to d in file and source order.
func (ix *Index) RefsTo(d *Decl) []*Ref {
	ix.build()
	return ix.refsTo[d]
}

// Decls iterates over the declarations of all files, modules included.
func (ix *Index) Decls() iter.Seq[*Decl] {
	return func(yield func(*Decl) bool) {
		for _, f := range ix.Files {
			if f.Module != nil && !yield(f.Module) {
				return
			}
			for _, d := range f.Decls {
				if !yield(d) {
					return
				}
			}
		}
	}
}

// Invalidate drops cached lookups. Call it after changing files in place,
// for example in a rename.
func (ix *Index) Invalidate() {
	ix.once = sync.Once{}
}
