package search

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"", ""},
		{"abc", "abc"},
		{"Hello World", "helloworld"},
		{"   trim   ", "trim"},
		{"霸道总裁", "霸道总裁"},
		{"ABC-123!@#", "abc123"},
		{"你好，世界！", "你好世界"},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.out {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestQueryVariantsDeduplicatesAndCompacts(t *testing.T) {
	got := QueryVariants("《一周一次》，作者名", 4)
	want := []string{"《一周一次》，作者名", "一周一次", "作者名", "一周一次作者名"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("QueryVariants() = %#v, want %#v", got, want)
	}
}

func TestQueryVariantsBounded(t *testing.T) {
	got := QueryVariants("甲，乙，丙，丁", 2)
	if len(got) != 2 {
		t.Fatalf("got %d variants, want 2: %#v", len(got), got)
	}
}

func TestQueryVariantsAddsBoundedCJKFuzzyFallbacks(t *testing.T) {
	got := QueryVariants("一周一次", 4)
	want := []string{"一周一次", "一周一", "周一次"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("QueryVariants() = %#v, want %#v", got, want)
	}
}
