package tui

import (
	"context"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type navigationAdapter struct{ name string }

func (a navigationAdapter) Name() string    { return a.name }
func (a navigationAdapter) BaseURL() string { return "https://example.test" }
func (a navigationAdapter) Search(context.Context, string) ([]search.SearchResult, error) {
	return nil, nil
}
func (a navigationAdapter) GetChapterList(context.Context, string) ([]search.Chapter, error) {
	return nil, nil
}
func (a navigationAdapter) GetChapterContent(context.Context, string) (string, error) { return "", nil }

func newNavigationModel(t *testing.T) *Model {
	t.Helper()
	registry := search.NewRegistry()
	registry.Register(navigationAdapter{name: "test"})
	history, err := store.LoadHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewModel(search.NewEngine(registry), scraper.NewEngine(scraper.DefaultConfig()), history, "test")
}

func TestEscapeFromProgressRestoresResults(t *testing.T) {
	m := newNavigationModel(t)
	m.selectedSite = "test"
	m.results = []search.SearchResult{{Title: "A"}, {Title: "B"}}
	m.section = SecProgress
	m.progress.done = true
	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, &cmds)
	if m.section != SecResults {
		t.Fatalf("section = %v, want SecResults", m.section)
	}
}

func TestTabSwitchesSearchAndResults(t *testing.T) {
	m := newNavigationModel(t)
	m.results = []search.SearchResult{{Title: "A"}}
	m.section = SecSearch
	var cmds []tea.Cmd
	m.handleSearchKeys("tab", &cmds)
	if m.section != SecResults {
		t.Fatalf("section = %v, want results", m.section)
	}
	m.handleSearchKeys("tab", &cmds)
	if m.section != SecSearch {
		t.Fatalf("section = %v, want search", m.section)
	}
}
