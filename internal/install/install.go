package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/store"
	"gopkg.in/yaml.v3"
)

type Options struct {
	Root       string
	BinDir     string
	ConfigPath string
	Version    string
}

type Result struct {
	Binary string `json:"binary"`
	Config string `json:"config"`
	Root   string `json:"root"`
	BinDir string `json:"bin_dir"`
}

// Install builds the project into a temporary executable and atomically replaces
// the user command. It never invokes a shell, escalates privileges or edits PATH.
func Install(ctx context.Context, options Options) (Result, error) {
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve install root: %w", err)
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(string(module), "module github.com/Eurekaimer/yuri_scraper") {
		return Result{}, fmt.Errorf("install root %s is not a yuri-scraper source checkout", root)
	}
	config := options.ConfigPath
	if config == "" {
		config = filepath.Join(root, "config.yaml")
	}
	config, err = filepath.Abs(config)
	if err != nil {
		return Result{}, fmt.Errorf("resolve config path: %w", err)
	}
	binDir := options.BinDir
	if binDir == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return Result{}, homeErr
		}
		binDir = filepath.Join(home, ".local", "bin")
	}
	binDir, err = filepath.Abs(binDir)
	if err != nil {
		return Result{}, err
	}
	locator, err := store.ConfigLocatorPath()
	if err != nil {
		return Result{}, err
	}
	binary := filepath.Join(binDir, "yuri-scraper")
	for _, path := range []string{binary, config, locator} {
		if err := checkTarget(path); err != nil {
			return Result{}, err
		}
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(locator), 0700); err != nil {
		return Result{}, err
	}
	if err := ensureConfig(config); err != nil {
		return Result{}, err
	}
	if _, err := store.LoadConfig(config); err != nil {
		return Result{}, fmt.Errorf("load install configuration: %w", err)
	}

	temporary, err := os.CreateTemp(binDir, ".yuri-scraper-build-*")
	if err != nil {
		return Result{}, err
	}
	tempPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(tempPath)
		return Result{}, err
	}
	defer os.Remove(tempPath)
	flags := "-X " + strconv.Quote("main.ProjectRoot="+root) + " -X " + strconv.Quote("main.Version="+options.Version)
	command := exec.CommandContext(ctx, "go", "build", "-ldflags", flags, "-o", tempPath, "./cmd/yuri_scraper")
	command.Dir = root
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("build user command: %w\n%s", err, strings.TrimSpace(output.String()))
	}
	if err := os.Chmod(tempPath, 0755); err != nil {
		return Result{}, err
	}
	file, err := os.OpenFile(tempPath, os.O_RDWR, 0)
	if err != nil {
		return Result{}, err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return Result{}, err
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	// Re-check targets after the build, before changing the installation.
	if err := checkTarget(binary); err != nil {
		return Result{}, err
	}
	if err := checkTarget(locator); err != nil {
		return Result{}, err
	}
	pointer, err := writeTemporary(filepath.Dir(locator), []byte(config+"\n"), 0600)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(pointer)
	if err := os.Rename(tempPath, binary); err != nil {
		return Result{}, fmt.Errorf("install user command: %w", err)
	}
	if err := os.Rename(pointer, locator); err != nil {
		return Result{}, fmt.Errorf("install config locator: %w", err)
	}
	return Result{Binary: binary, Config: config, Root: root, BinDir: binDir}, nil
}

func checkTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular file %s", path)
	}
	return nil
}

func ensureConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(store.DefaultConfig())
	if err != nil {
		return err
	}
	temporary, err := writeTemporary(filepath.Dir(path), data, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	// Linking installs the complete file without overwriting a concurrently
	// created user configuration.
	if err := os.Link(temporary, path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func writeTemporary(dir string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, ".yuri-scraper-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
