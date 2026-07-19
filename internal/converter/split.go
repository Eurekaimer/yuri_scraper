package converter

import (
	"regexp"
	"strings"
)

var reChapterHead = regexp.MustCompile(`(?m)^第[\d零一二三四五六七八九十百千]+章[^\n]*`)

// SplitChapters scans text for "第N章" markers and returns structured chapters.
// If fewer than 3 chapter markers are found, returns the original text as one chapter.
func SplitChapters(rawText string, baseIndex int) []Chapter {
	// Find all chapter marker positions
	locs := reChapterHead.FindAllStringIndex(rawText, -1)
	if len(locs) < 3 {
		// Not enough markers — treat as one page
		return []Chapter{{
			Index:   baseIndex,
			Title:   "",
			Content: rawText,
		}}
	}

	var chapters []Chapter
	for i, loc := range locs {
		start := loc[0]
		var end int
		if i+1 < len(locs) {
			end = locs[i+1][0]
		} else {
			end = len(rawText)
		}
		title := strings.TrimSpace(rawText[loc[0]:loc[1]])
		content := strings.TrimSpace(rawText[start:end])

		chapters = append(chapters, Chapter{
			Index:   baseIndex + i,
			Title:   title,
			Content: content,
		})
	}
	return chapters
}
