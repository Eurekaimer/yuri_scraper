package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
)

type taskSource struct{ gate <-chan struct{} }

func (taskSource) Name() string                                                  { return "fixture" }
func (taskSource) BaseURL() string                                               { return "https://example.test" }
func (taskSource) Search(context.Context, string) ([]search.SearchResult, error) { return nil, nil }
func (taskSource) GetChapterList(context.Context, string) ([]search.Chapter, error) {
	return []search.Chapter{{Index: 1, Title: "第一章", URL: "https://example.test/1"}, {Index: 2, Title: "第二章", URL: "https://example.test/2"}}, nil
}
func (s taskSource) GetChapterContent(ctx context.Context, url string) (string, error) {
	if s.gate != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-s.gate:
		}
	}
	return "Chapter body for " + url, nil
}

func testService(t *testing.T, root string, source taskSource) *Service {
	t.Helper()
	cfg := store.DefaultConfig()
	cfg.OutputDir = root
	cfg.Format = "txt"
	cfg.RateLimit = 20
	cfg.RetryCount = 0
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := search.NewRegistry()
	r.Register(source)
	s.Search = search.NewEngine(r)
	t.Cleanup(s.Cancel)
	return s
}
func waitTerminal(t *testing.T, s *Service) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p := s.Snapshot()
		if p.Terminal() {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not terminate")
	return Snapshot{}
}
func testBook() search.SearchResult {
	return search.SearchResult{Title: "Novel", SiteName: "fixture", BookURL: "https://example.test/book"}
}

func TestUnobservedTaskCommitsHistoryAndFinalSnapshot(t *testing.T) {
	s := testService(t, t.TempDir(), taskSource{})
	stream, err := s.Start(context.Background(), testBook(), search.ChapterPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	final := waitTerminal(t, s)
	if final.State != "completed" || final.Completed != 2 {
		t.Fatalf("unexpected final result: %#v", final)
	}
	history, err := store.LoadHistory(s.Config.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	if list := history.List(); len(list) != 1 || list[0].FilePath != final.OutputFile || list[0].Chapters != 2 {
		t.Fatalf("history not committed independently of UI: %#v", list)
	}
	data, err := os.ReadFile(final.OutputFile)
	var received Snapshot
	for p := range stream {
		received = p
	}
	first, second := strings.Index(string(data), "https://example.test/1"), strings.Index(string(data), "https://example.test/2")
	if err != nil || first < 0 || second <= first {
		t.Fatal("completed output lost or reordered chapter bodies")
	}
	if received.State != final.State || received.OutputFile != final.OutputFile || received.Completed != 2 {
		t.Fatal("terminal outcome was lost to progress backpressure")
	}
}

func TestSingleTaskAndCrossProcessLockReleaseOnCancel(t *testing.T) {
	root := t.TempDir()
	gate := make(chan struct{})
	s := testService(t, root, taskSource{gate: gate})
	other := testService(t, root, taskSource{})
	stream, err := s.Start(context.Background(), testBook(), search.ChapterPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), testBook(), search.ChapterPlanOptions{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("same service allowed duplicate: %v", err)
	}
	if _, err := other.Start(context.Background(), testBook(), search.ChapterPlanOptions{}); !errors.Is(err, store.ErrLibraryBusy) {
		t.Fatalf("second writer bypassed library lock: %v", err)
	}
	s.Cancel()
	final := waitTerminal(t, s)
	for range stream {
	}
	if final.State != "canceled" {
		t.Fatalf("cancel result: %#v", final)
	}
	next, err := other.Start(context.Background(), testBook(), search.ChapterPlanOptions{})
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	for range next {
	}
	if other.Snapshot().State != "completed" {
		t.Fatal("next writer could not complete")
	}
}

func TestMetadataFailureDoesNotReplaceExistingHistory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".history.json")
	original := []byte("broken metadata")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := store.DefaultConfig()
	cfg.OutputDir = root
	if _, err := New(cfg); err == nil {
		t.Fatal("corrupt library accepted for writes")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatal("existing metadata was lost")
	}
}
