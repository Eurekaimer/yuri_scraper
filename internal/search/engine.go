package search

import (
	"context"
	"fmt"
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

// SearchSite searches a single selected source.
func (e *Engine) SearchSite(ctx context.Context, siteName, query string) ([]SearchResult, error) {
	return e.SearchSiteScoped(ctx, siteName, query, nil)
}

// SearchSiteScoped searches optional source-defined sections. Sources without
// scoped search continue to use the basic SiteAdapter method.
func (e *Engine) SearchSiteScoped(ctx context.Context, siteName, query string, scopeIDs []string) ([]SearchResult, error) {
	adapter := e.registry.Get(siteName)
	if adapter == nil {
		return nil, fmt.Errorf("unknown source: %s", siteName)
	}
	var results []SearchResult
	var err error
	if scoped, ok := adapter.(ScopedSearcher); ok {
		results, err = scoped.SearchScoped(ctx, query, scopeIDs)
	} else {
		results, err = adapter.Search(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	return FuzzyRank(query, results), nil
}
