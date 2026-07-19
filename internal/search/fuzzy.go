package search

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

func normalize(s string) string {
	s = strings.TrimSpace(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

// FuzzyMatch computes a similarity score between query and target text.
// Score range: 0–100. Higher = more similar.
// Handles Chinese text natively: character overlap + edit distance.
func FuzzyMatch(query, target string) float64 {
	if query == "" || target == "" {
		return 0
	}

	q := normalize(query)
	t := normalize(target)

	// Exact match
	if q == t {
		return 100
	}

	// Contains — strong signal
	if strings.Contains(t, q) {
		return 85
	}
	if strings.Contains(q, t) {
		return 75
	}

	// Character-level overlap (core for Chinese fuzzy matching)
	qRunes := []rune(q)
	tRunes := []rune(t)

	overlap := charOverlap(qRunes, tRunes)
	qLen := float64(len(qRunes))
	tLen := float64(len(tRunes))

	// Jaccard-like overlap score
	var score float64
	if qLen > 0 && tLen > 0 {
		// Weighted: how much of query is covered
		coverageInTarget := overlap / qLen
		coverageInQuery := overlap / tLen
		score = 50 * (coverageInTarget*0.7 + coverageInQuery*0.3)
	}

	// N-gram overlap boost (handles transpositions)
	bigramScore := bigramOverlap(qRunes, tRunes) * 15

	// Edit distance penalty (for strings of similar length)
	editScore := editDistanceScore(qRunes, tRunes) * 10

	return score + bigramScore + editScore
}

// charOverlap counts unique characters appearing in both sets.
func charOverlap(a, b []rune) float64 {
	set := make(map[rune]bool, len(a))
	for _, r := range a {
		set[r] = true
	}
	count := 0.0
	seen := make(map[rune]bool)
	for _, r := range b {
		if set[r] && !seen[r] {
			count++
			seen[r] = true
		}
	}
	return count
}

// bigramOverlap computes the ratio of shared bigrams.
func bigramOverlap(a, b []rune) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	grams := make(map[string]int, len(a)-1)
	for i := 0; i < len(a)-1; i++ {
		grams[string(a[i:i+2])]++
	}
	overlap := 0.0
	for i := 0; i < len(b)-1; i++ {
		bg := string(b[i : i+2])
		if count, ok := grams[bg]; ok && count > 0 {
			overlap++
			grams[bg]--
		}
	}
	return overlap / float64(max(len(a)-1, len(b)-1))
}

// editDistanceScore returns a normalized similarity score from Levenshtein distance.
// Returns 0–1 scaled value.
func editDistanceScore(a, b []rune) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dist := levenshtein(a, b)
	maxLen := float64(max(len(a), len(b)))
	return 1.0 - float64(dist)/maxLen
}

func levenshtein(a, b []rune) int {
	n, m := len(a), len(b)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	// Use two rows for memory efficiency
	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = min3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[m]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// FuzzyRank re-ranks search results using fuzzy matching against title, author, and description.
// Returns results sorted by fuzzy score (high to low), preserving original order for ties.
func FuzzyRank(query string, results []SearchResult) []SearchResult {
	if query == "" || len(results) <= 1 {
		return results
	}

	type scored struct {
		r     SearchResult
		score float64
	}
	maxPopularity := 0.0
	for _, result := range results {
		maxPopularity = max(maxPopularity, result.Popularity)
	}
	items := make([]scored, len(results))
	for i, r := range results {
		score := FuzzyMatch(query, r.Title)
		score += FuzzyMatch(query, r.Author) * 0.6
		if r.Description != "" {
			score += FuzzyMatch(query, r.Description) * 0.3
		}
		// Popularity is a bounded bonus: it breaks similarly relevant matches
		// without allowing a hot but unrelated thread to outrank an exact title.
		if maxPopularity > 0 && r.Popularity > 0 {
			score += 15 * math.Log1p(r.Popularity) / math.Log1p(maxPopularity)
		}
		items[i] = scored{r: r, score: score}
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })

	out := make([]SearchResult, len(items))
	for i, s := range items {
		out[i] = s.r
	}
	return out
}

// HasCJK returns true if the string contains CJK characters.
func HasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
