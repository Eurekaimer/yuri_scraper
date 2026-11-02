package repair

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/converter"
)

const adLine = "小贴士:找看好看得小说，就来52书库呀~www.52shuku.net"

func writeFixture(t *testing.T, path string) {
	t.Helper()
	source := converter.ChapterSource(func(yield func(converter.Chapter) error) error {
		for _, chapter := range []converter.Chapter{
			{Title: "第1章", Content: "沈楝第一次见到郑汀雨。\n" + adLine + "\n她漫无目的的逛着。\n她的的事，没人知道。"},
			{Title: "Page 2", Content: "续文第一段。\n续文第二段。"},
			{Title: "Page 3", Content: ""},
			{Title: "第2章", Content: "第二章正文。"},
		} {
			if err := yield(chapter); err != nil {
				return err
			}
		}
		return nil
	})
	var book bytes.Buffer
	if err := converter.WriteEPUBStream(&book, converter.BookMeta{Title: "测试书", Author: "作者"}, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, book.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func readArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := make(map[string]string, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = string(content)
	}
	return entries
}

func TestBuildMergesPagesRebuildsNavigationAndDropsAds(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "book.epub")
	writeFixture(t, input)
	output := filepath.Join(directory, "repaired.epub")
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	stats, buildErr := Build(input, file)
	if err := file.Close(); buildErr == nil {
		buildErr = err
	}
	if buildErr != nil {
		t.Fatalf("Build failed: %v", buildErr)
	}
	if stats.Chapters != 2 || stats.MergedPages != 2 || stats.RemovedLines != 1 || stats.Paragraphs != 6 {
		t.Fatalf("stats = %#v; want 2 chapters, 2 merged pages, 1 removed line, 6 paragraphs", stats)
	}

	entries := readArchive(t, output)
	opf := entries["OEBPS/content.opf"]
	for _, want := range []string{"<dc:title>测试书</dc:title>", "<dc:creator>作者</dc:creator>"} {
		if !strings.Contains(opf, want) {
			t.Fatalf("package metadata lost %q:\n%s", want, opf)
		}
	}
	nav := entries["OEBPS/nav.xhtml"]
	if count := strings.Count(nav, "<li>"); count != 2 {
		t.Fatalf("navigation has %d entries; want 2:\n%s", count, nav)
	}
	if strings.Contains(nav, "Page") {
		t.Fatalf("navigation still lists a page title:\n%s", nav)
	}
	if count := strings.Count(entries["OEBPS/toc.ncx"], "<navPoint "); count != 2 {
		t.Fatalf("toc.ncx has %d entries; want 2", count)
	}

	var body strings.Builder
	paragraphs := 0
	for name, content := range entries {
		if !strings.HasPrefix(name, "OEBPS/chapter_") {
			continue
		}
		body.WriteString(content)
		paragraphs += strings.Count(content, "<p>")
	}
	if paragraphs != 6 {
		t.Fatalf("body has %d paragraphs; want 6", paragraphs)
	}
	text := body.String()
	for _, want := range []string{"续文第二段。", "她漫无目的地逛着。", "她的事，没人知道。", "第二章正文。"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repaired body lost %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"52shuku", "小贴士", "的的"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("repaired body still contains %q:\n%s", unwanted, text)
		}
	}
}

func TestBuildSplitsParagraphsCollapsedIntoOneLine(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "book.epub")
	source := converter.ChapterSource(func(yield func(converter.Chapter) error) error {
		// One <p> holding what were three separate paragraphs.
		return yield(converter.Chapter{Title: "第1章", Content: "第一段句子。      第二段句子。      第三段句子。"})
	})
	var book bytes.Buffer
	if err := converter.WriteEPUBStream(&book, converter.BookMeta{Title: "折叠", Author: "作者"}, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, book.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "repaired.epub")
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	stats, buildErr := Build(input, file)
	if err := file.Close(); buildErr == nil {
		buildErr = err
	}
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	if stats.Paragraphs != 3 {
		t.Fatalf("collapsed line produced %d paragraphs, want 3", stats.Paragraphs)
	}
	body := readArchive(t, output)["OEBPS/chapter_001.xhtml"]
	for _, want := range []string{"<p>第一段句子。</p>", "<p>第二段句子。</p>", "<p>第三段句子。</p>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("repaired body lost paragraph %q:\n%s", want, body)
		}
	}
}

func TestBuildRejectsMissingPackage(t *testing.T) {
	output := bytes.Buffer{}
	if _, err := Build(filepath.Join(t.TempDir(), "absent.epub"), &output); err == nil {
		t.Fatal("Build accepted a missing input file")
	}
}
