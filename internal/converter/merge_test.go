package converter

import (
	"errors"
	"testing"
)

func TestIsPageTitle(t *testing.T) {
	for _, title := range []string{"Page 2", "Page  12", "第12页", "第 12 页"} {
		if !IsPageTitle(title) {
			t.Fatalf("IsPageTitle(%q) = false; want true", title)
		}
	}
	for _, title := range []string{"", "Page2", "第1章", "第12页 上", "番外"} {
		if IsPageTitle(title) {
			t.Fatalf("IsPageTitle(%q) = true; want false", title)
		}
	}
}

func collect(t *testing.T, source ChapterSource) []Chapter {
	t.Helper()
	var chapters []Chapter
	if err := source(func(chapter Chapter) error {
		chapters = append(chapters, chapter)
		return nil
	}); err != nil {
		t.Fatalf("source failed: %v", err)
	}
	return chapters
}

func TestMergePageContinuationsFoldsContinuationPages(t *testing.T) {
	source := MergePageContinuations(sliceSource([]Chapter{
		{Index: 1, Title: "第1章", Content: "a"},
		{Index: 2, Title: "Page 2", Content: "b"},
		{Index: 3, Title: "Page 3", Content: ""},
		{Index: 4, Title: "第2章", Content: "c"},
		{Index: 5, Title: "Page 4", Content: "d"},
	}))
	chapters := collect(t, source)
	want := []Chapter{{Index: 1, Title: "第1章", Content: "a\nb"}, {Index: 4, Title: "第2章", Content: "c\nd"}}
	if len(chapters) != len(want) {
		t.Fatalf("chapters = %#v; want %d chapters", chapters, len(want))
	}
	for index, expected := range want {
		if chapters[index].Title != expected.Title || chapters[index].Content != expected.Content {
			t.Fatalf("chapter %d = %#v; want %#v", index, chapters[index], expected)
		}
	}
}

func TestMergePageContinuationsKeepsLeadingAndFailedPages(t *testing.T) {
	source := MergePageContinuations(sliceSource([]Chapter{
		{Index: 1, Title: "Page 1", Content: "x"},
		{Index: 2, Title: "第1章", Content: "a"},
		{Index: 3, Title: "Page 5", Content: "[Download failed: boom]\nSource: url"},
		{Index: 4, Title: "Page 6", Content: "tail"},
	}))
	chapters := collect(t, source)
	want := []Chapter{
		{Index: 1, Title: "Page 1", Content: "x"},
		{Index: 2, Title: "第1章", Content: "a"},
		{Index: 3, Title: "Page 5", Content: "[Download failed: boom]\nSource: url"},
		{Index: 4, Title: "Page 6", Content: "tail"},
	}
	if len(chapters) != len(want) {
		t.Fatalf("chapters = %#v; want %d chapters", chapters, len(want))
	}
	for index, expected := range want {
		if chapters[index].Title != expected.Title || chapters[index].Content != expected.Content {
			t.Fatalf("chapter %d = %#v; want %#v", index, chapters[index], expected)
		}
	}
}

func TestMergePageContinuationsFillsEmptyChapter(t *testing.T) {
	source := MergePageContinuations(sliceSource([]Chapter{
		{Index: 1, Title: "第9章", Content: ""},
		{Index: 2, Title: "Page 16", Content: "body"},
	}))
	chapters := collect(t, source)
	if len(chapters) != 1 || chapters[0].Title != "第9章" || chapters[0].Content != "body" {
		t.Fatalf("chapters = %#v; want one 第9章 chapter holding body", chapters)
	}
}

func TestMergePageContinuationsPropagatesErrors(t *testing.T) {
	writerErr := errors.New("writer failed")
	source := MergePageContinuations(sliceSource([]Chapter{
		{Title: "第1章", Content: "a"},
		{Title: "Page 2", Content: "b"},
	}))
	err := source(func(chapter Chapter) error { return writerErr })
	if !errors.Is(err, writerErr) {
		t.Fatalf("merged source lost the writer error: %v", err)
	}
}
