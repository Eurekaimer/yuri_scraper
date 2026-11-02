package sites

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/time/rate"
)

const (
	yamiboBaseURL        = "https://bbs.yamibo.com/"
	yamiboMaxConcurrency = 12
	yamiboMaxRPS         = 10.0
)

var (
	yamiboPID     = regexp.MustCompile(`(?:[?&]pid=|pid)(\d+)`)
	yamiboTID     = regexp.MustCompile(`(?:[?&]tid=|thread-)(\d+)`)
	yamiboPage    = regexp.MustCompile(`thread-\d+-(\d+)-\d+\.html`)
	yamiboPageArg = regexp.MustCompile(`(?:[?&]|&amp;)page=(\d+)`)
	yamiboChapter = regexp.MustCompile(`(?i)(第\s*[\d一二三四五六七八九十百千零两]+\s*[章节卷话]|番外|序章|终章|后记|chapter|episode|ep\.?\s*\d+)`)
	yamiboNoise   = regexp.MustCompile(`(只看该作者|评分|收藏|回复|举报|电梯直达|复制链接|楼主|沙发|板凳)`)
	yamiboIndent  = regexp.MustCompile(`[ \t]{4,}`)
)

// Yamibo adapts authenticated Yamibo forum threads to the common book source.
type Yamibo struct {
	baseURL         string
	client          *http.Client
	limiter         *rate.Limiter
	maxConcurrency  int64
	maxRPS          float64
	mu              sync.RWMutex
	generation      uint64
	loginCancel     context.CancelFunc
	loginDone       chan struct{}
	browser         *yamiboBrowser
	browserHeadless bool
	cookie          string
	authenticated   bool
}

func NewYamibo() *Yamibo {
	return &Yamibo{
		baseURL:        yamiboBaseURL,
		client:         newYamiboHTTPClient(),
		limiter:        rate.NewLimiter(rate.Limit(yamiboMaxRPS), 1),
		maxConcurrency: yamiboMaxConcurrency,
		maxRPS:         yamiboMaxRPS,
	}
}

func NewYamiboWithPolicy(maxConcurrency int64, maxRPS float64) *Yamibo {
	y := NewYamibo()
	if maxConcurrency > 0 && maxConcurrency <= yamiboMaxConcurrency {
		y.maxConcurrency = maxConcurrency
	}
	if maxRPS > 0 && maxRPS <= yamiboMaxRPS {
		y.maxRPS = maxRPS
		y.limiter.SetLimit(rate.Limit(maxRPS))
	}
	return y
}

func (y *Yamibo) Name() string                  { return SourceYamibo }
func (y *Yamibo) BaseURL() string               { return y.baseURL }
func (y *Yamibo) RequiresAuthentication() bool  { return true }
func (y *Yamibo) MaxConcurrency() int64         { return y.maxConcurrency }
func (y *Yamibo) MaxRequestsPerSecond() float64 { return y.maxRPS }
func (y *Yamibo) IsAuthenticated() bool {
	y.mu.RLock()
	defer y.mu.RUnlock()
	return y.authenticated || strings.TrimSpace(y.cookie) != ""
}

// SetCookie remains available for opt-in live tests and API compatibility.
// The TUI uses Login and never asks users to copy browser cookies.
func (y *Yamibo) SetCookie(cookie string) {
	y.mu.Lock()
	cancel, done, browser := y.loginCancel, y.loginDone, y.browser
	y.loginCancel, y.loginDone, y.browser = nil, nil, nil
	y.generation++
	y.cookie = strings.TrimSpace(cookie)
	y.authenticated = y.cookie != ""
	y.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if browser != nil {
		browser.close()
	}
	if done != nil {
		<-done
	}
}

func (y *Yamibo) document(ctx context.Context, rawURL string) (*goquery.Document, error) {
	y.mu.RLock()
	client, browser, generation := y.client, y.browser, y.generation
	cookie, authenticated := y.cookie, y.authenticated || y.cookie != ""
	y.mu.RUnlock()
	if !authenticated {
		return nil, &yamiboAuthError{"百合会需要登录", "请返回来源页使用账号密码登录。", nil}
	}

	var data []byte
	var err error
	if browser != nil {
		data, err = browser.document(ctx, y.baseURL, rawURL, y.limiter)
		if err != nil {
			if browser.closed.Load() || browser.tabCtx.Err() != nil {
				y.markUnauthenticated(generation)
			}
			return nil, err
		}
	} else {
		if err := y.limiter.Wait(ctx); err != nil {
			return nil, yamiboRequestError(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, yamiboRequestError(err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124.0 Safari/537.36")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		req.Header.Set("Referer", y.baseURL+"forum.php")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, yamiboRequestError(err)
		}
		defer resp.Body.Close()
		reader := io.Reader(resp.Body)
		if resp.StatusCode != http.StatusOK {
			reader = io.LimitReader(resp.Body, 64<<10)
		}
		data, err = io.ReadAll(reader)
		if err != nil {
			return nil, yamiboRequestError(err)
		}
		if isYamiboWAF(resp.StatusCode, resp.Header, data) {
			return nil, &yamiboAuthError{"百合会的网站安全验证拦截了请求", "请重新登录并在浏览器窗口完成验证。", nil}
		}
		if resp.StatusCode != http.StatusOK {
			return nil, yamiboStatusError(resp.StatusCode, resp.Header)
		}
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, &yamiboAuthError{"无法读取百合会页面", "请稍后重试。", err}
	}
	if doc.Find("form#loginform, .loginbox").Length() > 0 && doc.Find("#postlist, a.xst").Length() == 0 {
		y.markUnauthenticated(generation)
		return nil, &yamiboAuthError{"百合会会话已失效", "请重新输入账号密码登录。", nil}
	}
	return doc, nil
}

func (y *Yamibo) GetChapterList(ctx context.Context, bookURL string) ([]search.Chapter, error) {
	plan, err := y.PlanChapters(ctx, bookURL, search.ChapterPlanOptions{})
	return plan.Chapters, err
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

func cleanYamiboContent(node *goquery.Selection) (string, error) {
	clone := node.Clone()
	clone.Find("script, style, i.pstatus, .pstatus, .quote, .blockquote, .attach_nopermission, .locked, .ignore_js_op, .aimg_tip, .tip").Remove()
	clone.Find("p, div, li").PrependHtml("\n").AppendHtml("\n")
	clone.Find("br").ReplaceWithHtml("\n")
	text := yamiboIndent.ReplaceAllString(strings.ReplaceAll(clone.Text(), "\u00a0", " "), "\n")
	lines := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
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
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		if match := yamiboTID.FindStringSubmatch(rawURL); len(match) > 1 {
			parsed.Path = "/thread-" + match[1] + "-" + strconv.Itoa(page) + "-1.html"
			parsed.RawQuery = ""
			parsed.Fragment = ""
			return parsed.String()
		}
		query := parsed.Query()
		query.Set("page", strconv.Itoa(page))
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if match := yamiboTID.FindStringSubmatch(rawURL); len(match) > 1 {
		return yamiboBaseURL + "thread-" + match[1] + "-" + strconv.Itoa(page) + "-1.html"
	}
	return rawURL
}
