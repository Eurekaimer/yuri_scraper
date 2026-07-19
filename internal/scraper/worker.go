package scraper

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
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

func (e *Engine) run(ctx context.Context, adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, cacheDir string, progress chan<- Progress) {
	defer close(progress)
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		progress <- Progress{ChapterIndex: -1, TotalChapters: len(chapters), Error: fmt.Errorf("create checkpoint directory: %w", err)}
		return
	}
	jobs, feedingDone := make(chan chapterJob), make(chan struct{})
	results := make(chan chapterResult, min(len(chapters), 256))
	maxWorkers := max64(16, e.cfg.Concurrency)
	if maxWorkers > 64 {
		maxWorkers = 64
	}
	var wg sync.WaitGroup
	for workerID := int64(0); workerID < maxWorkers; workerID++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			for {
				if err := e.waitUntilEnabled(ctx, id, feedingDone); err != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					results <- e.fetchChapter(ctx, adapter, cacheDir, job)
				}
			}
		}(workerID)
	}
	go func() {
		defer func() { close(jobs); close(feedingDone) }()
		for i, chapter := range chapters {
			select {
			case <-ctx.Done():
				return
			case jobs <- chapterJob{index: i, chapter: chapter}:
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	paths := make(map[int]string, len(chapters))
	failed := make([]FailedChapter, 0)
	for result := range results {
		chapter := chapters[result.index]
		if result.err != nil {
			failed = append(failed, FailedChapter{Index: result.index, Title: chapter.Title, URL: chapter.URL, Error: result.err.Error()})
			progress <- Progress{ChapterIndex: result.index, ChapterTitle: chapter.Title, ChapterURL: chapter.URL, TotalChapters: len(chapters), Error: result.err}
			continue
		}
		paths[result.index] = result.path
		progress <- Progress{ChapterIndex: result.index, ChapterTitle: chapter.Title, TotalChapters: len(chapters), Done: true, Cached: result.cached}
	}
	if err := ctx.Err(); err != nil {
		progress <- Progress{ChapterIndex: -1, TotalChapters: len(chapters), Error: err}
		return
	}
	outputFile, err := e.writeOutput(ctx, adapter, meta, chapters, paths, failed)
	if err != nil {
		progress <- Progress{ChapterIndex: -1, TotalChapters: len(chapters), Error: fmt.Errorf("write output: %w", err)}
		return
	}
	failedFile := ""
	if len(failed) > 0 {
		failedFile, err = SaveFailedChapters(e.cfg.OutputDir, meta.Title, failed)
		if err != nil {
			progress <- Progress{ChapterIndex: -1, TotalChapters: len(chapters), OutputFile: outputFile, Error: fmt.Errorf("save failed chapters: %w", err)}
			return
		}
	} else {
		_ = os.RemoveAll(cacheDir)
		_ = os.Remove(filepath.Join(e.cfg.OutputDir, safeFilename(meta.Title)+".failed.json"))
	}
	progress <- Progress{ChapterIndex: -1, TotalChapters: len(chapters), OutputFile: outputFile, FailedFile: failedFile}
}

func (e *Engine) waitUntilEnabled(ctx context.Context, workerID int64, feedingDone <-chan struct{}) error {
	for workerID >= e.concurrency.Load() {
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-feedingDone:
			timer.Stop()
			return context.Canceled
		case <-timer.C:
		}
	}
	return nil
}

func (e *Engine) fetchChapter(ctx context.Context, adapter search.SiteAdapter, cacheDir string, job chapterJob) chapterResult {
	path := filepath.Join(cacheDir, shortHash(job.chapter.URL)+".txt")
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return chapterResult{index: job.index, path: path, cached: true}
	}
	var content string
	var lastErr error
	for attempt := 0; attempt <= e.cfg.RetryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return chapterResult{index: job.index, err: ctx.Err()}
			case <-time.After(retryDelay(e.cfg.RetryDelay, attempt, job.index)):
			}
		}
		if err := e.limiter.Wait(ctx); err != nil {
			return chapterResult{index: job.index, err: err}
		}
		content, lastErr = adapter.GetChapterContent(ctx, job.chapter.URL)
		if lastErr == nil && strings.TrimSpace(content) != "" {
			break
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("empty chapter content")
		}
		log.Printf("[retry] chapter %d attempt %d: %v", job.index+1, attempt+1, lastErr)
	}
	if lastErr != nil {
		return chapterResult{index: job.index, err: lastErr}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return chapterResult{index: job.index, err: err}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return chapterResult{index: job.index, err: err}
	}
	return chapterResult{index: job.index, path: path}
}

func retryDelay(base time.Duration, attempt, index int) time.Duration {
	delay := base << min(attempt-1, 4)
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay + time.Duration((index+attempt*17)%750)*time.Millisecond
}
