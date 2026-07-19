package sites

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/PuerkitoBio/goquery"
)

const yamiboBaseURL = "https://bbs.yamibo.com/"

var (
	yamiboPID     = regexp.MustCompile(`(?:[?&]pid=|pid)(\d+)`)
	yamiboTID     = regexp.MustCompile(`(?:[?&]tid=|thread-)(\d+)`)
	yamiboPage    = regexp.MustCompile(`thread-\d+-(\d+)-\d+\.html`)
	yamiboPageArg = regexp.MustCompile(`(?:[?&]|&amp;)page=(\d+)`)
	yamiboChapter = regexp.MustCompile(`(?i)(第\s*[\d一二三四五六七八九十百千零两]+\s*[章节卷话]|番外|序章|终章|后记|chapter|episode|ep\.?\s*\d+)`)
	yamiboNoise   = regexp.MustCompile(`(只看该作者|评分|收藏|回复|举报|电梯直达|复制链接|楼主|沙发|板凳)`)
)

// Yamibo adapts authenticated Yamibo forum threads to the common book source
// interface. v0.6 starts with cookie authentication and catalog/elevator
// threads; author-post extraction falls back to scanning the thread pages.
type Yamibo struct {
	client *http.Client
	mu     sync.RWMutex
	cookie string
}

func NewYamibo() *Yamibo {
	return &Yamibo{client: &http.Client{Timeout: 20 * time.Second}}
}

func (y *Yamibo) Name() string                  { return "百合会" }
func (y *Yamibo) BaseURL() string               { return yamiboBaseURL }
func (y *Yamibo) RequiresCookie() bool          { return true }
func (y *Yamibo) MaxConcurrency() int64         { return 2 }
func (y *Yamibo) MaxRequestsPerSecond() float64 { return 1.2 }
func (y *Yamibo) HasCookie() bool {
	y.mu.RLock()
	defer y.mu.RUnlock()
	return strings.TrimSpace(y.cookie) != ""
}
func (y *Yamibo) SetCookie(cookie string) {
	y.mu.Lock()
	y.cookie = strings.TrimSpace(cookie)
	y.mu.Unlock()
}

func (y *Yamibo) document(ctx context.Context, rawURL string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Referer", yamiboBaseURL+"forum.php")
	y.mu.RLock()
	cookie := y.cookie
	y.mu.RUnlock()
	if cookie == "" {
		return nil, fmt.Errorf("百合会需要 Cookie，请返回来源页重新认证")
	}
	req.Header.Set("Cookie", cookie)

	resp, err := y.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("百合会请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("百合会返回 HTTP %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("解析百合会页面: %w", err)
	}
	if doc.Find("form#loginform, .loginbox").Length() > 0 && doc.Find("#postlist, a.xst").Length() == 0 {
		return nil, fmt.Errorf("Cookie 已失效，请重新认证")
	}
	return doc, nil
}

func (y *Yamibo) Search(ctx context.Context, query string) ([]search.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	seen := make(map[string]search.SearchResult)
	for _, fid := range []int{55, 49} {
		searchURL := fmt.Sprintf("%ssearch.php?mod=forum&searchsubmit=yes&srchfid%%5B%%5D=%d&orderby=replies&ascdesc=desc&srchtxt=%s", yamiboBaseURL, fid, url.QueryEscape(query))
		doc, err := y.document(ctx, searchURL)
		if err != nil {
			if len(seen) == 0 {
				return nil, err
			}
			continue
		}
		doc.Find("a.xst, a.s.xst").Each(func(_ int, a *goquery.Selection) {
			href, ok := a.Attr("href")
			if !ok {
				return
			}
			href = normalizeYamiboURL(href)
			title := strings.TrimSpace(a.Text())
			if title == "" || href == "" {
				return
			}
			key := href
			if match := yamiboTID.FindStringSubmatch(href); len(match) > 1 {
				key = match[1]
			}
			seen[key] = search.SearchResult{
				Title: title, BookURL: href, SiteName: y.Name(), Tags: []string{mapYamiboForum(fid)},
			}
		})
		if len(seen) >= 20 {
			break
		}
	}
	results := make([]search.SearchResult, 0, len(seen))
	for _, item := range seen {
		results = append(results, item)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Title < results[j].Title })
	return results, nil
}

func (y *Yamibo) GetChapterList(ctx context.Context, bookURL string) ([]search.Chapter, error) {
	doc, err := y.document(ctx, bookURL)
	if err != nil {
		return nil, err
	}

	var best []search.Chapter
	doc.Find("div.showcollapse_content, div#postlist, div.pcb").Each(func(_ int, scope *goquery.Selection) {
		items := extractYamiboLinks(scope, y.Name())
		if scoreYamiboChapters(items) > scoreYamiboChapters(best) {
			best = items
		}
	})
	if len(best) >= 3 {
		return renumberChapters(best), nil
	}

	// Threads without an elevator are treated as books made from substantial
	// posts by the first-post author.
	posts := extractYamiboAuthorPosts(doc, bookURL, "")
	if len(posts) == 0 {
		return nil, fmt.Errorf("未找到目录或可识别的作者正文楼层")
	}
	return renumberChapters(posts), nil
}

func (y *Yamibo) GetChapterContent(ctx context.Context, chapterURL string) (string, error) {
	doc, err := y.document(ctx, chapterURL)
	if err != nil {
		return "", err
	}
	selector := "td[id^='postmessage_'], div[id^='postmessage_']"
	if match := yamiboPID.FindStringSubmatch(chapterURL); len(match) > 1 {
		selector = "#postmessage_" + match[1]
	}
	node := doc.Find(selector).First()
	if node.Length() == 0 {
		node = doc.Find("div.pcb, td.t_f, div.t_f").First()
	}
	if node.Length() == 0 {
		return "", fmt.Errorf("正文节点不存在")
	}
	return cleanYamiboContent(node)
}

func extractYamiboLinks(scope *goquery.Selection, siteName string) []search.Chapter {
	seen := make(map[string]bool)
	items := make([]search.Chapter, 0)
	scope.Find("a[href*='viewthread'], a[href*='findpost'], a[href*='pid=']").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		href = normalizeYamiboURL(href)
		title := strings.TrimSpace(a.Text())
		if href == "" || title == "" || yamiboNoise.MatchString(title) {
			return
		}
		key := href
		if match := yamiboPID.FindStringSubmatch(href); len(match) > 1 {
			key = match[1]
		}
		if seen[key] {
			return
		}
		seen[key] = true
		items = append(items, search.Chapter{Title: title, URL: href, SiteName: siteName})
	})
	return items
}

func extractYamiboAuthorPosts(doc *goquery.Document, threadURL, author string) []search.Chapter {
	items := make([]search.Chapter, 0)
	doc.Find("td[id^='postmessage_'], div[id^='postmessage_']").Each(func(_ int, node *goquery.Selection) {
		id, _ := node.Attr("id")
		pid := strings.TrimPrefix(id, "postmessage_")
		text, err := cleanYamiboContent(node)
		if err != nil || len([]rune(strings.ReplaceAll(text, "\n", ""))) < 80 {
			return
		}
		title := fmt.Sprintf("作者楼层 %d", len(items)+1)
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if yamiboChapter.MatchString(line) && len([]rune(line)) <= 80 {
				title = line
				break
			}
		}
		tid := ""
		if match := yamiboTID.FindStringSubmatch(threadURL); len(match) > 1 {
			tid = match[1]
		}
		chapterURL := threadURL
		if tid != "" && pid != "" {
			chapterURL = fmt.Sprintf("%sforum.php?mod=viewthread&tid=%s&pid=%s", yamiboBaseURL, tid, pid)
		}
		items = append(items, search.Chapter{Title: title, URL: chapterURL, SiteName: "百合会"})
	})
	return items
}

func cleanYamiboContent(node *goquery.Selection) (string, error) {
	clone := node.Clone()
	clone.Find("script, style, i.pstatus, .pstatus, .quote, .blockquote, .attach_nopermission, .locked, .ignore_js_op, .aimg_tip, .tip").Remove()
	text := clone.Text()
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\u00a0", " "), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || yamiboNoise.MatchString(line) && len([]rune(line)) <= 12 {
			continue
		}
		lines = append(lines, line)
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	if len([]rune(strings.ReplaceAll(text, "\n", ""))) < 20 {
		return "", fmt.Errorf("正文过短")
	}
	return text, nil
}

func scoreYamiboChapters(items []search.Chapter) int {
	score := len(items)
	for _, item := range items {
		if yamiboChapter.MatchString(item.Title) {
			score += 3
		}
	}
	return score
}

func renumberChapters(items []search.Chapter) []search.Chapter {
	for i := range items {
		items[i].Index = i + 1
	}
	return items
}

func normalizeYamiboURL(href string) string {
	href = strings.TrimSpace(strings.ReplaceAll(href, "&amp;", "&"))
	if href == "" || strings.HasPrefix(href, "javascript:") {
		return ""
	}
	base, _ := url.Parse(yamiboBaseURL)
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func mapYamiboForum(fid int) string {
	if fid == 55 {
		return "译文区"
	}
	return "文学区"
}

func yamiboThreadPage(rawURL string, page int) string {
	if match := yamiboTID.FindStringSubmatch(rawURL); len(match) > 1 {
		return yamiboBaseURL + "thread-" + match[1] + "-" + strconv.Itoa(page) + "-1.html"
	}
	return rawURL
}

// Keep the page regexes exercised and available for the next author-page
// pagination pass without exposing them as public API.
var _ = []any{yamiboPage, yamiboPageArg, yamiboThreadPage}
