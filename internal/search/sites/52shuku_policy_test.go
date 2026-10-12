package sites

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/transport"
)

func fixture52(t *testing.T, server *httptest.Server) *Site52Shuku {
	t.Helper()
	source := New52Shuku()
	if err := source.SetBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.client.CloseIdleConnections)
	return source
}

func Test52StatusErrorsCarryRetryGuidance(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(status)
			}))
			defer server.Close()
			source := fixture52(t, server)
			_, err := source.GetChapterContent(context.Background(), server.URL+"/chapter")
			var statusError *transport.HTTPError
			if !errors.As(err, &statusError) || statusError.StatusCode != status || statusError.RetryAfter() != 2*time.Second {
				t.Fatalf("status guidance was lost: %v", err)
			}
			wantRetry := status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable
			if transport.CanRetry(err) != wantRetry {
				t.Fatalf("HTTP %d retry decision = %v", status, transport.CanRetry(err))
			}
		})
	}
}

func Test52ParserFailuresAreNotRetried(t *testing.T) {
	for _, content := range []string{"<html>login page</html>", `<article class="article-content" id="nr1"><p>目录 上一页 下一页</p></article>`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, content) }))
		source := fixture52(t, server)
		_, err := source.GetChapterContent(context.Background(), server.URL+"/chapter")
		server.Close()
		var permanent *transport.PermanentError
		if !errors.As(err, &permanent) || transport.CanRetry(err) {
			t.Fatalf("parser failure was retryable: %v", err)
		}
	}
}

func Test52AllRequestKindsFollowConfiguredBudget(t *testing.T) {
	starts := make(chan time.Time, 3)
	var active, maximum atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
		}
		starts <- time.Now()
		<-r.Context().Done()
	}))
	defer server.Close()
	// A conservative configured budget must survive construction unchanged.
	source := New52ShukuWithPolicy(2, 1.0)
	if err := source.SetBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	defer source.client.CloseIdleConnections()
	if source.MaxConcurrency() != 2 || source.MaxRequestsPerSecond() != 1.0 {
		t.Fatalf("configured budget was replaced: %d workers, %g req/s", source.MaxConcurrency(), source.MaxRequestsPerSecond())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var workers sync.WaitGroup
	for _, request := range []func(){
		func() { _, _ = source.Search(ctx, "query") },
		func() { _, _ = source.GetBookMeta(ctx, server.URL+"/book") },
		func() { _, _ = source.GetChapterContent(ctx, server.URL+"/chapter") },
	} {
		workers.Add(1)
		go func() { defer workers.Done(); request() }()
	}
	var first, second time.Time
	select {
	case first = <-starts:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	select {
	case second = <-starts:
	case <-time.After(4 * time.Second):
		t.Fatal("second request did not start")
	}
	if gap := second.Sub(first); gap < 900*time.Millisecond {
		t.Fatalf("configured request rate was exceeded: %v", gap)
	}
	select {
	case <-starts:
		t.Fatal("third request exceeded the configured two-connection cap")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled request/slot wait did not finish")
	}
	if maximum.Load() > 2 {
		t.Fatalf("source opened %d concurrent requests", maximum.Load())
	}
}

func Test52RequestBudgetIsClampedOnlyToSanityCeiling(t *testing.T) {
	source := New52ShukuWithPolicy(1000, 1000)
	defer source.client.CloseIdleConnections()
	if source.MaxConcurrency() != shukuMaxConcurrency || source.MaxRequestsPerSecond() != shukuMaxRPS {
		t.Fatalf("sanity ceiling not applied: %d workers, %g req/s", source.MaxConcurrency(), source.MaxRequestsPerSecond())
	}
	if fallback := New52ShukuWithPolicy(0, 0); fallback.MaxConcurrency() != shukuDefaultConcurrency || fallback.MaxRequestsPerSecond() != shukuDefaultRPS {
		t.Fatalf("zero configuration did not fall back to defaults: %d workers, %g req/s", fallback.MaxConcurrency(), fallback.MaxRequestsPerSecond())
	}
	// New52Shuku must stay aligned with store.DefaultConfig so a YAML default
	// download actually uses the configured budget.
	if defaults := New52Shuku(); defaults.MaxConcurrency() != 8 || defaults.MaxRequestsPerSecond() != 2.0 {
		t.Fatalf("New52Shuku diverged from the documented default: %d workers, %g req/s", defaults.MaxConcurrency(), defaults.MaxRequestsPerSecond())
	}
}

func Test52RedirectHopsUseRateBudget(t *testing.T) {
	starts := make(chan time.Time, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts <- time.Now()
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/actual", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<article class="article-content" id="nr1"><p>第1章 Start</p><p>first &amp; second</p></article>`)
	}))
	defer server.Close()
	source := fixture52(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	content, err := source.GetChapterContent(ctx, server.URL+"/start")
	if err != nil {
		t.Fatal(err)
	}
	first, second := <-starts, <-starts
	if gap := second.Sub(first); gap < 450*time.Millisecond {
		t.Fatalf("redirect bypassed request budget: %v", gap)
	}
	if content != "第1章 Start\nfirst & second" {
		t.Fatalf("redirect content was not cleaned: %q", content)
	}
}

func Test52RejectsForeignOriginAndRemoteFixtureOverrides(t *testing.T) {
	var contacted atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Add(1) }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/target", http.StatusFound)
	}))
	defer server.Close()
	source := fixture52(t, server)
	_, err := source.GetChapterContent(context.Background(), server.URL+"/redirect")
	var permanent *transport.PermanentError
	if !errors.As(err, &permanent) || contacted.Load() != 0 {
		t.Fatalf("foreign redirect escaped origin guard: %v, requests %d", err, contacted.Load())
	}
	for _, override := range []string{"https://localhost", "http://192.0.2.1", "http://127.0.0.1/path", "http://localhost?query=1", "http://user@localhost"} {
		if err := source.SetBaseURL(override); err == nil {
			t.Fatalf("accepted unsafe fixture origin %q", override)
		}
	}
}

func Test52ChapterContentDropsDuplicatedSynopsis(t *testing.T) {
	// The first reading page repeats the book blurb before the story. It must not
	// become chapter text, while a page that genuinely starts with prose keeps it.
	page := `<article class="article-content">` +
		`<p>　　[ＧＬ百合] 《怦然为你（gl）》作者：闵然【完结】</p>` +
		`<p>　　文案：</p>` +
		`<p>　　时懿的人生，从笔直的高岭之花到弯成回形针。</p>` +
		`<p>　　一句话简介：温水煮女神。</p>` +
		`<p>　　立意：宽容，是对自己最大的善待。</p>` +
		`<p>　　第1章 </p>` +
		`<p>　　凌晨十二点十分，商业街已经进入了沉睡状态。</p>` +
		`</article>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	}))
	defer server.Close()
	content, err := fixture52(t, server).GetChapterContent(context.Background(), server.URL+"/gl/h1R2_2.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "高岭之花") || strings.Contains(content, "一句话简介") {
		t.Fatalf("duplicated synopsis leaked into the chapter body: %q", content)
	}
	if !strings.Contains(content, "第1章") || !strings.Contains(content, "商业街已经进入了沉睡状态") {
		t.Fatalf("story text was removed with the synopsis: %q", content)
	}

	prose := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<article class="article-content"><p>第5章</p><p>她推开门，风灌了进来。</p></article>`)
	}))
	defer prose.Close()
	content, err = fixture52(t, prose).GetChapterContent(context.Background(), prose.URL+"/gl/h1R2_6.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "她推开门") {
		t.Fatalf("ordinary chapter content was dropped: %q", content)
	}
}

func Test52ChapterContentAcceptsLiveMarkupWithoutID(t *testing.T) {
	// Live reading pages carry <article class="article-content"> with no id;
	// requiring id="nr1" made every real chapter page fail extraction.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<article class="article-content" ><script>var ad=1;</script><p>第1章</p><p>凌晨十二点十分，商业街已经沉睡。</p></article>`)
	}))
	defer server.Close()
	source := fixture52(t, server)
	content, err := source.GetChapterContent(context.Background(), server.URL+"/gl/h1R2_2.html")
	if err != nil {
		t.Fatalf("live article markup was rejected: %v", err)
	}
	if !strings.Contains(content, "第1章") || !strings.Contains(content, "商业街已经沉睡") {
		t.Fatalf("chapter body was not extracted: %q", content)
	}
	if strings.Contains(content, "var ad") {
		t.Fatalf("inline script leaked into the chapter body: %q", content)
	}
}

func Test52CatalogLabelsPageEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ul class="list clearfix">
<li class="mulu"><a href="/gl/h1R2_2.html">第1页</a></li>
<li class="mulu"><a href="/gl/h1R2_3.html">第2页</a></li>
<li class="mulu"><a href="/gl/h1R2_4.html">第一章 相遇</a></li></ul>`)
	}))
	defer server.Close()
	source := fixture52(t, server)
	chapters, err := source.GetChapterList(context.Background(), server.URL+"/gl/h1R2.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 3 {
		t.Fatalf("expected three catalog entries, got %d", len(chapters))
	}
	if chapters[0].Title != "Page 1" || chapters[1].Title != "Page 2" {
		t.Fatalf("page labels were not normalized: %q, %q", chapters[0].Title, chapters[1].Title)
	}
	if chapters[2].Title != "第一章 相遇" {
		t.Fatalf("a real chapter heading was overwritten: %q", chapters[2].Title)
	}
	if chapters[0].Index != 1 || chapters[2].Index != 3 {
		t.Fatalf("catalog order was not preserved: %+v", chapters)
	}
}

func Test52SearchSplitsHighlightedHeadingIntoIdentity(t *testing.T) {
	// The live site wraps the matched query text in <em>: "1. <em>怦然</em><em>为你</em>_闵然【完结】".
	// Highlighting must not be mistaken for the author, and the status bracket
	// must survive as a separate field.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<article class="excerpt"><header><h2> <a href="/gl/h1R2.html"><h4>1. <em>怦然</em><em>为你</em>_闵然【完结】</h4></a> </h2></header>
<span class="note"> [ＧＬ百合] 《<em>怦然</em><em>为你</em>（GL）》作者：闵然【完结】 文案： 时懿的人生，从笔直的高岭之花到弯成回形针。 <p class="auth-span"><span class="muted">(所属栏目：<a href="/gl/">GL百合</a> 获赞：16187 )</span></span> </article>`)
	}))
	defer server.Close()
	source := fixture52(t, server)
	results, err := source.Search(context.Background(), "怦然为你")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	result := results[0]
	if result.Title != "怦然为你" {
		t.Fatalf("highlighted query leaked into the title: %q", result.Title)
	}
	if result.Author != "闵然" {
		t.Fatalf("author was not read from the underscore separator: %q", result.Author)
	}
	if result.Status != "完结" {
		t.Fatalf("status was not separated from the title: %q", result.Status)
	}
	if len(result.Tags) != 1 || result.Tags[0] != "GL百合" {
		t.Fatalf("category was not parsed: %v", result.Tags)
	}
	if strings.Contains(result.Description, "所属栏目") || strings.Contains(result.Description, "获赞") {
		t.Fatalf("note metadata leaked into the description: %q", result.Description)
	}
}

func Test52ResponseBodyBoundsIncludeChunkedBodies(t *testing.T) {
	for _, declared := range []bool{false, true} {
		t.Run(fmt.Sprint(declared), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if declared {
					w.Header().Set("Content-Length", fmt.Sprint(shukuMaxBodyBytes+1))
					return
				}
				w.(http.Flusher).Flush()
				chunk := strings.Repeat("x", 32<<10)
				for range int(shukuMaxBodyBytes/int64(len(chunk))) + 1 {
					if _, err := fmt.Fprint(w, chunk); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			source := fixture52(t, server)
			_, err := source.GetChapterContent(context.Background(), server.URL+"/large")
			var permanent *transport.PermanentError
			if !errors.As(err, &permanent) || transport.CanRetry(err) {
				t.Fatalf("oversized response was not permanently rejected: %v", err)
			}
		})
	}
}

func Test52FixtureParsesRelativeCatalogAndDirectMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book/index.html":
			fmt.Fprint(w, `<h1 class="article-title">书名_作者【完结】</h1><ul><li class="other mulu"><a data-extra="yes" href="page_2.html">Page 1</a></li><li class="mulu"><a href="/book/page_3.html">Page 2</a></li></ul>`)
		case "/so/search.php":
			if r.URL.Query().Get("q") != "书名 & 作者" {
				t.Errorf("search query was not encoded: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `<article class="excerpt"><header><h2><a href="/book/index.html"><h4>1. 书名_<em>作者</em>【完结】</h4></a></h2></header></article>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	source := fixture52(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	book, err := source.ResolveBook(ctx, server.URL+"/book/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "书名" || book.Author != "作者" || book.Status != "完结" {
		t.Fatalf("book identity was not resolved: %+v", book)
	}
	chapters, err := source.GetChapterList(ctx, book.BookURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 || chapters[0].URL != server.URL+"/book/page_2.html" || chapters[1].URL != server.URL+"/book/page_3.html" {
		t.Fatalf("catalog relative links were not resolved: %+v", chapters)
	}
	results, err := source.Search(ctx, "书名 & 作者")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].BookURL != book.BookURL || results[0].Title != book.Title {
		t.Fatalf("search did not return resolved book identity: %+v", results)
	}
}

func Test52CancellationInterruptsBodyRead(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	source := fixture52(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := source.GetChapterContent(ctx, server.URL+"/slow"); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("body read did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("body cancellation was lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt body read")
	}
}

func Test52LowerConnectionPolicyWaitIsCancelable(t *testing.T) {
	started := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	source := New52ShukuWithPolicy(1, 0.5)
	if err := source.SetBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	defer source.client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	go func() { _, err := source.GetChapterContent(ctx, server.URL+"/first"); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	go func() { _, err := source.GetChapterContent(ctx, server.URL+"/second"); done <- err }()
	select {
	case <-started:
		t.Fatal("configured one-connection source policy was ignored")
	case <-time.After(80 * time.Millisecond):
	}
	cancel()
	for range 2 {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation of bounded request was lost: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled connection-slot waiter did not finish")
		}
	}
}

func TestRetryAfterParsesHTTPDateAndIgnoresExpiredOrInvalidGuidance(t *testing.T) {
	now := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)
	if delay := parseRetryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now); delay != 3*time.Second {
		t.Fatalf("HTTP-date retry guidance = %v", delay)
	}
	for _, raw := range []string{"invalid", "-1", "0", now.Add(-time.Second).Format(http.TimeFormat)} {
		if delay := parseRetryAfter(raw, now); delay != 0 {
			t.Fatalf("expired or invalid retry guidance %q = %v", raw, delay)
		}
	}
}
