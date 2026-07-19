package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		if strings.Contains(r.URL.Path, "thread-42-2-") {
			page2Requests++
			_, _ = w.Write(page2)
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
}
