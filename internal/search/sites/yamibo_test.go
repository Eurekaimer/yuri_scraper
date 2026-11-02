package sites

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/time/rate"
)

func TestNormalizeYamiboURL(t *testing.T) {
	got := normalizeYamiboURL("forum.php?mod=viewthread&tid=42&amp;pid=7")
	want := "https://bbs.yamibo.com/forum.php?mod=viewthread&tid=42&pid=7"
	if got != want {
		t.Fatalf("normalizeYamiboURL() = %q, want %q", got, want)
	}
}

func fixtureDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestYamiboCatalogCandidateScoring(t *testing.T) {
	candidates := yamiboCatalogCandidates(fixtureDocument(t, "yamibo_catalog.html"), "百合会")
	if len(candidates) < 2 {
		t.Fatalf("got %d candidates, want at least 2", len(candidates))
	}
	if !strings.Contains(candidates[0].Source, "showcollapse_content") || len(candidates[0].Chapters) != 3 {
		t.Fatalf("unexpected best candidate: %#v", candidates[0])
	}
	if candidates[0].Preview[0] != "第一章 初遇" || candidates[0].Score <= candidates[1].Score {
		t.Fatalf("candidate ranking did not prefer chapter-like elevator: %#v", candidates)
	}
}

func TestYamiboAuthorPaginationDeduplicatesAndResumes(t *testing.T) {
	page1, err := os.ReadFile(filepath.Join("testdata", "yamibo_author_page1.html"))
	if err != nil {
		t.Fatal(err)
	}
	page2, err := os.ReadFile(filepath.Join("testdata", "yamibo_author_page2.html"))
	if err != nil {
		t.Fatal(err)
	}
	page2Requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("authorid") == "7" {
			if r.URL.Query().Get("page") == "2" {
				page2Requests++
				_, _ = w.Write(page2)
				return
			}
			_, _ = w.Write(page1)
			return
		}
		_, _ = w.Write(page1)
	}))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test"}
	checkpoint := filepath.Join(t.TempDir(), "scan.json")
	savedBodies := 0
	opts := search.ChapterPlanOptions{
		Mode: search.ChapterModeAuthor, Author: "7", MaxScanPages: 2, MinPostChars: 40,
		CheckpointPath: checkpoint,
		ContentSink:    func(search.Chapter, string) error { savedBodies++; return nil },
	}
	threadURL := server.URL + "/forum.php?mod=viewthread&tid=42"
	plan, err := y.PlanChapters(context.Background(), threadURL, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalPages != 2 || plan.ScannedPages != 2 || len(plan.Chapters) != 2 || savedBodies != 2 {
		t.Fatalf("unexpected plan: %#v (saved=%d)", plan, savedBodies)
	}
	if !strings.Contains(plan.Chapters[0].Title, "第一章") || plan.Chapters[1].Title != "作者楼层 2" {
		t.Fatalf("unexpected ordered titles: %#v", plan.Chapters)
	}
	if page2Requests != 1 {
		t.Fatalf("page 2 requests = %d, want 1", page2Requests)
	}

	plan, err = y.PlanChapters(context.Background(), threadURL, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CheckpointHits != 2 || page2Requests != 1 {
		t.Fatalf("resume missed checkpoint: plan=%#v page2Requests=%d", plan, page2Requests)
	}
	data, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "cookie") || strings.Contains(string(data), "session=test") {
		t.Fatalf("checkpoint leaked credentials: %s", data)
	}
}

func TestYamiboSessionInvalidFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "yamibo_cookie_invalid.html"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "expired=true"}
	_, err = y.document(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "会话已失效") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func yamiboTestPostHTML(pid, uid, name, content string) string {
	return fmt.Sprintf(`<div id="post_%s"><div class="authi"><a class="xw1" href="space-uid-%s.html">%s</a></div><div id="postmessage_%s">%s</div></div>`, pid, uid, name, pid, content)
}

func yamiboPlainThreadFixture(uid string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body>`)
	b.WriteString(`<div class="pg"><a href="thread-42-1-1.html">1</a><a href="thread-42-907-1.html">907</a></div>`)
	b.WriteString(`<a href="forum.php?mod=viewthread&amp;tid=42&amp;page=1&amp;authorid=`)
	b.WriteString(uid)
	b.WriteString(`">只看该作者</a>`)
	b.WriteString(yamiboTestPostHTML("9001", uid, "Alice", "第1章 起点<br>这是楼主发布的正文开头，内容长度足够触发作者正文识别，而且这一段会出现在未过滤的第一页。"))
	b.WriteString(yamiboTestPostHTML("9002", "999", "Reader", "读者长回复<br>这是其他用户发布的长回复，不应被下载为章节，即使内容长度已经超过阈值。"))
	b.WriteString(`</body></html>`)
	return b.String()
}

func yamiboFilteredThreadFixture(page, total int, uid string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body><div class="pg">`)
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, `<a href="forum.php?mod=viewthread&amp;tid=42&amp;page=%d&amp;authorid=%s">%d</a>`, i, uid, i)
	}
	b.WriteString(`</div>`)
	b.WriteString(yamiboTestPostHTML(strconv.Itoa(10000+page), uid, "Alice",
		fmt.Sprintf("第%d章 作者正文<br>这是作者过滤视图中第 %d 页的正文内容，长度足够被章节解析器接受并保存。", page, page)))
	if page == 1 {
		b.WriteString(yamiboTestPostHTML("13000", "999", "Reader", "页面中的其他作者<br>作者过滤视图理论只含楼主，这里额外放入其他用户楼层以确认客户端仍然会排除它。"))
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func TestYamiboAuthorIDFilterHonorsPageLimit(t *testing.T) {
	const starterUID = "406769"
	var filteredRequests, unfilteredPageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		query := r.URL.Query()
		if query.Get("authorid") == starterUID {
			filteredRequests.Add(1)
			page, _ := strconv.Atoi(query.Get("page"))
			_, _ = w.Write([]byte(yamiboFilteredThreadFixture(page, 26, starterUID)))
			return
		}
		if query.Get("page") != "" {
			unfilteredPageRequests.Add(1)
		}
		_, _ = w.Write([]byte(yamiboPlainThreadFixture(starterUID)))
	}))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test"}
	savedBodies := 0
	plan, err := y.PlanChapters(context.Background(), server.URL+"/forum.php?mod=viewthread&tid=42", search.ChapterPlanOptions{
		MaxScanPages: 10, MinPostChars: 40,
		ContentSink: func(search.Chapter, string) error { savedBodies++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != search.ChapterModeAuthor || plan.Author != starterUID {
		t.Fatalf("unexpected plan strategy/author: %#v", plan)
	}
	if plan.TotalPages != 26 || plan.ScannedPages != 10 {
		t.Fatalf("author scan did not honor page limit: %#v", plan)
	}
	if len(plan.Chapters) != 10 || savedBodies != 10 {
		t.Fatalf("got %d chapters with %d saved bodies, want 10", len(plan.Chapters), savedBodies)
	}
	for _, chapter := range plan.Chapters {
		if strings.Contains(chapter.URL, "pid=13000") {
			t.Fatalf("included another author's post: %#v", chapter)
		}
	}
	if filteredRequests.Load() != 10 {
		t.Fatalf("author-filtered requests = %d, want 10", filteredRequests.Load())
	}
	if unfilteredPageRequests.Load() != 0 {
		t.Fatalf("scanned %d unrelated thread pages", unfilteredPageRequests.Load())
	}
}

func TestYamiboAuthorIDFilterFailsOnMissingAuthorPage(t *testing.T) {
	const starterUID = "42"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		query := r.URL.Query()
		if query.Get("authorid") == starterUID {
			page, _ := strconv.Atoi(query.Get("page"))
			if page == 2 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(yamiboFilteredThreadFixture(page, 3, starterUID)))
			return
		}
		_, _ = w.Write([]byte(yamiboPlainThreadFixture(starterUID)))
	}))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test"}
	plan, err := y.PlanChapters(context.Background(), server.URL+"/forum.php?mod=viewthread&tid=42", search.ChapterPlanOptions{
		MaxScanPages: 10, MinPostChars: 40,
	})
	if err == nil || !strings.Contains(err.Error(), "第 2 页") {
		t.Fatalf("missing author page was not reported: %v", err)
	}
	if len(plan.Chapters) != 1 || plan.TotalPages != 3 {
		t.Fatalf("unexpected partial plan: %#v", plan)
	}
}

func TestYamiboExplicitAuthorDoesNotUseStarterFilter(t *testing.T) {
	const starterUID = "7"
	var authorIDRequests atomic.Int32
	page1 := `<!doctype html><html><body>` +
		`<div class="pg"><a href="thread-42-1-1.html">1</a><a href="thread-42-2-1.html">2</a></div>` +
		`<a href="forum.php?mod=viewthread&amp;tid=42&amp;page=1&amp;authorid=7">只看该作者</a>` +
		yamiboTestPostHTML("201", starterUID, "Starter", "第一章 楼主正文<br>楼主发布的内容长度足够，但它不是用户明确选择的作者，因此不应被收录。") +
		yamiboTestPostHTML("202", "8", "Reader", "第二章 读者正文<br>用户明确选择读者作为作者，这一段应该被收录，并且扫描不得借用楼主的 authorid 过滤。") +
		`</body></html>`
	page2 := `<!doctype html><html><body>` +
		yamiboTestPostHTML("203", "8", "Reader", "第三章 读者续篇<br>第二页仍然包含读者正文，长度足够并被纳入作者模式的限页扫描范围。") +
		`</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		query := r.URL.Query()
		if query.Get("authorid") != "" {
			authorIDRequests.Add(1)
		}
		if query.Get("page") == "2" || strings.Contains(r.URL.Path, "thread-42-2-") {
			_, _ = w.Write([]byte(page2))
			return
		}
		_, _ = w.Write([]byte(page1))
	}))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test"}
	plan, err := y.PlanChapters(context.Background(), server.URL+"/forum.php?mod=viewthread&tid=42", search.ChapterPlanOptions{
		Mode: search.ChapterModeAuthor, Author: "Reader", MaxScanPages: 2, MinPostChars: 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Author != "Reader" || len(plan.Chapters) != 2 {
		t.Fatalf("explicit author selection was not preserved: %#v", plan)
	}
	for _, chapter := range plan.Chapters {
		if strings.Contains(chapter.URL, "pid=201") {
			t.Fatalf("included the starter instead of the selected author: %#v", chapter)
		}
	}
	if authorIDRequests.Load() != 0 {
		t.Fatalf("used the starter authorid filter %d times", authorIDRequests.Load())
	}
}

func TestYamiboExplicitMissingAuthorIsNotSilentSuccess(t *testing.T) {
	page := `<!doctype html><html><body>` +
		`<div class="pg"><a href="thread-42-1-1.html">1</a></div>` +
		yamiboTestPostHTML("301", "7", "Starter", "第一章 楼主正文<br>这是帖子里唯一的正文楼层，长度足够，但不属于用户指定的作者。") +
		`</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test"}
	plan, err := y.PlanChapters(context.Background(), server.URL+"/forum.php?mod=viewthread&tid=42", search.ChapterPlanOptions{
		Mode: search.ChapterModeAuthor, Author: "Ghost", MaxScanPages: 1, MinPostChars: 40,
	})
	if err == nil || !strings.Contains(err.Error(), "Ghost") {
		t.Fatalf("missing author was not reported: %v", err)
	}
	if len(plan.Chapters) != 0 {
		t.Fatalf("missing author produced chapters: %#v", plan.Chapters)
	}
}

func TestYamiboShortOpeningDoesNotSelectReplyAuthor(t *testing.T) {
	first := yamiboTestPostHTML("1", "7", "Starter", "简介") +
		yamiboTestPostHTML("2", "8", "Reader", "这是另一人的长回复，不应被当作楼主正文。"+strings.Repeat("路人文字", 20))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("authorid") == "7" {
			fmt.Fprint(w, yamiboTestPostHTML("3", "7", "Starter", "第一章 楼主正文"+strings.Repeat("正文", 40)))
			return
		}
		fmt.Fprint(w, first)
	}))
	defer server.Close()
	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test"}
	plan, err := y.PlanChapters(context.Background(), server.URL+"/forum.php?mod=viewthread&tid=42",
		search.ChapterPlanOptions{Mode: search.ChapterModeAuthor, MinPostChars: 40})
	if err != nil || plan.Author != "7" || len(plan.Chapters) != 1 || !strings.Contains(plan.Chapters[0].URL, "pid=3") {
		t.Fatalf("selected reply author instead of thread starter: %#v, %v", plan, err)
	}
}

func TestExtractYamiboLinksPrefersUniquePosts(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
        <div id="catalog">
          <a href="forum.php?mod=viewthread&amp;tid=42&amp;pid=7">第一章 开始</a>
          <a href="forum.php?mod=viewthread&amp;tid=42&amp;pid=7">第一章重复</a>
          <a href="forum.php?mod=viewthread&amp;tid=42&amp;pid=8">第二章 后续</a>
          <a href="javascript:void(0)">复制链接</a>
        </div>`))
	if err != nil {
		t.Fatal(err)
	}
	items := extractYamiboLinks(doc.Find("#catalog"), "百合会")
	if len(items) != 2 {
		t.Fatalf("got %d chapters, want 2", len(items))
	}
	if items[0].Title != "第一章 开始" || items[1].Title != "第二章 后续" {
		t.Fatalf("unexpected titles: %#v", items)
	}
}

func TestCleanYamiboContent(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
        <table><tr><td id="postmessage_7"><div class="quote">quoted noise</div>
        第一章 开始<br>这是足够长的正文内容，用来确认清洗器能够保留小说正文而不是论坛控件，
        并且不会错误地把真正需要保存的段落当成界面噪声删除。</td></tr></table>`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := cleanYamiboContent(doc.Find("#postmessage_7"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "quoted noise") || !strings.Contains(text, "第一章") {
		t.Fatalf("unexpected cleaned content: %q", text)
	}
	if !strings.Contains(text, "第一章 开始\n这是足够长的正文内容") {
		t.Fatalf("chapter line breaks lost: %q", text)
	}
}
