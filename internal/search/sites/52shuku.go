package sites

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/hzg/yuri_scraper/internal/search"
)

// Site52Shuku adapts 52shuku.net.
type Site52Shuku struct {
	baseURL string
	client  *http.Client
}

// New52Shuku creates the 52shuku adapter.
func New52Shuku() *Site52Shuku {
	return &Site52Shuku{
		baseURL: "https://www.52shuku.net",
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (s *Site52Shuku) Name() string    { return "52书库" }
func (s *Site52Shuku) BaseURL() string { return s.baseURL }

func (s *Site52Shuku) doGet(ctx context.Context, urlStr string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, diagnoseError(err, urlStr)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 503 || resp.StatusCode == 403 {
		// Possible Cloudflare block
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if strings.Contains(string(body), "Cloudflare") || strings.Contains(string(body), "cf-") {
			return nil, fmt.Errorf("网站开启了 Cloudflare 防护，请稍后重试 (HTTP %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("网站拒绝访问 (HTTP %d)，可能 IP 被限制", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回 HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("解析页面失败: %w", err)
	}
	return doc, nil
}

// diagnoseError wraps network errors with actionable Chinese messages.
func diagnoseError(err error, urlStr string) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("网络超时: 52书库响应较慢，请检查网络或稍后重试")
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("无法连接 52书库: 网站可能暂时不可用")
	case strings.Contains(msg, "no such host"):
		return fmt.Errorf("无法解析域名: 请检查 DNS 设置")
	case strings.Contains(msg, "EOF") || strings.Contains(msg, "reset"):
		return fmt.Errorf("连接被重置: 网站可能限制了访问频率，请降低速度档位")
	default:
		return fmt.Errorf("网络错误: %w", err)
	}
}

func (s *Site52Shuku) Search(ctx context.Context, query string) ([]search.SearchResult, error) {
	// 52shuku search: /so/search.php?q=<query>
	searchURL := fmt.Sprintf("%s/so/search.php?q=%s", s.baseURL, url.QueryEscape(query))

	doc, err := s.doGet(ctx, searchURL)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	var results []search.SearchResult

	// Parse <article class="excerpt"> blocks
	doc.Find("article.excerpt").Each(func(_ int, sel *goquery.Selection) {
		// Title link: <a href="..."><h4>N. 书名_<em>作者</em>【状态】</h4></a>
		link := sel.Find("header h2 a").First()
		href, _ := link.Attr("href")

		fullTitle := strings.TrimSpace(link.Find("h4").Text())
		fullTitle = rx(`^\d+\.\s*`).ReplaceAllString(fullTitle, "")

		author := strings.TrimSpace(link.Find("em").First().Text())
		title := fullTitle
		if author != "" {
			title = strings.Replace(title, author, "", 1)
		}
		title = rx(`【[^】]*】$`).ReplaceAllString(title, "")
		title = strings.Trim(title, "_ -\u3000")
		title = strings.TrimSpace(title)

		// Status from title
		status := ""
		if strings.Contains(fullTitle, "完结") {
			status = "已完结"
		}

		// Description from <span class="note">
		noteText := strings.TrimSpace(sel.Find("span.note").Text())
		desc := rx(`\[.*?\]`).ReplaceAllString(noteText, "")
		desc = rx(`《.*?》作者：[^【]*【.*?】`).ReplaceAllString(desc, "")
		desc = strings.TrimSpace(strings.TrimPrefix(desc, "简介："))
		desc = strings.TrimSpace(strings.TrimPrefix(desc, "文案："))
		desc = strings.Trim(desc, "\u3000 \t\n\r")

		// Category from <span class="muted">
		category := ""
		sel.Find("span.muted a").Each(func(_ int, aSel *goquery.Selection) {
			cat := strings.TrimSpace(aSel.Text())
			if cat != "" && category == "" {
				category = cat
			}
		})

		if title != "" && href != "" && !strings.HasPrefix(href, "#") {
			result := search.SearchResult{
				Title:       title,
				Author:      author,
				BookURL:     href,
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

var reChapterLink = regexp.MustCompile(`<li class="mulu"><a href="(.*?)">(.*?)</a></li>`)

func (s *Site52Shuku) GetChapterList(ctx context.Context, bookURL string) ([]search.Chapter, error) {
	doc, err := s.doGet(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("fetch chapter list: %w", err)
	}

	html, err := doc.Html()
	if err != nil {
		return nil, fmt.Errorf("get html: %w", err)
	}

	matches := reChapterLink.FindAllStringSubmatch(html, -1)
	chapters := make([]search.Chapter, 0, len(matches))

	// 52shuku: the directory page URL itself is often chapter 1
	// and _2.html, _3.html are subsequent pages

	for i, m := range matches {
		link := m[1]
		title := strings.TrimSpace(m[2])

		// Resolve relative URLs
		if !strings.HasPrefix(link, "http") {
			if strings.HasPrefix(link, "/") {
				link = s.baseURL + link
			} else if strings.Contains(bookURL, "/") {
				// Relative to book URL directory
				parts := strings.Split(bookURL, "/")
				parts = parts[:len(parts)-1]
				link = strings.Join(parts, "/") + "/" + link
			}
		}

		chapters = append(chapters, search.Chapter{
			Index:    i + 1,
			Title:    title,
			URL:      link,
			SiteName: s.Name(),
		})
	}

	// 52shuku pages are numbered _2, _3, ... — the main page has no content
	// Do NOT insert bookURL as a page
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapter pages found on %s", bookURL)
	}

	// Renumber after potential insertion
	for i := range chapters {
		chapters[i].Index = i + 1
	}

	return chapters, nil
}

var (
	reScript    = regexp.MustCompile(`<script[\s\S]*?</script>`)
	reHTMLTag   = regexp.MustCompile(`<[^>]+>`)
	reBr        = regexp.MustCompile(`(?i)</p>|<br\s*/?>`)
	reCount     = regexp.MustCompile(`共(\d+)页`)
	reBoiler    = regexp.MustCompile(`(?i)(目录|上一页|下一页|尾页|哦豁.*|传送门.*|Tips.*|52书库App.*|阅读记录.*|加入收藏.*|关灯|护眼|字体.*|[|].*书单推荐.*)`)
	reChapterH  = regexp.MustCompile(`第[\d零一二三四五六七八九十百千]+章\s*[^\n]*`)
)
func (s *Site52Shuku) GetChapterContent(ctx context.Context, pageURL string) (string, error) {
	doc, err := s.doGet(ctx, pageURL)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}

	// Extract content from <article class="article-content" id="nr1">
	article := doc.Find("article.article-content#nr1")
	if article.Length() == 0 {
		return "", fmt.Errorf("content block not found in %s", pageURL)
	}

	html, err := article.Html()
	if err != nil {
		return "", fmt.Errorf("get article html: %w", err)
	}

	// Clean: remove scripts
	html = reScript.ReplaceAllString(html, "")
	// Replace </p> and <br> with newlines
	html = reBr.ReplaceAllString(html, "\n")
	// Remove all remaining HTML tags
	html = reHTMLTag.ReplaceAllString(html, "")
	// Decode entities
	html = htmlUnescape(html)
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

	return strings.Join(cleaned, "\n"), nil
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
// GetBookMeta implements search.BookMetaProvider.
func (s *Site52Shuku) GetBookMeta(ctx context.Context, bookURL string) (*search.BookMetaDetail, error) {
	doc, err := s.doGet(ctx, bookURL)
	if err != nil {
		return nil, fmt.Errorf("fetch book page: %w", err)
	}

	detail := &search.BookMetaDetail{}

	// Parse title line: <h1 class="article-title">书名_作者【状态】</h1>
	titleText := strings.TrimSpace(doc.Find("h1.article-title").First().Text())
	if titleText != "" {
		// Pattern: "书名_作者【完结】" or "书名_作者"
		if idx := strings.LastIndex(titleText, "【"); idx > 0 {
			endIdx := strings.LastIndex(titleText, "】")
			if endIdx > idx {
				detail.Status = titleText[idx+len("【") : endIdx]
			}
			titleText = titleText[:idx]
		}
		// Split by last underscore to get author
		if idx := strings.LastIndex(titleText, "_"); idx > 0 {
			detail.Protagonist = "" // will try to detect from description
			_ = titleText[:idx]     // title part before author
			_ = titleText[idx+1:]   // author part
		}
	}

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
		// Trim full-width spaces first, then strip "简介：" prefix
		descText = strings.Trim(descText, "\u3000 \t\n\r")
		descText = strings.TrimSpace(strings.TrimPrefix(descText, "简介："))
		descText = strings.TrimSpace(strings.TrimPrefix(descText, "简介:"))
		detail.Description = strings.TrimSpace(descText)

		// Try to detect protagonist from description
		detail.Protagonist = detectProtagonist(detail.Description)
	}

	// Parse tags from "所属专题" section only
	articleContent.Find("p").Each(func(_ int, sel *goquery.Selection) {
		if strings.Contains(sel.Text(), "所属专题") {
			sel.Find("a").Each(func(_ int, aSel *goquery.Selection) {
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

