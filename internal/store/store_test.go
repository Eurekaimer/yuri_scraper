package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.OutputDir != "novels" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "novels")
	}
	if cfg.Format != "epub" {
		t.Errorf("Format = %q, want %q", cfg.Format, "epub")
	}
	if cfg.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", cfg.Concurrency)
	}
	if cfg.RateLimit != 2.0 {
		t.Errorf("RateLimit = %f, want 2.0", cfg.RateLimit)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3", cfg.RetryCount)
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.OutputDir != "novels" {
		t.Errorf("expected default OutputDir, got %q", cfg.OutputDir)
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("output_dir: mybooks\nformat: txt\nconcurrency: 4\nrate_limit: 1.5\nretry_count: 5\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.OutputDir != "mybooks" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "mybooks")
	}
	if cfg.Format != "txt" {
		t.Errorf("Format = %q, want %q", cfg.Format, "txt")
	}
	if cfg.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", cfg.Concurrency)
	}
	if cfg.RateLimit != 1.5 {
		t.Errorf("RateLimit = %f, want 1.5", cfg.RateLimit)
	}
	if cfg.RetryCount != 5 {
		t.Errorf("RetryCount = %d, want 5", cfg.RetryCount)
	}
}

func TestLoadConfig_PartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("concurrency: 2\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Concurrency != 2 {
		t.Errorf("Concurrency = %d, want 2", cfg.Concurrency)
	}
	if cfg.OutputDir != "novels" {
		t.Errorf("OutputDir should be default, got %q", cfg.OutputDir)
	}
}

func TestSaveAndLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := Config{
		OutputDir:   "downloads",
		Format:      "epub",
		Concurrency: 10,
		RateLimit:   3.0,
		RetryCount:  2,
	}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.OutputDir != cfg.OutputDir {
		t.Errorf("OutputDir roundtrip: %q != %q", loaded.OutputDir, cfg.OutputDir)
	}
	if loaded.Concurrency != cfg.Concurrency {
		t.Errorf("Concurrency roundtrip: %d != %d", loaded.Concurrency, cfg.Concurrency)
	}
}

func TestLoadConfigMergesMissingYamiboDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("output_dir: books\nformat: txt\nconcurrency: 4\nrate_limit: 1\nretry_count: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Yamibo.MaxConcurrency != 2 || cfg.Yamibo.RateLimit != 1.2 || cfg.Yamibo.MaxScanPages != 10 || cfg.Yamibo.MinPostChars != 80 {
		t.Fatalf("missing defaults were not merged: %#v", cfg.Yamibo)
	}
}

func TestLoadConfigRejectsUnsafeYamiboRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "output_dir: books\nformat: txt\nconcurrency: 4\nrate_limit: 1\nretry_count: 2\nyamibo:\n  rate_limit: 2.5\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "不超过 1.2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelPathResolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("output_dir: ./downloads\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if filepath.IsAbs(cfg.OutputDir) {
		t.Errorf("expected relative path, got absolute: %s", cfg.OutputDir)
	}
	if cfg.OutputDir != "downloads" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "downloads")
	}
}

// --- History tests ---

func TestLoadHistory_NoFile(t *testing.T) {
	h, err := LoadHistory(t.TempDir())
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(h.Entries))
	}
}

func TestHistory_AddAndList(t *testing.T) {
	dir := t.TempDir()
	h, _ := LoadHistory(dir)

	e1 := HistoryEntry{Title: "书1", Author: "作者A", BookURL: "http://a", SiteName: "站点1", Format: "epub", Chapters: 100}
	e2 := HistoryEntry{Title: "书2", Author: "作者B", BookURL: "http://b", SiteName: "站点2", Format: "txt", Chapters: 50}

	if err := h.Add(e1); err != nil {
		t.Fatalf("Add e1: %v", err)
	}
	if err := h.Add(e2); err != nil {
		t.Fatalf("Add e2: %v", err)
	}

	list := h.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	// List returns newest first
	if list[0].Title != "书2" {
		t.Errorf("newest first: got %q", list[0].Title)
	}
	if list[1].Title != "书1" {
		t.Errorf("oldest second: got %q", list[1].Title)
	}
}

func TestHistory_Exists(t *testing.T) {
	dir := t.TempDir()
	h, _ := LoadHistory(dir)
	h.Add(HistoryEntry{Title: "x", BookURL: "http://exists", Chapters: 1})

	if !h.Exists("http://exists") {
		t.Error("Exists returned false for known URL")
	}
	if h.Exists("http://missing") {
		t.Error("Exists returned true for unknown URL")
	}
}

func TestHistory_Persistence(t *testing.T) {
	dir := t.TempDir()
	h, _ := LoadHistory(dir)
	h.Add(HistoryEntry{Title: "书", BookURL: "http://x", Chapters: 10, Format: "epub", SiteName: "test"})

	// Reload
	h2, err := LoadHistory(dir)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(h2.Entries) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(h2.Entries))
	}
	if h2.Entries[0].Title != "书" {
		t.Errorf("Title = %q", h2.Entries[0].Title)
	}
}

func TestHistory_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".history.json")
	os.WriteFile(path, []byte("not json"), 0644)
	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatalf("LoadHistory should not error on corrupt file: %v", err)
	}
	if len(h.Entries) != 0 {
		t.Errorf("expected 0 entries from corrupt file, got %d", len(h.Entries))
	}
}
