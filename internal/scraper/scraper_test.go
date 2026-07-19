package scraper

import (
	"testing"
	"time"
)

func TestSpeedProfile_Concurrency(t *testing.T) {
	tests := []struct {
		profile SpeedProfile
		want    int64
	}{
		{SpeedSlow, 2},
		{SpeedNormal, 8},
		{SpeedFast, 16},
		{SpeedProfile(99), 8}, // unknown defaults to normal
	}
	for _, tt := range tests {
		got := tt.profile.concurrency()
		if got != tt.want {
			t.Errorf("%v concurrency() = %d, want %d", tt.profile, got, tt.want)
		}
	}
}

func TestSpeedProfile_RateLimit(t *testing.T) {
	tests := []struct {
		profile SpeedProfile
		want    float64
	}{
		{SpeedSlow, 0.5},
		{SpeedNormal, 2.0},
		{SpeedFast, 5.0},
		{SpeedProfile(99), 2.0},
	}
	for _, tt := range tests {
		got := tt.profile.rateLimit()
		if got != tt.want {
			t.Errorf("%v rateLimit() = %f, want %f", tt.profile, got, tt.want)
		}
	}
}

func TestSpeedProfile_String(t *testing.T) {
	tests := []struct {
		profile SpeedProfile
		want    string
	}{
		{SpeedSlow, "慢速"},
		{SpeedNormal, "正常"},
		{SpeedFast, "快速"},
	}
	for _, tt := range tests {
		got := tt.profile.String()
		if got != tt.want {
			t.Errorf("%d String() = %q, want %q", tt.profile, got, tt.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", cfg.Concurrency)
	}
	if cfg.RateLimit != 2.0 {
		t.Errorf("RateLimit = %f, want 2.0", cfg.RateLimit)
	}
	if cfg.OutputDir != "novels" {
		t.Errorf("OutputDir = %q, want novels", cfg.OutputDir)
	}
	if cfg.Format != "epub" {
		t.Errorf("Format = %q, want epub", cfg.Format)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3", cfg.RetryCount)
	}
	if cfg.RetryDelay != 2*time.Second {
		t.Errorf("RetryDelay = %v, want 2s", cfg.RetryDelay)
	}
	if cfg.SpeedProfile != SpeedNormal {
		t.Errorf("SpeedProfile = %v, want SpeedNormal", cfg.SpeedProfile)
	}
}

func TestConfig_ProfileFallback(t *testing.T) {
	// When Concurrency/RateLimit are 0, the profile values should be used.
	// This is tested indirectly via ScrapeBook, but we verify the logic.
	cfg := Config{
		Concurrency:  0,
		RateLimit:    0,
		SpeedProfile: SpeedFast,
		OutputDir:    "test",
		Format:       "txt",
		RetryCount:   1,
		RetryDelay:   time.Second,
	}
	if cfg.SpeedProfile.concurrency() != 16 {
		t.Errorf("SpeedFast concurrency = %d, want 16", cfg.SpeedProfile.concurrency())
	}
	if cfg.SpeedProfile.rateLimit() != 5.0 {
		t.Errorf("SpeedFast rateLimit = %f, want 5.0", cfg.SpeedProfile.rateLimit())
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"normal", "normal"},
		{"file:name", "file_name"},
		{"a/b\\c", "a_b_c"},
		{"test<>.txt", "test__.txt"},
		{"你好世界", "你好世界"},
		{"a?b*c\"d|e", "a_b_c_d_e"},
	}
	for _, tt := range tests {
		got := safeFilename(tt.in)
		if got != tt.out {
			t.Errorf("safeFilename(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestOrDefault(t *testing.T) {
	if got := orDefault("hello", "default"); got != "hello" {
		t.Errorf("orDefault(nonempty) = %q, want %q", got, "hello")
	}
	if got := orDefault("", "default"); got != "default" {
		t.Errorf("orDefault(empty) = %q, want %q", got, "default")
	}
}

func TestNewEngine(t *testing.T) {
	cfg := DefaultConfig()
	e := NewEngine(cfg)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.cfg.Concurrency != cfg.Concurrency {
		t.Error("engine config mismatch")
	}
}
