package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigLocatorPath is the per-user pointer to the installed project's config.
func ConfigLocatorPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration: %w", err)
	}
	return filepath.Join(dir, "yuri-scraper", "location"), nil
}

// ResolvePaths resolves configuration without creating files or directories.
// Precedence: explicit argument, YURI_SCRAPER_CONFIG, the locator written by
// `install`, an embedded build-time project root, a source checkout containing
// the running binary, a configuration file beside the running binary, then the
// project root or configuration file of the current directory.
func ResolvePaths(explicit, projectRoot string) (configPath, root string, err error) {
	selected := explicit
	if selected == "" {
		selected = os.Getenv("YURI_SCRAPER_CONFIG")
	}
	if selected != "" {
		configPath, err = filepath.Abs(selected)
		if err != nil {
			return "", "", fmt.Errorf("resolve configuration path: %w", err)
		}
		return configPath, filepath.Dir(configPath), nil
	}
	locator, err := ConfigLocatorPath()
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(locator)
	if err == nil {
		configPath = strings.TrimSpace(string(data))
		if !filepath.IsAbs(configPath) {
			return "", "", fmt.Errorf("configuration locator %s must contain an absolute config path", locator)
		}
		configPath = filepath.Clean(configPath)
		return configPath, filepath.Dir(configPath), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("read configuration locator: %w", err)
	}
	if projectRoot != "" {
		if !filepath.IsAbs(projectRoot) {
			return "", "", fmt.Errorf("embedded project root must be absolute")
		}
		root = filepath.Clean(projectRoot)
		return filepath.Join(root, "config.yaml"), root, nil
	}
	// Prefer the executable's project over the caller's current directory.
	if executable, executableErr := os.Executable(); executableErr == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
			executable = resolved
		}
		dir := filepath.Dir(executable)
		if root = findProjectRoot(dir); root != "" {
			return filepath.Join(root, "config.yaml"), root, nil
		}
		if dir = existingConfigDir(dir); dir != "" {
			return filepath.Join(dir, "config.yaml"), dir, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve working directory: %w", err)
	}
	if root = findProjectRoot(cwd); root != "" {
		return filepath.Join(root, "config.yaml"), root, nil
	}
	if dir := existingConfigDir(cwd); dir != "" {
		return filepath.Join(dir, "config.yaml"), dir, nil
	}
	return "", "", fmt.Errorf("cannot locate a yuri-scraper configuration; place config.yaml next to the program, or pass --config FILE, set YURI_SCRAPER_CONFIG, or run install --root DIR")
}

// existingConfigDir reports a directory that already holds a recognisable
// yuri-scraper configuration, so a released binary next to its config works
// without a source checkout. A foreign config.yaml in the current directory is
// not adopted silently.
func existingConfigDir(dir string) string {
	path := filepath.Join(dir, "config.yaml")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "output_dir:") {
		return ""
	}
	return dir
}

func findProjectRoot(start string) string {
	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(data), "module github.com/Eurekaimer/yuri_scraper") {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}
