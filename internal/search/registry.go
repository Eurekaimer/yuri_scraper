package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Registry holds all registered site adapters.
type Registry struct {
	mu       sync.RWMutex
	adapters []SiteAdapter
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds an adapter.
func (r *Registry) Register(a SiteAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters = append(r.adapters, a)
}

// All returns all registered adapters.
func (r *Registry) All() []SiteAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SiteAdapter, len(r.adapters))
	copy(out, r.adapters)
	return out
}

// Get returns the adapter with the given name, or nil.
func (r *Registry) Get(name string) SiteAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.adapters {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

// SearchAll queries every registered adapter in parallel and aggregates results.
func (r *Registry) SearchAll(ctx context.Context, query string) ([]SearchResult, error) {
	adapters := r.All()
	if len(adapters) == 0 {
		return nil, fmt.Errorf("no site adapters registered")
	}

	type result struct {
		items []SearchResult
		err   error
		name  string
	}

	ch := make(chan result, len(adapters))
	var wg sync.WaitGroup

	for _, a := range adapters {
		wg.Add(1)
		go func(adapter SiteAdapter) {
			defer wg.Done()
			items, err := adapter.Search(ctx, query)
			ch <- result{items: items, err: err, name: adapter.Name()}
		}(a)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var all []SearchResult
	var searchErrors []error
	succeeded := 0
	for r := range ch {
		if r.err != nil {
			searchErrors = append(searchErrors, fmt.Errorf("site %s: %w", r.name, r.err))
			continue
		}
		succeeded++
		all = append(all, r.items...)
	}
	if succeeded == 0 {
		return nil, errors.Join(searchErrors...)
	}
	return all, nil
}
