package converter

import (
	"fmt"
	"io"
	"strings"
)

var textSeparator = "\n\n" + strings.Repeat("━", 40) + "\n"

// WriteTXTStream writes chapters as they are produced, without book aggregation.
func WriteTXTStream(w io.Writer, source ChapterSource) error {
	return source(func(ch Chapter) error {
		if _, err := fmt.Fprintf(w, "\n%s\n\n", ch.Title); err != nil {
			return err
		}
		if _, err := io.WriteString(w, ch.Content); err != nil {
			return err
		}
		_, err := io.WriteString(w, textSeparator)
		return err
	})
}
