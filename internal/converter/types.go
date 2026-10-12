package converter

// Chapter represents a cleaned chapter ready for output.
type Chapter struct {
	Index   int
	Title   string
	Content string
}

// ChapterSource yields chapters in reading order and stops when the writer
// returns an error. Producers need retain only the current page or chapter.
type ChapterSource func(yield func(Chapter) error) error

type BookMeta struct {
	Title       string
	Author      string
	Description string   // synopsis
	Protagonist string   // main character(s)
	Tags        []string // genre / category tags
	Language    string   // "zh-CN" etc, default "zh-CN"
}
