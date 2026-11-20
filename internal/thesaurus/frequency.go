package thesaurus

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// parseFrequencies returns the normalized log frequency of each word in a
// "word count" list, log(1+count) / log(1+maxcount), so the most common
// word scores 1 and a word seen once scores close to 0. We ignore entries
// that aren't words and sum up case variants. If fix isn't nil, it repairs
// mis-encoded characters before we key the words.
func parseFrequencies(r io.Reader, fix *strings.Replacer) (map[string]float64, error) {
	counts := map[string]float64{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		word, count, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff")), " ")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(count), 10, 64)
		if err != nil || !isWord(word) {
			continue
		}
		if fix != nil {
			word = fix.Replace(word)
		}
		counts[Key(word)] += float64(n)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("unable to read word frequencies: %w", err)
	}
	maxCount := 0.0
	for _, c := range counts {
		maxCount = max(maxCount, c)
	}
	if maxCount == 0 {
		return counts, nil
	}
	denom := math.Log1p(maxCount)
	for w, c := range counts {
		counts[w] = math.Log1p(c) / denom
	}
	return counts, nil
}

// isWord reports whether s has at least one letter and nothing but letters,
// combining marks, apostrophes and hyphens.
func isWord(s string) bool {
	letters := 0
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsMark(r), r == '\'', r == '\u2019', r == '-':
		default:
			return false
		}
	}
	return letters > 0
}
