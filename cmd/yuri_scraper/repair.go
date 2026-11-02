package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/repair"
)

type repairResult struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	Backup string `json:"backup,omitempty"`
	repair.Stats
}

// runRepair rebuilds every given EPUB. One broken file never stops the rest.
func runRepair(args []string, options commandOptions, stdout, stderr io.Writer) int {
	results := make([]repairResult, 0, len(args))
	code := 0
	for _, input := range args {
		result, err := repairFile(input, options.inPlace)
		if err != nil {
			fmt.Fprintln(stderr, "Error:", err)
			code = 1
			continue
		}
		results = append(results, result)
		if !options.json {
			fmt.Fprintln(stdout, result.Output)
		}
	}
	if options.json {
		if encodeCode := encodeResult(stdout, stderr, results); encodeCode != 0 {
			return encodeCode
		}
	}
	return code
}

func repairFile(input string, inPlace bool) (repairResult, error) {
	info, err := os.Stat(input)
	if err != nil {
		return repairResult{}, fmt.Errorf("%s: %w", input, err)
	}
	target := input
	if !inPlace {
		target = strings.TrimSuffix(input, filepath.Ext(input)) + ".repaired.epub"
	}
	result := repairResult{Input: input, Output: target}
	if inPlace {
		backup := input + ".bak"
		if _, err := os.Stat(backup); err != nil {
			if !os.IsNotExist(err) {
				return repairResult{}, fmt.Errorf("%s: %w", backup, err)
			}
			if err := copyFile(input, backup, info.Mode().Perm()); err != nil {
				return repairResult{}, err
			}
		}
		result.Backup = backup
	}

	temp, err := os.CreateTemp(filepath.Dir(target), ".yuri-repair-*")
	if err != nil {
		return repairResult{}, fmt.Errorf("%s: %w", target, err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		if !committed {
			os.Remove(tempPath)
		}
	}()

	stats, err := repair.Build(input, temp)
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return repairResult{}, err
	}
	if err := os.Chmod(tempPath, info.Mode().Perm()); err != nil {
		return repairResult{}, fmt.Errorf("%s: %w", tempPath, err)
	}
	if err := replaceFile(tempPath, target); err != nil {
		return repairResult{}, fmt.Errorf("%s: %w", target, err)
	}
	committed = true
	result.Stats = stats
	return result, nil
}

// copyFile duplicates src to dst without ever overwriting a readable dst.
func copyFile(src, dst string, mode os.FileMode) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	defer source.Close()
	destination, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("%s: %w", dst, err)
	}
	_, copyErr := io.Copy(destination, source)
	syncErr := destination.Sync()
	closeErr := destination.Close()
	switch {
	case copyErr != nil:
		os.Remove(dst)
		return fmt.Errorf("%s: %w", dst, copyErr)
	case syncErr != nil:
		os.Remove(dst)
		return fmt.Errorf("%s: %w", dst, syncErr)
	case closeErr != nil:
		os.Remove(dst)
		return fmt.Errorf("%s: %w", dst, closeErr)
	}
	return nil
}

// replaceFile renames temp onto target; Windows refuses to rename over an
// existing file, so the stale target is removed and the rename retried.
func replaceFile(temp, target string) error {
	renameErr := os.Rename(temp, target)
	if renameErr == nil {
		return nil
	}
	if _, statErr := os.Stat(target); statErr != nil {
		return fmt.Errorf("rename repaired file into place: %w", renameErr)
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	if err := os.Rename(temp, target); err != nil {
		return fmt.Errorf("rename repaired file into place: %w", err)
	}
	return nil
}
