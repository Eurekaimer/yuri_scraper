package sites

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestYamiboBrowserWAF(t *testing.T) {
	if os.Getenv("YURISCRAPER_BROWSER_TEST") != "1" {
		t.Skip("set YURISCRAPER_BROWSER_TEST=1 to exercise an installed browser")
	}
	if _, err := findYamiboBrowser(); err != nil {
		t.Skip("no supported browser installed")
	}

	t.Run("login and protected chapter", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(serveYamiboBrowserTestPage))
		t.Cleanup(server.Close)
		y := newYamiboBrowserTestSite(server)
		defer y.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := y.Login(ctx, " alice ", "secret"); err != nil {
			t.Fatal(err)
		}
		if !y.IsAuthenticated() {
			t.Fatal("authenticated browser session was not retained")
		}
		chapter, err := y.GetChapterContent(ctx, server.URL+"/forum.php?pid=42")
		if err != nil || !strings.Contains(chapter, "只有登录后才能读取") {
			t.Fatalf("protected chapter unavailable: %q, %v", chapter, err)
		}
		y.browser.close()
		if _, err := y.GetChapterContent(ctx, server.URL+"/forum.php?pid=42"); err == nil || y.IsAuthenticated() {
			t.Fatalf("closed browser was still authenticated: %v", err)
		}
	})

	t.Run("wrong password never authenticates", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(serveYamiboBrowserTestPage))
		t.Cleanup(server.Close)
		y := newYamiboBrowserTestSite(server)
		defer y.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := y.Login(ctx, "alice", "do-not-leak")
		if err == nil || y.IsAuthenticated() || strings.Contains(err.Error(), "do-not-leak") {
			t.Fatalf("invalid credentials accepted or leaked: %v", err)
		}
	})

	t.Run("parallel chapters overlap on authenticated tabs", func(t *testing.T) {
		state := newYamiboBrowserParallelServer()
		server := httptest.NewServer(state)
		t.Cleanup(func() {
			state.unblockAll()
			server.Close()
		})
		y := newYamiboBrowserTestSite(server)
		defer y.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := y.Login(ctx, "alice", "secret"); err != nil {
			t.Fatal(err)
		}

		type chapterResult struct {
			pid  string
			body string
			err  error
		}
		results := make(chan chapterResult, 2)
		for _, pid := range []string{"1", "2"} {
			go func() {
				body, err := y.GetChapterContent(ctx, fmt.Sprintf("%s/forum.php?pid=%s", server.URL, pid))
				results <- chapterResult{pid: pid, body: body, err: err}
			}()
		}
		chapters := map[string]string{}
		for range 2 {
			result := <-results
			if result.err != nil {
				t.Fatalf("chapter pid=%s failed: %v", result.pid, result.err)
			}
			chapters[result.pid] = result.body
		}
		// Both responses are only written once both requests reached the
		// server, so a serialized single-tab implementation cannot pass.
		if !strings.Contains(chapters["1"], "第一章 并行正文甲") ||
			!strings.Contains(chapters["2"], "第二章 并行正文乙") {
			t.Fatalf("parallel chapters returned wrong bodies: %q", chapters)
		}
		// The auth cookie is HttpOnly and was only ever set inside the
		// browser, so seeing it on both tabs proves they share the isolated
		// authenticated context instead of a copy.
		arrived := state.cookies()
		if arrived["1"] != "session" || arrived["2"] != "session" {
			t.Fatalf("pooled tabs did not carry the HttpOnly auth cookie: %v", arrived)
		}
		if timeouts := state.barrierTimeouts(); timeouts != 0 {
			t.Fatalf("chapter requests were serialized past the barrier %d time(s)", timeouts)
		}

		// Cancelling a navigation that is stuck on the server must return
		// promptly and leave the pooled browser usable.
		blocked := state.block("9")
		blockedCtx, blockedCancel := context.WithCancel(context.Background())
		blockedErr := make(chan error, 1)
		go func() {
			_, err := y.GetChapterContent(blockedCtx, server.URL+"/blocked.php?pid=9")
			blockedErr <- err
		}()
		select {
		case <-blocked.started:
		case <-time.After(20 * time.Second):
			t.Fatal("blocked chapter never reached the server")
		}
		blockedCancel()
		select {
		case err := <-blockedErr:
			if err == nil {
				t.Fatal("cancelled chapter reported success")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("cancelled chapter did not return")
		}
		blocked.unblock()
		followCtx, followCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer followCancel()
		if body, err := y.GetChapterContent(followCtx, server.URL+"/forum.php?pid=2"); err != nil ||
			!strings.Contains(body, "第二章 并行正文乙") {
			t.Fatalf("browser unusable after cancellation: %q, %v", body, err)
		}

		// Closing the browser while a navigation is in flight must release
		// the request instead of deadlocking on the tab pool.
		closing := state.block("10")
		closeErr := make(chan error, 1)
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		go func() {
			_, err := y.GetChapterContent(closeCtx, server.URL+"/blocked.php?pid=10")
			closeErr <- err
		}()
		select {
		case <-closing.started:
		case <-time.After(20 * time.Second):
			t.Fatal("chapter never reached the server before close")
		}
		if err := y.Close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
		select {
		case err := <-closeErr:
			if err == nil {
				t.Fatal("chapter reported success after the browser closed")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("closing the browser did not release the blocked chapter")
		}
		if y.IsAuthenticated() {
			t.Fatal("browser session still authenticated after close")
		}
		closing.unblock()
	})
}

func newYamiboBrowserTestSite(server *httptest.Server) *Yamibo {
	y := NewYamibo()
	y.baseURL = server.URL + "/"
	y.client = server.Client()
	y.browserHeadless = true
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	return y
}

func serveYamiboBrowserTestPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/member.php" {
		serveYamiboBrowserTestLogin(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := r.Cookie("discuz_auth"); err != nil {
		fmt.Fprint(w, `<form id="loginform"></form>`)
		return
	}
	fmt.Fprint(w, `<div id="postlist"><table><tr><td id="postmessage_42">第一章 正文<br>这是只有登录后才能读取的章节内容。</td></tr></table></div>`)
}

func serveYamiboBrowserTestLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodGet {
		if _, err := r.Cookie("nox_pass"); err != nil {
			w.Header().Set("Server", "BAIDU_WAF")
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprint(w, `<script>window.__noxExpire=1; document.cookie='nox_pass=1; path=/'; location.reload()</script>`)
			return
		}
		fmt.Fprint(w, `<form action="/member.php?mod=logging&amp;action=login" method="post"><input name="formhash" value="abc123"><input name="username"><input name="password" type="password"><button name="loginsubmit" type="submit">登录</button></form>`)
		return
	}
	_ = r.ParseForm()
	if r.Form.Get("username") == "alice" && r.Form.Get("password") == "secret" {
		http.SetCookie(w, &http.Cookie{Name: "discuz_auth", Value: "session", Path: "/", HttpOnly: true})
		fmt.Fprint(w, `<a href="/home.php?mod=space">alice</a>`)
		return
	}
	fmt.Fprint(w, `<div id="messagetext">登录失败</div>`)
}

// yamiboBrowserTestBlocker holds one protected navigation on the server until
// the test releases it or the request goes away.
type yamiboBrowserTestBlocker struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *yamiboBrowserTestBlocker) wait(r *http.Request) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
	case <-r.Context().Done():
	case <-time.After(30 * time.Second):
	}
}

func (b *yamiboBrowserTestBlocker) unblock() {
	b.once.Do(func() { close(b.release) })
}

// yamiboBrowserParallelServer serves two protected chapters and only answers
// them once both requests are in flight.
type yamiboBrowserParallelServer struct {
	mu       sync.Mutex
	arrived  map[string]string
	gated    bool
	gate     chan struct{}
	blocks   map[string]*yamiboBrowserTestBlocker
	barrierN int
}

func newYamiboBrowserParallelServer() *yamiboBrowserParallelServer {
	return &yamiboBrowserParallelServer{
		arrived: map[string]string{},
		gate:    make(chan struct{}),
		blocks:  map[string]*yamiboBrowserTestBlocker{},
	}
}

func (s *yamiboBrowserParallelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/member.php" {
		serveYamiboBrowserTestLogin(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cookie, err := r.Cookie("discuz_auth")
	if err != nil || cookie.Value != "session" {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `<form id="loginform"></form>`)
		return
	}

	pid := r.URL.Query().Get("pid")
	if r.URL.Path == "/blocked.php" {
		s.block(pid).wait(r)
		fmt.Fprintf(w, `<div id="postlist"><table><tr><td id="postmessage_%s">释放后的正文</td></tr></table></div>`, pid)
		return
	}

	body := map[string]string{
		"1": "第一章 并行正文甲：这是第一个标签页读取到的章节内容，用于验证并发加载。",
		"2": "第二章 并行正文乙：这是第二个标签页读取到的章节内容，用于验证并发加载。",
	}[pid]
	if body == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.Lock()
	s.arrived[pid] = cookie.Value
	if !s.gated && len(s.arrived) >= 2 {
		s.gated = true
		close(s.gate)
	}
	gate := s.gate
	s.mu.Unlock()

	select {
	case <-gate:
	case <-r.Context().Done():
		return
	case <-time.After(15 * time.Second):
		s.mu.Lock()
		s.barrierN++
		s.mu.Unlock()
		w.WriteHeader(http.StatusGatewayTimeout)
		fmt.Fprint(w, `<div id="postlist">parallel barrier timed out</div>`)
		return
	}
	fmt.Fprintf(w, `<div id="postlist"><table><tr><td id="postmessage_%s">%s</td></tr></table></div>`, pid, body)
}

func (s *yamiboBrowserParallelServer) block(pid string) *yamiboBrowserTestBlocker {
	s.mu.Lock()
	defer s.mu.Unlock()
	block := s.blocks[pid]
	if block == nil {
		block = &yamiboBrowserTestBlocker{started: make(chan struct{}, 1), release: make(chan struct{})}
		s.blocks[pid] = block
	}
	return block
}

func (s *yamiboBrowserParallelServer) cookies() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.arrived))
	for pid, value := range s.arrived {
		out[pid] = value
	}
	return out
}

func (s *yamiboBrowserParallelServer) barrierTimeouts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.barrierN
}

func (s *yamiboBrowserParallelServer) unblockAll() {
	s.mu.Lock()
	blocks := make([]*yamiboBrowserTestBlocker, 0, len(s.blocks))
	for _, block := range s.blocks {
		blocks = append(blocks, block)
	}
	s.mu.Unlock()
	for _, block := range blocks {
		block.unblock()
	}
}
