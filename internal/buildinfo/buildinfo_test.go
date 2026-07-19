package buildinfo

import "testing"

func TestVersionFormatting(t *testing.T) {
	tests := []struct{ input, label, full string }{
		{"0.6.1", "v0.6.1", "YuriScraper v0.6.1"},
		{"v0.6.1", "v0.6.1", "YuriScraper v0.6.1"},
		{"", "dev", "YuriScraper dev"},
		{" dev ", "dev", "YuriScraper dev"},
	}
	for _, tt := range tests {
		if got := VersionLabel(tt.input); got != tt.label {
			t.Errorf("VersionLabel(%q) = %q, want %q", tt.input, got, tt.label)
		}
		if got := FullVersion(tt.input); got != tt.full {
			t.Errorf("FullVersion(%q) = %q, want %q", tt.input, got, tt.full)
		}
	}
}
