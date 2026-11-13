// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"errors"
	"fmt"
	"strings"
)

var errBadGlob = errors.New("syntax error in glob")

// FieldPattern is a parsed dot-separated field path pattern. Each segment is
// either "**", which matches zero or more nested fields, or a glob matched
// against a single field name. Globs use path.Match syntax, except that '/'
// is an ordinary character, so "*" also matches keys such as
// "vault.hashicorp.com/agent-inject-secret-db". Lists are traversed
// transparently, so a pattern applies to every element of a list.
type FieldPattern []string

// String returns the pattern in its dot-separated form.
func (p FieldPattern) String() string {
	return strings.Join(p, ".")
}

// ParseFieldPattern parses and validates a dot-separated field path pattern,
// such as "spec.template.spec.containers.env", "spec.*.secretRef" or
// "**.password".
func ParseFieldPattern(pattern string) (FieldPattern, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("field pattern cannot be empty")
	}
	if strings.ContainsAny(pattern, ",:|") {
		return nil, fmt.Errorf("field pattern %q cannot contain ',', ':' or '|'", pattern)
	}

	segments := strings.Split(pattern, ".")
	for i, segment := range segments {
		switch {
		case segment == "":
			return nil, fmt.Errorf("field pattern %q contains an empty segment", pattern)
		case segment == "**":
			if i == len(segments)-1 {
				return nil, fmt.Errorf("field pattern %q cannot end with '**'", pattern)
			}
		default:
			if err := validateGlob(segment); err != nil {
				return nil, fmt.Errorf("field pattern %q has invalid segment %q: %w", pattern, segment, err)
			}
			if hasRecursiveToken(segment) {
				return nil, fmt.Errorf("field pattern %q: '**' must be a whole segment", pattern)
			}
		}
	}
	return FieldPattern(segments), nil
}

// ParseFieldPatterns parses a list of field path patterns.
func ParseFieldPatterns(patterns []string) ([]FieldPattern, error) {
	parsed := make([]FieldPattern, 0, len(patterns))
	for _, p := range patterns {
		fp, err := ParseFieldPattern(p)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, fp)
	}
	return parsed, nil
}

// DropMatchingFields removes the parts of value matched by patterns. fieldPath
// is the dot-separated location of value within the object, and patterns are
// relative to the object root, so a pattern may select value itself, an
// ancestor of it, or fields nested within it. It returns false when value
// itself is dropped. value is never modified; a copy is filtered instead.
func DropMatchingFields(fieldPath string, value any, patterns []FieldPattern) (any, bool) {
	if len(patterns) == 0 {
		return value, true
	}

	var pathSegments []string
	if fieldPath != "" {
		pathSegments = strings.Split(fieldPath, ".")
	}

	var remainders []FieldPattern
	for _, p := range patterns {
		for _, rest := range matchPrefix(p, pathSegments) {
			if len(rest) == 0 {
				return nil, false
			}
			remainders = append(remainders, rest)
		}
	}
	if len(remainders) == 0 {
		return value, true
	}

	filtered := deepCopyValue(value)
	for _, rest := range remainders {
		dropMatches(filtered, rest)
	}
	return filtered, true
}

// matchPrefix matches pattern against fieldPath and returns the unmatched
// pattern suffixes to apply to the value at fieldPath. An empty suffix means
// the pattern matches fieldPath itself or one of its ancestors.
func matchPrefix(pattern FieldPattern, fieldPath []string) []FieldPattern {
	if len(pattern) == 0 {
		return []FieldPattern{{}}
	}
	if len(fieldPath) == 0 {
		return []FieldPattern{pattern}
	}

	if pattern[0] == "**" {
		results := matchPrefix(pattern[1:], fieldPath)
		return append(results, matchPrefix(pattern, fieldPath[1:])...)
	}
	if matchGlob(pattern[0], fieldPath[0]) {
		return matchPrefix(pattern[1:], fieldPath[1:])
	}
	return nil
}

// dropMatches removes fields matched by a non-empty pattern from value in place.
func dropMatches(value any, pattern FieldPattern) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			dropMatches(item, pattern)
		}
	case map[string]any:
		if pattern[0] == "**" {
			dropMatches(v, pattern[1:])
			for _, child := range v {
				dropMatches(child, pattern)
			}
			return
		}
		for key, child := range v {
			if !matchGlob(pattern[0], key) {
				continue
			}
			if len(pattern) == 1 {
				delete(v, key)
				continue
			}
			dropMatches(child, pattern[1:])
		}
	}
}

func deepCopyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = deepCopyValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = deepCopyValue(child)
		}
		return out
	default:
		return v
	}
}

// validateGlob reports whether pattern is well-formed: every '\\' escapes a
// character and every '[' starts a valid character class.
func validateGlob(pattern string) error {
	p := []rune(pattern)
	for i := 0; i < len(p); {
		switch p[i] {
		case '\\':
			if i+1 >= len(p) {
				return errBadGlob
			}
			i += 2
		case '[':
			_, width, err := matchClass(p[i:], 0)
			if err != nil {
				return err
			}
			i += width
		default:
			i++
		}
	}
	return nil
}

// hasRecursiveToken reports whether a validated glob contains two adjacent
// wildcard '*' characters, ignoring escaped stars and character classes.
func hasRecursiveToken(pattern string) bool {
	p := []rune(pattern)
	prevStar := false
	for i := 0; i < len(p); {
		switch p[i] {
		case '*':
			if prevStar {
				return true
			}
			prevStar = true
			i++
			continue
		case '\\':
			i += 2
		case '[':
			_, width, _ := matchClass(p[i:], 0)
			i += width
		default:
			i++
		}
		prevStar = false
	}
	return false
}

// matchGlob matches name against a validated glob. Unlike path.Match, '*',
// '?' and character classes also match '/', which is common in Kubernetes
// label and annotation keys.
func matchGlob(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	px, nx := 0, 0
	starPx, starNx := -1, -1
	for px < len(p) || nx < len(n) {
		if px < len(p) {
			switch p[px] {
			case '*':
				starPx, starNx = px, nx
				px++
				continue
			case '?':
				if nx < len(n) {
					px++
					nx++
					continue
				}
			case '[':
				if nx < len(n) {
					if ok, width, err := matchClass(p[px:], n[nx]); err == nil && ok {
						px += width
						nx++
						continue
					}
				}
			case '\\':
				if px+1 < len(p) && nx < len(n) && n[nx] == p[px+1] {
					px += 2
					nx++
					continue
				}
			default:
				if nx < len(n) && n[nx] == p[px] {
					px++
					nx++
					continue
				}
			}
		}
		if starPx >= 0 && starNx < len(n) {
			starNx++
			px, nx = starPx+1, starNx
			continue
		}
		return false
	}
	return true
}

// matchClass matches c against the character class at the start of p, which
// must begin with '['. It returns whether c matched and the class width.
func matchClass(p []rune, c rune) (bool, int, error) {
	i := 1
	negated := false
	if i < len(p) && p[i] == '^' {
		negated = true
		i++
	}
	matched := false
	for ranges := 0; ; ranges++ {
		if i < len(p) && p[i] == ']' && ranges > 0 {
			return matched != negated, i + 1, nil
		}
		lo, next, err := classChar(p, i)
		if err != nil {
			return false, 0, err
		}
		i = next
		hi := lo
		if i < len(p) && p[i] == '-' {
			if hi, i, err = classChar(p, i+1); err != nil {
				return false, 0, err
			}
			if hi < lo {
				return false, 0, errBadGlob
			}
		}
		if lo <= c && c <= hi {
			matched = true
		}
	}
}

// classChar reads one possibly escaped character of a character class.
func classChar(p []rune, i int) (rune, int, error) {
	if i >= len(p) || p[i] == '-' || p[i] == ']' {
		return 0, 0, errBadGlob
	}
	if p[i] == '\\' {
		if i+1 >= len(p) {
			return 0, 0, errBadGlob
		}
		return p[i+1], i + 2, nil
	}
	return p[i], i + 1, nil
}
