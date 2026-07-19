package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// HistoryEntry records a completed download.
type HistoryEntry struct {
	Title      string    `json:"title"`
	Author     string    `json:"author"`
	SiteName   string    `json:"site_name"`
	BookURL    string    `json:"book_url"`
	Format     string    `json:"format"`
	FilePath   string    `json:"file_path"`
	Chapters   int       `json:"chapters"`
	Downloaded time.Time `json:"downloaded"`
}

// History manages download history.
type History struct {
	path    string
	Entries []HistoryEntry `json:"entries"`
}

// LoadHistory reads the history file.
func LoadHistory(outputDir string) (*History, error) {
	h := &History{
		path:    filepath.Join(outputDir, ".history.json"),
		Entries: make([]HistoryEntry, 0),
	}

	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return h, nil
		}
		return h, err
	}

	if err := json.Unmarshal(data, &h.Entries); err != nil {
		return h, nil // corrupt history → start fresh
	}

	return h, nil
}

// Add records a new download.
func (h *History) Add(entry HistoryEntry) error {
	entry.Downloaded = time.Now()
	h.Entries = append(h.Entries, entry)
	return h.save()
}

// Exists checks if a book URL has been downloaded.
func (h *History) Exists(bookURL string) bool {
	for _, e := range h.Entries {
		if e.BookURL == bookURL {
			return true
		}
	}
	return false
}

// List returns all entries, newest first.
func (h *History) List() []HistoryEntry {
	result := make([]HistoryEntry, len(h.Entries))
	for i, e := range h.Entries {
		result[len(h.Entries)-1-i] = e
	}
	return result
}

func (h *History) save() error {
	data, err := json.MarshalIndent(h.Entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, data, 0644)
}
