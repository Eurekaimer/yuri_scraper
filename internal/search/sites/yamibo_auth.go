package sites

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/transport"
	"github.com/PuerkitoBio/goquery"
)

const yamiboLoginPath = "member.php?mod=logging&action=login"

var yamiboFormhash = regexp.MustCompile(`(?i)name=["']formhash["'][^>]*value=["']([a-zA-Z0-9]+)["']`)

var errYamiboWAF = errors.New("yamibo security challenge")

type yamiboAuthError struct {
	message, guidance string
	cause             error
}

func (e *yamiboAuthError) Error() string    { return e.message }
func (e *yamiboAuthError) Unwrap() error    { return e.cause }
func (e *yamiboAuthError) Guidance() string { return e.guidance }

func yamiboRequestError(err error) error {
	if errors.Is(err, context.Canceled) {
		return &yamiboAuthError{"百合会登录或请求已取消", "需要时请重新登录或重试。", err}
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		return &yamiboAuthError{"连接百合会超时", "请检查网络、DNS 和代理后重试。", err}
	}
	return &yamiboAuthError{"无法连接百合会", "请检查网络、DNS 和代理后重试。", err}
}

func yamiboStatusError(status int, headers http.Header) error {
	cause := &transport.HTTPError{StatusCode: status, Delay: parseRetryAfter(headers.Get("Retry-After"), time.Now())}
	switch {
	case status == http.StatusMethodNotAllowed:
		return &yamiboAuthError{"百合会拒绝当前登录请求方式，可能是网站限制或登录接口发生变化", "请用浏览器检查网站并稍后重试。", cause}
	case status == http.StatusForbidden:
		return &yamiboAuthError{"百合会拒绝访问", "请用浏览器检查网站是否要求验证，并稍后重试。", cause}
	case status == http.StatusTooManyRequests:
		return &yamiboAuthError{"访问百合会过于频繁", "请降低速度，或稍后重试。", cause}
	case status >= 500:
		return &yamiboAuthError{"百合会网站暂时异常", "请稍后重试。", cause}
	default:
		return &yamiboAuthError{"百合会未接受请求", "请用浏览器检查网站并稍后重试。", cause}
	}
}

func isYamiboWAF(status int, headers http.Header, body []byte) bool {
	return bytes.Contains(body, []byte("window.__nox")) && bytes.Contains(body, []byte("nox_")) ||
		(status == http.StatusForbidden || status == http.StatusMethodNotAllowed) &&
			strings.EqualFold(strings.TrimSpace(headers.Get("Server")), "BAIDU_WAF")
}

func sameYamiboOrigin(baseURL, rawURL string) bool {
	base, err := url.Parse(baseURL)
	if err != nil || base.User != nil {
		return false
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.User != nil || base.Hostname() == "" || target.Hostname() == "" {
		return false
	}
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		if strings.EqualFold(u.Scheme, "http") {
			return "80"
		}
		return ""
	}
	return port(base) != "" && strings.EqualFold(base.Scheme, target.Scheme) &&
		strings.EqualFold(base.Hostname(), target.Hostname()) && port(base) == port(target)
}

func newYamiboHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: 20 * time.Second, Jar: jar}
}

// Login keeps credentials in memory and uses a browser only for a security challenge.
func (y *Yamibo) Login(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return &yamiboAuthError{"百合会账号和密码不能为空", "请输入完整的账号和密码。", nil}
	}

	ctx, cancel, done, client, generation := y.beginLogin(ctx)
	defer func() {
		cancel()
		y.mu.Lock()
		if y.generation == generation {
			y.loginCancel, y.loginDone = nil, nil
		}
		y.mu.Unlock()
		close(done)
	}()
	err := y.nativeLogin(ctx, client, username, password)
	var browser *yamiboBrowser
	if errors.Is(err, errYamiboWAF) {
		browser, err = newYamiboBrowser(ctx, y.browserHeadless)
		if err == nil {
			err = browser.login(ctx, y.baseURL, username, password)
		}
	}
	if err != nil {
		if browser != nil {
			browser.close()
		}
		return err
	}
	y.mu.Lock()
	if ctx.Err() != nil || y.generation != generation {
		y.mu.Unlock()
		if browser != nil {
			browser.close()
		}
		return yamiboRequestError(context.Canceled)
	}
	y.browser = browser
	y.authenticated = true
	y.mu.Unlock()
	return nil
}

func (y *Yamibo) nativeLogin(ctx context.Context, client *http.Client, username, password string) error {
	loginPageURL := resolveYamiboURL(y.baseURL, yamiboLoginPath)
	body, err := y.authRequest(ctx, client, http.MethodGet, loginPageURL, nil, "")
	if err != nil {
		return fmt.Errorf("打开百合会登录: %w", err)
	}
	actionURL, formhash, err := parseYamiboLoginForm(y.baseURL, body)
	if err != nil {
		return fmt.Errorf("打开百合会登录: %w", err)
	}
	if !sameYamiboOrigin(y.baseURL, actionURL) {
		return &yamiboAuthError{"百合会登录表单的目标不安全", "请检查网站后重试。", nil}
	}
	values := url.Values{
		"formhash":    {formhash},
		"referer":     {resolveYamiboURL(y.baseURL, "forum.php")},
		"loginfield":  {"username"},
		"username":    {username},
		"password":    {password},
		"questionid":  {"0"},
		"answer":      {""},
		"loginsubmit": {"true"},
	}
	if _, err := y.authRequest(ctx, client, http.MethodPost, actionURL, strings.NewReader(values.Encode()), loginPageURL); err != nil {
		return fmt.Errorf("提交百合会登录: %w", err)
	}

	profileURL := resolveYamiboURL(y.baseURL, "home.php?mod=space")
	profile, err := y.authRequest(ctx, client, http.MethodGet, profileURL, nil, resolveYamiboURL(y.baseURL, "forum.php"))
	if err != nil {
		return fmt.Errorf("验证百合会登录: %w", err)
	}
	if !strings.Contains(string(profile), username) && !hasYamiboAuthCookie(client, y.baseURL) {
		return &yamiboAuthError{"账号密码可能有误，或网站要求验证码或安全提问", "请检查账号密码，并在网站要求时完成验证。", nil}
	}
	return nil
}

func (y *Yamibo) beginLogin(parent context.Context) (context.Context, context.CancelFunc, chan struct{}, *http.Client, uint64) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	y.mu.Lock()
	oldCancel, oldDone, oldBrowser := y.loginCancel, y.loginDone, y.browser
	y.generation++
	generation := y.generation
	client := y.client
	if client == nil {
		client = newYamiboHTTPClient()
	}
	copyClient := *client
	copyClient.Jar, _ = cookiejar.New(nil)
	y.client = &copyClient
	loginClient := copyClient
	redirect := loginClient.CheckRedirect
	loginClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !sameYamiboOrigin(y.baseURL, req.URL.String()) {
			return errors.New("redirect leaves Yamibo")
		}
		if redirect != nil {
			return redirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	y.loginCancel, y.loginDone, y.browser = cancel, done, nil
	y.cookie, y.authenticated = "", false
	y.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if oldBrowser != nil {
		oldBrowser.close()
	}
	if oldDone != nil {
		<-oldDone
	}
	return ctx, cancel, done, &loginClient, generation
}

func (y *Yamibo) markUnauthenticated(generation uint64) {
	y.mu.Lock()
	var browser *yamiboBrowser
	if y.generation == generation {
		y.generation++
		y.authenticated = false
		y.cookie = ""
		browser, y.browser = y.browser, nil
	}
	y.mu.Unlock()
	if browser != nil {
		browser.close()
	}
}

// Close releases any browser associated with this source.
func (y *Yamibo) Close() error {
	y.mu.Lock()
	cancel, done, browser := y.loginCancel, y.loginDone, y.browser
	y.loginCancel, y.loginDone, y.browser = nil, nil, nil
	y.generation++
	y.cookie, y.authenticated = "", false
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
	return nil
}

func (y *Yamibo) authRequest(ctx context.Context, client *http.Client, method, rawURL string, body io.Reader, referer string) ([]byte, error) {
	if err := y.limiter.Wait(ctx); err != nil {
		return nil, yamiboRequestError(err)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, yamiboRequestError(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, yamiboRequestError(err)
	}
	defer resp.Body.Close()
	reader := io.Reader(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		reader = io.LimitReader(resp.Body, 64<<10)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, yamiboRequestError(err)
	}
	if isYamiboWAF(resp.StatusCode, resp.Header, data) {
		return nil, errYamiboWAF
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, yamiboStatusError(resp.StatusCode, resp.Header)
	}
	return data, nil
}

func parseYamiboLoginForm(baseURL string, body []byte) (string, string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", &yamiboAuthError{"无法读取百合会登录页", "请检查网站后重试。", err}
	}
	form := doc.Find("form#loginform_Lz0, form#loginform, form[action*='mod=logging'][action*='action=login'], form[id*='login']").First()
	if form.Length() == 0 {
		return "", "", &yamiboAuthError{"登录页面结构已变化或被访问限制", "请用浏览器检查网站，并稍后重试。", nil}
	}
	action := ""
	if form.Length() > 0 {
		action, _ = form.Attr("action")
	}
	actionURL := resolveYamiboURL(baseURL, action)
	if strings.TrimSpace(action) == "" {
		actionURL = resolveYamiboURL(baseURL, yamiboLoginPath+"&loginsubmit=yes")
	}
	formhash, _ := form.Find("input[name='formhash']").First().Attr("value")
	if formhash == "" {
		formhash, _ = doc.Find("input[name='formhash']").First().Attr("value")
	}
	if formhash == "" {
		if match := yamiboFormhash.FindSubmatch(body); len(match) > 1 {
			formhash = string(match[1])
		}
	}
	if strings.TrimSpace(formhash) == "" {
		return "", "", &yamiboAuthError{"登录页面结构已变化或被访问限制", "请用浏览器检查网站，并稍后重试。", nil}
	}
	return actionURL, formhash, nil
}

func resolveYamiboURL(baseURL, href string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	ref, err := url.Parse(strings.TrimSpace(strings.ReplaceAll(href, "&amp;", "&")))
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func hasYamiboAuthCookie(client *http.Client, baseURL string) bool {
	if client == nil || client.Jar == nil {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	for _, cookie := range client.Jar.Cookies(base) {
		name := strings.ToLower(cookie.Name)
		if name == "auth" || strings.HasSuffix(name, "_auth") {
			return true
		}
	}
	return false
}
