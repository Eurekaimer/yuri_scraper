package search

import "context"

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
