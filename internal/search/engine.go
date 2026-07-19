package search

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

// Engine wraps the registry with ranking and smart suggestions.
type Engine struct {
	registry *Registry
}

// NewEngine creates a search engine.
func NewEngine(reg *Registry) *Engine {
	return &Engine{registry: reg}
}
// Registry returns the underlying site registry.
func (e *Engine) Registry() *Registry {
	return e.registry
}


// Search performs a multi-site search and ranks results.
func (e *Engine) Search(ctx context.Context, query string) ([]SearchResult, error) {
	results, err := e.registry.SearchAll(ctx, query)
	if err != nil {
		return nil, err
	}
	return FuzzyRank(query, results), nil
}

// rank sorts results by relevance.
func (e *Engine) rank(query string, results []SearchResult) []SearchResult {
	q := normalize(query)
	if q == "" {
		return results
	}

	type scored struct {
		r     SearchResult
		score int
	}

	items := make([]scored, len(results))
	for i, r := range results {
		items[i] = scored{r: r, score: matchScore(q, r)}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	out := make([]SearchResult, len(items))
	for i, s := range items {
		out[i] = s.r
	}
	return out
}

// matchScore computes relevance (higher = better).
func matchScore(query string, r SearchResult) int {
	if query == "" {
		return 0
	}
	score := 0
	title := normalize(r.Title)
	author := normalize(r.Author)
	desc := normalize(r.Description)

	if title == query {
		score += 100
	}
	if strings.Contains(title, query) {
		score += 50
	}
	if strings.Contains(query, title) {
		score += 40
	}
	if strings.Contains(author, query) {
		score += 30
	}
	if strings.Contains(desc, query) {
		score += 10
	}

	qWords := strings.Fields(query)
	tWords := strings.Fields(title)
	for _, qw := range qWords {
		for _, tw := range tWords {
			if qw == tw {
				score += 20
			}
		}
	}

	if r.ChapterCount > 100 {
		score += 5
	}

	return score
}

func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
	return s
}
