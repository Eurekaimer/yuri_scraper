package search

import (
	"fmt"
	"testing"
)

func TestFuzzyMatch_Exact(t *testing.T) {
	if s := FuzzyMatch("秦寺", "秦寺"); s != 100 {
		t.Errorf("exact match = %f, want 100", s)
	}
}

func TestFuzzyMatch_Contains(t *testing.T) {
	s := FuzzyMatch("秦寺", "耀眼[娱乐圈]_秦寺【完结】")
	if s < 80 {
		t.Errorf("substring match too low: %f", s)
	}
}

func TestFuzzyMatch_PartialOverlap(t *testing.T) {
	s := FuzzyMatch("秦青晏", "秦青晏清冷矜贵")
	if s < 80 {
		t.Errorf("full query in target: %f", s)
	}
}

func TestFuzzyMatch_NoMatch(t *testing.T) {
	s := FuzzyMatch("秦寺", "霸道总裁爱上我")
	if s > 30 {
		t.Errorf("unrelated strings should score low: %f", s)
	}
}

func TestFuzzyMatch_ChinesePartial(t *testing.T) {
	// "秦寺" shares no chars with "明月为什么皎皎" — low score
	low := FuzzyMatch("秦寺", "明月为什么皎皎")
	// But "秦寺" is IN "耀眼_秦寺【完结】" — high score
	high := FuzzyMatch("秦寺", "耀眼_秦寺【完结】")
	if low >= high {
		t.Errorf("partial overlap (%f) should be lower than contain (%f)", low, high)
	}
}

func TestFuzzyMatch_Empty(t *testing.T) {
	if s := FuzzyMatch("", "test"); s != 0 {
		t.Errorf("empty query = %f, want 0", s)
	}
	if s := FuzzyMatch("test", ""); s != 0 {
		t.Errorf("empty target = %f, want 0", s)
	}
}

func TestFuzzyMatch_SingleChar(t *testing.T) {
	// Single Chinese character queries
	s := FuzzyMatch("秦", "秦青晏")
	if s < 70 {
		t.Errorf("single char in target: %f", s)
	}
}

func TestFuzzyRank_ByRelevance(t *testing.T) {
	results := []SearchResult{
		{Title: "明月为什么皎皎", Author: "秦寺"},
		{Title: "耀眼[娱乐圈]_秦寺【完结】", Author: "秦寺"},
		{Title: "霸道总裁爱上我", Author: "无名"},
	}
	ranked := FuzzyRank("秦寺", results)
	if ranked[0].Title != "耀眼[娱乐圈]_秦寺【完结】" {
		t.Errorf("best match should be first, got %q", ranked[0].Title)
	}
}

func TestFuzzyRank_Empty(t *testing.T) {
	results := []SearchResult{
		{Title: "A", Author: ""},
		{Title: "B", Author: ""},
	}
	ranked := FuzzyRank("", results)
	if len(ranked) != 2 {
		t.Fatalf("empty query should preserve order: %d results", len(ranked))
	}
}

func TestFuzzyRankUsesPopularityAsBoundedTieBreaker(t *testing.T) {
	results := []SearchResult{
		{Title: "一周一次 番外", Popularity: 10},
		{Title: "一周一次 正文", Popularity: 10000},
		{Title: "完全无关但热门", Popularity: 1e12},
	}
	ranked := FuzzyRank("一周一次", results)
	if ranked[0].Title != "一周一次 正文" {
		t.Fatalf("hot relevant result should win tie, got %q", ranked[0].Title)
	}
	if ranked[len(ranked)-1].Title != "完全无关但热门" {
		t.Fatalf("popularity overrode relevance: %#v", ranked)
	}
}

func TestLevenshtein(t *testing.T) {
	if d := levenshtein([]rune("abc"), []rune("abc")); d != 0 {
		t.Errorf("levenshtein(abc,abc) = %d, want 0", d)
	}
	if d := levenshtein([]rune("abc"), []rune("abd")); d != 1 {
		t.Errorf("levenshtein(abc,abd) = %d, want 1", d)
	}
	if d := levenshtein([]rune(""), []rune("abc")); d != 3 {
		t.Errorf("levenshtein('',abc) = %d, want 3", d)
	}
}

func TestHasCJK(t *testing.T) {
	if !HasCJK("秦寺") {
		t.Error("HasCJK(秦寺) should be true")
	}
	if !HasCJK("hello秦world") {
		t.Error("HasCJK(hello秦world) should be true")
	}
	if HasCJK("hello") {
		t.Error("HasCJK(hello) should be false")
	}
}

func BenchmarkFuzzyRank40(b *testing.B) {
	results := make([]SearchResult, 40)
	for i := range results {
		results[i] = SearchResult{
			Title:      fmt.Sprintf("一周一次 第%d篇", i),
			Author:     fmt.Sprintf("作者%d", i%7),
			Popularity: float64((i + 1) * 1234),
		}
	}
	b.ResetTimer()
	for range b.N {
		_ = FuzzyRank("一周一次", results)
	}
}
