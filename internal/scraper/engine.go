package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/converter"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"golang.org/x/time/rate"
)

// SpeedProfile controls both worker concurrency and request rate.
type SpeedProfile int

const (
	SpeedSlow SpeedProfile = iota
	SpeedNormal
	SpeedFast
)

func (sp SpeedProfile) concurrency() int64 {
	switch sp {
	case SpeedSlow:
		return 2
	case SpeedFast:
		return 16
	default:
		return 8
	}
}

func (sp SpeedProfile) rateLimit() float64 {
	switch sp {
	case SpeedSlow:
		return 0.5
	case SpeedFast:
		return 5
	default:
		return 2
	}
}

func (sp SpeedProfile) String() string {
	switch sp {
	case SpeedSlow:
		return "慢速"
	case SpeedFast:
		return "快速"
	default:
		return "正常"
	}
}

type Config struct {
	Concurrency  int64
	RateLimit    float64
	OutputDir    string
	Format       string
	RetryCount   int
	RetryDelay   time.Duration
	SpeedProfile SpeedProfile
	StartChapter int
	EndChapter   int
}

func (c *Config) ScrapeRange(start, end int) {
	if start > 0 {
		c.StartChapter = start - 1
	}
	c.EndChapter = end
}

func DefaultConfig() Config {
	return Config{
		Concurrency: 8, RateLimit: 2, OutputDir: "novels", Format: "epub",
		RetryCount: 3, RetryDelay: 2 * time.Second, SpeedProfile: SpeedNormal,
	}
}

type FailedChapter struct {
	Index int    `json:"index"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Error string `json:"error"`
}

type Progress struct {
	ChapterIndex  int
	ChapterTitle  string
	ChapterURL    string
	TotalChapters int
	Done          bool
	Cached        bool
	Error         error
	OutputFile    string
	FailedFile    string
}

// Engine orchestrates a bounded worker pool. Concurrency and request rate can
// be changed while a scrape is running.
type Engine struct {
	cfg         Config
	limiter     *rate.Limiter
	concurrency atomic.Int64
	speed       atomic.Int64
	maxWorkers  atomic.Int64
	maxRateBits atomic.Uint64
}

func NewEngine(cfg Config) *Engine {
	defaults := DefaultConfig()
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaults.OutputDir
	}
	if cfg.Format == "" {
		cfg.Format = defaults.Format
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = defaults.RetryDelay
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = defaults.RetryCount
	}
	workers := cfg.Concurrency
	if workers <= 0 {
		workers = cfg.SpeedProfile.concurrency()
	}
	rps := cfg.RateLimit
	if rps <= 0 {
		rps = cfg.SpeedProfile.rateLimit()
	}
	e := &Engine{cfg: cfg, limiter: rate.NewLimiter(rate.Limit(rps), 1)}
	e.speed.Store(int64(cfg.SpeedProfile))
	e.maxWorkers.Store(64)
	e.maxRateBits.Store(math.Float64bits(math.MaxFloat64))
	e.concurrency.Store(workers)
	return e
}

func (e *Engine) SetSpeed(sp SpeedProfile) {
	e.speed.Store(int64(sp))
	e.applySpeed(sp)
}

func (e *Engine) applySpeed(sp SpeedProfile) {
	workers := sp.concurrency()
	if maximum := e.maxWorkers.Load(); maximum > 0 && workers > maximum {
		workers = maximum
	}
	rps := sp.rateLimit()
	if maximum := math.Float64frombits(e.maxRateBits.Load()); maximum > 0 && rps > maximum {
		rps = maximum
	}
	e.concurrency.Store(workers)
	e.limiter.SetLimit(rate.Limit(rps))
	// A burst of one prevents a long idle period from becoming a request spike.
	e.limiter.SetBurst(1)
}

func (e *Engine) ScrapeBook(ctx context.Context, adapter search.SiteAdapter, bookURL string, meta search.SearchResult) (<-chan Progress, error) {
	e.maxWorkers.Store(64)
	e.maxRateBits.Store(math.Float64bits(math.MaxFloat64))
	if policy, ok := adapter.(search.ScrapePolicyProvider); ok {
		if maximum := policy.MaxConcurrency(); maximum > 0 {
			e.maxWorkers.Store(maximum)
		}
		if maximum := policy.MaxRequestsPerSecond(); maximum > 0 {
			e.maxRateBits.Store(math.Float64bits(maximum))
		}
	}
	e.applySpeed(SpeedProfile(e.speed.Load()))
	chapters, err := adapter.GetChapterList(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("get chapter list: %w", err)
	}
	chapters = e.applyRange(chapters)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in range")
	}

	progress := make(chan Progress, min(len(chapters)+2, 256))
	go e.run(ctx, adapter, meta, chapters, progress)
	return progress, nil
}

func (e *Engine) applyRange(chapters []search.Chapter) []search.Chapter {
	start, end := e.cfg.StartChapter, e.cfg.EndChapter
	if start < 0 {
		start = 0
	}
	if end <= 0 || end > len(chapters) {
		end = len(chapters)
	}
	if start >= end {
		return nil
	}
	return chapters[start:end]
}

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

func (e *Engine) run(ctx context.Context, adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, progress chan<- Progress) {
	defer close(progress)

	cacheDir := filepath.Join(e.cfg.OutputDir, ".cache", shortHash(meta.BookURL))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		progress <- Progress{ChapterIndex: -1, TotalChapters: len(chapters), Error: fmt.Errorf("create checkpoint directory: %w", err)}
		return
	}

	jobs := make(chan chapterJob)
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
				if err := e.waitUntilEnabled(ctx, id); err != nil {
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
		defer close(jobs)
		for i, chapter := range chapters {
			select {
			case <-ctx.Done():
				return
			case jobs <- chapterJob{index: i, chapter: chapter}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	paths := make(map[int]string, len(chapters))
	failed := make([]FailedChapter, 0)
	for result := range results {
		chapter := chapters[result.index]
		if result.err != nil {
			failed = append(failed, FailedChapter{
				Index: result.index, Title: chapter.Title, URL: chapter.URL, Error: result.err.Error(),
			})
			progress <- Progress{
				ChapterIndex: result.index, ChapterTitle: chapter.Title, ChapterURL: chapter.URL,
				TotalChapters: len(chapters), Error: result.err,
			}
			continue
		}
		paths[result.index] = result.path
		progress <- Progress{
			ChapterIndex: result.index, ChapterTitle: chapter.Title,
			TotalChapters: len(chapters), Done: true, Cached: result.cached,
		}
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

func (e *Engine) waitUntilEnabled(ctx context.Context, workerID int64) error {
	for workerID >= e.concurrency.Load() {
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
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
			delay := retryDelay(e.cfg.RetryDelay, attempt, job.index)
			select {
			case <-ctx.Done():
				return chapterResult{index: job.index, err: ctx.Err()}
			case <-time.After(delay):
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

func (e *Engine) writeOutput(ctx context.Context, adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, paths map[int]string, failed []FailedChapter) (string, error) {
	if err := os.MkdirAll(e.cfg.OutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	var rawText strings.Builder
	failedByIndex := make(map[int]FailedChapter, len(failed))
	for _, item := range failed {
		failedByIndex[item.Index] = item
	}
	for i, chapter := range chapters {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if path := paths[i]; path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			rawText.Write(data)
			rawText.WriteByte('\n')
			continue
		}
		if item, ok := failedByIndex[i]; ok {
			fmt.Fprintf(&rawText, "\n第%d章 %s\n【下载失败：%s】\n", i+1, chapter.Title, item.Error)
		}
	}
	ordered := converter.SplitChapters(rawText.String(), 1)
	if len(ordered) == 0 {
		return "", fmt.Errorf("no content to write")
	}

	description, protagonist, tags := meta.Description, meta.Protagonist, meta.Tags
	if provider, ok := adapter.(search.BookMetaProvider); ok {
		detail, err := provider.GetBookMeta(ctx, meta.BookURL)
		if err == nil && detail != nil {
			if detail.Description != "" {
				description = detail.Description
			}
			if detail.Protagonist != "" {
				protagonist = detail.Protagonist
			}
			if len(detail.Tags) > 0 {
				tags = converter.MergeTags(tags, detail.Tags)
			}
		}
	}

	bookMeta := converter.BookMeta{
		Title: meta.Title, Author: orDefault(meta.Author, "Unknown"), Description: description,
		Protagonist: protagonist, Tags: tags, Language: "zh-CN", Chapters: ordered,
	}
	ext := ".txt"
	if e.cfg.Format == "epub" {
		ext = ".epub"
	}
	outFile := filepath.Join(e.cfg.OutputDir, safeFilename(meta.Title)+ext)
	tmpFile := outFile + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return "", err
	}
	if ext == ".epub" {
		err = converter.WriteEPUB(f, bookMeta)
	} else {
		err = converter.WriteTXT(f, ordered)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	if err := os.Rename(tmpFile, outFile); err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	log.Printf("Saved: %s (%d chapters)", outFile, len(ordered))
	return outFile, nil
}

// SaveFailedChapters writes a book-specific retry manifest and returns its path.
func SaveFailedChapters(outputDir, bookTitle string, failed []FailedChapter) (string, error) {
	if len(failed) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outputDir, safeFilename(bookTitle)+".failed.json")
	data, err := json.MarshalIndent(failed, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func safeFilename(s string) string {
	repl := map[rune]rune{'/': '_', '\\': '_', ':': '_', '*': '_', '?': '_', '"': '_', '<': '_', '>': '_', '|': '_'}
	cleaned := strings.Map(func(r rune) rune {
		if v, ok := repl[r]; ok {
			return v
		}
		return r
	}, s)
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" {
		return "book"
	}
	return cleaned
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
