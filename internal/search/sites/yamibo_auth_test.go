package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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

func TestParseYamiboLoginFormRequiresFormhash(t *testing.T) {
	_, _, err := parseYamiboLoginForm("https://example.test/", []byte(`<form id="loginform"></form>`))
	if err == nil || !strings.Contains(err.Error(), "formhash") {
		t.Fatalf("unexpected error: %v", err)
	}
}
