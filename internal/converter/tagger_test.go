package converter

import (
	"reflect"
	"sort"
	"testing"
)

func TestAutoDetectTags_GLGenre(t *testing.T) {
	// From the real book: 晏色正好gl by 一点风声
	tags := AutoDetectTags("晏色正好gl", "黏人忠犬年下VS温柔智慧年上 人人都说秦青晏清冷矜贵")
	hasTag := func(want string) bool {
		for _, t := range tags {
			if t == want {
				return true
			}
		}
		return false
	}
	if !hasTag("纯爱") {
		t.Errorf("expected '纯爱' tag for GL content, got %v", tags)
	}
}

func TestAutoDetectTags_PresidentGenre(t *testing.T) {
	tags := AutoDetectTags("霸道总裁爱上我", "豪门恩怨，总裁的契约新娘")
	if !containsTag(tags, "豪门总裁") {
		t.Errorf("expected '豪门总裁' tag, got %v", tags)
	}
}

func TestAutoDetectTags_MultipleGenres(t *testing.T) {
	tags := AutoDetectTags("重生之修仙高手", "穿越到异界修仙世界，都市修真传奇")
	expected := []string{"重生穿越", "仙侠修真"}
	for _, exp := range expected {
		if !containsTag(tags, exp) {
			t.Errorf("expected '%s' tag, got %v", exp, tags)
		}
	}
}

func TestAutoDetectTags_Empty(t *testing.T) {
	tags := AutoDetectTags("", "")
	if len(tags) != 0 {
		t.Errorf("expected no tags for empty input, got %v", tags)
	}
}

func TestMergeTags_Dedup(t *testing.T) {
	siteTags := []string{"年下", "娱乐圈"}
	autoTags := []string{"纯爱", "娱乐圈", "校园青春"}
	merged := MergeTags(siteTags, autoTags)
	// Should have: 年下, 娱乐圈, 纯爱, 校园青春 (4 unique)
	if len(merged) != 4 {
		t.Errorf("expected 4 unique tags, got %d: %v", len(merged), merged)
	}
	// Site tags should come first
	if merged[0] != "年下" || merged[1] != "娱乐圈" {
		t.Errorf("site tags should come first, got %v", merged)
	}
}

func TestMergeTags_Empty(t *testing.T) {
	merged := MergeTags(nil, nil)
	if len(merged) != 0 {
		t.Errorf("expected empty, got %v", merged)
	}
	merged = MergeTags([]string{"a"}, nil)
	if !reflect.DeepEqual(merged, []string{"a"}) {
		t.Errorf("expected [a], got %v", merged)
	}
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func TestTagPatterns_NoOverlap(t *testing.T) {
	// Each keyword should map to exactly one tag
	seen := map[string]string{}
	for _, p := range tagPatterns {
		for _, kw := range p.keywords {
			if existing, ok := seen[kw]; ok {
				t.Errorf("keyword %q appears in both %q and %q", kw, existing, p.tag)
			}
			seen[kw] = p.tag
		}
	}
}

func TestTagPatterns_Sorted(t *testing.T) {
	// Verify tags are deterministic by sorting
	tags1 := AutoDetectTags("总裁的重生修仙之路", "穿越豪门都市修真")
	tags2 := AutoDetectTags("总裁的重生修仙之路", "穿越豪门都市修真")
	if !reflect.DeepEqual(tags1, tags2) {
		t.Errorf("tags should be deterministic: %v vs %v", tags1, tags2)
	}
	sort.Strings(tags1)
	sort.Strings(tags2)
}
