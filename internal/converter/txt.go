package converter

import (
	"fmt"
	"io"
	"strings"
)

// WriteTXT writes chapters to a plain text file.
func WriteTXT(w io.Writer, chapters []Chapter) error {
	for _, ch := range chapters {
		if _, err := fmt.Fprintf(w, "\n%s\n\n", ch.Title); err != nil {
			return err
		}
		if _, err := io.WriteString(w, ch.Content); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n\n"+strings.Repeat("━", 40)+"\n"); err != nil {
			return err
		}
	}
	return nil
}
