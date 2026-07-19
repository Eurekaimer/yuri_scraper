package converter

// Chapter represents a cleaned chapter ready for output.
type Chapter struct {
	Index   int
	Title   string
	Content string
}

type BookMeta struct {
	Title       string
	Author      string
	Description string   // synopsis
	Protagonist string   // main character(s)
	Tags        []string // genre / category tags
	Language    string   // "zh-CN" etc, default "zh-CN"
	Chapters    []Chapter
}
