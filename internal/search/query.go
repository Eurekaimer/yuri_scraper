package search

import (
	"strings"
	"unicode"
)

// QueryVariants returns a small, deterministic set of useful search forms.
// Keeping this source-agnostic lets forum adapters reuse the same tokenization
// while bounding the number of remote requests.
func QueryVariants(query string, limit int) []string {
	raw := strings.TrimSpace(query)
	if raw == "" || limit <= 0 {
		return nil
	}
	variants := []string{raw}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，、。；;：《》〈〉『』「」()（）[]【】", r)
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len([]rune(part)) >= 2 {
			variants = append(variants, part)
		}
	}
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, raw)
	variants = append(variants, compact)
	// Discuz search cannot rank strings it never returns. For longer CJK
	// queries, bounded trigrams provide typo/variant recall only when the full
	// query produced no hits; adapters stop requesting variants after a hit.
	compactRunes := []rune(compact)
	if HasCJK(compact) && len(compactRunes) >= 4 {
		for i := 0; i+3 <= len(compactRunes); i++ {
			variants = append(variants, string(compactRunes[i:i+3]))
		}
	}

	seen := make(map[string]bool, len(variants))
	result := make([]string, 0, min(limit, len(variants)))
	for _, variant := range variants {
		variant = strings.TrimSpace(variant)
		if variant == "" || seen[variant] {
			continue
		}
		seen[variant] = true
		result = append(result, variant)
		if len(result) == limit {
			break
		}
	}
	return result
}
