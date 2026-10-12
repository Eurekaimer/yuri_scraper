package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/store"
)

func TestScanReconcilesImportedAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, "百合 novel.pdf")
	if err := os.WriteFile(present, []byte("reader content"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing.epub")
	if err := os.Mkdir(filepath.Join(root, ".cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cache", "ignore.txt"), []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := Scan(root, []store.HistoryEntry{{Title: "Tracked", BookURL: "https://example.test/book", SiteName: "source", Format: "epub", FilePath: missing}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("library has %d entries, expected imported and missing", len(entries))
	}
	byPath := map[string]Entry{}
	for _, e := range entries {
		byPath[e.FilePath] = e
	}
	if byPath[present].State != "imported" || byPath[present].Missing || !byPath[missing].Missing {
		t.Fatal("presence and imported state not reconciled")
	}
	if byPath[present].ID == byPath[missing].ID {
		t.Fatal("different books share an identity")
	}
	again, err := Scan(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].ID != byPath[present].ID {
		t.Fatal("imported identity is not stable")
	}
}

func TestTrashIsRecoverableAndCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "novel.epub")
	if err := os.WriteFile(path, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, Entry{ID: "book", FilePath: path}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("original file remains")
	}
	files, err := os.ReadDir(filepath.Join(root, ".trash"))
	if err != nil || len(files) != 1 {
		t.Fatalf("trash: %v %#v", err, files)
	}
	data, err := os.ReadFile(filepath.Join(root, ".trash", files[0].Name()))
	if err != nil || string(data) != "preserve me" {
		t.Fatal("file was deleted instead of preserved")
	}
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	os.WriteFile(outside, []byte("safe"), 0o600)
	if err := Remove(root, Entry{ID: "outside", FilePath: outside}); err == nil {
		t.Fatal("external file was accepted")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "safe" {
		t.Fatal("outside data was modified")
	}
}

func TestFindRejectsAmbiguousPrefix(t *testing.T) {
	entries := []Entry{{ID: "abc1"}, {ID: "abc2"}}
	if _, err := Find(entries, "abc"); err == nil {
		t.Fatal("ambiguous prefix selected an arbitrary book")
	}
	entry, err := Find(entries, "abc2")
	if err != nil || entry.ID != "abc2" {
		t.Fatal("exact ID did not resolve")
	}
}
