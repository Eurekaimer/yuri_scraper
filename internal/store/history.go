package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	State      string    `json:"state,omitempty"`
	FailedFile string    `json:"failed_file,omitempty"`
}

// History manages download history.
type History struct {
	path    string
	mu      sync.Mutex
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
		return nil, fmt.Errorf("read history %s: %w (original file preserved)", h.path, err)
	}

	return h, nil
}

// Add records a new download.
func (h *History) Add(entry HistoryEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry.Downloaded = time.Now()
	entries := append([]HistoryEntry(nil), h.Entries...)
	for i := range entries {
		if entry.BookURL != "" && entries[i].BookURL == entry.BookURL && entries[i].SiteName == entry.SiteName && entries[i].Format == entry.Format {
			entries[i] = entry
			return h.commit(entries)
		}
	}
	return h.commit(append(entries, entry))
}

// List returns all entries, newest first.
func (h *History) List() []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]HistoryEntry, len(h.Entries))
	for i, e := range h.Entries {
		result[len(h.Entries)-1-i] = e
	}
	return result
}

// RemovePath forgets metadata only; it never removes a downloaded file.
func (h *History) RemovePath(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	resolve := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(h.path), p)
		}
		return filepath.Clean(p)
	}
	entries := make([]HistoryEntry, 0, len(h.Entries))
	for _, entry := range h.Entries {
		if resolve(entry.FilePath) != resolve(path) {
			entries = append(entries, entry)
		}
	}
	return h.commit(entries)
}

func (h *History) commit(entries []HistoryEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(h.path), ".history-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), h.path); err != nil {
		return err
	}
	h.Entries = entries
	return nil
}
