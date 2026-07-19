package converter

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestWriteTXT_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := WriteTXT(&buf, nil)
	if err != nil {
		t.Fatalf("WriteTXT(nil): %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got %d bytes", buf.Len())
	}
}

func TestWriteTXT_SingleChapter(t *testing.T) {
	chapters := []Chapter{
		{Index: 0, Title: "第一章", Content: "故事开始了。"},
	}
	var buf bytes.Buffer
	err := WriteTXT(&buf, chapters)
	if err != nil {
		t.Fatalf("WriteTXT: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "第一章") {
		t.Error("output missing chapter title")
	}
	if !strings.Contains(out, "故事开始了。") {
		t.Error("output missing chapter content")
	}
	if !strings.Contains(out, "━") {
		t.Error("output missing chapter separator")
	}
}

func TestWriteTXT_OrderPreserved(t *testing.T) {
	chapters := []Chapter{
		{Index: 1, Title: "第2章", Content: "second"},
		{Index: 0, Title: "第1章", Content: "first"},
	}
	var buf bytes.Buffer
	if err := WriteTXT(&buf, chapters); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	idx1 := strings.Index(out, "first")
	idx2 := strings.Index(out, "second")
	if idx1 < 0 || idx2 < 0 {
		t.Fatal("missing content")
	}
	// chapters are written in the order they appear in the slice
	if idx2 >= idx1 {
		t.Error("expected first chapter before second in output order")
	}
}

func TestWriteEPUB_ValidZip(t *testing.T) {
	chapters := []Chapter{
		{Index: 0, Title: "第一章 序", Content: "<p>测试内容</p>"},
	}
	meta := BookMeta{
		Title:    "测试书名",
		Author:   "测试作者",
		Chapters: chapters,
	}
	var buf bytes.Buffer
	err := WriteEPUB(&buf, meta)
	if err != nil {
		t.Fatalf("WriteEPUB: %v", err)
	}

	// Verify it's a valid ZIP
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("not a valid ZIP: %v", err)
	}

	required := map[string]bool{
		"mimetype":               true,
		"META-INF/container.xml": true,
		"OEBPS/content.opf":      true,
		"OEBPS/toc.ncx":          true,
		"OEBPS/nav.xhtml":        true,
	}
	for _, f := range zr.File {
		delete(required, f.Name)
	}
	if len(required) > 0 {
		for name := range required {
			t.Errorf("missing required EPUB file: %s", name)
		}
	}

	// Verify mimetype content
	mimeFile := findZipFile(zr, "mimetype")
	if mimeFile == nil {
		t.Fatal("mimetype not found")
	}
	rc, _ := mimeFile.Open()
	data, _ := io.ReadAll(rc)
	rc.Close()
	if strings.TrimSpace(string(data)) != "application/epub+zip" {
		t.Errorf("wrong mimetype: %q", string(data))
	}

	// Verify container references content.opf
	containerFile := findZipFile(zr, "META-INF/container.xml")
	if containerFile == nil {
		t.Fatal("container.xml not found")
	}
	rc, _ = containerFile.Open()
	containerData, _ := io.ReadAll(rc)
	rc.Close()
	if !strings.Contains(string(containerData), "content.opf") {
		t.Error("container.xml does not reference content.opf")
	}
}

func TestWriteEPUB_ChapterFilesPresent(t *testing.T) {
	chapters := []Chapter{
		{Index: 0, Title: "第一章", Content: "one"},
		{Index: 1, Title: "第二章", Content: "two"},
	}
	meta := BookMeta{Title: "书", Author: "人", Chapters: chapters}
	var buf bytes.Buffer
	if err := WriteEPUB(&buf, meta); err != nil {
		t.Fatal(err)
	}

	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "OEBPS/chapter-") && strings.HasSuffix(f.Name, ".xhtml") {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			content := string(data)
			if !strings.Contains(content, "<html") {
				t.Errorf("%s: missing <html> tag", f.Name)
			}
		}
	}
}

func TestWriteEPUB_MinimalMeta(t *testing.T) {
	meta := BookMeta{Title: "", Author: "", Chapters: nil}
	var buf bytes.Buffer
	err := WriteEPUB(&buf, meta)
	if err != nil {
		t.Fatalf("WriteEPUB with empty meta: %v", err)
	}
	// Should not panic or error — must still produce valid ZIP
	if _, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatalf("empty meta EPUB is not valid ZIP: %v", err)
	}
}

func TestWriteEPUB_SpecialCharsInTitle(t *testing.T) {
	chapters := []Chapter{
		{Index: 0, Title: "序", Content: "start"},
	}
	meta := BookMeta{
		Title:    "测试: 书名<>&\"'",
		Author:   "作者",
		Chapters: chapters,
	}
	var buf bytes.Buffer
	err := WriteEPUB(&buf, meta)
	if err != nil {
		t.Fatalf("WriteEPUB with special chars: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	opfFile := findZipFile(zr, "OEBPS/content.opf")
	if opfFile == nil {
		t.Fatal("content.opf not found")
	}
	rc, _ := opfFile.Open()
	data, _ := io.ReadAll(rc)
	rc.Close()
	if !strings.Contains(string(data), "测试") {
		t.Error("content.opf missing title")
	}
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}
