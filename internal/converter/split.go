package converter

import (
	"regexp"
	"strings"
)

var reChapterHead = regexp.MustCompile(`(?m)^第[\d零一二三四五六七八九十百千]+章[^\n]*`)

// SplitChapters preserves every real chapter marker within a fetched page,
// including pages with only one or two chapters and text before the first marker.
func SplitChapters(rawText string, baseIndex int) []Chapter {
	locs := reChapterHead.FindAllStringIndex(rawText, -1)
	if len(locs) == 0 {
		return []Chapter{{Index: baseIndex, Content: rawText}}
	}
	chapters := make([]Chapter, 0, len(locs)+1)
	if prefix := strings.TrimSpace(rawText[:locs[0][0]]); prefix != "" {
		chapters = append(chapters, Chapter{Index: baseIndex, Content: prefix})
	}
	for i, loc := range locs {
		end := len(rawText)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		chapters = append(chapters, Chapter{
			Index:   baseIndex + len(chapters),
			Title:   strings.TrimSpace(rawText[loc[0]:loc[1]]),
			Content: strings.TrimSpace(rawText[loc[1]:end]),
		})
	}
	return chapters
}
