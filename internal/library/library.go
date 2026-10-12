// Package library reconciles downloaded metadata with local reader files.
package library

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/store"
)

type Entry struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Author     string    `json:"author,omitempty"`
	SiteName   string    `json:"site_name,omitempty"`
	BookURL    string    `json:"book_url,omitempty"`
	Format     string    `json:"format"`
	FilePath   string    `json:"file_path"`
	Chapters   int       `json:"chapters,omitempty"`
	Downloaded time.Time `json:"downloaded"`
	Missing    bool      `json:"missing"`
	State      string    `json:"state"`
	FailedFile string    `json:"failed_file,omitempty"`
}

func supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".epub", ".pdf", ".txt":
		return true
	}
	return false
}

func recordedPath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	// Earlier versions stored paths such as novels/book.epub relative to cwd.
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, filepath.Base(root)+string(filepath.Separator)) {
		return filepath.Join(filepath.Dir(root), clean)
	}
	return filepath.Join(root, clean)
}

func contained(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func identity(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func Scan(outputDir string, history []store.HistoryEntry) ([]Entry, error) {
	root, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]Entry, len(history))
	for _, h := range history {
		path := recordedPath(root, h.FilePath)
		if h.FilePath == "" || !contained(root, path) || !supported(path) {
			continue
		}
		state := h.State
		if state == "" {
			state = "completed"
		}
		id := identity(h.SiteName + "\n" + h.BookURL + "\n" + h.Format)
		if h.BookURL == "" {
			rel, _ := filepath.Rel(root, path)
			id = identity(rel)
		}
		failedFile := ""
		if h.FailedFile != "" {
			failedFile = recordedPath(root, h.FailedFile)
		}
		byPath[path] = Entry{ID: id, Title: h.Title, Author: h.Author, SiteName: h.SiteName, BookURL: h.BookURL, Format: h.Format, FilePath: path, Chapters: h.Chapters, Downloaded: h.Downloaded, Missing: true, State: state, FailedFile: failedFile}
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !supported(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		entry, ok := byPath[path]
		if !ok {
			rel, _ := filepath.Rel(root, path)
			entry = Entry{ID: identity(rel), Title: strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), Format: strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")), FilePath: path, Downloaded: info.ModTime(), State: "imported"}
		}
		entry.Missing = false
		byPath[path] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(byPath))
	for _, entry := range byPath {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].Downloaded.Equal(entries[j].Downloaded) {
			return entries[i].Downloaded.After(entries[j].Downloaded)
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func Find(entries []Entry, id string) (Entry, error) {
	if id == "" {
		return Entry{}, fmt.Errorf("book ID is required")
	}
	var found *Entry
	for i := range entries {
		if entries[i].ID == id {
			return entries[i], nil
		}
		if strings.HasPrefix(entries[i].ID, id) {
			if found != nil {
				return Entry{}, fmt.Errorf("ambiguous book ID %q", id)
			}
			found = &entries[i]
		}
	}
	if found == nil {
		return Entry{}, fmt.Errorf("book ID %q not found", id)
	}
	return *found, nil
}

func regular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reader file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular reader file: %s", path)
	}
	return nil
}

func openPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("system opener failed: %w; configure a default application for this file type", err)
	}
	return nil
}

func Open(entry Entry) error {
	if err := regular(entry.FilePath); err != nil {
		return err
	}
	return openPath(entry.FilePath)
}

func Reveal(entry Entry) error {
	if err := regular(entry.FilePath); err != nil {
		return err
	}
	return openPath(filepath.Dir(entry.FilePath))
}

// Remove moves a file to a recoverable local trash; it never deletes permanently.
func Remove(outputDir string, entry Entry) error {
	root, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(entry.FilePath)
	if err != nil {
		return err
	}
	if !contained(root, path) {
		return fmt.Errorf("refusing to remove a file outside the library")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if !contained(realRoot, filepath.Join(parent, filepath.Base(path))) {
		return fmt.Errorf("refusing to follow an external directory link")
	}
	if err := regular(path); err != nil {
		return err
	}
	trash := filepath.Join(root, ".trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(trash, entry.ID+"-*")
	if err != nil {
		return err
	}
	dest := f.Name()
	if err = f.Close(); err != nil {
		os.Remove(dest)
		return err
	}
	if err := os.Rename(path, dest+filepath.Ext(path)); err != nil {
		os.Remove(dest)
		return err
	}
	os.Remove(dest)
	return nil
}
