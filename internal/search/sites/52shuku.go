package sites

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/diagnostics"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/transport"
	"github.com/PuerkitoBio/goquery"
)

const (
	// Source52Shuku and SourceYamibo are the canonical source names registered in
	// the registry. Callers resolve user aliases to these values instead of
	// repeating display strings.
	Source52Shuku = "52书库"
	SourceYamibo  = "百合会"
)

const (
	// Absolute sanity ceilings for the request budget. 52shuku is a public book
	// site with no recorded source-specific hard limit, so the configured
	// concurrency and rate_limit of the application stay authoritative up to
	// these bounds instead of being silently replaced by a fixed profile.
	shukuMaxConcurrency int64 = 64
	shukuMaxRPS               = 20.0
	shukuMaxBodyBytes   int64 = 8 << 20

	// Defaults used by New52Shuku, matching store.DefaultConfig.
	shukuDefaultConcurrency int64 = 8
	shukuDefaultRPS               = 2.0
)

// Site52Shuku budgets every request, including search, metadata and redirects.
type Site52Shuku struct {
	mu             sync.RWMutex
	baseURL        string
	client         *http.Client
	maxConcurrency int64
	maxRPS         float64
}

func New52Shuku() *Site52Shuku {
	return New52ShukuWithPolicy(shukuDefaultConcurrency, shukuDefaultRPS)
}

// New52ShukuWithPolicy honors the requested request budget and clamps it only to
// the absolute sanity ceiling. Lower configured values are preserved so a
// conservative configuration is never silently raised.
func New52ShukuWithPolicy(maxConcurrency int64, maxRPS float64) *Site52Shuku {
	if maxConcurrency <= 0 {
		maxConcurrency = shukuDefaultConcurrency
	}
	if !(maxRPS > 0) || math.IsNaN(maxRPS) || math.IsInf(maxRPS, 0) {
		maxRPS = shukuDefaultRPS
	}
	maxConcurrency = min(maxConcurrency, shukuMaxConcurrency)
	maxRPS = min(maxRPS, shukuMaxRPS)
	s := &Site52Shuku{baseURL: "https://www.52shuku.net", maxConcurrency: maxConcurrency, maxRPS: maxRPS}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.MaxConnsPerHost = int(maxConcurrency)
	base.MaxIdleConnsPerHost = int(maxConcurrency)
	base.ResponseHeaderTimeout = 15 * time.Second
	base.MaxResponseHeaderBytes = 64 << 10
	s.client = &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport.NewBoundedTransport(base, maxConcurrency, maxRPS, shukuMaxBodyBytes, s.validateURL),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return &transport.PermanentError{Err: fmt.Errorf("too many redirects")}
			}
			return s.validateURL(req.URL)
		},
	}
	return s
}

func (s *Site52Shuku) Name() string { return Source52Shuku }
func (s *Site52Shuku) BaseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.baseURL
}
func (s *Site52Shuku) MaxConcurrency() int64         { return s.maxConcurrency }
func (s *Site52Shuku) MaxRequestsPerSecond() float64 { return s.maxRPS }

// SetBaseURL allows only local HTTP fixtures; it never changes request budgets.
func (s *Site52Shuku) SetBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return &transport.PermanentError{Err: fmt.Errorf("invalid fixture URL: %w", err)}
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if parsed.Scheme != "http" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return &transport.PermanentError{Err: fmt.Errorf("base URL override must be an HTTP loopback origin")}
	}
	s.mu.Lock()
	s.baseURL = strings.TrimSuffix(parsed.String(), "/")
	s.mu.Unlock()
	return nil
}

func (s *Site52Shuku) validateURL(target *url.URL) error {
	base, _ := url.Parse(s.BaseURL())
	allowed := target.User == nil
	if base.Scheme == "http" {
		allowed = allowed && target.Scheme == base.Scheme && target.Host == base.Host
	} else {
		host := strings.ToLower(target.Hostname())
		allowed = allowed && target.Scheme == "https" && (host == "www.52shuku.net" || host == "52shuku.net") && (target.Port() == "" || target.Port() == "443")
	}
	if !allowed {
		return &transport.PermanentError{Err: fmt.Errorf("request URL is outside the selected 52shuku origin")}
	}
	return nil
}

func (s *Site52Shuku) doGet(ctx context.Context, rawURL string) (document *goquery.Document, resultErr error) {
	started := time.Now()
	status := 0
	// Never persist URL queries, credentials, headers or response bodies.
	endpoint := ""
	if target, err := url.Parse(rawURL); err == nil {
		endpoint = target.Host + target.EscapedPath()
	}
	defer func() {
		if resultErr != nil {
			slog.Error("52shuku.request.failed", "endpoint", endpoint, "status", status, "elapsed_ms", time.Since(started).Milliseconds(), diagnostics.Failure(resultErr))
		} else {
			slog.Debug("52shuku.request.completed", "endpoint", endpoint, "status", status, "elapsed_ms", time.Since(started).Milliseconds())
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &transport.PermanentError{Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("52shuku request failed: %w", err)
	}
	defer resp.Body.Close()
	status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return nil, &transport.HTTPError{URL: rawURL, StatusCode: resp.StatusCode, Delay: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		if transport.CanRetry(err) {
			return nil, fmt.Errorf("read HTML page: %w", err)
		}
		return nil, &transport.PermanentError{Err: fmt.Errorf("parse HTML page: %w", err)}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return doc, nil
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(raw); err == nil {
		return max(date.Sub(now), 0)
	}
	return 0
}

func (s *Site52Shuku) Search(ctx context.Context, query string) ([]search.SearchResult, error) {
	// 52shuku search: /so/search.php?q=<query>
	searchURL := fmt.Sprintf("%s/so/search.php?q=%s", s.BaseURL(), url.QueryEscape(query))

	doc, err := s.doGet(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	var results []search.SearchResult

	// Parse <article class="excerpt"> blocks
	doc.Find("article.excerpt").Each(func(_ int, sel *goquery.Selection) {
		// Title link: <a href="..."><h4>N. 书名_作者【状态】</h4></a>. The site
		// wraps the matched query text in <em>, so the heading is treated as
		// plain text and split exactly like the book page: "title_author【status】".
		link := sel.Find("header h2 a").First()
		href, _ := link.Attr("href")

		fullTitle := strings.TrimSpace(link.Find("h4").Text())
		fullTitle = rx(`^\d+\.\s*`).ReplaceAllString(fullTitle, "")
		title, author, status := parseBookTitle(fullTitle)

		// Category from <span class="muted">
		category := ""
		sel.Find("span.muted a").Each(func(_ int, aSel *goquery.Selection) {
			cat := strings.TrimSpace(aSel.Text())
			if cat != "" && category == "" {
				category = cat
			}
		})

		// The category/heat line is nested inside the note, so drop it before
		// reading the synopsis; otherwise "所属栏目/获赞" leaks into descriptions.
		note := sel.Find("span.note")
		note.Find("p.auth-span, span.muted").Remove()
		noteText := strings.TrimSpace(note.Text())
		desc := rx(`\[.*?\]`).ReplaceAllString(noteText, "")
		desc = rx(`《.*?》作者：[^【]*【.*?】`).ReplaceAllString(desc, "")
		desc = strings.TrimSpace(strings.TrimPrefix(desc, "简介："))
		desc = strings.TrimSpace(strings.TrimPrefix(desc, "文案："))
		desc = strings.Trim(desc, "\u3000 \t\n\r")

		if title != "" && href != "" && !strings.HasPrefix(href, "#") {
			resolved, err := s.resolveLink(searchURL, href)
			if err != nil {
				return
			}
			result := search.SearchResult{
				Title:       title,
				Author:      author,
				BookURL:     resolved,
				SiteName:    s.Name(),
				Description: truncate(desc, 200),
				Status:      status,
			}
			if category != "" {
				result.Tags = []string{category}
			}
			results = append(results, result)
		}
	})

	return results, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func (s *Site52Shuku) resolveLink(baseURL, href string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", &transport.PermanentError{Err: err}
	}
	link, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", &transport.PermanentError{Err: err}
	}
	resolved := base.ResolveReference(link)
	resolved.Fragment = ""
	if err := s.validateURL(resolved); err != nil {
		return "", err
	}
	return resolved.String(), nil
}

func (s *Site52Shuku) GetChapterList(ctx context.Context, bookURL string) ([]search.Chapter, error) {
	doc, err := s.doGet(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("fetch chapter list: %w", err)
	}
	var chapters []search.Chapter
	seen := make(map[string]bool)
	var linkErr error
	doc.Find("li.mulu a[href]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		href, _ := selection.Attr("href")
		link, err := s.resolveLink(bookURL, href)
		if err != nil {
			linkErr = err
			return false
		}
		if seen[link] {
			return true
		}
		seen[link] = true
		chapters = append(chapters, search.Chapter{Index: len(chapters) + 1, Title: pageTitle(strings.TrimSpace(selection.Text()), len(chapters)+1), URL: link, SiteName: s.Name()})
		return true
	})
	if linkErr != nil {
		return nil, linkErr
	}
	if len(chapters) == 0 {
		return nil, &transport.PermanentError{Err: fmt.Errorf("no chapter pages found on %s", bookURL)}
	}
	return chapters, nil
}

// pageTitle labels a catalog entry. 52shuku names its catalog entries "第N页",
// which is a reading page rather than a story chapter, so an empty or generic
// label becomes "Page N" and keeps the output language consistent.
func pageTitle(label string, position int) string {
	if label == "" {
		return fmt.Sprintf("Page %d", position)
	}
	if strings.HasPrefix(label, "第") && strings.HasSuffix(label, "页") {
		return fmt.Sprintf("Page %d", position)
	}
	return label
}

var (
	reScript  = regexp.MustCompile(`<script[\s\S]*?</script>`)
	reHTMLTag = regexp.MustCompile(`<[^>]+>`)
	reBr      = regexp.MustCompile(`(?i)</(?:p|div|h[1-6])>|<br\s*/?>`)
	// The synopsis block on the first reading page begins with a bracketed
	// category line and ends immediately before the first chapter heading. Go's
	// \s does not include the ideographic space used in these pages, so it is
	// listed explicitly; the captured heading is kept by the replacement.
	reSynopsisHead = regexp.MustCompile(`(?s)^[\s　]*\[[^\]]*\][\s　]*《.*?(\n[\s　]*第[0-9零一二三四五六七八九十百千]+章)`)
)

func (s *Site52Shuku) GetChapterContent(ctx context.Context, pageURL string) (string, error) {
	doc, err := s.doGet(ctx, pageURL)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}

	// Live reading pages use <article class="article-content">; older pages and
	// fixtures may also carry id="nr1". Matching the class alone covers both.
	article := doc.Find("article.article-content").First()
	if article.Length() == 0 {
		article = doc.Find("#nr1").First()
	}
	if article.Length() == 0 {
		return "", &transport.PermanentError{Err: fmt.Errorf("content block not found in %s", pageURL)}
	}

	html, err := article.Html()
	if err != nil {
		return "", &transport.PermanentError{Err: fmt.Errorf("get article HTML: %w", err)}
	}

	// Clean: remove scripts
	html = reScript.ReplaceAllString(html, "")
	// Replace </p> and <br> with newlines
	html = reBr.ReplaceAllString(html, "\n")
	// Remove all remaining HTML tags
	html = reHTMLTag.ReplaceAllString(html, "")
	// Decode entities
	html = htmlUnescape(html)
	// The first reading page repeats the book page's synopsis block before the
	// story starts; the EPUB already carries that synopsis as metadata, so drop
	// it instead of duplicating it as chapter text. The cut is anchored on the
	// first real chapter heading so it does not depend on summary field names.
	// Go's \s does not include the ideographic space used here, so it is listed.
	html = reSynopsisHead.ReplaceAllString(html, "$1")
	// Clean up whitespace and filter boilerplate
	lines := strings.Split(html, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Trim(line, "\u3000 \t\r")
		if line == "" || isBoilerplate(line) {
			continue
		}
		cleaned = append(cleaned, line)
	}

	content := strings.Join(cleaned, "\n")
	if strings.TrimSpace(content) == "" {
		return "", &transport.PermanentError{Err: fmt.Errorf("chapter page contains no readable content")}
	}
	return content, nil
}

// isBoilerplate filters out 52shuku navigation/promotional text.
func isBoilerplate(line string) bool {
	switch {
	case strings.Contains(line, "目录") && strings.Contains(line, "上一页") && strings.Contains(line, "下一页"):
		return true
	case strings.Contains(line, "哦豁") && strings.Contains(line, "52书库"):
		return true
	case strings.Contains(line, "传送门"):
		return true
	case strings.Contains(line, "关灯") && strings.Contains(line, "护眼") && strings.Contains(line, "字体"):
		return true
	case strings.Contains(line, "52书库App") || strings.Contains(line, "阅读记录") && strings.Contains(line, "加入收藏"):
		return true
	case strings.Contains(line, "Tips") && strings.Contains(line, "52"):
		return true
	}
	return false
}

// ResolveBook derives direct-download identity and metadata from the book page.
func (s *Site52Shuku) ResolveBook(ctx context.Context, bookURL string) (*search.SearchResult, error) {
	doc, err := s.doGet(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("resolve book: %w", err)
	}
	title, author, status := parseBookTitle(strings.TrimSpace(doc.Find("h1.article-title").First().Text()))
	if title == "" {
		return nil, &transport.PermanentError{Err: fmt.Errorf("book title not found on %s", bookURL)}
	}
	return &search.SearchResult{Title: title, Author: author, Status: status, BookURL: bookURL, SiteName: s.Name()}, nil
}

// parseBookTitle derives display metadata from a "书名_作者【状态】" heading. The
// trailing bracket is optional and the underscore separator may be absent, so
// both book pages and search results reuse this single rule.
func parseBookTitle(raw string) (title, author, status string) {
	title = strings.TrimSpace(raw)
	if start := strings.LastIndex(title, "【"); start > 0 && strings.HasSuffix(title, "】") {
		status = strings.TrimSpace(strings.TrimSuffix(title[start+len("【"):], "】"))
		title = strings.TrimSpace(title[:start])
	}
	if split := strings.LastIndex(title, "_"); split > 0 {
		author, title = strings.TrimSpace(title[split+1:]), strings.TrimSpace(title[:split])
	}
	return title, author, status
}

// GetBookMeta implements search.BookMetaProvider.
func (s *Site52Shuku) GetBookMeta(ctx context.Context, bookURL string) (*search.BookMetaDetail, error) {
	doc, err := s.doGet(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("fetch book page: %w", err)
	}

	detail := &search.BookMetaDetail{}
	_, _, detail.Status = parseBookTitle(strings.TrimSpace(doc.Find("h1.article-title").First().Text()))

	// Parse description from <article class="article-content">
	articleContent := doc.Find("article.article-content")
	// Get the second <p> (first is "小说简介：", second is the actual description)
	descParas := articleContent.Find("p")
	if descParas.Length() >= 2 {
		descText := descParas.Eq(1).Text()
		// Clean up description: remove [GL百合] etc prefix
		descText = rx(`\[.*?\]`).ReplaceAllString(descText, "")
		// Remove "《书名》作者：XX【状态】" prefix
		descText = rx(`《.*?》作者：[^【]*【.*?】`).ReplaceAllString(descText, "")
		// Trim full-width spaces first, then strip the synopsis label.
		descText = strings.Trim(descText, "\u3000 \t\n\r")
		for _, prefix := range []string{"简介：", "简介:", "文案：", "文案:"} {
			descText = strings.TrimSpace(strings.TrimPrefix(descText, prefix))
		}
		detail.Description = strings.TrimSpace(descText)

		// Try to detect protagonist from description
		detail.Protagonist = detectProtagonist(detail.Description)
	}

	// Parse tags from "所属专题" only. That line links the author page first and
	// the genre tags after it, so only genre links become subjects.
	articleContent.Find("p").Each(func(_ int, sel *goquery.Selection) {
		if strings.Contains(sel.Text(), "所属专题") {
			sel.Find("a").Each(func(_ int, aSel *goquery.Selection) {
				href, _ := aSel.Attr("href")
				if !strings.Contains(href, "/Tags_Nan/") {
					return
				}
				tagText := strings.TrimSpace(aSel.Text())
				if tagText != "" {
					detail.Tags = append(detail.Tags, tagText)
				}
			})
		}
	})

	// Get breadcrumb category (e.g., "GL百合", "现代耽美")
	// Get breadcrumb category — only the first breadcrumbs div (category path)
	doc.Find(".breadcrumbs").First().Find("a").Each(func(_ int, sel *goquery.Selection) {
		cat := strings.TrimSpace(sel.Text())
		if cat != "" && cat != "52书库" && cat != "耽美小说" && !strings.Contains(cat, "在线阅读") {
			found := false
			for _, t := range detail.Tags {
				if t == cat {
					found = true
					break
				}
			}
			if !found {
				detail.Tags = append(detail.Tags, cat)
			}
		}
	})

	// Get release date from meta tag
	dateMeta, exists := doc.Find("meta[property='og:release_date']").Attr("content")
	if exists {
		detail.LastUpdated = strings.TrimSpace(dateMeta)
	}

	return detail, nil
}

// detectProtagonist tries to find the main character name from the description.
// 52shuku descriptions often start with character introductions.
func detectProtagonist(desc string) string {
	// Look for patterns like "XXX清冷矜贵" or "XXX是..."
	// Common pattern: 2-3 Chinese character name followed by description
	names := rx(`([\p{Han}]{2,3})(?:清冷|温柔|霸道|高冷|可爱|冷酷|聪慧|善良|美艳|俊美|帅气|漂亮|聪明|腹黑|忠犬|傲娇)`)
	matches := names.FindAllStringSubmatch(desc, 3)
	// Words that look like names but are descriptive
	nonNames := map[string]bool{
		"黏人": true, "人人都": true, "人人都说": true,
		"妥妥": true, "一名": true, "一位": true,
		"无数": true, "大批": true, "不到": true,
	}
	var found []string
	for _, m := range matches {
		if len(m) >= 2 {
			name := m[1]
			if !nonNames[name] && len([]rune(name)) >= 2 {
				found = append(found, name)
			}
		}
	}
	if len(found) > 0 {
		return strings.Join(found, "、")
	}
	return ""
}
