package command

import (
	"fmt"
	"iter"
	"slices"
)

func closest[T any](name string, candidates []T, maxDist int, key func(T) string) (string, bool) {
	best, bestDist := "", maxDist+1
	for _, candidate := range candidates {
		nameOf := key(candidate)
		if d := Distance(name, nameOf); d < bestDist {
			best, bestDist = nameOf, d
		}
	}
	return best, bestDist <= maxDist
}

// Suggest formats the closest candidate that's within three edits of name,
// or returns "" if there's none. We take a key func so callers don't have to
// build a second list of candidate names.
func Suggest[T any](name string, candidates []T, key func(T) string, format string) string {
	if best, ok := closest(name, candidates, 3, key); ok {
		return fmt.Sprintf(format, best)
	}
	return ""
}

// ParseRuleIgnore checks that the rule IDs exist and returns them as a set.
// The caller does the tokenizing, so each command can keep the option syntax
// it already accepts.
func ParseRuleIgnore[T any](ids iter.Seq[string], rules []T, key func(T) string, tool string) (map[string]bool, error) {
	ignored := map[string]bool{}
	for id := range ids {
		if !slices.ContainsFunc(rules, func(rule T) bool { return key(rule) == id }) {
			suggestion := Suggest(id, rules, key, " (did you mean `%s`?)")
			return nil, Usagef("unknown rule `%s`%s, see `myoo %s --list-rules`", id, suggestion, tool)
		}
		ignored[id] = true
	}
	return ignored, nil
}
