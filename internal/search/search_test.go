package search

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"", ""},
		{"abc", "abc"},
		{"Hello World", "hello world"},
		{"   trim   ", "trim"},
		{"霸道总裁", "霸道总裁"},
		{"ABC-123!@#", "abc123"}, // punctuation including '-' stripped
		{"你好，世界！", "你好世界"},
		{"  \t\n  x  \n  ", "x"},
	}
	for _, tt := range tests {
		got := normalize(tt.in)
		if got != tt.out {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestMatchScore_ExactTitle(t *testing.T) {
	r := SearchResult{Title: "霸道总裁爱上我", Author: "某人"}
	score := matchScore("霸道总裁爱上我", r)
	if score < 100 {
		t.Errorf("exact title match score too low: %d", score)
	}
}

func TestMatchScore_PartialTitle(t *testing.T) {
	r := SearchResult{Title: "霸道总裁爱上我", Author: "某人"}
	exact := matchScore("霸道总裁爱上我", r)
	partial := matchScore("总裁", r)
	if partial >= exact {
		t.Errorf("partial match (%d) should score lower than exact (%d)", partial, exact)
	}
}

func TestMatchScore_AuthorMatch(t *testing.T) {
	r := SearchResult{Title: "无名书", Author: "金庸"}
	score := matchScore("金庸", r)
	if score <= 0 {
		t.Error("author match should have positive score")
	}
}

func TestMatchScore_EmptyQuery(t *testing.T) {
	r := SearchResult{Title: "测试", Author: "作者"}
	score := matchScore("", r)
	if score != 0 {
		t.Errorf("empty query score should be 0, got %d", score)
	}
}

func TestMatchScore_NoMatch(t *testing.T) {
	r := SearchResult{Title: "ABCD", Author: "XYZ"}
	score := matchScore("zzzzzz", r)
	if score != 0 {
		t.Errorf("no match score should be 0, got %d", score)
	}
}

func TestMatchScore_ChapterBonus(t *testing.T) {
	withCh := SearchResult{Title: "测试书", ChapterCount: 1000}
	noCh := SearchResult{Title: "测试书", ChapterCount: 0}
	if matchScore("测试书", noCh) >= matchScore("测试书", withCh) {
		t.Error("book with chapters should score higher than without")
	}
}

func TestRank_ByRelevance(t *testing.T) {
	e := NewEngine(NewRegistry())
	results := []SearchResult{
		{Title: "无关书", Author: "未知"},
		{Title: "总裁文", Author: "某人"},
		{Title: "霸道总裁爱上我", Author: "某人"},
	}
	ranked := e.rank("霸道总裁", results)
	if ranked[0].Title != "霸道总裁爱上我" {
		t.Errorf("best match should be first, got %q", ranked[0].Title)
	}
}

func TestRank_EmptyQuery(t *testing.T) {
	e := NewEngine(NewRegistry())
	results := []SearchResult{
		{Title: "B", Author: ""},
		{Title: "A", Author: ""},
	}
	ranked := e.rank("", results)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 results, got %d", len(ranked))
	}
}

func TestRank_SingleResult(t *testing.T) {
	e := NewEngine(NewRegistry())
	results := []SearchResult{
		{Title: "独本", Author: "作者"},
	}
	ranked := e.rank("独本", results)
	if len(ranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(ranked))
	}
	if ranked[0].Title != "独本" {
		t.Errorf("title mismatch: %q", ranked[0].Title)
	}
}
