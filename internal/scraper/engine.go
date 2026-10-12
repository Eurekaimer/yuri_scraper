package scraper

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
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
		return "Slow"
	case SpeedFast:
		return "Fast"
	default:
		return "Normal"
	}
}

// SpeedProfiles lists the presets from slowest to fastest so interfaces can
// move between them without hard-coding the order.
func SpeedProfiles() []SpeedProfile { return []SpeedProfile{SpeedSlow, SpeedNormal, SpeedFast} }

// Slower returns the next lower preset, reporting false at the slowest one.
func (sp SpeedProfile) Slower() (SpeedProfile, bool) {
	if sp <= SpeedSlow {
		return SpeedSlow, false
	}
	return sp - 1, true
}

// Faster returns the next higher preset, reporting false at the fastest one.
func (sp SpeedProfile) Faster() (SpeedProfile, bool) {
	if sp >= SpeedFast {
		return SpeedFast, false
	}
	return sp + 1, true
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

// Progress is a latest-state event. Intermediate events may be coalesced when a
// consumer is slow; cumulative counts and the last terminal event are authoritative.
type Progress struct {
	ChapterIndex                   int
	ChapterTitle, ChapterURL       string
	TotalChapters                  int
	Completed, Failed, CachedCount int
	Phase                          string
	Done, Cached                   bool
	Error                          error
	OutputFile, FailedFile         string
}

var ErrEngineBusy = errors.New("scraper engine already has an active book")

// Engine owns one active scrape, including planning and export. Chapter bodies
// are checkpointed on disk rather than retained in the worker result queue.
type Engine struct {
	cfg                          Config
	limiter                      *rate.Limiter
	running                      atomic.Bool
	mu                           sync.Mutex
	requestedWorkers, maxWorkers int64
	requestedRate, maxRate       float64
	profile                      SpeedProfile
	concurrency                  int64
	activeWorkers                int64
	changed                      chan struct{}
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
	if rps <= 0 || math.IsNaN(rps) || math.IsInf(rps, 0) {
		rps = cfg.SpeedProfile.rateLimit()
	}
	e := &Engine{cfg: cfg, limiter: rate.NewLimiter(rate.Limit(rps), 1), requestedWorkers: workers, requestedRate: rps, profile: cfg.SpeedProfile, maxWorkers: 64, maxRate: math.MaxFloat64, changed: make(chan struct{})}
	e.concurrency = min(workers, e.maxWorkers)
	return e
}

// SetSpeed explicitly replaces configured startup policy; source limits still win.
func (e *Engine) SetSpeed(sp SpeedProfile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sp < SpeedSlow || sp > SpeedFast {
		sp = SpeedNormal
	}
	e.profile = sp
	e.requestedWorkers, e.requestedRate = sp.concurrency(), sp.rateLimit()
	e.applyPolicyLocked()
}

// Speed reports the preset currently in effect, which is what an interface
// should display and step from.
func (e *Engine) Speed() SpeedProfile {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.profile
}

// Budget reports the worker count and request rate currently in effect after
// configured values, the selected speed profile, and the source policy are
// reconciled. Interfaces show these numbers instead of naming a profile that a
// source cap may have reduced.
func (e *Engine) Budget() (workers int64, requestsPerSecond float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return min(e.concurrency, e.maxWorkers), min(e.requestedRate, e.maxRate)
}

func (e *Engine) applyPolicyLocked() {
	e.concurrency = min(e.requestedWorkers, e.maxWorkers)
	e.limiter.SetLimit(rate.Limit(min(e.requestedRate, e.maxRate)))
	close(e.changed)
	e.changed = make(chan struct{})
}

func (e *Engine) acquireWorker(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.mu.Lock()
		if e.activeWorkers < e.concurrency {
			e.activeWorkers++
			e.mu.Unlock()
			return nil
		}
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (e *Engine) releaseWorker() {
	e.mu.Lock()
	e.activeWorkers--
	close(e.changed)
	e.changed = make(chan struct{})
	e.mu.Unlock()
}

func (e *Engine) ScrapeBook(ctx context.Context, adapter search.SiteAdapter, bookURL string, meta search.SearchResult) (<-chan Progress, error) {
	return e.ScrapeBookWithOptions(ctx, adapter, bookURL, meta, search.ChapterPlanOptions{})
}

func (e *Engine) ScrapeBookWithOptions(ctx context.Context, adapter search.SiteAdapter, bookURL string, meta search.SearchResult, options search.ChapterPlanOptions) (<-chan Progress, error) {
	if !e.running.CompareAndSwap(false, true) {
		return nil, ErrEngineBusy
	}
	started := false
	defer func() {
		if !started {
			e.running.Store(false)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if adapter == nil {
		return nil, fmt.Errorf("source adapter is required")
	}
	if e.cfg.Format != "epub" && e.cfg.Format != "txt" {
		return nil, fmt.Errorf("unsupported output format %q", e.cfg.Format)
	}
	e.mu.Lock()
	e.maxWorkers, e.maxRate = 64, math.MaxFloat64
	if policy, ok := adapter.(search.ScrapePolicyProvider); ok {
		if maximum := policy.MaxConcurrency(); maximum > 0 {
			e.maxWorkers = min(maximum, e.maxWorkers)
		}
		if maximum := policy.MaxRequestsPerSecond(); maximum > 0 && !math.IsNaN(maximum) {
			e.maxRate = min(maximum, e.maxRate)
		}
	}
	e.applyPolicyLocked()
	workerCount := min(e.maxWorkers, max(e.requestedWorkers, SpeedFast.concurrency()))
	e.mu.Unlock()
	meta.BookURL, meta.SiteName = bookURL, adapter.Name()
	cacheRoot := filepath.Join(e.cfg.OutputDir, ".cache")
	cacheDir := filepath.Join(cacheRoot, shortHash(bookCacheKey(adapter.Name(), bookURL)))
	if err := os.MkdirAll(e.cfg.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	for _, dir := range []string{cacheRoot, cacheDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create checkpoint directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("secure checkpoint directory: %w", err)
		}
	}
	chapters, err := e.resolveChapters(ctx, adapter, bookURL, cacheDir, options)
	if err != nil {
		return nil, fmt.Errorf("get chapter list: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	chapters = e.applyRange(chapters)
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters in range")
	}
	progress := make(chan Progress, 1)
	started = true
	go e.run(ctx, adapter, meta, chapters, cacheDir, int(min(workerCount, int64(len(chapters)))), progress)
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
		if err := ctx.Err(); err != nil {
			return err
		}
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
	start, end := max(e.cfg.StartChapter, 0), e.cfg.EndChapter
	if end <= 0 || end > len(chapters) {
		end = len(chapters)
	}
	if start >= end {
		return nil
	}
	return chapters[start:end]
}
