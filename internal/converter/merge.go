package converter

import (
	"regexp"
	"strings"
)

// rePageTitle matches the synthetic titles the downloader gives to a book page
// that carries only the continuation of a chapter.
var rePageTitle = regexp.MustCompile(`^(?:Page[ \t]+\d+|第[ \t]*\d+[ \t]*页)$`)

// IsPageTitle reports whether a chapter title is a synthetic page title rather
// than a real chapter heading.
func IsPageTitle(title string) bool {
	return rePageTitle.MatchString(strings.TrimSpace(title))
}

// MergePageContinuations folds continuation pages into the chapter they belong
// to, so navigation lists real chapters only. It is streaming: at most one
// chapter is buffered. Download failures keep their own entry instead of
// corrupting the chapter they belong to.
func MergePageContinuations(source ChapterSource) ChapterSource {
	return func(yield func(Chapter) error) error {
		var pending *Chapter
		flush := func() error {
			if pending == nil {
				return nil
			}
			chapter := *pending
			pending = nil
			return yield(chapter)
		}
		err := source(func(chapter Chapter) error {
			if !IsPageTitle(chapter.Title) {
				if err := flush(); err != nil {
					return err
				}
				buffered := chapter
				pending = &buffered
				return nil
			}
			if strings.TrimSpace(chapter.Content) == "" {
				return nil
			}
			if strings.HasPrefix(strings.TrimSpace(chapter.Content), "[Download failed:") {
				if err := flush(); err != nil {
					return err
				}
				return yield(chapter)
			}
			if pending == nil {
				// A book whose first page is a continuation keeps its text.
				return yield(chapter)
			}
			if pending.Content == "" {
				pending.Content = chapter.Content
			} else {
				pending.Content += "\n" + chapter.Content
			}
			return nil
		})
		if err != nil {
			return err
		}
		return flush()
	}
}
