package codelint

import (
	"strings"
	"unicode"
)

// identWords splits an identifier into lowercase parts at underscores,
// digits and case changes: "getHTTPServer_v2" -> get, http, server, v, 2.
func identWords(id string) []string {
	var out []string
	rs := []rune(id)
	start := -1
	emit := func(end int) {
		if start >= 0 && end > start {
			out = append(out, strings.ToLower(string(rs[start:end])))
		}
		start = -1
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			emit(i)
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		prev := rs[i-1]
		switch {
		case unicode.IsUpper(r) && unicode.IsLower(prev),
			unicode.IsDigit(r) != unicode.IsDigit(prev),
			unicode.IsUpper(r) && unicode.IsUpper(prev) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
			emit(i)
			start = i
		}
	}
	emit(len(rs))
	return out
}

// stem strips common English inflections so "returns", "returned" and
// "returning" all compare equal to "return".
func stem(w string) string {
	for _, suf := range []string{"ing", "ies", "ed", "es", "s", "e"} {
		if len(w)-len(suf) < 3 || !strings.HasSuffix(w, suf) || suf == "s" && strings.HasSuffix(w, "ss") {
			continue
		}
		w = strings.TrimSuffix(w, suf)
		if suf == "ies" {
			w += "y"
		}
		break
	}
	return w
}

// wordMatches reports whether two words are the same word if you ignore
// inflection or abbreviation ("init" and "initialize").
func wordMatches(a, b string) bool {
	if a == b {
		return true
	}
	sa, sb := stem(a), stem(b)
	if sa == sb {
		return true
	}
	if len(sa) > len(sb) {
		sa, sb = sb, sa
	}
	return len(sa) >= 4 && strings.HasPrefix(sb, sa)
}

// covered reports whether all words of ws match a word of vocab, or are in
// exact as they are or with the inflection stripped.
func covered(ws []string, vocab, exact map[string]bool) bool {
	for _, w := range ws {
		if vocab[w] || exact[w] || exact[stem(w)] {
			continue
		}
		found := false
		for v := range vocab {
			if wordMatches(w, v) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func setOf(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}
