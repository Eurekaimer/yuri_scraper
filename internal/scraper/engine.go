package scraper

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"golang.org/x/time/rate"
)

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
	Concurrency              int64
	RateLimit                float64
	OutputDir, Format        string
	RetryCount               int
	RetryDelay               time.Duration
	SpeedProfile             SpeedProfile
	StartChapter, EndChapter int
}

func (c *Config) ScrapeRange(start, end int) {
	if start > 0 {
		c.StartChapter = start - 1
	}
	c.EndChapter = end
}
func DefaultConfig() Config {
	return Config{Concurrency: 8, RateLimit: 2, OutputDir: "novels", Format: "epub", RetryCount: 3, RetryDelay: 2 * time.Second, SpeedProfile: SpeedNormal}
}

type FailedChapter struct {
	Index int    `json:"index"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Error string `json:"error"`
}
type Progress struct {
	ChapterIndex             int
	ChapterTitle, ChapterURL string
	TotalChapters            int
	Done, Cached             bool
	Error                    error
	OutputFile, FailedFile   string
}

// Engine owns scrape policy and orchestration. Worker execution and output
// persistence live in worker.go and output.go respectively.
type Engine struct {
	cfg                            Config
	limiter                        *rate.Limiter
	concurrency, speed, maxWorkers atomic.Int64
	maxRateBits                    atomic.Uint64
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

func (e *Engine) SetSpeed(sp SpeedProfile) { e.speed.Store(int64(sp)); e.applySpeed(sp) }
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
	e.limiter.SetBurst(1)
}

func (e *Engine) ScrapeBook(ctx context.Context, adapter search.SiteAdapter, bookURL string, meta search.SearchResult) (<-chan Progress, error) {
	return e.ScrapeBookWithOptions(ctx, adapter, bookURL, meta, search.ChapterPlanOptions{})
}
func (e *Engine) ScrapeBookWithOptions(ctx context.Context, adapter search.SiteAdapter, bookURL string, meta search.SearchResult, options search.ChapterPlanOptions) (<-chan Progress, error) {
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
	cacheDir := filepath.Join(e.cfg.OutputDir, ".cache", shortHash(bookCacheKey(adapter.Name(), bookURL)))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create checkpoint directory: %w", err)
	}
	chapters, err := e.resolveChapters(ctx, adapter, bookURL, cacheDir, options)
	if err != nil {
		return nil, fmt.Errorf("get chapter list: %w", err)
	}
	chapters = e.applyRange(chapters)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in range")
	}
	progress := make(chan Progress, min(len(chapters)+2, 256))
	go e.run(ctx, adapter, meta, chapters, cacheDir, progress)
	return progress, nil
}

func (e *Engine) resolveChapters(ctx context.Context, adapter search.SiteAdapter, bookURL, cacheDir string, options search.ChapterPlanOptions) ([]search.Chapter, error) {
	planner, ok := adapter.(search.ChapterPlanner)
	if !ok {
		return adapter.GetChapterList(ctx, bookURL)
	}
	options.CheckpointPath = filepath.Join(cacheDir, "chapter-plan.json")
	upstreamSink := options.ContentSink
	options.ContentSink = func(chapter search.Chapter, content string) error {
		if upstreamSink != nil {
			if err := upstreamSink(chapter, content); err != nil {
				return err
			}
		}
		return writeChapterCheckpoint(cacheDir, chapter.URL, content)
	}
	plan, err := planner.PlanChapters(ctx, bookURL, options)
	if err != nil {
		return nil, err
	}
	return plan.Chapters, nil
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
