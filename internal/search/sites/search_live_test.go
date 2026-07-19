package sites

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSearchLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live search test in short mode")
	}

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := s.Search(ctx, "秦寺")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned 0 results — possible Cloudflare block or parsing issue")
	}
	t.Logf("Found %d results:", len(results))
	for i, r := range results {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] %s — %s (%s)", i+1, r.Title, r.Author, r.BookURL)
	}
}

func TestSearchRawResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping raw response test")
	}

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	doc, err := s.doGet(ctx, fmt.Sprintf("%s/so/search.php?q=%s", s.BaseURL(), "秦寺"))
	if err != nil {
		t.Fatalf("doGet error: %v", err)
	}

	html, _ := doc.Html()
	if strings.Contains(html, "Cloudflare") || strings.Contains(html, "cf-browser-verify") || strings.Contains(html, "__cf") {
		t.Error("Got Cloudflare challenge page, not search results!")
		t.Logf("First 500 chars: %s", html[:min(500, len(html))])
		return
	}

	count := doc.Find("article.excerpt").Length()
	t.Logf("Found %d article.excerpt elements", count)
	if count == 0 {
		t.Error("No search result articles found — HTML structure may have changed")
		// Dump a snippet for debugging
		body := doc.Find("body").Text()
		if len(body) > 300 {
			body = body[:300]
		}
		t.Logf("Body snippet: %s", body)
	}
}
