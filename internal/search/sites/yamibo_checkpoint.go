package sites

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func loadYamiboScanCheckpoint(path string) (yamiboScanCheckpoint, error) {
	if path == "" {
		return yamiboScanCheckpoint{}, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return yamiboScanCheckpoint{}, err
	}
	var checkpoint yamiboScanCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return yamiboScanCheckpoint{}, err
	}
	return checkpoint, nil
}

func saveYamiboScanCheckpoint(path string, checkpoint yamiboScanCheckpoint) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
