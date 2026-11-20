package code

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"slices"
	"strings"
)

type ignoreRule struct {
	base     string // directory of the .gitignore, slash-separated, "" for root
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool // there's a slash before the last character of the pattern
}

// ignoreList holds rules in file order. Later rules win, like in git.
type ignoreList []ignoreRule

func parseGitignore(base string, data []byte) ignoreList {
	var rules ignoreList
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if !strings.HasSuffix(line, `\ `) {
			line = strings.TrimRight(line, " ")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{base: base}
		if strings.HasPrefix(line, "!") {
			r.negate, line = true, line[1:]
		} else if strings.HasPrefix(line, `\`) {
			line = line[1:]
		}
		if before, ok := strings.CutSuffix(line, "/"); ok {
			r.dirOnly, line = true, before
		}
		if strings.Contains(line, "/") {
			r.anchored, line = true, strings.TrimPrefix(line, "/")
		}
		if line == "" {
			continue
		}
		// gitignore spells negated classes [!...], path.Match spells them [^...].
		r.pattern = strings.ReplaceAll(line, "[!", "[^")
		rules = append(rules, r)
	}
	return rules
}

// rel is slash-separated and relative to the workspace root. decided is false
// if no rule applies.
func (l ignoreList) match(rel string, isDir bool) (ignored, decided bool) {
	for _, r := range slices.Backward(l) {
		if r.dirOnly && !isDir {
			continue
		}
		p := rel
		if r.base != "" {
			if !strings.HasPrefix(rel, r.base+"/") {
				continue
			}
			p = rel[len(r.base)+1:]
		}
		var ok bool
		if r.anchored {
			ok = globMatch(r.pattern, p)
		} else {
			ok = globMatch(r.pattern, path.Base(p))
		}
		if ok {
			return !r.negate, true
		}
	}
	return false, false
}

// CheckIgnorePattern reports a gitignore-style pattern whose syntax is
// malformed, like an unclosed "[" or a trailing "\". Such a pattern can't
// match, so it would silently skip nothing.
func CheckIgnorePattern(pattern string) error {
	p := strings.TrimSuffix(strings.TrimPrefix(pattern, "!"), "/")
	for seg := range strings.SplitSeq(strings.ReplaceAll(p, "[!", "[^"), "/") {
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Errorf("malformed pattern `%s`: %w", pattern, err)
		}
	}
	return nil
}

// globMatch matches a gitignore glob against a slash-separated path. "**"
// matches any number of path segments.
func globMatch(pattern, name string) bool {
	if !strings.Contains(pattern, "**") {
		ok, err := path.Match(pattern, name)
		return err == nil && ok
	}
	pp := strings.Split(pattern, "/")
	np := strings.Split(name, "/")
	return matchSegments(pp, np)
}

func matchSegments(pp, np []string) bool {
	for len(pp) > 0 {
		if pp[0] == "**" {
			if len(pp) == 1 {
				// A trailing "/**" matches everything inside, not the
				// directory itself.
				return len(np) > 0
			}
			for i := range len(np) + 1 {
				if matchSegments(pp[1:], np[i:]) {
					return true
				}
			}
			return false
		}
		if len(np) == 0 {
			return false
		}
		ok, err := path.Match(pp[0], np[0])
		if err != nil || !ok {
			return false
		}
		pp, np = pp[1:], np[1:]
	}
	return len(np) == 0
}
