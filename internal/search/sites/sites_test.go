package sites

import (
	"testing"
)

func TestHTMLUnescape(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"", ""},
		{"plain text", "plain text"},
		{"hello&nbsp;world", "hello world"},
		{"x &lt; y &amp;&amp; z &gt; 0", "x < y && z > 0"},
		{"&quot;hello&quot;", `"hello"`},
		{"it&#39;s", "it's"},
		{"a&nbsp;&nbsp;b", "a  b"},
	}
	for _, tt := range tests {
		got := htmlUnescape(tt.in)
		if got != tt.out {
			t.Errorf("htmlUnescape(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestRX(t *testing.T) {
	re := rx(`\d+`)
	if re == nil {
		t.Fatal("rx returned nil")
	}
	if !re.MatchString("abc123def") {
		t.Error("expected match")
	}
	if re.MatchString("abcdef") {
		t.Error("unexpected match")
	}
}
