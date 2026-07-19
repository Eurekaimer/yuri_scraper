package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/converter"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
)

func (e *Engine) writeOutput(ctx context.Context, adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, paths map[int]string, failed []FailedChapter) (string, error) {
	if err := os.MkdirAll(e.cfg.OutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}
	var rawText strings.Builder
	failedByIndex := make(map[int]FailedChapter, len(failed))
	for _, item := range failed {
		failedByIndex[item.Index] = item
	}
	for i, chapter := range chapters {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if path := paths[i]; path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			rawText.Write(data)
			rawText.WriteByte('\n')
			continue
		}
		if item, ok := failedByIndex[i]; ok {
			fmt.Fprintf(&rawText, "\n第%d章 %s\n【下载失败：%s】\n", i+1, chapter.Title, item.Error)
		}
	}
	ordered := converter.SplitChapters(rawText.String(), 1)
	if len(ordered) == 0 {
		return "", fmt.Errorf("no content to write")
	}
	description, protagonist, tags := meta.Description, meta.Protagonist, meta.Tags
	if provider, ok := adapter.(search.BookMetaProvider); ok {
		if detail, err := provider.GetBookMeta(ctx, meta.BookURL); err == nil && detail != nil {
			if detail.Description != "" {
				description = detail.Description
			}
			if detail.Protagonist != "" {
				protagonist = detail.Protagonist
			}
			if len(detail.Tags) > 0 {
				tags = converter.MergeTags(tags, detail.Tags)
			}
		}
	}
	bookMeta := converter.BookMeta{Title: meta.Title, Author: orDefault(meta.Author, "Unknown"), Description: description, Protagonist: protagonist, Tags: tags, Language: "zh-CN", Chapters: ordered}
	ext := ".txt"
	if e.cfg.Format == "epub" {
		ext = ".epub"
	}
	outFile := filepath.Join(e.cfg.OutputDir, safeFilename(meta.Title)+ext)
	tmpFile := outFile + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return "", err
	}
	if ext == ".epub" {
		err = converter.WriteEPUB(f, bookMeta)
	} else {
		err = converter.WriteTXT(f, ordered)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	if err := os.Rename(tmpFile, outFile); err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	log.Printf("Saved: %s (%d chapters)", outFile, len(ordered))
	return outFile, nil
}

func SaveFailedChapters(outputDir, bookTitle string, failed []FailedChapter) (string, error) {
	if len(failed) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outputDir, safeFilename(bookTitle)+".failed.json")
	data, err := json.MarshalIndent(failed, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func safeFilename(s string) string {
	repl := map[rune]rune{'/': '_', '\\': '_', ':': '_', '*': '_', '?': '_', '"': '_', '<': '_', '>': '_', '|': '_'}
	cleaned := strings.Map(func(r rune) rune {
		if v, ok := repl[r]; ok {
			return v
		}
		return r
	}, s)
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" {
		return "book"
	}
	return cleaned
}
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
func writeChapterCheckpoint(cacheDir, chapterURL, content string) error {
	path := filepath.Join(cacheDir, shortHash(chapterURL)+".txt")
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
func bookCacheKey(sourceName, rawURL string) string {
	if parsed, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		parsed.Fragment = ""
		rawURL = parsed.String()
	}
	return sourceName + "\n" + rawURL
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
