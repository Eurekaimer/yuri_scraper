package scraper

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
)

type plannedAdapter struct{ contentRequests int }

func (a *plannedAdapter) Name() string    { return "planned" }
func (a *plannedAdapter) BaseURL() string { return "https://example.test" }
func (a *plannedAdapter) Search(context.Context, string) ([]search.SearchResult, error) {
	return nil, nil
}
func (a *plannedAdapter) GetChapterList(context.Context, string) ([]search.Chapter, error) {
	return nil, nil
}
func (a *plannedAdapter) GetChapterContent(context.Context, string) (string, error) {
	a.contentRequests++
	return "", nil
}
func (a *plannedAdapter) PlanChapters(_ context.Context, _ string, opts search.ChapterPlanOptions) (search.ChapterPlan, error) {
	chapter := search.Chapter{Index: 1, Title: "第一章", URL: "https://example.test/post?pid=1", SiteName: a.Name()}
	if err := opts.ContentSink(chapter, "第一章\n这是由规划器直接提供的正文内容，不应再次发起章节请求。"); err != nil {
		return search.ChapterPlan{}, err
	}
	return search.ChapterPlan{Chapters: []search.Chapter{chapter}, Strategy: search.ChapterModeAuthor}, nil
}

func TestPlannedContentPrimesChapterCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutputDir, cfg.Format = dir, "txt"
	engine := NewEngine(cfg)
	adapter := &plannedAdapter{}
	meta := search.SearchResult{Title: "checkpoint", BookURL: "https://example.test/thread?tid=1", SiteName: adapter.Name()}
	progress, err := engine.ScrapeBookWithOptions(context.Background(), adapter, meta.BookURL, meta, search.ChapterPlanOptions{Mode: search.ChapterModeAuthor})
	if err != nil {
		t.Fatal(err)
	}
	for range progress {
	}
	if adapter.contentRequests != 0 {
		t.Fatalf("content requests = %d, want 0", adapter.contentRequests)
	}
	data, err := os.ReadFile(filepath.Join(dir, "checkpoint.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "规划器直接提供") {
		t.Fatalf("unexpected output: %s", data)
	}
}

func TestSpeedHonorsSourceCaps(t *testing.T) {
	e := NewEngine(DefaultConfig())
	e.maxWorkers.Store(2)
	e.maxRateBits.Store(math.Float64bits(1.2))
	e.SetSpeed(SpeedFast)
	if got := e.concurrency.Load(); got != 2 {
		t.Fatalf("concurrency = %d, want capped value 2", got)
	}
	if got := float64(e.limiter.Limit()); got != 1.2 {
		t.Fatalf("rate = %v, want capped value 1.2", got)
	}
}

func TestRetryDelayUsesBackoff(t *testing.T) {
	first := retryDelay(time.Second, 1, 0)
	second := retryDelay(time.Second, 2, 0)
	if second <= first {
		t.Fatalf("second retry delay %v must exceed first %v", second, first)
	}
}

func TestSaveFailedChaptersUsesBookSpecificFile(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveFailedChapters(dir, "A/B", []FailedChapter{{Index: 1, Title: "two"}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "A_B.failed.json" {
		t.Fatalf("unexpected manifest path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "two") {
		t.Fatalf("manifest missing chapter: %s", data)
	}
}

func TestSpeedProfile_Concurrency(t *testing.T) {
	tests := []struct {
		profile SpeedProfile
		want    int64
	}{
		{SpeedSlow, 2},
		{SpeedNormal, 8},
		{SpeedFast, 16},
		{SpeedProfile(99), 8}, // unknown defaults to normal
	}
	for _, tt := range tests {
		got := tt.profile.concurrency()
		if got != tt.want {
			t.Errorf("%v concurrency() = %d, want %d", tt.profile, got, tt.want)
		}
	}
}

func TestSpeedProfile_RateLimit(t *testing.T) {
	tests := []struct {
		profile SpeedProfile
		want    float64
	}{
		{SpeedSlow, 0.5},
		{SpeedNormal, 2.0},
		{SpeedFast, 5.0},
		{SpeedProfile(99), 2.0},
	}
	for _, tt := range tests {
		got := tt.profile.rateLimit()
		if got != tt.want {
			t.Errorf("%v rateLimit() = %f, want %f", tt.profile, got, tt.want)
		}
	}
}

func TestSpeedProfile_String(t *testing.T) {
	tests := []struct {
		profile SpeedProfile
		want    string
	}{
		{SpeedSlow, "慢速"},
		{SpeedNormal, "正常"},
		{SpeedFast, "快速"},
	}
	for _, tt := range tests {
		got := tt.profile.String()
		if got != tt.want {
			t.Errorf("%d String() = %q, want %q", tt.profile, got, tt.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", cfg.Concurrency)
	}
	if cfg.RateLimit != 2.0 {
		t.Errorf("RateLimit = %f, want 2.0", cfg.RateLimit)
	}
	if cfg.OutputDir != "novels" {
		t.Errorf("OutputDir = %q, want novels", cfg.OutputDir)
	}
	if cfg.Format != "epub" {
		t.Errorf("Format = %q, want epub", cfg.Format)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3", cfg.RetryCount)
	}
	if cfg.RetryDelay != 2*time.Second {
		t.Errorf("RetryDelay = %v, want 2s", cfg.RetryDelay)
	}
	if cfg.SpeedProfile != SpeedNormal {
		t.Errorf("SpeedProfile = %v, want SpeedNormal", cfg.SpeedProfile)
	}
}

func TestConfig_ProfileFallback(t *testing.T) {
	// When Concurrency/RateLimit are 0, the profile values should be used.
	// This is tested indirectly via ScrapeBook, but we verify the logic.
	cfg := Config{
		Concurrency:  0,
		RateLimit:    0,
		SpeedProfile: SpeedFast,
		OutputDir:    "test",
		Format:       "txt",
		RetryCount:   1,
		RetryDelay:   time.Second,
	}
	if cfg.SpeedProfile.concurrency() != 16 {
		t.Errorf("SpeedFast concurrency = %d, want 16", cfg.SpeedProfile.concurrency())
	}
	if cfg.SpeedProfile.rateLimit() != 5.0 {
		t.Errorf("SpeedFast rateLimit = %f, want 5.0", cfg.SpeedProfile.rateLimit())
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"normal", "normal"},
		{"file:name", "file_name"},
		{"a/b\\c", "a_b_c"},
		{"test<>.txt", "test__.txt"},
		{"你好世界", "你好世界"},
		{"a?b*c\"d|e", "a_b_c_d_e"},
	}
	for _, tt := range tests {
		got := safeFilename(tt.in)
		if got != tt.out {
			t.Errorf("safeFilename(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestOrDefault(t *testing.T) {
	if got := orDefault("hello", "default"); got != "hello" {
		t.Errorf("orDefault(nonempty) = %q, want %q", got, "hello")
	}
	if got := orDefault("", "default"); got != "default" {
		t.Errorf("orDefault(empty) = %q, want %q", got, "default")
	}
}

func TestNewEngine(t *testing.T) {
	cfg := DefaultConfig()
	e := NewEngine(cfg)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.cfg.Concurrency != cfg.Concurrency {
		t.Error("engine config mismatch")
	}
}
