package sites

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestChapterListAndContent(t *testing.T) {
	requireLiveTest(t)

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bookURL := "https://www.52shuku.net/gl/24_b/bjR5y.html"

	// Get chapter list (pages)
	chapters, err := s.GetChapterList(ctx, bookURL)
	if err != nil {
		t.Fatalf("GetChapterList: %v", err)
	}
	t.Logf("Found %d pages", len(chapters))
	if len(chapters) == 0 {
		t.Fatal("no pages found")
	}

	// First page should NOT be the book URL (which has no content)
	if chapters[0].URL == bookURL {
		t.Error("first page should NOT be the book URL (no content there)")
	}

	// Fetch first content page
	content, err := s.GetChapterContent(ctx, chapters[0].URL)
	if err != nil {
		t.Fatalf("GetChapterContent(%s): %v", chapters[0].URL, err)
	}
	if content == "" {
		t.Fatal("empty content")
	}

	// Content should have actual text, not just HTML structure
	if len(content) < 100 {
		t.Errorf("content too short (%d chars): %s", len(content), content[:min(len(content), 100)])
	}

	// Should contain actual novel text characters
	if !strings.Contains(content, "许缘凡") && !strings.Contains(content, "耀眼") {
		t.Logf("content may not contain expected text: %s", content[:min(len(content), 200)])
	}

	t.Logf("Page 1: %s (%d chars)", chapters[0].Title, len(content))
	t.Logf("First 200 chars: %s", content[:min(len(content), 200)])
}
