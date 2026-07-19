package sites

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const yamiboLoginPath = "member.php?mod=logging&action=login"

var yamiboFormhash = regexp.MustCompile(`(?i)name=["']formhash["'][^>]*value=["']([a-zA-Z0-9]+)["']`)

func newYamiboHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: 20 * time.Second, Jar: jar}
}

// Login follows Yamibo's Discuz login form. The password is only encoded into
// this request and is never retained by Yamibo after the call returns.
func (y *Yamibo) Login(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return fmt.Errorf("百合会账号和密码不能为空")
	}

	client := y.resetSession()
	loginPageURL := resolveYamiboURL(y.baseURL, yamiboLoginPath)
	body, err := y.authRequest(ctx, client, http.MethodGet, loginPageURL, nil, "")
	if err != nil {
		return fmt.Errorf("打开百合会登录页: %w", err)
	}

	actionURL, formhash, err := parseYamiboLoginForm(y.baseURL, body)
	if err != nil {
		return err
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
		return fmt.Errorf("百合会账号密码登录失败，请检查凭据；如网站要求验证码，请先在浏览器完成验证")
	}

	y.mu.Lock()
	y.authenticated = true
	y.mu.Unlock()
	return nil
}

func (y *Yamibo) resetSession() *http.Client {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.client == nil {
		y.client = newYamiboHTTPClient()
	}
	jar, _ := cookiejar.New(nil)
	y.client.Jar = jar
	y.cookie = ""
	y.authenticated = false
	return y.client
}

func (y *Yamibo) markUnauthenticated() {
	y.mu.Lock()
	y.authenticated = false
	y.cookie = ""
	y.mu.Unlock()
}

func (y *Yamibo) authRequest(ctx context.Context, client *http.Client, method, rawURL string, body io.Reader, referer string) ([]byte, error) {
	if err := y.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func parseYamiboLoginForm(baseURL string, body []byte) (string, string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return "", "", fmt.Errorf("解析百合会登录页: %w", err)
	}
	form := doc.Find("form#loginform_Lz0, form#loginform, form[action*='mod=logging'][action*='action=login'], form[id*='login']").First()
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
		return "", "", fmt.Errorf("未找到百合会登录 formhash，请检查登录页结构或访问限制")
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
