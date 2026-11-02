package sites

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"golang.org/x/time/rate"
)

const (
	yamiboBrowserLoginWait = 180 * time.Second
	yamiboBrowserAuthWait  = 180 * time.Second
	yamiboBrowserPageWait  = 90 * time.Second
	yamiboBrowserWAFWait   = 30 * time.Second
	yamiboBrowserStartWait = 60 * time.Second
	yamiboBrowserPoll      = 400 * time.Millisecond
)

const yamiboLoginFormSelector = `form[action*='mod=logging'][action*='action=login']`

// yamiboBrowser keeps one isolated browser context for a challenged Yamibo
// session. Pressing the login form and later page loads happen in that context
// so the site sees the same browser session; cookies are never copied out.
//
// The context is a dedicated incognito BrowserContext created through CDP, so
// the login tab and every page tab share it. Page requests run on background
// tabs of that context; each request leases one tab, so different pages can
// load at the same time without sharing a document.
type yamiboBrowser struct {
	tabCtx        context.Context
	tabCancel     context.CancelFunc
	browserCancel context.CancelFunc
	allocCancel   context.CancelFunc
	// mu serialises login against page requests while leaving page requests
	// free to run in parallel with each other.
	mu        sync.RWMutex
	closed    atomic.Bool
	closeOnce sync.Once

	slots chan struct{}
	done  chan struct{}
}

// yamiboBrowserTab is one background tab of the browser context. Only the
// goroutine that leased it runs actions on it, so it is never shared.
type yamiboBrowserTab struct {
	ctx    context.Context
	cancel context.CancelFunc
}

var errYamiboBrowserStartTimeout = errors.New("yamibo browser start timed out")

func yamiboBrowserError(message, guidance string, cause error) error {
	return &yamiboAuthError{message: message, guidance: guidance, cause: cause}
}

func newYamiboBrowser(ctx context.Context, headless bool) (*yamiboBrowser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, yamiboBrowserContextError(ctx)
	}
	execPath, err := findYamiboBrowser()
	if err != nil {
		return nil, yamiboBrowserError(
			"未找到可用于百合会安全验证的浏览器",
			"请安装 Google Chrome、Chromium 或 Microsoft Edge，然后重试。",
			err,
		)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(execPath),
		chromedp.Flag("disable-notifications", true),
	)
	if !headless {
		opts = append(opts, chromedp.VisibleWindow)
	}

	// The browser lives until close, not until the login or request context
	// that happened to create it.
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(func(string, ...any) {}),
		chromedp.WithErrorf(func(string, ...any) {}),
	)
	b := &yamiboBrowser{
		browserCancel: browserCancel,
		allocCancel:   allocCancel,
		slots:         make(chan struct{}, yamiboMaxConcurrency),
		done:          make(chan struct{}),
	}
	deadline := time.Now().Add(yamiboBrowserStartWait)

	if err := startYamiboBrowser(ctx, browserCtx, deadline); err != nil {
		b.close()
		return nil, yamiboBrowserStartFailure(ctx, err)
	}

	// The login tab and every page tab live in one dedicated incognito
	// BrowserContext. New tabs written by the browser under --incognito do not
	// share that window's context, so the context is created explicitly and
	// every child context inherits its ID. A new BrowserContext has no window
	// yet, so its first tab opens one.
	tabCtx, tabCancel := chromedp.NewContext(browserCtx,
		chromedp.WithNewBrowserContext(),
		chromedp.WithNewWindow(false),
	)
	b.tabCtx, b.tabCancel = tabCtx, tabCancel
	if err := startYamiboBrowser(ctx, tabCtx, deadline); err != nil {
		b.close()
		return nil, yamiboBrowserStartFailure(ctx, err)
	}
	if !headless {
		// The session has its own window. Close the blank starter tab so the
		// user sees one window, and so closing that window ends the browser
		// and all in-flight page requests with it.
		if info := chromedp.FromContext(browserCtx); info != nil && info.Target != nil {
			closeCtx, cancelClose := context.WithDeadline(browserCtx, deadline)
			_, _ = chromedp.CallBrowser(closeCtx, target.CloseTarget, target.CloseTargetParams{TargetID: info.Target.TargetID})
			cancelClose()
		}
	}
	if ctx.Err() != nil {
		err := yamiboBrowserContextError(ctx)
		b.close()
		return nil, err
	}
	return b, nil
}

// startYamiboBrowser runs the first action of a context and waits for it, so
// a hanging browser start cannot outlive the caller's context.
func startYamiboBrowser(ctx, runCtx context.Context, deadline time.Time) error {
	done := make(chan error, 1)
	go func() { done <- chromedp.Do(runCtx) }()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errYamiboBrowserStartTimeout
	}
}

func yamiboBrowserStartFailure(ctx context.Context, err error) error {
	if errors.Is(err, chromedp.ErrNoDisplay) {
		return yamiboBrowserError(
			"当前环境没有图形桌面，无法打开网站验证浏览器",
			"请在图形桌面会话中运行，或安装并配置可用的浏览器后重试。",
			err,
		)
	}
	if ctx.Err() != nil {
		return yamiboBrowserContextError(ctx)
	}
	if errors.Is(err, errYamiboBrowserStartTimeout) {
		return yamiboBrowserError(
			"启动用于百合会安全验证的浏览器超时",
			"请确认本机浏览器可以正常打开，然后重试。",
			nil,
		)
	}
	return yamiboBrowserError(
		"无法启动用于百合会安全验证的浏览器",
		"请确认本机浏览器可以正常打开，然后重试。",
		err,
	)
}

func (b *yamiboBrowser) close() {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		close(b.done)
		// Cancelling the session context closes the login tab and every page
		// tab that is still open.
		if b.tabCancel != nil {
			// This also disposes the incognito BrowserContext.
			b.tabCancel()
		}
		if b.browserCancel != nil {
			// CancelWait reaps the browser process; allocCancel then removes
			// its isolated temporary profile.
			b.browserCancel()
		}
		b.allocCancel()
	})
}

func (b *yamiboBrowser) login(ctx context.Context, baseURL, username, password string) (err error) {
	defer func() {
		if err != nil {
			b.close()
		}
	}()
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return yamiboBrowserError("百合会账号和密码不能为空", "请输入完整的账号和密码。", nil)
	}
	loginURL := resolveYamiboURL(baseURL, yamiboLoginPath)
	if !sameYamiboOrigin(baseURL, loginURL) {
		return yamiboBrowserError("百合会登录页面的地址异常", "请检查网站地址后重试。", nil)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed.Load() {
		return yamiboBrowserError("百合会浏览器会话已关闭", "请重新登录。", nil)
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return yamiboBrowserError("百合会登录超时", "请检查网络连接后重试。", ctx.Err())
		}
		return yamiboBrowserError("百合会登录已取消", "请重新点击登录；如需继续，请再次输入账号密码。", ctx.Err())
	}

	formCtx, cancelForm := b.operationContext(ctx, yamiboBrowserLoginWait)
	defer cancelForm()
	if _, err := chromedp.Run(formCtx, chromedp.NavigateResponse(loginURL)); err != nil {
		if state := b.loginWaitState(ctx, formCtx,
			"等待百合会登录页面超时",
			"请检查网络连接后重试。",
		); state != nil {
			return state
		}
		var loadErr *chromedp.LoadError
		if !errors.As(err, &loadErr) || !strings.Contains(loadErr.ErrorText, "ERR_ABORTED") {
			return yamiboBrowserError("无法打开百合会登录页面", "请检查网络连接后重试。", err)
		}
	}

	form, err := b.waitLoginForm(ctx, formCtx, baseURL)
	if err != nil {
		return err
	}
	if !sameYamiboOrigin(baseURL, form.Page) || !sameYamiboOrigin(baseURL, form.Action) {
		return yamiboBrowserError(
			"百合会登录页面的跳转地址不在预期站点",
			"为防止账号信息泄露，登录已停止；请检查网络或代理设置后重试。",
			nil,
		)
	}

	usernameSelector := yamiboLoginFormSelector + " input[name='username']"
	passwordSelector := yamiboLoginFormSelector + " input[name='password']"
	submitSelector := yamiboLoginFormSelector + " button[name='loginsubmit'], " +
		yamiboLoginFormSelector + " input[name='loginsubmit'], " +
		yamiboLoginFormSelector + " button[type='submit'], " +
		yamiboLoginFormSelector + " input[type='submit']"
	if err := chromedp.Do(formCtx,
		chromedp.Clear(usernameSelector),
		chromedp.Clear(passwordSelector),
		chromedp.SendKeys(usernameSelector, username),
		chromedp.SendKeys(passwordSelector, password),
		chromedp.Click(submitSelector),
	); err != nil {
		if state := b.loginWaitState(ctx, formCtx,
			"填写百合会登录表单超时",
			"请重新登录；如果网站页面已变化，请稍后重试。",
		); state != nil {
			return state
		}
		// Never attach browser input errors here: they may echo submitted
		// values.
		return yamiboBrowserError(
			"无法填写百合会登录表单",
			"请重新登录；如果网站页面已变化，请稍后重试。",
			nil,
		)
	}
	cancelForm()

	authCtx, cancelAuth := b.operationContext(ctx, yamiboBrowserAuthWait)
	defer cancelAuth()
	cookieURLs := []string{
		baseURL,
		loginURL,
		resolveYamiboURL(baseURL, "forum.php"),
		resolveYamiboURL(baseURL, "home.php?mod=space"),
	}
	host := yamiboBrowserHost(baseURL)
	for {
		if state := b.loginWaitState(ctx, authCtx,
			"等待百合会登录结果超时",
			"请确认账号密码正确；如果网站要求验证码或安全提问，请在浏览器窗口中完成后重试。",
		); state != nil {
			return state
		}
		if result, callErr := chromedp.Call(authCtx, network.GetCookies, network.GetCookiesParams{URLs: cookieURLs}); callErr == nil {
			if yamiboBrowserHasAuthCookie(result.Cookies, host) {
				return nil
			}
		}
		if yamiboBrowserPageShowsUser(authCtx, username) {
			return nil
		}
		if yamiboBrowserLoginRejected(authCtx) {
			return yamiboBrowserError(
				"账号密码可能有误，或网站要求验证码或安全提问",
				"请检查账号密码，按网站要求完成验证后重试。",
				nil,
			)
		}
		if sleepErr := sleepYamiboBrowser(authCtx, yamiboBrowserPoll); sleepErr != nil {
			continue
		}
	}
}

func (b *yamiboBrowser) document(ctx context.Context, baseURL, rawURL string, limiter *rate.Limiter) ([]byte, error) {
	if !sameYamiboOrigin(baseURL, rawURL) {
		return nil, yamiboBrowserError(
			"百合会页面地址不在允许的站点范围内",
			"为防止账号信息泄露，请求已停止；请返回来源页重试。",
			nil,
		)
	}

	// Login owns the whole browser while it runs. Page requests only take the
	// shared side of the lock, so different chapters can load at the same
	// time, each on its own tab.
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed.Load() {
		return nil, yamiboBrowserError("百合会浏览器会话已关闭", "请重新输入账号密码登录。", nil)
	}

	waitCtx, cancelWait := b.operationContext(ctx, yamiboBrowserPageWait)
	defer cancelWait()
	if limiter != nil {
		if err := limiter.Wait(waitCtx); err != nil {
			return nil, b.documentError(ctx, waitCtx, err)
		}
	}

	tab, err := b.leaseTab(waitCtx)
	if err != nil {
		return nil, b.documentError(ctx, waitCtx, err)
	}
	// A tab whose target died (crash, closed window) cannot be reused.
	tabBroken := false
	defer func() {
		if tabBroken {
			b.dropTab(tab)
			return
		}
		b.releaseTab(tab)
	}()

	pageCtx, cancel := yamiboBrowserLinkContext(tab.ctx, waitCtx)
	defer cancel()

	response, err := chromedp.Run(pageCtx, chromedp.NavigateResponse(rawURL))
	if err != nil {
		// A canceled navigation can leave the renderer loading a request that
		// will never finish. Abort it so the tab stays usable; the shared
		// renderer process would otherwise stall later same-origin pages.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			b.stopTab(tab)
		} else {
			tabBroken = true
		}
		return nil, b.documentError(ctx, pageCtx, err)
	}
	status := http.StatusOK
	headers := http.Header{}
	if response != nil {
		status = int(response.Status)
		headers = yamiboNetworkHeaders(response.Headers)
		if response.URL != "" && !sameYamiboOrigin(baseURL, response.URL) {
			return nil, yamiboBrowserError(
				"百合会页面跳转到了其他网站",
				"为防止账号信息泄露，请求已停止；请稍后重试或联系网站管理员。",
				nil,
			)
		}
	}

	body, err := b.pageHTML(pageCtx)
	if err != nil {
		tabBroken = true
		return nil, b.documentError(ctx, pageCtx, err)
	}
	if location, locErr := chromedp.Run(pageCtx, chromedp.Location()); locErr == nil && !sameYamiboOrigin(baseURL, location) {
		return nil, yamiboBrowserError(
			"百合会页面跳转到了其他网站",
			"为防止账号信息泄露，请求已停止；请稍后重试或联系网站管理员。",
			nil,
		)
	}

	clearedChallenge := false
	if isYamiboWAF(status, headers, body) {
		// A verification challenge can need a human. Bring the background tab
		// that hit it to the front of the shared window.
		b.revealTab(pageCtx)
		original := string(body)
		deadline := time.Now().Add(yamiboBrowserWAFWait)
		for {
			if err := sleepYamiboBrowser(pageCtx, yamiboBrowserPoll); err != nil {
				return nil, b.documentError(ctx, pageCtx, err)
			}
			current, htmlErr := b.pageHTML(pageCtx)
			if htmlErr == nil {
				body = current
				if !isYamiboWAF(http.StatusOK, nil, body) &&
					(string(body) != original || status == http.StatusOK) {
					clearedChallenge = true
					break
				}
			}
			if time.Now().After(deadline) {
				return nil, yamiboBrowserError(
					"百合会的网站安全验证拦截了页面请求",
					"请在打开的浏览器窗口完成验证后重试；如仍被拦截，请稍后重试或联系网站管理员。",
					nil,
				)
			}
		}
	}

	if !clearedChallenge && (status < 200 || status >= 300) {
		return nil, yamiboStatusError(status, headers)
	}
	if len(body) == 0 {
		return nil, yamiboBrowserError(
			"百合会页面内容为空",
			"请稍后重试，或重新登录后再试。",
			nil,
		)
	}
	if clearedChallenge {
		if location, locErr := chromedp.Run(pageCtx, chromedp.Location()); locErr == nil && !sameYamiboOrigin(baseURL, location) {
			return nil, yamiboBrowserError(
				"百合会页面跳转到了其他网站",
				"为防止账号信息泄露，请求已停止；请稍后重试或联系网站管理员。",
				nil,
			)
		}
	}
	return body, nil
}

// leaseTab reserves one pool slot and creates a page tab in the shared
// browser context. A tab serves exactly one page: Chrome throttles background
// tabs, and a reused tab can then stop delivering load events.
func (b *yamiboBrowser) leaseTab(ctx context.Context) (*yamiboBrowserTab, error) {
	select {
	case <-b.done:
		return nil, yamiboBrowserError("百合会浏览器会话已关闭", "请重新输入账号密码登录。", nil)
	case <-ctx.Done():
		return nil, ctx.Err()
	case b.slots <- struct{}{}:
	}
	if b.closed.Load() {
		b.returnSlot()
		return nil, yamiboBrowserError("百合会浏览器会话已关闭", "请重新输入账号密码登录。", nil)
	}
	if err := b.tabCtx.Err(); err != nil {
		b.returnSlot()
		return nil, yamiboBrowserError("浏览器窗口已关闭", "请重新输入账号密码登录。", err)
	}
	// A child context inherits the browser context ID of the login tab, so the
	// site sees the same authenticated incognito session without any cookie
	// being copied. WithNewWindow(false) keeps these tabs in the existing
	// window instead of creating one window per request.
	tabCtx, tabCancel := chromedp.NewContext(b.tabCtx, chromedp.WithNewWindow(false))
	return &yamiboBrowserTab{ctx: tabCtx, cancel: tabCancel}, nil
}

func (b *yamiboBrowser) releaseTab(tab *yamiboBrowserTab) {
	if tab != nil {
		tab.cancel()
	}
	b.returnSlot()
}

// stopTab aborts a pending load on one tab so its renderer is not left waiting
// for a request the caller already abandoned.
func (b *yamiboBrowser) stopTab(tab *yamiboBrowserTab) {
	if tab == nil || tab.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(tab.ctx, 5*time.Second)
	defer cancel()
	_, _ = chromedp.Run(ctx, chromedp.Stop())
}

// dropTab closes a tab that failed in a way that may have killed its target,
// and returns the pool slot without making the tab available again.
func (b *yamiboBrowser) dropTab(tab *yamiboBrowserTab) {
	if tab == nil {
		return
	}
	tab.cancel()
	b.returnSlot()
}

func (b *yamiboBrowser) returnSlot() {
	b.slots <- struct{}{}
}

// revealTab brings a pooled tab to the front so the user can finish an
// interactive verification. A failure only means the window could not be
// focused; the page itself keeps working.
func (b *yamiboBrowser) revealTab(ctx context.Context) {
	info := chromedp.FromContext(ctx)
	if info == nil || info.Target == nil {
		return
	}
	_, _ = chromedp.CallBrowser(ctx, target.ActivateTarget, target.ActivateTargetParams{TargetID: info.Target.TargetID})
}

// yamiboBrowserLinkContext ties an operation on one tab to the request
// context that governs it.
func yamiboBrowserLinkContext(base, parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(base)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

type yamiboLoginForm struct {
	Page   string `json:"page"`
	Action string `json:"action"`
}

func (b *yamiboBrowser) waitLoginForm(ctx, formCtx context.Context, baseURL string) (*yamiboLoginForm, error) {
	for {
		if state := b.loginWaitState(ctx, formCtx,
			"等待百合会登录表单超时",
			"请检查网络连接，或重新登录并在浏览器中完成网站验证。",
		); state != nil {
			return nil, state
		}
		form, err := chromedp.Run(formCtx, chromedp.Evaluate[*yamiboLoginForm](`(() => {
			const form = document.querySelector("form[action*='mod=logging'][action*='action=login']");
			if (!form) return null;
			if (!form.querySelector("input[name='username']") || !form.querySelector("input[name='password']")) return null;
			return {page: location.href, action: form.action || ""};
		})()`))
		if err == nil && form != nil {
			return form, nil
		}
		if sleepErr := sleepYamiboBrowser(formCtx, yamiboBrowserPoll); sleepErr != nil {
			continue
		}
	}
}

func (b *yamiboBrowser) pageHTML(ctx context.Context) ([]byte, error) {
	html, err := chromedp.Run(ctx, chromedp.Evaluate[string](
		`document.documentElement ? document.documentElement.outerHTML : ""`,
	))
	if err != nil {
		return nil, err
	}
	return []byte(html), nil
}

func (b *yamiboBrowser) operationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	opCtx, cancel := context.WithTimeout(b.tabCtx, timeout)
	stop := context.AfterFunc(ctx, cancel)
	return opCtx, func() {
		stop()
		cancel()
	}
}

func (b *yamiboBrowser) loginWaitState(ctx, waitCtx context.Context, timeoutMessage, timeoutGuidance string) error {
	switch {
	case b.closed.Load():
		return yamiboBrowserError("百合会浏览器会话已关闭", "请重新登录。", nil)
	case ctx != nil && ctx.Err() != nil:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return yamiboBrowserError(timeoutMessage, timeoutGuidance, ctx.Err())
		}
		return yamiboBrowserError("百合会登录已取消", "请重新点击登录；如需继续，请再次输入账号密码。", ctx.Err())
	case b.tabCtx.Err() != nil:
		return yamiboBrowserError("浏览器窗口已关闭", "请重新登录，并在新窗口中完成网站验证。", nil)
	case errors.Is(waitCtx.Err(), context.DeadlineExceeded):
		return yamiboBrowserError(timeoutMessage, timeoutGuidance, waitCtx.Err())
	}
	return nil
}

func (b *yamiboBrowser) documentError(ctx, pageCtx context.Context, cause error) error {
	switch {
	case b.closed.Load():
		return yamiboBrowserError("百合会浏览器会话已关闭", "请重新输入账号密码登录。", cause)
	case ctx != nil && ctx.Err() != nil:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return yamiboBrowserError("访问百合会页面超时", "请检查网络连接后重试。", cause)
		}
		return yamiboBrowserError("百合会页面请求已取消", "需要时请重试。", cause)
	case b.tabCtx.Err() != nil:
		return yamiboBrowserError("浏览器窗口已关闭", "请重新输入账号密码登录。", cause)
	case errors.Is(pageCtx.Err(), context.DeadlineExceeded) || errors.Is(cause, context.DeadlineExceeded):
		return yamiboBrowserError("访问百合会页面超时", "请检查网络连接后重试。", cause)
	case errors.Is(cause, context.Canceled):
		return yamiboBrowserError("百合会页面请求已取消", "需要时请重试。", cause)
	default:
		return yamiboBrowserError("无法通过浏览器打开百合会页面", "请检查网络连接，或重新登录后重试。", cause)
	}
}

func yamiboBrowserContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return yamiboBrowserError("启动百合会验证浏览器超时", "请稍后重试。", ctx.Err())
	}
	return yamiboBrowserError("百合会浏览器启动已取消", "需要时请重新登录。", ctx.Err())
}

func yamiboBrowserPageShowsUser(ctx context.Context, username string) bool {
	if username == "" {
		return false
	}
	encoded, _ := json.Marshal(username)
	shown, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const name = `+string(encoded)+`;
		if (!document.body || /[?&]mod=logging|[?&]action=login/.test(location.href)) return false;
		return [...document.querySelectorAll("a[href*='mod=space'], a[href*='space-uid']")]
			.some(link => (link.textContent || "").includes(name));
	})()`))
	return err == nil && shown
}

func yamiboBrowserLoginRejected(ctx context.Context) bool {
	rejected, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const text = document.querySelector("#messagetext")?.innerText || "";
		return /登录失败|密码错误|密码不正确|用户名或密码/.test(text);
	})()`))
	return err == nil && rejected
}

func yamiboBrowserHasAuthCookie(cookies []*network.Cookie, host string) bool {
	for _, cookie := range cookies {
		if cookie == nil || cookie.Value == "" {
			continue
		}
		name := strings.ToLower(cookie.Name)
		if name != "auth" && !(strings.HasSuffix(name, "_auth") && len(name) > len("_auth")) {
			continue
		}
		if strings.Contains(name, "nox") {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		if host == "" || domain == "" || domain == host || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func yamiboBrowserHost(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func yamiboNetworkHeaders(headers network.Headers) http.Header {
	out := make(http.Header, len(headers))
	for name, value := range headers {
		switch typed := value.(type) {
		case string:
			out.Set(name, typed)
		case []string:
			for _, item := range typed {
				out.Add(name, item)
			}
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					out.Add(name, text)
				}
			}
		}
	}
	return out
}

func sleepYamiboBrowser(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func findYamiboBrowser() (string, error) {
	candidates := []string{
		"google-chrome-stable",
		"google-chrome",
		"chromium",
		"chromium-browser",
		"microsoft-edge-stable",
		"microsoft-edge",
		"chrome",
		"msedge",
	}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		)
	case "windows":
		for _, dir := range []string{
			os.Getenv("PROGRAMFILES"),
			os.Getenv("PROGRAMFILES(X86)"),
			os.Getenv("LOCALAPPDATA"),
		} {
			if dir == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(dir, "Chromium", "Application", "chrome.exe"),
				filepath.Join(dir, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
	default:
		candidates = append(candidates,
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			"/opt/google/chrome/chrome",
		)
	}

	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("no Chrome, Chromium, or Edge executable found")
}
