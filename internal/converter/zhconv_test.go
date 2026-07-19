package converter

import (
	"testing"
)

func TestToSimplified_CommonChars(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"", ""},
		{"hello", "hello"},
		{"簡單", "简单"},
		{"我們是中國人", "我们是中国人"},
		{"學習與實踐", "学习与实践"},
		{"電腦網絡", "电脑网络"},
		{"繁體字測試", "繁体字测试"},
		{"已經完成", "已经完成"},
	}
	for _, tt := range tests {
		got := ToSimplified(tt.in)
		if got != tt.out {
			t.Errorf("ToSimplified(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestToSimplified_NoChange(t *testing.T) {
	// Already simplified text should remain unchanged
	input := "你好世界，这是简体中文测试"
	got := ToSimplified(input)
	if got != input {
		t.Errorf("simplified text modified: %q -> %q", input, got)
	}
}

func TestIsTraditional(t *testing.T) {
	tests := []struct {
		in   string
		trad bool
	}{
		{"", false},
		{"hello world", false},
		{"你好世界", false},             // simplified
		{"我們是中國人", true},            // traditional
		{"簡單的測試", true},             // traditional
		{"简体中文测试", false},           // simplified
		{"混合Traditional和简体", false}, // mixed, below threshold
	}
	for _, tt := range tests {
		got := IsTraditional(tt.in)
		if got != tt.trad {
			t.Errorf("IsTraditional(%q) = %v, want %v", tt.in, got, tt.trad)
		}
	}
}

func TestToSimplified_RoundTrip(t *testing.T) {
	// Converting traditional then checking IsTraditional should be false
	trad := "這是繁體中文的測試文本"
	got := ToSimplified(trad)
	if IsTraditional(got) {
		t.Errorf("after conversion, text should not be detected as traditional: %q", got)
	}
	if got == trad {
		t.Error("text was not converted")
	}
}
