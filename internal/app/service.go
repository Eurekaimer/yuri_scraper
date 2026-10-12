// Package app owns download lifecycle and persistence independently of the UI.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/library"
	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/search/sites"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
)

var ErrBusy = errors.New("a book is already active; cancel or wait before starting another")

type Snapshot struct {
	ID          string  `json:"id"`
	State       string  `json:"state"`
	Title       string  `json:"title"`
	Current     string  `json:"current,omitempty"`
	OutputFile  string  `json:"output_file,omitempty"`
	FailedFile  string  `json:"failed_file,omitempty"`
	Error       string  `json:"error,omitempty"`
	Total       int     `json:"total"`
	Completed   int     `json:"completed"`
	Failed      int     `json:"failed"`
	Cached      int     `json:"cached"`
	Workers     int64   `json:"workers,omitempty"`
	RequestsPer float64 `json:"requests_per_second,omitempty"`
	Speed       string  `json:"speed,omitempty"`
	Revision    uint64  `json:"revision"`
}

func (p Snapshot) Terminal() bool {
	switch p.State {
	case "completed", "partial", "failed", "canceled":
		return true
	}
	return false
}

// Config and Search are configured before use and must not be mutated by clients.
// Each job gets a fresh Engine. Only this service owns completion persistence.
type Service struct {
	Search   *search.Engine
	Config   store.Config
	mu       sync.Mutex
	cancel   context.CancelFunc
	engine   *scraper.Engine
	progress Snapshot
	sequence uint64
	done     chan struct{}
}

func New(cfg store.Config) (*Service, error) { return NewWithBaseURL(cfg, "", "") }

func NewWithBaseURL(cfg store.Config, source, baseURL string) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(cfg.OutputDir)
	if err != nil {
		return nil, err
	}
	cfg.OutputDir = root
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	if _, err := store.LoadHistory(root); err != nil {
		return nil, err
	}
	registry := search.NewRegistry()
	public := sites.New52ShukuWithPolicy(int64(cfg.Concurrency), cfg.RateLimit)
	if baseURL != "" {
		if source != sites.Source52Shuku {
			return nil, fmt.Errorf("fixture base URL requires the 52shuku source")
		}
		if err := public.SetBaseURL(baseURL); err != nil {
			return nil, err
		}
	}
	registry.Register(public)
	registry.Register(sites.NewYamiboWithPolicy(int64(cfg.Yamibo.MaxConcurrency), cfg.Yamibo.RateLimit))
	return &Service{Search: search.NewEngine(registry), Config: cfg}, nil
}

func (s *Service) ResolveBook(ctx context.Context, source, rawURL, title string) (search.SearchResult, error) {
	adapter := s.Search.Registry().Get(source)
	if adapter == nil {
		return search.SearchResult{}, fmt.Errorf("unknown source %q", source)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return search.SearchResult{}, err
	}
	base, err := url.Parse(adapter.BaseURL())
	if err != nil {
		return search.SearchResult{}, err
	}
	if parsed.Scheme != base.Scheme || !strings.EqualFold(parsed.Host, base.Host) || parsed.User != nil {
		return search.SearchResult{}, fmt.Errorf("book URL must belong to %s", adapter.BaseURL())
	}
	parsed.Fragment = ""
	rawURL = parsed.String()
	meta := search.SearchResult{Title: strings.TrimSpace(title), SiteName: source, BookURL: rawURL}
	if resolver, ok := adapter.(interface {
		ResolveBook(context.Context, string) (*search.SearchResult, error)
	}); ok {
		resolved, err := resolver.ResolveBook(ctx, rawURL)
		if err != nil {
			return meta, err
		}
		if resolved != nil {
			meta = *resolved
			meta.SiteName = source
			meta.BookURL = rawURL
		}
		if strings.TrimSpace(title) != "" {
			meta.Title = strings.TrimSpace(title)
		}
	}
	if meta.Title == "" {
		return meta, fmt.Errorf("source cannot resolve the title; specify --title")
	}
	return meta, nil
}

func (s *Service) Snapshot() Snapshot { s.mu.Lock(); defer s.mu.Unlock(); return s.progress }
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// SetSpeed selects a preset and publishes the resulting effective budget.
func (s *Service) SetSpeed(speed scraper.SpeedProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return
	}
	s.engine.SetSpeed(speed)
	s.progress.Workers, s.progress.RequestsPer = s.engine.Budget()
	s.progress.Speed = s.engine.Speed().String()
}

// StepSpeed moves one preset slower or faster, reporting whether it moved and
// returning the resulting profile so interfaces can render it immediately.
func (s *Service) StepSpeed(faster bool) (scraper.SpeedProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return scraper.SpeedNormal, false
	}
	current := s.engine.Speed()
	var next scraper.SpeedProfile
	var moved bool
	if faster {
		next, moved = current.Faster()
	} else {
		next, moved = current.Slower()
	}
	if moved {
		s.engine.SetSpeed(next)
	}
	s.progress.Workers, s.progress.RequestsPer = s.engine.Budget()
	s.progress.Speed = s.engine.Speed().String()
	return s.engine.Speed(), moved
}

// Wait lets an interface restore the terminal only after the job releases files.
func (s *Service) Wait(ctx context.Context) error {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Start(ctx context.Context, book search.SearchResult, options search.ChapterPlanOptions) (<-chan Snapshot, error) {
	return s.start(ctx, book, options, s.Config.Format)
}

func (s *Service) start(ctx context.Context, book search.SearchResult, options search.ChapterPlanOptions, format string) (<-chan Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	adapter := s.Search.Registry().Get(book.SiteName)
	if adapter == nil || book.BookURL == "" || strings.TrimSpace(book.Title) == "" {
		return nil, fmt.Errorf("a title, book URL and registered source are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, ErrBusy
	}
	lock, err := store.AcquireLibraryLock(s.Config.OutputDir)
	if err != nil {
		return nil, err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	s.sequence++
	id := fmt.Sprintf("%d-%d", os.Getpid(), s.sequence)
	s.progress = Snapshot{ID: id, State: "planning", Title: book.Title, Revision: 1}
	s.cancel = cancel
	s.done = make(chan struct{})
	s.engine = scraper.NewEngine(scraper.Config{Concurrency: int64(s.Config.Concurrency), RateLimit: s.Config.RateLimit, OutputDir: s.Config.OutputDir, Format: format, RetryCount: s.Config.RetryCount, RetryDelay: 2 * time.Second, SpeedProfile: scraper.SpeedNormal})
	s.progress.Workers, s.progress.RequestsPer = s.engine.Budget()
	stream := make(chan Snapshot, 1)
	stream <- s.progress
	slog.Info("task.started", "task_id", id, "source", book.SiteName, "format", format)
	go s.run(jobCtx, cancel, lock, adapter, book, options, format, s.engine, stream, s.done)
	return stream, nil
}

// publish replaces stale progress without making a UI consumer a job dependency.
func publish(stream chan Snapshot, snapshot Snapshot) {
	select {
	case stream <- snapshot:
		return
	default:
	}
	select {
	case <-stream:
	default:
	}
	stream <- snapshot
}

func (s *Service) update(p scraper.Progress) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress.Revision++
	if p.Phase != "" {
		s.progress.State = p.Phase
	} else {
		s.progress.State = "downloading"
	}
	s.progress.Total = p.TotalChapters
	s.progress.Completed = p.Completed
	s.progress.Failed = p.Failed
	s.progress.Cached = p.CachedCount
	if p.ChapterTitle != "" {
		s.progress.Current = p.ChapterTitle
	}
	if p.OutputFile != "" {
		s.progress.OutputFile = p.OutputFile
	}
	if p.FailedFile != "" {
		s.progress.FailedFile = p.FailedFile
	}
	if p.Error != nil && p.ChapterIndex < 0 {
		s.progress.Error = p.Error.Error()
	}
	// Source caps are applied when the scrape starts, so report the effective
	// budget on every snapshot instead of only the requested one.
	if s.engine != nil {
		s.progress.Workers, s.progress.RequestsPer = s.engine.Budget()
	}
	result := s.progress
	if result.Terminal() {
		s.progress.State = "exporting"
	}
	return result
}

func (s *Service) run(ctx context.Context, cancel context.CancelFunc, lock *os.File, adapter search.SiteAdapter, book search.SearchResult, options search.ChapterPlanOptions, format string, engine *scraper.Engine, stream chan Snapshot, done chan struct{}) {
	defer close(stream)
	defer close(done)
	defer cancel()
	snapshot := s.Snapshot()
	updates, err := engine.ScrapeBookWithOptions(ctx, adapter, book.BookURL, book, options)
	if err == nil {
		for p := range updates {
			snapshot = s.update(p)
			if !snapshot.Terminal() {
				publish(stream, snapshot)
			}
		}
	} else {
		snapshot.Error = err.Error()
		snapshot.State = "failed"
	}
	if ctx.Err() != nil && snapshot.OutputFile == "" {
		snapshot.State = "canceled"
		snapshot.Error = ctx.Err().Error()
	}
	if !snapshot.Terminal() {
		snapshot.State = "failed"
		snapshot.Error = "download ended without a final result"
	}
	if snapshot.OutputFile != "" {
		history, loadErr := store.LoadHistory(s.Config.OutputDir)
		if loadErr == nil {
			loadErr = history.Add(store.HistoryEntry{Title: book.Title, Author: book.Author, SiteName: book.SiteName, BookURL: book.BookURL, Format: format, FilePath: snapshot.OutputFile, Chapters: snapshot.Completed, State: snapshot.State, FailedFile: snapshot.FailedFile})
		}
		if loadErr != nil {
			snapshot.State = "failed"
			snapshot.Error = fmt.Sprintf("output saved but history could not be committed: %v", loadErr)
		}
	}
	s.mu.Lock()
	if closeErr := lock.Close(); closeErr != nil {
		snapshot.State = "failed"
		snapshot.Error = fmt.Sprintf("release library lock: %v", closeErr)
	}
	snapshot.Revision = s.progress.Revision + 1
	s.progress = snapshot
	s.cancel = nil
	s.engine = nil
	s.mu.Unlock()
	slog.Info("task.finished", "task_id", snapshot.ID, "state", snapshot.State, "completed", snapshot.Completed, "failed", snapshot.Failed, "cached", snapshot.Cached, "output_saved", snapshot.OutputFile != "", "has_error", snapshot.Error != "")
	publish(stream, snapshot)
}

func (s *Service) Library() ([]library.Entry, error) {
	history, err := store.LoadHistory(s.Config.OutputDir)
	if err != nil {
		return nil, err
	}
	return library.Scan(s.Config.OutputDir, history.List())
}

func (s *Service) Retry(ctx context.Context, id string) (<-chan Snapshot, error) {
	entries, err := s.Library()
	if err != nil {
		return nil, err
	}
	entry, err := library.Find(entries, id)
	if err != nil {
		return nil, err
	}
	if entry.BookURL == "" || entry.SiteName == "" {
		return nil, fmt.Errorf("this file has no source metadata to retry")
	}
	return s.start(ctx, search.SearchResult{Title: entry.Title, Author: entry.Author, BookURL: entry.BookURL, SiteName: entry.SiteName}, search.ChapterPlanOptions{MaxScanPages: s.Config.Yamibo.MaxScanPages, MinPostChars: s.Config.Yamibo.MinPostChars}, entry.Format)
}

func (s *Service) mutate(id string, removeFile bool) error {
	lock, err := store.AcquireLibraryLock(s.Config.OutputDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	entries, err := s.Library()
	if err != nil {
		return err
	}
	entry, err := library.Find(entries, id)
	if err != nil {
		return err
	}
	if removeFile {
		if err := library.Remove(s.Config.OutputDir, entry); err != nil {
			return err
		}
	}
	history, err := store.LoadHistory(s.Config.OutputDir)
	if err != nil {
		return err
	}
	if err := history.RemovePath(entry.FilePath); err != nil {
		return fmt.Errorf("metadata could not be committed; reader files remain recoverable in .trash: %w", err)
	}
	return nil
}
func (s *Service) Remove(id string) error       { return s.mutate(id, true) }
func (s *Service) RemoveRecord(id string) error { return s.mutate(id, false) }
