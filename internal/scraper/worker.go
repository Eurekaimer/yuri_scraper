package scraper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/transport"
)

type chapterJob struct {
	index   int
	chapter search.Chapter
}
type chapterResult struct {
	index  int
	path   string
	cached bool
	err    error
}

// Only the coordinator publishes; dropping an old intermediate event leaves a
// bounded mailbox with the most recent counts, including the final result.
func publishProgress(progress chan Progress, event Progress) {
	select {
	case progress <- event:
		return
	default:
	}
	select {
	case <-progress:
	default:
	}
	progress <- event
}

func (e *Engine) run(ctx context.Context, adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, cacheDir string, workerCount int, progress chan Progress) {
	defer func() { e.running.Store(false); close(progress) }()
	state := Progress{ChapterIndex: -1, TotalChapters: len(chapters), Phase: "downloading"}
	publishProgress(progress, state)
	jobs := make(chan chapterJob, workerCount)
	results := make(chan chapterResult, workerCount)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					if err := e.acquireWorker(ctx); err != nil {
						return
					}
					result := e.fetchChapter(ctx, adapter, cacheDir, job)
					e.releaseWorker()
					select {
					case results <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i, chapter := range chapters {
			select {
			case <-ctx.Done():
				return
			case jobs <- chapterJob{index: i, chapter: chapter}:
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	paths := make([]string, len(chapters))
	failed := make([]FailedChapter, 0)
	for result := range results {
		chapter := chapters[result.index]
		event := state
		event.ChapterIndex, event.ChapterTitle, event.ChapterURL = result.index, chapter.Title, chapter.URL
		event.Error, event.Done, event.Cached = result.err, result.err == nil, result.cached
		if result.err != nil {
			failed = append(failed, FailedChapter{Index: result.index, Title: chapter.Title, URL: chapter.URL, Error: result.err.Error()})
			state.Failed++
		} else {
			paths[result.index] = result.path
			state.Completed++
			if result.cached {
				state.CachedCount++
			}
		}
		event.Completed, event.Failed, event.CachedCount = state.Completed, state.Failed, state.CachedCount
		publishProgress(progress, event)
	}
	if err := ctx.Err(); err != nil {
		state.Phase, state.Error = "canceled", err
		publishProgress(progress, state)
		return
	}
	sort.Slice(failed, func(i, j int) bool { return failed[i].Index < failed[j].Index })
	manifestPath := filepath.Join(e.cfg.OutputDir, bookOutputStem(meta)+".failed.json")
	if len(failed) > 0 {
		var err error
		state.FailedFile, err = saveFailedChapters(manifestPath, failed)
		if err != nil {
			state.Phase, state.Error = "failed", fmt.Errorf("save failed chapters: %w", err)
			publishProgress(progress, state)
			return
		}
	}
	if state.Completed == 0 {
		state.Phase, state.Error = "failed", fmt.Errorf("all %d chapter pages failed", len(chapters))
		publishProgress(progress, state)
		return
	}
	state.Phase = "exporting"
	publishProgress(progress, state)
	outputFile, err := e.writeOutput(ctx, adapter, meta, chapters, paths, failed)
	if err != nil {
		state.Phase = "failed"
		if ctx.Err() != nil {
			state.Phase = "canceled"
		}
		state.Error = fmt.Errorf("write output: %w", err)
		publishProgress(progress, state)
		return
	}
	state.OutputFile, state.Phase = outputFile, "completed"
	if len(failed) > 0 {
		state.Phase = "partial"
	} else {
		if err := os.RemoveAll(cacheDir); err != nil {
			state.Phase, state.Error = "failed", fmt.Errorf("remove completed checkpoint cache: %w", err)
		}
		if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
			state.Phase, state.Error = "failed", fmt.Errorf("remove obsolete failure manifest: %w", err)
		}
	}
	publishProgress(progress, state)
}

func (e *Engine) fetchChapter(ctx context.Context, adapter search.SiteAdapter, cacheDir string, job chapterJob) chapterResult {
	if err := ctx.Err(); err != nil {
		return chapterResult{index: job.index, err: err}
	}
	path := filepath.Join(cacheDir, shortHash(job.chapter.URL)+".txt")
	if cached, err := validCheckpoint(path); err != nil {
		return chapterResult{index: job.index, err: err}
	} else if cached {
		return chapterResult{index: job.index, path: path, cached: true}
	}
	if auth, ok := adapter.(search.CredentialsAuthenticator); ok && auth.RequiresAuthentication() && !auth.IsAuthenticated() {
		return chapterResult{index: job.index, err: &transport.PermanentError{Err: fmt.Errorf("source authentication is required")}}
	}
	var content string
	var lastErr error
	for attempt := range e.cfg.RetryCount + 1 {
		if attempt > 0 {
			delay := max(retryDelay(e.cfg.RetryDelay, attempt), transport.RetryAfter(lastErr))
			if err := waitContext(ctx, delay); err != nil {
				return chapterResult{index: job.index, err: err}
			}
		}
		if err := e.limiter.Wait(ctx); err != nil {
			return chapterResult{index: job.index, err: err}
		}
		content, lastErr = adapter.GetChapterContent(ctx, job.chapter.URL)
		if err := ctx.Err(); err != nil {
			return chapterResult{index: job.index, err: err}
		}
		if lastErr == nil && strings.TrimSpace(content) == "" {
			lastErr = &transport.PermanentError{Err: fmt.Errorf("empty chapter content")}
		}
		if lastErr == nil {
			break
		}
		if !transport.CanRetry(lastErr) {
			break
		}
	}
	if lastErr != nil {
		return chapterResult{index: job.index, err: lastErr}
	}
	if err := writeChapterCheckpoint(cacheDir, job.chapter.URL, content); err != nil {
		return chapterResult{index: job.index, err: err}
	}
	return chapterResult{index: job.index, path: path}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	base = min(base, 30*time.Second)
	delay := base << min(max(attempt-1, 0), 4)
	return min(delay, 30*time.Second)
}
