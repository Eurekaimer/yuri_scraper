package converter

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSplitChaptersKeepsSingleAndDoubleMarkersAndPreface(t *testing.T) {
	for _, test := range []struct {
		text   string
		titles []string
		bodies []string
	}{
		{text: "第1章 One\nfirst body", titles: []string{"第1章 One"}, bodies: []string{"first body"}},
		{text: "第1章 One\nfirst body\n第2章 Two\nsecond body", titles: []string{"第1章 One", "第2章 Two"}, bodies: []string{"first body", "second body"}},
		{text: "preface\n第1章 One\nfirst body", titles: []string{"", "第1章 One"}, bodies: []string{"preface", "first body"}},
	} {
		chapters := SplitChapters(test.text, 7)
		if len(chapters) != len(test.titles) {
			t.Fatalf("lost actual boundaries for %q: %+v", test.text, chapters)
		}
		for i, chapter := range chapters {
			if chapter.Index != 7+i || chapter.Title != test.titles[i] || chapter.Content != test.bodies[i] {
				t.Fatalf("chapter %d lost heading, preface or body: %+v", i, chapter)
			}
		}
	}
}

func TestTXTStreamWritesBeforeRequestingNextChapter(t *testing.T) {
	var output bytes.Buffer
	producerErr := errors.New("checkpoint read failed")
	err := WriteTXTStream(&output, func(yield func(Chapter) error) error {
		if err := yield(Chapter{Title: "One", Content: "first body"}); err != nil {
			return err
		}
		if !strings.Contains(output.String(), "first body") {
			t.Fatal("writer aggregated chapters before writing the first body")
		}
		return producerErr
	})
	if !errors.Is(err, producerErr) {
		t.Fatalf("producer error was lost: %v", err)
	}
	if !strings.Contains(output.String(), "first body") {
		t.Fatal("already-streamed content disappeared after a later read error")
	}
}

func TestStreamingWritersStopProducerOnWriterFailure(t *testing.T) {
	for name, writer := range map[string]func(io.Writer, ChapterSource) error{
		"txt": WriteTXTStream,
		"epub": func(w io.Writer, source ChapterSource) error {
			return WriteEPUBStream(w, BookMeta{Title: "book"}, source)
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := writer(failingWriter{}, func(yield func(Chapter) error) error {
				if err := yield(Chapter{Title: "One", Content: strings.Repeat("content", 10000)}); err != nil {
					return err
				}
				t.Fatal("producer continued after output storage failed")
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "disk full") {
				t.Fatalf("writer failure was lost: %v", err)
			}
		})
	}
}

func TestEPUBNavigationTargetsFilesRegardlessOfCallerIndices(t *testing.T) {
	var output bytes.Buffer
	meta := BookMeta{Title: "book"}
	source := sliceSource([]Chapter{{Index: 40, Title: "First", Content: "one"}, {Index: 0, Title: "Second", Content: "two"}})
	if err := WriteEPUBStream(&output, meta, source); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OEBPS/nav.xhtml", "OEBPS/toc.ncx"} {
		file := findZipFile(archive, name)
		if file == nil {
			t.Fatalf("missing navigation %s", name)
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoder := xml.NewDecoder(bytes.NewReader(data))
		var targets []string
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if start, ok := token.(xml.StartElement); ok {
				for _, attribute := range start.Attr {
					if (start.Name.Local == "a" && attribute.Name.Local == "href") || (start.Name.Local == "content" && attribute.Name.Local == "src") {
						targets = append(targets, attribute.Value)
					}
				}
			}
		}
		if len(targets) != 2 || targets[0] != "chapter_001.xhtml" || targets[1] != "chapter_002.xhtml" {
			t.Fatalf("%s has broken file targets: %v", name, targets)
		}
		for _, target := range targets {
			if findZipFile(archive, "OEBPS/"+target) == nil {
				t.Fatalf("navigation references nonexistent file %s", target)
			}
		}
	}
}
