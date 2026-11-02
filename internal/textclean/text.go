// Package textclean normalizes Chinese body text and drops the 52shuku
// advertisement/navigation lines that scraping leaves in exported books.
package textclean

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

const (
	trimCutset  = " \t\r\n\u3000"
	sentenceEnd = "。，！？；：、"
)

// wordFixes are high-confidence confusions that appear in the scraped corpus.
// Order is fixed: an earlier fix must not create the pattern of a later one.
var wordFixes = []struct{ from, to string }{
	{"漫无目的的逛", "漫无目的地逛"},
	{"按耐", "按捺"},
	{"直接了当", "直截了当"},
	{"做为", "作为"},
	{"谈笑风声", "谈笑风生"},
}

var fullWidthPunctuation = map[rune]rune{
	',': '，',
	';': '；',
	':': '：',
	'!': '！',
	'?': '？',
}

var navigationLine = regexp.MustCompile(`^目录 *上一页( *下一页)?$`)

// Paragraph normalizes one Chinese body paragraph. It repairs doubled HTML
// escaping, high-confidence typos, a duplicated 的, half-width punctuation that
// sits next to Chinese text, and spaces that break Chinese sentences.
func Paragraph(s string) string {
	for range 2 {
		unescaped := html.UnescapeString(s)
		if unescaped == s {
			break
		}
		s = unescaped
	}
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.Trim(s, trimCutset)
	if s == "" {
		return ""
	}
	for _, fix := range wordFixes {
		s = strings.ReplaceAll(s, fix.from, fix.to)
	}
	s = replaceDoubleDe(s)
	s = toFullWidthPunctuation(s)
	s = compressSpaces(s)
	return strings.Trim(s, trimCutset)
}

// replaceDoubleDe collapses a run of 的 into one, except when the run is the
// first half of 的的确确 ("目的的的确" and "最合适的的确" keep every 的).
func replaceDoubleDe(s string) string {
	runes := []rune(s)
	var out strings.Builder
	out.Grow(len(s))
	for index := 0; index < len(runes); {
		if runes[index] != '的' || index+1 >= len(runes) || runes[index+1] != '的' {
			out.WriteRune(runes[index])
			index++
			continue
		}
		end := index
		for end < len(runes) && runes[end] == '的' {
			end++
		}
		if end < len(runes) && runes[end] == '确' {
			out.WriteString(string(runes[index:end]))
		} else {
			out.WriteRune('的')
		}
		index = end
	}
	return out.String()
}

// toFullWidthPunctuation converts a half-width , ; : ! ? run when a Chinese
// character sits on either side of it, so "以后, 傅斯恬" becomes full width but
// "1.0", "http://a" and "12:30" stay untouched. Dots and quotes are never
// rewritten: the corpus uses . as a censored-character separator.
func toFullWidthPunctuation(s string) string {
	runes := []rune(s)
	out := make([]rune, len(runes))
	copy(out, runes)
	for index := 0; index < len(runes); {
		if _, ok := fullWidthPunctuation[runes[index]]; !ok {
			index++
			continue
		}
		start := index
		for index < len(runes) {
			if _, ok := fullWidthPunctuation[runes[index]]; !ok {
				break
			}
			index++
		}
		left, hasLeft := previousNonSpace(runes, start-1)
		right, hasRight := nextNonSpace(runes, index)
		if (hasLeft && unicode.Is(unicode.Han, left)) || (hasRight && unicode.Is(unicode.Han, right)) {
			for position := start; position < index; position++ {
				out[position] = fullWidthPunctuation[runes[position]]
			}
		}
	}
	return string(out)
}

func previousNonSpace(runes []rune, from int) (rune, bool) {
	for index := from; index >= 0; index-- {
		if runes[index] != ' ' && runes[index] != '\t' {
			return runes[index], true
		}
	}
	return 0, false
}

func nextNonSpace(runes []rune, from int) (rune, bool) {
	for index := from; index < len(runes); index++ {
		if runes[index] != ' ' && runes[index] != '\t' {
			return runes[index], true
		}
	}
	return 0, false
}

// compressSpaces removes ASCII space runs that break Chinese sentences. Lines
// without Chinese sentence punctuation (lyrics like "我的心事 蒸发成云") keep
// their spacing.
func compressSpaces(s string) string {
	if !strings.ContainsAny(s, sentenceEnd) {
		return s
	}
	runes := []rune(s)
	var out strings.Builder
	out.Grow(len(s))
	for index := 0; index < len(runes); {
		if runes[index] != ' ' && runes[index] != '\t' {
			out.WriteRune(runes[index])
			index++
			continue
		}
		start := index
		for index < len(runes) && (runes[index] == ' ' || runes[index] == '\t') {
			index++
		}
		drop := false
		if start > 0 && index < len(runes) {
			left, right := runes[start-1], runes[index]
			drop = unicode.Is(unicode.Han, left) && unicode.Is(unicode.Han, right) ||
				strings.ContainsRune(sentenceEnd, left) && unicode.Is(unicode.Han, right)
		}
		if !drop {
			out.WriteString(string(runes[start:index]))
		}
	}
	return out.String()
}

// IsAdLine reports whether one line is 52shuku navigation or advertising text.
// Whitespace is normalized first because exported navigation lines separate
// 目录 and 上一页 with a no-break space, which \s and Fields split differently.
func IsAdLine(line string) bool {
	line = strings.NewReplacer("\u00a0", " ", "\u3000", " ").Replace(line)
	line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "52shuku.net"):
		return true
	case strings.Contains(line, "52书库") && (strings.Contains(line, "小贴士") || strings.Contains(lower, "www")):
		return true
	case navigationLine.MatchString(line):
		return true
	case strings.Contains(line, "哦豁") && strings.Contains(line, "52书库"):
		return true
	case strings.Contains(line, "传送门"):
		return true
	case strings.Contains(line, "关灯") && strings.Contains(line, "护眼") && strings.Contains(line, "字体"):
		return true
	case strings.Contains(line, "52书库App") || strings.Contains(line, "阅读记录") && strings.Contains(line, "加入收藏"):
		return true
	case strings.Contains(line, "Tips") && strings.Contains(line, "52"):
		return true
	}
	return false
}

// CleanText drops advertisement lines, normalizes every remaining line and
// removes blank lines.
func CleanText(text string) string {
	kept := make([]string, 0, 64)
	for _, line := range strings.Split(text, "\n") {
		line = strings.Trim(line, trimCutset)
		if line == "" || IsAdLine(line) {
			continue
		}
		if paragraph := Paragraph(line); paragraph != "" {
			kept = append(kept, paragraph)
		}
	}
	return strings.Join(kept, "\n")
}
