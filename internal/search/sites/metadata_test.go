package sites

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hzg/yuri_scraper/internal/search"
)

func TestGetBookMeta_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bookURL := "https://www.52shuku.net/gl/30_b/bjYp6.html"
	detail, err := s.GetBookMeta(ctx, bookURL)
	if err != nil {
		t.Fatalf("GetBookMeta: %v", err)
	}
	if detail == nil {
		t.Fatal("GetBookMeta returned nil")
	}

	// Verify description contains expected content
	if detail.Description == "" {
		t.Error("expected non-empty description")
	} else {
		if !strings.Contains(detail.Description, "年下") && !strings.Contains(detail.Description, "忠犬") {
			t.Logf("description may not contain expected content: %s", detail.Description)
		}
	}

	// Verify protagonist detection
	if detail.Protagonist == "" {
		t.Log("protagonist not detected (may need improvement)")
	} else {
		if !strings.Contains(detail.Protagonist, "秦青晏") {
			t.Logf("expected protagonist 秦青晏, got %q", detail.Protagonist)
		}
	}

	// Verify status
	if detail.Status == "" {
		t.Log("status not detected")
	} else if detail.Status != "完结" {
		t.Logf("status = %q, want '完结'", detail.Status)
	}

	// Verify tags
	if len(detail.Tags) == 0 {
		t.Error("expected at least some tags")
	} else {
		hasTag := func(want string) bool {
			for _, tag := range detail.Tags {
				if strings.Contains(tag, want) {
					return true
				}
			}
			return false
		}
		if !hasTag("GL") && !hasTag("百合") {
			t.Logf("expected GL/百合 in tags: %v", detail.Tags)
		}
		if !hasTag("年下") {
			t.Logf("expected 年下 in tags: %v", detail.Tags)
		}
	}

	t.Logf("Title info - Status: %q", detail.Status)
	t.Logf("Description: %s", detail.Description[:min(len(detail.Description), 100)])
	t.Logf("Protagonist: %q", detail.Protagonist)
	t.Logf("Tags: %v", detail.Tags)
	t.Logf("LastUpdated: %q", detail.LastUpdated)
}

func TestGetBookMeta_InvalidURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := s.GetBookMeta(ctx, "https://www.52shuku.net/nonexistent.html")
	if err == nil {
		t.Error("expected error for nonexistent URL")
	}
}

// Verify Site52Shuku implements BookMetaProvider
var _ search.BookMetaProvider = (*Site52Shuku)(nil)
