package sites

import (
	"regexp"
	"strings"
)

// rx is a shorthand for regexp.MustCompile used across site adapters.
func rx(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	return s
}
