package converter

import (
	"fmt"
	"io"
	"strings"
)

// WriteTXT writes chapters to a plain text file.
func WriteTXT(w io.Writer, chapters []Chapter) error {
	var buf strings.Builder
	for _, ch := range chapters {
		buf.WriteString(fmt.Sprintf("\n%s\n\n", ch.Title))
		buf.WriteString(ch.Content)
		buf.WriteString("\n\n")
		buf.WriteString(strings.Repeat("━", 40))
		buf.WriteString("\n")
	}
	_, err := io.WriteString(w, buf.String())
	return err
}
