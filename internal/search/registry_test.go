package search

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockAdapter implements SiteAdapter for testing.
type mockAdapter struct {
	name     string
	baseURL  string
	results  []SearchResult
	chapters []Chapter
	content  map[string]string
	err      error
}

func (m *mockAdapter) Name() string    { return m.name }
func (m *mockAdapter) BaseURL() string { return m.baseURL }
func (m *mockAdapter) Search(ctx context.Context, query string) ([]SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}
func (m *mockAdapter) GetChapterList(ctx context.Context, bookURL string) ([]Chapter, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.chapters, nil
}
func (m *mockAdapter) GetChapterContent(ctx context.Context, chapterURL string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.content[chapterURL], nil
}

func TestRegistry_RegisterAndAll(t *testing.T) {
	r := NewRegistry()
	if len(r.All()) != 0 {
		t.Error("new registry should be empty")
	}

	m1 := &mockAdapter{name: "site1"}
	m2 := &mockAdapter{name: "site2"}
	r.Register(m1)
	r.Register(m2)

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 adapters, got %d", len(all))
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	m := &mockAdapter{name: "test-site"}
	r.Register(m)

	if a := r.Get("test-site"); a == nil {
		t.Error("Get returned nil for registered adapter")
	}
	if a := r.Get("nonexistent"); a != nil {
		t.Error("Get returned non-nil for unknown adapter")
	}
}

func TestRegistry_SearchAllAggregates(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{
		name:    "A",
		results: []SearchResult{{Title: "书1", SiteName: "A"}},
	})
	r.Register(&mockAdapter{
		name:    "B",
		results: []SearchResult{{Title: "书2", SiteName: "B"}, {Title: "书3", SiteName: "B"}},
	})

	results, err := r.SearchAll(context.Background(), "test")
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestRegistry_SearchAllPartialErrors(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{
		name:    "good",
		results: []SearchResult{{Title: "good-book"}},
	})
	r.Register(&mockAdapter{
		name: "bad",
		err:  errors.New("network error"),
	})

	results, err := r.SearchAll(context.Background(), "test")
	if err != nil {
		t.Fatalf("SearchAll should not fail on partial errors: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result from good site, got %d", len(results))
	}
}

func TestRegistry_SearchAllReturnsErrorWhenEverySiteFails(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{name: "first", err: errors.New("first failure")})
	r.Register(&mockAdapter{name: "second", err: errors.New("second failure")})
	results, err := r.SearchAll(context.Background(), "test")
	if err == nil {
		t.Fatal("expected combined error")
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0", len(results))
	}
	if !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("combined error missed a source: %v", err)
	}
}

func TestRegistry_SearchAllNoAdapters(t *testing.T) {
	r := NewRegistry()
	_, err := r.SearchAll(context.Background(), "test")
	if err == nil {
		t.Error("expected error when no adapters registered")
	}
}

func TestEngineSearchWithRanking(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAdapter{
		name: "site",
		results: []SearchResult{
			{Title: "陌生标题", Author: ""},
			{Title: "测试小说", Author: "测试"},
		},
	})
	e := NewEngine(r)

	results, err := e.Search(context.Background(), "测试")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "测试小说" {
		t.Errorf("expected '测试小说' first, got %q", results[0].Title)
	}
}
