package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hzg/yuri_scraper/internal/converter"
	"github.com/hzg/yuri_scraper/internal/search"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)


// SpeedProfile controls the aggressiveness of scraping.
type SpeedProfile int

const (
	SpeedSlow   SpeedProfile = iota // gentle: 2 workers, 0.5 req/s
	SpeedNormal                      // balanced: 8 workers, 2 req/s
	SpeedFast                        // aggressive: 16 workers, 5 req/s
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
		return 5.0
	default:
		return 2.0
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
	StartChapter int // 0 = from beginning
	EndChapter   int // 0 = to end
}

// ScrapeRange sets the chapter range to download.
func (c *Config) ScrapeRange(start, end int) {
	if start > 0 {
		c.StartChapter = start - 1
	}
	c.EndChapter = end
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Concurrency:  8,
		RateLimit:    2,
		OutputDir:    "novels",
		Format:       "epub",
		RetryCount:   3,
		RetryDelay:   2 * time.Second,
		SpeedProfile: SpeedNormal,
	}
}
// FailedChapter records a chapter that failed to download.
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
	Error         error
	OutputFile    string
}

// Engine orchestrates concurrent chapter scraping.
type Engine struct {
	cfg     Config
	limiter *rate.Limiter // dynamic rate limiter, updated by SetSpeed
}

func NewEngine(cfg Config) *Engine {
	e := &Engine{cfg: cfg}
	rateLimit := cfg.RateLimit
	if rateLimit <= 0 {
		rateLimit = cfg.SpeedProfile.rateLimit()
	}
	e.limiter = rate.NewLimiter(rate.Limit(rateLimit), 1)
	return e
}

func (e *Engine) SetSpeed(sp SpeedProfile) {
	e.cfg.SpeedProfile = sp
	e.limiter.SetLimit(rate.Limit(sp.rateLimit()))
	e.limiter.SetBurst(int(sp.concurrency() / 2))
	if e.limiter.Burst() < 1 {
		e.limiter.SetBurst(1)
	}
}

// ScrapeBook downloads all chapters of a book and saves as TXT or EPUB.
func (e *Engine) ScrapeBook(ctx context.Context, adapter search.SiteAdapter, bookURL string, meta search.SearchResult) (<-chan Progress, error) {
	chapters, err := adapter.GetChapterList(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("get chapter list: %w", err)
	}
	// Apply chapter range
	if e.cfg.EndChapter > 0 || e.cfg.StartChapter > 0 {
		start := e.cfg.StartChapter
		end := e.cfg.EndChapter
		if end == 0 || end > len(chapters) {
			end = len(chapters)
		}
		if start < len(chapters) {
			chapters = chapters[start:end]
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in range")
	}
	ch := make(chan Progress, len(chapters))
	concurrency := e.cfg.Concurrency
	if concurrency <= 0 {
		concurrency = e.cfg.SpeedProfile.concurrency()
	}
	sem := semaphore.NewWeighted(concurrency)
	// Use engine's shared limiter for dynamic speed control
	limiter := e.limiter

	// Ordered storage for chapters
	type result struct {
		index   int
		content string
		err     error
	}
	results := make(chan result, len(chapters))

	var wg sync.WaitGroup

	go func() {
		for i, chapter := range chapters {
			wg.Add(1)
			go func(idx int, chap search.Chapter) {
				defer wg.Done()

				// Acquire semaphore
				if err := sem.Acquire(ctx, 1); err != nil {
					results <- result{index: idx, err: err}
					return
				}
				defer sem.Release(1)

				// Wait for rate limiter
				if err := limiter.Wait(ctx); err != nil {
					results <- result{index: idx, err: err}
					return
				}

				// Download with retries
				var content string
				var lastErr error
				for attempt := 0; attempt <= e.cfg.RetryCount; attempt++ {
					if attempt > 0 {
						select {
						case <-ctx.Done():
							results <- result{index: idx, err: ctx.Err()}
							return
						case <-time.After(e.cfg.RetryDelay):
						}
					}

					content, lastErr = adapter.GetChapterContent(ctx, chap.URL)
					if lastErr == nil {
						break
					}
					log.Printf("[retry] chapter %d attempt %d: %v", idx+1, attempt+1, lastErr)
				}

				results <- result{index: idx, content: content, err: lastErr}
			}(i, chapter)
		}

		wg.Wait()
		close(results)
	}()

	// Goroutine to save results and emit progress
	go func() {
		defer close(ch)

		// Collect ordered results
		collected := make(map[int]string)
		var lastErr error
		total := len(chapters)
	for r := range results {
		if r.err != nil {
			ch <- Progress{
				ChapterIndex:  r.index,
				ChapterTitle:  chapters[r.index].Title,
				ChapterURL:    chapters[r.index].URL,
				TotalChapters: total,
				Error:         r.err,
			}
			lastErr = r.err
			continue
		}
		collected[r.index] = r.content

		ch <- Progress{
			ChapterIndex:  r.index,
			ChapterTitle:  chapters[r.index].Title,
			TotalChapters: total,
			Done:          true,
		}
	}

	if lastErr != nil {
		ch <- Progress{
			ChapterIndex:  -1,
			TotalChapters: total,
			Error:         fmt.Errorf("下载未完成: %w", lastErr),
		}
		return
	}

	outputFile, err := e.writeOutput(adapter, meta, chapters, collected)
	if err != nil {
		ch <- Progress{
			ChapterIndex:  -1,
			TotalChapters: total,
			Error:         fmt.Errorf("write output: %w", err),
		}
		return
	}
	ch <- Progress{
		ChapterIndex:  -1,
		TotalChapters: total,
		OutputFile:    outputFile,
	}
	}()

	return ch, nil
}

func (e *Engine) writeOutput(adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, content map[int]string) (string, error) {
	if err := os.MkdirAll(e.cfg.OutputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	// Safe filename
	fname := safeFilename(meta.Title)
	outPath := filepath.Join(e.cfg.OutputDir, fname)

	// Concatenate all page content and split by chapter markers
	var rawText strings.Builder
	for i := range len(chapters) {
		if c, ok := content[i]; ok {
			rawText.WriteString(c)
			rawText.WriteString("\n")
		}
	}
	ordered := converter.SplitChapters(rawText.String(), 1)
	if len(ordered) == 0 {
		return "", fmt.Errorf("no content to write")
	}

	// Fetch detailed book metadata if available
	description := meta.Description
	protagonist := meta.Protagonist
	tags := meta.Tags
	if provider, ok := adapter.(search.BookMetaProvider); ok {
		detail, err := provider.GetBookMeta(context.Background(), meta.BookURL)
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
		Title:       meta.Title,
		Author:      orDefault(meta.Author, "Unknown"),
		Description: description,
		Protagonist: protagonist,
		Tags:        tags,
		Language:    "zh-CN",
		Chapters:    ordered,
	}

	var outFile string
	switch e.cfg.Format {
	case "epub":
		outFile = outPath + ".epub"
		f, err := os.Create(outFile)
		if err != nil {
			return "", fmt.Errorf("create epub: %w", err)
		}
		defer f.Close()
		if err := converter.WriteEPUB(f, bookMeta); err != nil {
			return "", fmt.Errorf("write epub: %w", err)
		}
	default:
		outFile = outPath + ".txt"
		f, err := os.Create(outFile)
		if err != nil {
			return "", fmt.Errorf("create txt: %w", err)
		}
		defer f.Close()
		if err := converter.WriteTXT(f, ordered); err != nil {
			return "", fmt.Errorf("write txt: %w", err)
		}
	}

	log.Printf("Saved: %s (%d chapters)", outFile, len(ordered))
	return outFile, nil
}

// RetryFailed attempts to re-download previously failed chapters.
func (e *Engine) RetryFailed(ctx context.Context, failed []FailedChapter) {
	for _, fc := range failed {
		// Simple retry — fetch the chapter again
		// In a full implementation, this would use the adapter
		log.Printf("[retry] chapter %d: %s (%s)", fc.Index+1, fc.Title, fc.URL)
	}
}

// SaveFailedChapters writes failed chapter info to .failed.json in the output directory.
func SaveFailedChapters(outputDir, bookTitle string, failed []FailedChapter) error {
	if len(failed) == 0 {
		return nil
	}
	path := filepath.Join(outputDir, ".failed.json")
	data, err := json.MarshalIndent(failed, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func safeFilename(s string) string {
	// Replace problematic characters
	repl := map[rune]rune{
		'/': '_', '\\': '_', ':': '_', '*': '_',
		'?': '_', '"': '_', '<': '_', '>': '_', '|': '_',
	}
	return strings.Map(func(r rune) rune {
		if r, ok := repl[r]; ok {
			return r
		}
		return r
	}, s)
}


func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

