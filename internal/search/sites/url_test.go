package sites

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"
)

func TestSearchURL(t *testing.T) {
	s := New52Shuku()
	q := url.QueryEscape("秦寺")
	u := fmt.Sprintf("%s/so/search.php?q=%s", s.BaseURL(), q)
	t.Logf("Search URL: %s", u)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	doc, err := s.doGet(ctx, u)
	if err != nil {
		// Try alternative encoding
		u2 := fmt.Sprintf("%s/so/search.php?q=%s", s.BaseURL(), url.PathEscape("秦寺"))
		t.Logf("Alt URL: %s", u2)
		doc, err = s.doGet(ctx, u2)
		if err != nil {
			t.Fatalf("Both URLs failed: %v", err)
		}
	}

	count := doc.Find("article.excerpt").Length()
	t.Logf("Found %d search results", count)
}
