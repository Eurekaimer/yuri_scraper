package search

import (
	"context"
	"time"
)

// SearchResult represents a book found by a site adapter.
type SearchResult struct {
	Title        string   `json:"title"`
	Author       string   `json:"author"`
	CoverURL     string   `json:"cover_url,omitempty"`
	BookURL      string   `json:"book_url"`
	SiteName     string   `json:"site_name"`
	ChapterCount int      `json:"chapter_count,omitempty"`
	Description  string   `json:"description,omitempty"`
	Protagonist  string   `json:"protagonist,omitempty"`
	Status       string   `json:"status,omitempty"` // "连载中" or "已完结"
	Tags         []string `json:"tags,omitempty"`
	WordCount    int      `json:"word_count,omitempty"`
	LastUpdated  string   `json:"last_updated,omitempty"`
}

// Chapter represents a single chapter in a book.
type Chapter struct {
	Index    int    `json:"index"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name,omitempty"`
}

// SiteAdapter is the interface every site scraper must implement.
type SiteAdapter interface {
	// Name returns the human-readable site name.
	Name() string

	// BaseURL returns the site's base URL.
	BaseURL() string

	// Search searches the site for books matching the query.
	Search(ctx context.Context, query string) ([]SearchResult, error)

	// GetChapterList fetches the full chapter list for a book.
	GetChapterList(ctx context.Context, bookURL string) ([]Chapter, error)

	// GetChapterContent fetches and cleans the text content of a single chapter.
	GetChapterContent(ctx context.Context, chapterURL string) (string, error)
}

// CredentialsAuthenticator is implemented by sources that can establish an
// authenticated session from an account and password. Credentials are supplied
// for one Login call only; implementations must not persist them.
type CredentialsAuthenticator interface {
	RequiresAuthentication() bool
	IsAuthenticated() bool
	Login(context.Context, string, string) error
}

// ScrapePolicyProvider lets a sensitive source cap the global speed profiles.
type ScrapePolicyProvider interface {
	MaxConcurrency() int64
	MaxRequestsPerSecond() float64
}

// ChapterResolveMode selects how a forum-like source turns a thread into an
// ordered chapter plan. Public book sites can ignore this optional protocol.
type ChapterResolveMode string

const (
	ChapterModeAuto    ChapterResolveMode = "auto"
	ChapterModeCatalog ChapterResolveMode = "catalog"
	ChapterModeAuthor  ChapterResolveMode = "author"
)

// ChapterCandidate describes a possible catalog/elevator found in a thread.
// Preview is intentionally small so a TUI can present candidates without
// retaining chapter bodies.
type ChapterCandidate struct {
	Source   string
	Chapters []Chapter
	Score    int
	Preview  []string
}

// ChapterPlanOptions controls an optional, source-specific planning pass.
// CheckpointPath must never contain credentials. ContentSink lets a resolver
// place already-fetched post bodies directly in the scraper checkpoint cache.
type ChapterPlanOptions struct {
	Mode             ChapterResolveMode
	Author           string
	MaxScanPages     int
	MinPostChars     int
	CatalogCandidate int
	CheckpointPath   string
	ContentSink      func(Chapter, string) error
	OnProgress       func(ChapterPlanProgress)
}

type ChapterPlanProgress struct {
	Strategy       ChapterResolveMode
	ScannedPages   int
	FoundChapters  int
	CheckpointHits int
	StartedAt      time.Time
}

type ChapterPlan struct {
	Chapters       []Chapter
	Candidates     []ChapterCandidate
	Strategy       ChapterResolveMode
	Author         string
	TotalPages     int
	ScannedPages   int
	CheckpointHits int
}

// ChapterPlanner is an optional richer protocol for sources whose chapter
// structure must be resolved from catalogs or author posts.
type ChapterPlanner interface {
	PlanChapters(context.Context, string, ChapterPlanOptions) (ChapterPlan, error)
}

// BookMetaProvider is an optional interface for adapters that can fetch detailed book metadata.
// SiteAdapter implementations MAY also implement this for richer EPUB output.
type BookMetaProvider interface {
	// GetBookMeta fetches detailed metadata for a book (synopsis, protagonist, tags, etc).
	// Returns partial data on error — caller should merge with SearchResult fallbacks.
	GetBookMeta(ctx context.Context, bookURL string) (*BookMetaDetail, error)
}

// BookMetaDetail holds rich book metadata fetched from the site.
type BookMetaDetail struct {
	Description string   // synopsis / summary
	Protagonist string   // main character name(s)
	Tags        []string // genre tags from the site
	Status      string   // completion status
	WordCount   int      // total word count
	LastUpdated string   // last update date string
}
