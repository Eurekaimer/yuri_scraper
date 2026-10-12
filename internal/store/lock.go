package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrLibraryBusy = errors.New("library is being modified by another process")

// AcquireLibraryLock uses an OS lock released even when a process is killed.
// The lock file remains in place so another process cannot lock a new inode.
func AcquireLibraryLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, ".library.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: %v", ErrLibraryBusy, err)
	}
	return f, nil
}
