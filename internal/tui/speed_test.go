package tui

import (
	"context"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

type speedTestSource struct{ started chan struct{} }

func (*speedTestSource) Name() string    { return "speed-test" }
func (*speedTestSource) BaseURL() string { return "https://example.test/" }
func (*speedTestSource) Search(context.Context, string) ([]search.SearchResult, error) {
	return nil, nil
}
func (s *speedTestSource) GetChapterList(ctx context.Context, _ string) ([]search.Chapter, error) {
	close(s.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (*speedTestSource) GetChapterContent(context.Context, string) (string, error) { return "", nil }
func (*speedTestSource) MaxConcurrency() int64                                     { return 64 }
func (*speedTestSource) MaxRequestsPerSecond() float64                             { return 20 }

func TestTaskSpeedKeysChangeEffectiveBudget(t *testing.T) {
	m := newNavigationModel(t)
	source := &speedTestSource{started: make(chan struct{})}
	m.service.Search.Registry().Register(source)
	book := search.SearchResult{Title: "book", SiteName: source.Name(), BookURL: source.BaseURL()}
	if _, err := m.service.Start(context.Background(), book, search.ChapterPlanOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.service.Cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.service.Wait(ctx); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-source.started:
	case <-time.After(3 * time.Second):
		t.Fatal("download planning did not start")
	}
	m.section, m.progress.state = SecProgress, "downloading"
	for _, tc := range []struct {
		key      rune
		workers  int64
		requests float64
	}{
		{'4', 24, 8}, {'5', 32, 12}, {'a', 24, 8}, {'1', 2, .5}, {'3', 16, 5},
	} {
		var commands []tea.Cmd
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}}, &commands)
		got := m.service.Snapshot()
		if got.Workers != tc.workers || got.RequestsPer != tc.requests || m.progress.workers != tc.workers || m.progress.requestsPerSecond != tc.requests {
			t.Fatalf("key %c: service=%d/%g view=%d/%g", tc.key, got.Workers, got.RequestsPer, m.progress.workers, m.progress.requestsPerSecond)
		}
	}
}
