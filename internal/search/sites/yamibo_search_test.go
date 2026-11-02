package sites

import (
	"context"
	"io"
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

func TestCollectYamiboSearchResultsExtractsSectionStatsAndAuthor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "yamibo_search_results.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	hits := make(map[string]*yamiboSearchHit)
	collectYamiboSearchResults(doc, hits, []int{49}, 49, "replies", yamiboBaseURL)
	if len(hits) != 1 {
		t.Fatalf("got %d scoped hits, want 1", len(hits))
	}
	got := hits["101"].result
	if got.Title != "一周一次" || got.Author != "测试作者" || got.Replies != 88 || got.Views != 12345 {
		t.Fatalf("unexpected result: %#v", got)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "文学区" {
		t.Fatalf("section not extracted: %#v", got.Tags)
	}
}

func TestYamiboSearchScopedUsesKeywordThenSearchID(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "yamibo_search_results.html"))
	if err != nil {
		t.Fatal(err)
	}
	// Discuz repeats the assigned search id in pagination and ordering links.
	page := strings.Replace(string(fixture), "</body>",
		`<a href="search.php?mod=forum&searchid=3848&page=2">2</a></body>`, 1)
	orders := make(map[string]int)
	submissions := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if query.Get("searchid") != "" {
			if query.Get("searchid") != "3848" || query.Get("orderby") != "views" {
				http.Error(w, "unexpected reuse of the search id", http.StatusBadRequest)
				return
			}
			if query.Get("srchtxt") != "" {
				http.Error(w, "keyword was submitted again", http.StatusBadRequest)
				return
			}
			orders["views"]++
			_, _ = io.WriteString(w, page)
			return
		}
		if query.Get("srchtxt") != "一周一次" || query.Get("kw") != "一周一次" {
			http.Error(w, "missing keyword parameters", http.StatusBadRequest)
			return
		}
		if got := query.Get("srchfid[]"); got != "49" {
			http.Error(w, "wrong forum: "+got, http.StatusBadRequest)
			return
		}
		submissions++
		orders[query.Get("orderby")]++
		_, _ = io.WriteString(w, page)
	}))
	defer server.Close()

	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test", authenticated: true}
	results, err := y.SearchScoped(context.Background(), "一周一次", []string{"49"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "一周一次" || results[0].Popularity <= 0 {
		t.Fatalf("unexpected ranked results: %#v", results)
	}
	if submissions != 1 || orders["replies"] != 1 || orders["views"] != 1 {
		t.Fatalf("submissions=%d orders=%#v, want one keyword submission and one id reuse", submissions, orders)
	}
	ranked := search.FuzzyRank("一周一次", results)
	if ranked[0].Title != "一周一次" {
		t.Fatalf("fuzzy ranking changed exact match: %#v", ranked)
	}
}

// Discuz search results are list items whose title anchor is followed by
// reply counters and a matched-post snippet that can quote the same thread
// URL. A partial Chinese query like 周次 matches several distinct threads
// (main story, 番外, 短篇合集); each must keep its own title and URL.
const yamiboAmbiguousSearchFixture = `<!doctype html>
<html><body>
<div class="tl"><div class="slst mtw" id="threadlist"><ul>
<li class="pbw" id="521519">
  <h3 class="xs3"><a href="forum.php?mod=viewthread&amp;tid=521519&amp;highlight=%E5%91%A8%E6%AC%A1" target="_blank">[个人翻译][长篇][羽田宇佐]一周一次买下同班同学的那些事（2026.10.30更新至第433话）</a></h3>
  <p class="xg1">433 回复 - 1,234,567 查看</p>
  <p>……<a href="forum.php?mod=viewthread&amp;tid=521519&amp;page=26&amp;authorid=406769" target="_blank">只看该作者</a></p>
  <p><span>2026-10-30</span> - <span><a href="home.php?mod=space&amp;uid=406769" target="_blank">羽田宇佐</a></span> - <span><a href="forum.php?mod=forumdisplay&amp;fid=55" target="_blank" class="xi1">译文区</a></span></p>
</li>
<li class="pbw" id="540704">
  <h3 class="xs3"><a href="forum.php?mod=viewthread&amp;tid=540704&amp;highlight=%E5%91%A8%E6%AC%A1" target="_blank">[个人翻译][番外][羽田宇佐]一周一次买下同班同学的那些事（2024.6.15更新蜜瓜小册子一篇）</a></h3>
  <p class="xg1">12 回复 - 3,456 查看</p>
  <p>……<a href="forum.php?mod=viewthread&amp;tid=540704&amp;page=3&amp;authorid=406769" target="_blank">只看该作者</a></p>
  <p><span>2024-06-15</span> - <span><a href="home.php?mod=space&amp;uid=406769" target="_blank">羽田宇佐</a></span> - <span><a href="forum.php?mod=forumdisplay&amp;fid=55" target="_blank" class="xi1">译文区</a></span></p>
</li>
<li class="pbw" id="549611">
  <h3 class="xs3"><a href="forum.php?mod=viewthread&amp;tid=549611&amp;highlight=%E5%91%A8%E6%AC%A1" target="_blank">【授权转载】[羽田宇佐]一周一次买下同班同学的那些事 短篇合集(12.22 更新DM附录＆全套购入特典)</a></h3>
  <p class="xg1">8 回复 - 2,100 查看</p>
  <p>……<a href="forum.php?mod=viewthread&amp;tid=549611&amp;page=1&amp;authorid=512000" target="_blank">只看该作者</a></p>
  <p><span>2025-01-30</span> - <span><a href="home.php?mod=space&amp;uid=512000" target="_blank">转载君</a></span> - <span><a href="forum.php?mod=forumdisplay&amp;fid=55" target="_blank" class="xi1">译文区</a></span></p>
</li>
<li class="pbw" id="777">
  <h3 class="xs3"><a href="forum.php?mod=viewthread&amp;tid=777" target="_blank">周次同人讨论</a></h3>
  <p class="xg1">3 回复 - 90 查看</p>
  <p><span>2025-02-01</span> - <span><a href="home.php?mod=space&amp;uid=9001" target="_blank">路人</a></span> - <span><a href="forum.php?mod=forumdisplay&amp;fid=49" target="_blank" class="xi1">文学区</a></span></p>
</li>
</ul><div class="pgs cl mbm"><a href="search.php?mod=forum&amp;page=2">2</a></div></div></div>
</body></html>`

func TestCollectYamiboSearchResultsKeepsCandidateIdentity(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(yamiboAmbiguousSearchFixture))
	if err != nil {
		t.Fatal(err)
	}
	hits := make(map[string]*yamiboSearchHit)
	collectYamiboSearchResults(doc, hits, []int{55}, 55, "replies", yamiboBaseURL)
	want := map[string]string{
		"521519": "[个人翻译][长篇][羽田宇佐]一周一次买下同班同学的那些事（2026.10.30更新至第433话）",
		"540704": "[个人翻译][番外][羽田宇佐]一周一次买下同班同学的那些事（2024.6.15更新蜜瓜小册子一篇）",
		"549611": "【授权转载】[羽田宇佐]一周一次买下同班同学的那些事 短篇合集(12.22 更新DM附录＆全套购入特典)",
	}
	if len(hits) != len(want) {
		t.Fatalf("got %d candidates %v, want %d scoped to 译文区", len(hits), hits, len(want))
	}
	for tid, title := range want {
		hit, ok := hits[tid]
		if !ok {
			t.Fatalf("candidate %s missing; got %v", tid, hits)
		}
		if hit.result.Title != title {
			t.Fatalf("candidate %s title = %q, want %q", tid, hit.result.Title, title)
		}
		if !strings.Contains(hit.result.BookURL, "tid="+tid) || strings.Contains(hit.result.BookURL, "authorid=") || strings.Contains(hit.result.BookURL, "page=") {
			t.Fatalf("candidate %s selected URL = %q, want the title link", tid, hit.result.BookURL)
		}
		if hit.result.Author == "" {
			t.Fatalf("candidate %s lost its author", tid)
		}
	}
	if got := hits["521519"].result.Replies; got != 433 {
		t.Fatalf("main story replies = %d, want 433", got)
	}
	if got := hits["540704"].result.Replies; got != 12 {
		t.Fatalf("番外 replies = %d, want 12", got)
	}
	if got := hits["521519"].replyRank; got != 1 {
		t.Fatalf("main story rank = %d, want 1", got)
	}
}

func TestYamiboSearchScopedExposesEveryPartialTitleCandidate(t *testing.T) {
	var keywords []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("srchtxt") != "周次" {
			http.Error(w, "missing keyword", http.StatusBadRequest)
			return
		}
		if got := query.Get("srchfid[]"); got != "55" {
			http.Error(w, "wrong forum: "+got, http.StatusBadRequest)
			return
		}
		keywords = append(keywords, query.Get("kw"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(yamiboAmbiguousSearchFixture))
	}))
	defer server.Close()

	y := &Yamibo{baseURL: server.URL + "/", client: server.Client(), limiter: rate.NewLimiter(rate.Inf, 1), cookie: "session=test", authenticated: true}
	results, err := y.SearchScoped(context.Background(), "周次", []string{"55"})
	if err != nil {
		t.Fatal(err)
	}
	// The reply-order and view-order passes must not collapse, rename or
	// re-point candidates: 周次 is an alias, so every thread keeps its own title.
	if len(results) != 3 {
		t.Fatalf("got %d candidates %#v, want 3", len(results), results)
	}
	byTID := make(map[string]search.SearchResult, len(results))
	for _, result := range results {
		switch {
		case strings.Contains(result.BookURL, "tid=521519"):
			byTID["521519"] = result
		case strings.Contains(result.BookURL, "tid=540704"):
			byTID["540704"] = result
		case strings.Contains(result.BookURL, "tid=549611"):
			byTID["549611"] = result
		default:
			t.Fatalf("unexpected candidate URL %q", result.BookURL)
		}
		if strings.Contains(result.BookURL, "authorid=") || strings.Contains(result.BookURL, "page=") {
			t.Fatalf("candidate %s URL drifted to a reply-count link: %q", result.Title, result.BookURL)
		}
	}
	if got := byTID["540704"].Title; !strings.Contains(got, "番外") {
		t.Fatalf("selected 番外 URL carries title %q", got)
	}
	if got := byTID["549611"].Title; !strings.Contains(got, "短篇合集") {
		t.Fatalf("selected 短篇合集 URL carries title %q", got)
	}
	if len(keywords) == 0 || keywords[0] != "周次" {
		t.Fatalf("keyword variants = %q, want the original query first", keywords)
	}

	ranked := search.FuzzyRank("周次", results)
	if len(ranked) != 3 || !strings.Contains(ranked[0].BookURL, "tid=521519") {
		t.Fatalf("fuzzy ranking lost the main story: %#v", ranked)
	}
	if ranked[1].BookURL == ranked[0].BookURL || ranked[2].BookURL == ranked[1].BookURL {
		t.Fatalf("distinct candidates collapsed after ranking: %#v", ranked)
	}
}

func TestYamiboSearchStatsKeepsZeroReplies(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<table><tr><td class="num"><a>0</a><em>321</em></td></tr></table>`))
	if err != nil {
		t.Fatal(err)
	}
	replies, views := yamiboSearchStats(doc.Find("tr"))
	if replies != 0 || views != 321 {
		t.Fatalf("stats = %d/%d, want 0/321", replies, views)
	}
}
