package code

import "slices"

// StringSet builds a membership lookup for lexer keywords and known names.
func StringSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// PunctLen returns the length of the punctuation token at the start of b,
// which is 3 or 2 if b starts with an entry of three or two, else 1. The
// longest match wins, so "<<=" isn't read as "<<" followed by "=".
func PunctLen(b []byte, three, two []string) int {
	for _, set := range [...]struct {
		n     int
		punct []string
	}{{3, three}, {2, two}} {
		if len(b) < set.n {
			continue
		}
		if slices.Contains(set.punct, string(b[:set.n])) {
			return set.n
		}
	}
	return 1
}
