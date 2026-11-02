package sites

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/transport"
	"golang.org/x/time/rate"
)

func TestYamiboLoginWithPasswordEstablishesSession(t *testing.T) {
	var submitted url.Values
	var parseErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch {
		case r.URL.Path == "/member.php" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`<form id="loginform" action="member.php?mod=logging&amp;action=login&amp;loginsubmit=yes"><input name="formhash" value="abc123"></form>`))
		case r.URL.Path == "/member.php" && r.Method == http.MethodPost:
			parseErr = r.ParseForm()
			submitted = r.PostForm
			if submitted.Get("username") == "alice" && submitted.Get("password") == "secret" && submitted.Get("formhash") == "abc123" {
				http.SetCookie(w, &http.Cookie{Name: "discuz_auth", Value: "session", Path: "/"})
			}
			_, _ = w.Write([]byte("ok"))
		case r.URL.Path == "/home.php":
			_, _ = w.Write([]byte("alice"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	y := NewYamibo()
	y.baseURL = server.URL + "/"
	y.client = server.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	if err := y.Login(context.Background(), " alice ", "secret"); err != nil {
		t.Fatal(err)
	}
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if !y.IsAuthenticated() || submitted.Get("loginfield") != "username" || submitted.Get("questionid") != "0" {
		t.Fatalf("login session or form fields were not established: %#v", submitted)
	}
}

func TestYamiboLoginRejectsInvalidCredentialsWithoutLeakingPassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/member.php" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`<form id="loginform" action="/member.php?mod=logging&amp;action=login"><input name="formhash" value="abc123"></form>`))
			return
		}
		_, _ = w.Write([]byte(`<form id="loginform"><input name="username"></form>`))
	}))
	defer server.Close()

	y := NewYamibo()
	y.baseURL = server.URL + "/"
	y.client = server.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	err := y.Login(context.Background(), "alice", "do-not-leak")
	if err == nil || y.IsAuthenticated() {
		t.Fatalf("invalid credentials were accepted: %v", err)
	}
	if strings.Contains(err.Error(), "alice") || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("credentials leaked into error: %v", err)
	}
}

func TestYamiboLoginFormMissing(t *testing.T) {
	_, _, err := parseYamiboLoginForm("https://example.test/", []byte(`<p>页面无法访问</p>`))
	var guidance interface{ Guidance() string }
	if err == nil || !strings.Contains(err.Error(), "登录页面") || !errors.As(err, &guidance) || guidance.Guidance() == "" {
		t.Fatalf("missing actionable login error: %v", err)
	}
}

func TestYamiboAuthDetectsChallengeAndOrdinary405(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		server    string
		body      string
		challenge bool
	}{
		{"waf status", 405, "BAIDU_WAF", "blocked", true},
		{"waf page", 200, "", `<script>window.__noxExpire=1;var nox_cookie=1</script>`, true},
		{"ordinary 405", 405, "", "method not allowed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Server", tc.server)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			y := NewYamibo()
			y.limiter = rate.NewLimiter(rate.Inf, 1)
			_, err := y.authRequest(context.Background(), s.Client(), http.MethodGet, s.URL, nil, "")
			if errors.Is(err, errYamiboWAF) != tc.challenge {
				t.Fatalf("challenge detection = %v, error %v", errors.Is(err, errYamiboWAF), err)
			}
			if !tc.challenge && (err == nil || strings.Contains(err.Error(), "405")) {
				t.Fatalf("ordinary 405 should have a Chinese explanation: %v", err)
			}
		})
	}
}

func TestYamiboOldRequestCannotInvalidateNewSession(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`<form id="loginform"></form>`))
	}))
	defer s.Close()
	y := NewYamibo()
	y.baseURL, y.client = s.URL+"/", s.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	y.SetCookie("old=session")
	done := make(chan error, 1)
	go func() {
		_, err := y.document(context.Background(), s.URL)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("old request did not start")
	}
	y.SetCookie("new=session")
	close(release)
	if err := <-done; err == nil {
		t.Fatal("old expired session unexpectedly succeeded")
	}
	if !y.IsAuthenticated() {
		t.Fatal("old request invalidated the new session")
	}
}

func TestYamiboLoginOrdinary405IsNotCalledWAF(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer s.Close()
	y := NewYamibo()
	y.baseURL, y.client = s.URL+"/", s.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	err := y.Login(context.Background(), "alice", "secret")
	var guided interface{ Guidance() string }
	if err == nil || !strings.Contains(err.Error(), "请求方式") || strings.Contains(err.Error(), "405") ||
		strings.Contains(err.Error(), "secret") || !errors.As(err, &guided) || guided.Guidance() == "" {
		t.Fatalf("ordinary 405 was misreported: %v", err)
	}
}

func TestYamiboCloseCancelsPendingLogin(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer s.Close()
	y := NewYamibo()
	y.baseURL, y.client = s.URL+"/", s.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	result := make(chan error, 1)
	go func() { result <- y.Login(context.Background(), "alice", "secret") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("login did not start")
	}
	if err := y.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) || y.IsAuthenticated() {
		t.Fatalf("pending login was not canceled: %v", err)
	}
}

func TestYamiboRateLimitKeepsRetryGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	y := NewYamibo()
	y.baseURL, y.client = server.URL+"/", server.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	y.SetCookie("session=secret")
	_, err := y.document(context.Background(), server.URL)
	if err == nil || !transport.CanRetry(err) || transport.RetryAfter(err) != 3*time.Second {
		t.Fatalf("rate-limit retry guidance was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "频繁") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("rate limit error is not safe and actionable: %v", err)
	}
}

func TestYamiboConfiguredSpeedCeiling(t *testing.T) {
	for _, tc := range []struct {
		workers int64
		rate    float64
	}{
		{2, 1.2},
		{12, 10},
	} {
		y := NewYamiboWithPolicy(tc.workers, tc.rate)
		if y.MaxConcurrency() != tc.workers || y.MaxRequestsPerSecond() != tc.rate {
			t.Fatalf("configured ceiling not honored: %d workers, %g requests/s", y.MaxConcurrency(), y.MaxRequestsPerSecond())
		}
	}
}
