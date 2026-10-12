package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Eurekaimer/yuri_scraper/internal/converter"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
)

func (e *Engine) writeOutput(ctx context.Context, adapter search.SiteAdapter, meta search.SearchResult, chapters []search.Chapter, paths []string, failed []FailedChapter) (string, error) {
	failedByIndex := make(map[int]FailedChapter, len(failed))
	for _, item := range failed {
		failedByIndex[item.Index] = item
	}
	description, protagonist, tags := meta.Description, meta.Protagonist, meta.Tags
	if provider, ok := adapter.(search.BookMetaProvider); ok {
		detail, _ := provider.GetBookMeta(ctx, meta.BookURL)
		if detail != nil {
			if detail.Description != "" {
				description = detail.Description
			}
			if detail.Protagonist != "" {
				protagonist = detail.Protagonist
			}
			if len(detail.Tags) > 0 {
				tags = detail.Tags
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	bookMeta := converter.BookMeta{Title: meta.Title, Author: orDefault(meta.Author, "Unknown"), Description: description, Protagonist: protagonist, Tags: tags, Language: "zh-CN"}
	// Page bodies are read only when the previous page's actual chapters have
	// been written. Navigation retains titles, never the complete book text.
	source := converter.ChapterSource(func(yield func(converter.Chapter) error) error {
		index := 1
		for i, page := range chapters {
			if err := ctx.Err(); err != nil {
				return err
			}
			if failure, missing := failedByIndex[i]; missing {
				chapter := converter.Chapter{Index: index, Title: orDefault(page.Title, fmt.Sprintf("Page %d", i+1)), Content: fmt.Sprintf("[Download failed: %s]\nSource: %s", failure.Error, failure.URL)}
				if err := yield(chapter); err != nil {
					return err
				}
				index++
				continue
			}
			data, err := os.ReadFile(paths[i])
			if err != nil {
				return fmt.Errorf("read chapter checkpoint: %w", err)
			}
			actual := converter.SplitChapters(string(data), index)
			for _, chapter := range actual {
				if err := ctx.Err(); err != nil {
					return err
				}
				if chapter.Title == "" {
					chapter.Title = orDefault(page.Title, fmt.Sprintf("Page %d", i+1))
				}
				if err := yield(chapter); err != nil {
					return err
				}
				index++
			}
		}
		return nil
	})
	outFile := filepath.Join(e.cfg.OutputDir, bookOutputStem(meta)+"."+e.cfg.Format)
	tmp, err := os.CreateTemp(e.cfg.OutputDir, ".yuri-export-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	writer := contextWriter{context: ctx, writer: tmp}
	if e.cfg.Format == "epub" {
		err = converter.WriteEPUBStream(writer, bookMeta, source)
	} else {
		err = converter.WriteTXTStream(writer, source)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), outFile); err != nil {
		return "", err
	}
	return outFile, nil
}

type contextWriter struct {
	context context.Context
	writer  io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.context.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}

func saveFailedChapters(path string, failed []FailedChapter) (string, error) {
	if len(failed) == 0 {
		return "", nil
	}
	data, err := json.MarshalIndent(failed, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicPrivateWrite(path, string(data)); err != nil {
		return "", err
	}
	return path, nil
}

func bookOutputStem(meta search.SearchResult) string {
	return safeFilename(meta.Title) + "-" + shortHash(bookCacheKey(meta.SiteName, meta.BookURL))
}

func safeFilename(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune("/\\:*?\"<>|", r) {
			return '_'
		}
		return r
	}, s)
	cleaned = strings.Trim(cleaned, " .")
	// Leave room for the identity suffix and manifest extension on common filesystems.
	for len(cleaned) > 180 {
		_, size := utf8.DecodeLastRuneInString(cleaned)
		cleaned = cleaned[:len(cleaned)-size]
	}
	if cleaned == "" {
		return "book"
	}
	return cleaned
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func validCheckpoint(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("checkpoint is not a regular file: %s", path)
	}
	if info.Size() == 0 {
		return false, nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func writeChapterCheckpoint(cacheDir, chapterURL, content string) error {
	path := filepath.Join(cacheDir, shortHash(chapterURL)+".txt")
	if cached, err := validCheckpoint(path); err != nil {
		return err
	} else if cached {
		return nil
	}
	return atomicPrivateWrite(path, content)
}

func atomicPrivateWrite(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".yuri-checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.WriteString(tmp, content)
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func bookCacheKey(sourceName, rawURL string) string {
	if parsed, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		parsed.Fragment = ""
		parsed.RawQuery = parsed.Query().Encode()
		rawURL = parsed.String()
	}
	return sourceName + "\n" + rawURL
}
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
