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

func TestYamiboSearchScopedUsesKeywordFallbackContractAndPopularity(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "yamibo_search_results.html"))
	if err != nil {
		t.Fatal(err)
	}
	orders := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("srchtxt") != "一周一次" || query.Get("kw") != "一周一次" {
			http.Error(w, "missing keyword parameters", http.StatusBadRequest)
			return
		}
		if got := query.Get("srchfid[]"); got != "49" {
			http.Error(w, "wrong forum: "+got, http.StatusBadRequest)
			return
		}
		orders[query.Get("orderby")]++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture)
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
	if orders["replies"] != 1 || orders["views"] != 1 {
		t.Fatalf("search orders = %#v, want one request each", orders)
	}
	ranked := search.FuzzyRank("一周一次", results)
	if ranked[0].Title != "一周一次" {
		t.Fatalf("fuzzy ranking changed exact match: %#v", ranked)
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
