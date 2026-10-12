package scraper

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/transport"
)

type testAdapter struct {
	chapters []search.Chapter
	fetch    func(context.Context, string) (string, error)
	list     func(context.Context) ([]search.Chapter, error)
	workers  int64
	rate     float64
}

func (a *testAdapter) Name() string                                                  { return "fixture" }
func (a *testAdapter) BaseURL() string                                               { return "https://example.test" }
func (a *testAdapter) Search(context.Context, string) ([]search.SearchResult, error) { return nil, nil }
func (a *testAdapter) GetChapterList(ctx context.Context, _ string) ([]search.Chapter, error) {
	if a.list != nil {
		return a.list(ctx)
	}
	return a.chapters, nil
}
func (a *testAdapter) GetChapterContent(ctx context.Context, rawURL string) (string, error) {
	return a.fetch(ctx, rawURL)
}
func (a *testAdapter) MaxConcurrency() int64         { return a.workers }
func (a *testAdapter) MaxRequestsPerSecond() float64 { return a.rate }

func testChapters(count int) []search.Chapter {
	chapters := make([]search.Chapter, count)
	for i := range chapters {
		chapters[i] = search.Chapter{Index: i + 1, Title: fmt.Sprintf("Page %d", i+1), URL: fmt.Sprintf("https://example.test/page/%d", i+1)}
	}
	return chapters
}
func testConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.OutputDir, cfg.Format = t.TempDir(), "txt"
	cfg.Concurrency, cfg.RateLimit, cfg.RetryCount, cfg.RetryDelay = 4, 100000, 0, time.Millisecond
	return cfg
}
func testBook(rawURL string) search.SearchResult {
	return search.SearchResult{Title: "Shared title", SiteName: "fixture", BookURL: rawURL}
}
func terminalProgress(t *testing.T, progress <-chan Progress) Progress {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var last Progress
	for {
		select {
		case event, ok := <-progress:
			if !ok {
				return last
			}
			last = event
		case <-deadline.C:
			t.Fatal("scrape did not close its progress channel")
			return Progress{}
		}
	}
}
func startTestBook(t *testing.T, engine *Engine, ctx context.Context, adapter search.SiteAdapter, rawURL string) <-chan Progress {
	t.Helper()
	progress, err := engine.ScrapeBook(ctx, adapter, rawURL, testBook(rawURL))
	if err != nil {
		t.Fatal(err)
	}
	return progress
}
func receiveURL(t *testing.T, requests <-chan string) string {
	t.Helper()
	select {
	case rawURL := <-requests:
		return rawURL
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
		return ""
	}
}

func TestConfiguredConcurrencyChangesOnlyAfterExplicitSpeed(t *testing.T) {
	cfg := testConfig(t)
	cfg.Concurrency = 1
	engine := NewEngine(cfg)
	requests := make(chan string, 4)
	release := make(chan struct{})
	defer close(release)
	adapter := &testAdapter{chapters: testChapters(4), workers: 2, rate: 100000, fetch: func(ctx context.Context, rawURL string) (string, error) {
		requests <- rawURL
		select {
		case <-release:
			return "body", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := startTestBook(t, engine, ctx, adapter, "https://example.test/book/1")
	receiveURL(t, requests)
	if _, err := engine.ScrapeBook(ctx, adapter, "https://example.test/overlap", testBook("https://example.test/overlap")); !errors.Is(err, ErrEngineBusy) {
		t.Fatalf("active download accepted an overlapping task: %v", err)
	}
	select {
	case <-requests:
		t.Fatal("configured concurrency of one was overwritten")
	case <-time.After(40 * time.Millisecond):
	}
	engine.SetSpeed(SpeedFast)
	receiveURL(t, requests)
	select {
	case <-requests:
		t.Fatal("speed change exceeded the source concurrency cap")
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	final := terminalProgress(t, progress)
	if final.Phase != "canceled" || !errors.Is(final.Error, context.Canceled) {
		t.Fatalf("unexpected final progress: %+v", final)
	}
}

func TestConfiguredRateSurvivesScrapeStartup(t *testing.T) {
	cfg := testConfig(t)
	cfg.RateLimit, cfg.SpeedProfile = 20, SpeedSlow
	engine := NewEngine(cfg)
	var mu sync.Mutex
	var starts []time.Time
	adapter := &testAdapter{chapters: testChapters(3), fetch: func(context.Context, string) (string, error) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		return "body", nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	final := terminalProgress(t, startTestBook(t, engine, ctx, adapter, "https://example.test/book/rate"))
	if final.Phase != "completed" || final.Completed != 3 {
		t.Fatalf("configured rate was replaced by a speed profile: %+v", final)
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < 35*time.Millisecond {
			t.Fatalf("requests exceeded the configured rate: %v", gap)
		}
	}
}

func TestEngineRejectsOverlapDuringPlanning(t *testing.T) {
	engine := NewEngine(testConfig(t))
	entered := make(chan struct{})
	adapter := &testAdapter{list: func(ctx context.Context) ([]search.Chapter, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() {
		_, err := engine.ScrapeBook(ctx, adapter, "https://example.test/planning", testBook("https://example.test/planning"))
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("planning did not start")
	}
	_, err := engine.ScrapeBook(ctx, adapter, "https://example.test/overlap", testBook("https://example.test/overlap"))
	if !errors.Is(err, ErrEngineBusy) {
		t.Fatalf("overlapping scrape returned %v", err)
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("planning returned %v", err)
	}
	adapter = &testAdapter{chapters: testChapters(1), fetch: func(context.Context, string) (string, error) { return "after planning", nil }}
	final := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, "https://example.test/restarted"))
	if final.Phase != "completed" {
		t.Fatalf("engine was not released after planning cancellation: %+v", final)
	}
}

func TestUnreadProgressDoesNotBlockCompletion(t *testing.T) {
	engine := NewEngine(testConfig(t))
	adapter := &testAdapter{chapters: testChapters(320), fetch: func(context.Context, string) (string, error) { return "chapter body", nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	progress := startTestBook(t, engine, ctx, adapter, "https://example.test/unread")
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for engine.running.Load() {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("download blocked on its unread progress channel")
		}
	}
	final := terminalProgress(t, progress)
	if final.Phase != "completed" || final.Completed != 320 || final.ChapterIndex != -1 {
		t.Fatalf("terminal mailbox lost authoritative progress: %+v", final)
	}
}

func TestStreamingEPUBPreservesOutOfOrderPagesAndActualChapters(t *testing.T) {
	cfg := testConfig(t)
	cfg.Format, cfg.Concurrency = "epub", 2
	engine := NewEngine(cfg)
	chapters := testChapters(2)
	chapters[0].Index, chapters[1].Index = 40, 90
	second := make(chan struct{})
	adapter := &testAdapter{chapters: chapters, fetch: func(ctx context.Context, rawURL string) (string, error) {
		if rawURL == chapters[0].URL {
			select {
			case <-second:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			return "第1章 First\nalpha\n第2章 Second\nbeta", nil
		}
		close(second)
		return "第3章 Third\ngamma\n第4章 Fourth\ndelta", nil
	}}
	final := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, "https://example.test/stream"))
	if final.Phase != "completed" || final.Completed != 2 {
		t.Fatalf("unexpected final progress: %+v", final)
	}
	archive, err := zip.OpenReader(final.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := make(map[string]string)
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[entry.Name] = string(data)
	}
	for i, body := range []string{"alpha", "beta", "gamma", "delta"} {
		name := fmt.Sprintf("chapter_%03d.xhtml", i+1)
		if !strings.Contains(entries["OEBPS/"+name], "<p>"+body+"</p>") {
			t.Fatalf("%s lost reading order or chapter boundary: %s", name, entries["OEBPS/"+name])
		}
		if !strings.Contains(entries["OEBPS/nav.xhtml"], `href="`+name+`"`) || !strings.Contains(entries["OEBPS/toc.ncx"], `src="`+name+`"`) {
			t.Fatalf("navigation does not target actual chapter file %s", name)
		}
	}
	if _, exists := entries["OEBPS/chapter_005.xhtml"]; exists {
		t.Fatal("page titles generated a spurious chapter")
	}
}

func TestPartialRetryReusesCheckpointsAndRemovesFailureManifest(t *testing.T) {
	cfg := testConfig(t)
	cfg.RetryCount = 3
	engine := NewEngine(cfg)
	chapters := testChapters(3)
	var mu sync.Mutex
	calls := make(map[string]int)
	fixed := false
	adapter := &testAdapter{chapters: chapters, fetch: func(_ context.Context, rawURL string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls[rawURL]++
		if rawURL == chapters[0].URL && !fixed {
			return "", &transport.HTTPError{StatusCode: http.StatusNotFound, URL: rawURL}
		}
		if rawURL == chapters[1].URL && calls[rawURL] == 1 {
			return "", &transport.HTTPError{StatusCode: http.StatusServiceUnavailable, URL: rawURL}
		}
		return fmt.Sprintf("body %s", rawURL), nil
	}}
	bookURL := "https://example.test/partial"
	first := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, bookURL))
	if first.Phase != "partial" || first.Completed != 2 || first.Failed != 1 || first.Error != nil {
		t.Fatalf("unexpected partial progress: %+v", first)
	}
	if calls[chapters[0].URL] != 1 || calls[chapters[1].URL] != 2 {
		t.Fatalf("permanent and transient retries were not distinguished: %v", calls)
	}
	data, err := os.ReadFile(first.FailedFile)
	if err != nil {
		t.Fatal(err)
	}
	var failed []FailedChapter
	if err := json.Unmarshal(data, &failed); err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Index != 0 || failed[0].URL != chapters[0].URL {
		t.Fatalf("failure manifest lost retry identity: %+v", failed)
	}
	output, err := os.ReadFile(first.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	failedAt, keptAt := strings.Index(string(output), "[Download failed:"), strings.Index(string(output), "body "+chapters[1].URL)
	if failedAt < 0 || keptAt < 0 || failedAt > keptAt {
		t.Fatal("failed chapter placeholder was missing or reordered")
	}
	cacheRoot := filepath.Join(cfg.OutputDir, ".cache")
	cacheDir := filepath.Join(cacheRoot, shortHash(bookCacheKey(adapter.Name(), bookURL)))
	for _, path := range []string{cacheRoot, cacheDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("cache directory permissions = %o", info.Mode().Perm())
		}
	}
	for _, path := range []string{first.FailedFile, filepath.Join(cacheDir, shortHash(chapters[1].URL)+".txt")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("private file permissions = %o", info.Mode().Perm())
		}
	}
	fixed = true
	second := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, bookURL))
	if second.Phase != "completed" || second.CachedCount != 2 || second.Completed != 3 || second.Failed != 0 || second.OutputFile != first.OutputFile {
		t.Fatalf("unexpected retry result: %+v", second)
	}
	if calls[chapters[1].URL] != 2 || calls[chapters[2].URL] != 1 {
		t.Fatalf("successful pages were downloaded again: %v", calls)
	}
	if _, err := os.Stat(first.FailedFile); !os.IsNotExist(err) {
		t.Fatalf("obsolete failure manifest remains: %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("successful retry left its cache: %v", err)
	}
}

func TestSameTitleBooksHaveDistinctOutputsAndManifests(t *testing.T) {
	cfg := testConfig(t)
	engine := NewEngine(cfg)
	adapter := &testAdapter{chapters: testChapters(2), fetch: func(_ context.Context, rawURL string) (string, error) {
		if strings.HasSuffix(rawURL, "/2") {
			return "", &transport.HTTPError{StatusCode: http.StatusForbidden}
		}
		return "kept body", nil
	}}
	first := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, "https://example.test/a"))
	second := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, "https://example.test/b"))
	if first.Phase != "partial" || second.Phase != "partial" {
		t.Fatalf("unexpected results: %+v %+v", first, second)
	}
	if first.OutputFile == second.OutputFile || first.FailedFile == second.FailedFile {
		t.Fatal("same-title books overwrote each other's files")
	}
	for _, path := range []string{first.OutputFile, first.FailedFile, second.OutputFile, second.FailedFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCancellationStopsWorkersAndRetryAfterWait(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Concurrency, cfg.RetryCount = 2, 4
			engine := NewEngine(cfg)
			entered := make(chan string, 2)
			var count atomic.Int64
			adapter := &testAdapter{chapters: testChapters(20), fetch: func(ctx context.Context, rawURL string) (string, error) {
				count.Add(1)
				entered <- rawURL
				if retry {
					return "", &transport.HTTPError{StatusCode: http.StatusTooManyRequests, Delay: time.Hour}
				}
				<-ctx.Done()
				return "", ctx.Err()
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			progress := startTestBook(t, engine, ctx, adapter, "https://example.test/cancel")
			receiveURL(t, entered)
			cancel()
			final := terminalProgress(t, progress)
			if final.Phase != "canceled" || !errors.Is(final.Error, context.Canceled) || final.OutputFile != "" {
				t.Fatalf("unexpected cancellation: %+v", final)
			}
			if count.Load() > 2 {
				t.Fatalf("canceled pipeline issued %d requests", count.Load())
			}
			if engine.running.Load() {
				t.Fatal("canceled engine still owns the task")
			}
		})
	}
}

type plannedAdapter struct{ requests atomic.Int64 }

func (a *plannedAdapter) Name() string    { return "planned" }
func (a *plannedAdapter) BaseURL() string { return "https://example.test" }
func (a *plannedAdapter) Search(context.Context, string) ([]search.SearchResult, error) {
	return nil, nil
}
func (a *plannedAdapter) GetChapterList(context.Context, string) ([]search.Chapter, error) {
	return nil, nil
}
func (a *plannedAdapter) GetChapterContent(context.Context, string) (string, error) {
	a.requests.Add(1)
	return "", nil
}
func (a *plannedAdapter) PlanChapters(_ context.Context, _ string, options search.ChapterPlanOptions) (search.ChapterPlan, error) {
	chapter := search.Chapter{Index: 1, Title: "First", URL: "https://example.test/planned/page"}
	if err := options.ContentSink(chapter, "primed planner body"); err != nil {
		return search.ChapterPlan{}, err
	}
	return search.ChapterPlan{Chapters: []search.Chapter{chapter}}, nil
}
func TestPlannedContentPrimesChapterCheckpoint(t *testing.T) {
	adapter := &plannedAdapter{}
	engine := NewEngine(testConfig(t))
	final := terminalProgress(t, startTestBook(t, engine, context.Background(), adapter, "https://example.test/planned"))
	if final.Phase != "completed" || final.CachedCount != 1 || adapter.requests.Load() != 0 {
		t.Fatalf("planner checkpoint was downloaded again: %+v", final)
	}
	data, err := os.ReadFile(final.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "primed planner body") {
		t.Fatalf("planner body missing: %s", data)
	}
}

func TestRetryAfterControlsRetryTiming(t *testing.T) {
	cfg := testConfig(t)
	cfg.RetryCount = 1
	var calls atomic.Int64
	first := make(chan time.Time, 1)
	second := make(chan time.Time, 1)
	adapter := &testAdapter{chapters: testChapters(1), fetch: func(context.Context, string) (string, error) {
		if calls.Add(1) == 1 {
			first <- time.Now()
			return "", &transport.HTTPError{StatusCode: http.StatusTooManyRequests, Delay: 70 * time.Millisecond}
		}
		second <- time.Now()
		return "retry succeeded", nil
	}}
	final := terminalProgress(t, startTestBook(t, NewEngine(cfg), context.Background(), adapter, "https://example.test/retry-after"))
	if final.Phase != "completed" || calls.Load() != 2 {
		t.Fatalf("retry did not recover: %+v", final)
	}
	if gap := (<-second).Sub(<-first); gap < 60*time.Millisecond {
		t.Fatalf("server retry delay was ignored: %v", gap)
	}
}

func TestAllFailedPagesKeepManifestWithoutPublishingEmptyBook(t *testing.T) {
	cfg := testConfig(t)
	cfg.RetryCount = 5
	var calls atomic.Int64
	adapter := &testAdapter{chapters: testChapters(2), fetch: func(context.Context, string) (string, error) {
		calls.Add(1)
		return "", fmt.Errorf("content selector no longer matches the page")
	}}
	final := terminalProgress(t, startTestBook(t, NewEngine(cfg), context.Background(), adapter, "https://example.test/all-failed"))
	if final.Phase != "failed" || final.Failed != 2 || final.Completed != 0 || final.OutputFile != "" || final.FailedFile == "" || final.Error == nil {
		t.Fatalf("all-failed result lost manifest or published an empty book: %+v", final)
	}
	if calls.Load() != 2 {
		t.Fatalf("untyped parser failure was blindly retried %d times", calls.Load())
	}
	data, err := os.ReadFile(final.FailedFile)
	if err != nil {
		t.Fatal(err)
	}
	var failed []FailedChapter
	if err := json.Unmarshal(data, &failed); err != nil {
		t.Fatal(err)
	}
	if len(failed) != 2 || failed[0].Index != 0 || failed[1].Index != 1 {
		t.Fatalf("failure manifest is incomplete or unordered: %+v", failed)
	}
}

func TestWorkerPoolHasHardUpperBound(t *testing.T) {
	cfg := testConfig(t)
	cfg.Concurrency = 1000
	engine := NewEngine(cfg)
	started := make(chan struct{}, 80)
	adapter := &testAdapter{chapters: testChapters(80), fetch: func(ctx context.Context, _ string) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := startTestBook(t, engine, ctx, adapter, "https://example.test/hard-bound")
	for range 64 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("bounded workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("worker pool exceeded its global hard bound")
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	final := terminalProgress(t, progress)
	if final.Phase != "canceled" {
		t.Fatalf("bounded workers did not stop: %+v", final)
	}
}
