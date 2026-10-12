package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/app"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
)

func TestHelpAndVersionBypassConfiguration(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "invalid.yaml")
	if err := os.WriteFile(config, []byte("[invalid YAML"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YURI_SCRAPER_CONFIG", config)
	for _, arguments := range [][]string{
		{"--help"}, {"--version"}, {"-version"}, {"version"},
		{"version", "--help"}, {"tui", "--help"}, {"search", "--help"},
		{"download", "--help"}, {"library", "remove", "--help"},
		{"config", "--help"}, {"install", "--help"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), arguments, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code %d: %s", code, &stderr)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected diagnostic: %s", &stderr)
			}
			if strings.Contains(stdout.String(), "\x1b") {
				t.Fatal("noninteractive metadata emitted terminal control sequences")
			}
			if _, err := os.Stat(filepath.Join(root, "novels")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("metadata initialized output directory: %v", err)
			}
		})
	}
}

func TestInvalidCommandsFailBeforeApplicationInitialization(t *testing.T) {
	root := t.TempDir()
	t.Setenv("YURI_SCRAPER_CONFIG", filepath.Join(root, "missing.yaml"))
	for _, arguments := range [][]string{
		{"--unknown"}, {"unknown"}, {"search"}, {"download"},
		{"download", "URL", "extra"}, {"download", "URL", "--password", "secret"},
		{"search", "query", "--source", "unknown"}, {"config", "change"},
		{"remove", "id"}, {"library", "forget", "id"}, {"library", "unknown"},
		{"search", "query", "--concurrency", "2"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), arguments, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d; want usage error", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("usage failure polluted stdout: %s", &stdout)
			}
			if _, err := os.Stat(filepath.Join(root, "novels")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("usage failure initialized output: %v", err)
			}
		})
	}
}

func TestConfigJSONUsesConfigurationDirectory(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(config, []byte("output_dir: books\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"config", "show", "--config", config, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d: %s", code, &stderr)
	}
	var cfg store.Config
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if cfg.OutputDir != filepath.Join(root, "books") {
		t.Fatalf("output directory = %s", cfg.OutputDir)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected extra stdout data: %v", err)
	}
	if _, err := os.Stat(cfg.OutputDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config show initialized output: %v", err)
	}
}

func TestConfigResolutionPrecedenceAndLocator(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	locator, err := store.ConfigLocatorPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(locator), 0700); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, "project", "config.yaml")
	if err := os.WriteFile(locator, []byte(installed+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(root, "environment.yaml")
	explicit := filepath.Join(root, "explicit.yaml")
	t.Setenv("YURI_SCRAPER_CONFIG", environment)
	for _, scenario := range []struct{ explicit, expected string }{{explicit, explicit}, {"", environment}} {
		config, resolvedRoot, err := store.ResolvePaths(scenario.explicit, filepath.Join(root, "embedded"))
		if err != nil {
			t.Fatal(err)
		}
		if config != scenario.expected || resolvedRoot != filepath.Dir(scenario.expected) {
			t.Fatalf("resolved %s at %s", config, resolvedRoot)
		}
	}
	t.Setenv("YURI_SCRAPER_CONFIG", "")
	config, resolvedRoot, err := store.ResolvePaths("", filepath.Join(root, "embedded"))
	if err != nil || config != installed || resolvedRoot != filepath.Dir(installed) {
		t.Fatalf("installed resolution = %s, %s, %v", config, resolvedRoot, err)
	}
	if err := os.WriteFile(locator, []byte("relative/config.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ResolvePaths("", filepath.Join(root, "embedded")); err == nil {
		t.Fatal("accepted corrupt relative locator")
	}
}

func TestTaskTerminalExitCodesAndCleanJSON(t *testing.T) {
	for state, expectedCode := range map[string]int{"completed": 0, "partial": 3, "failed": 1, "canceled": 130} {
		t.Run(state, func(t *testing.T) {
			updates := make(chan app.Snapshot, 2)
			updates <- app.Snapshot{State: "downloading", Total: 2, Completed: 1}
			updates <- app.Snapshot{State: state, Total: 2, Completed: 1, Failed: 1}
			close(updates)
			var stdout, stderr bytes.Buffer
			if code := consumeTask(updates, true, &stdout, &stderr); code != expectedCode {
				t.Fatalf("exit code = %d", code)
			}
			var result app.Snapshot
			decoder := json.NewDecoder(&stdout)
			if err := decoder.Decode(&result); err != nil {
				t.Fatalf("invalid terminal JSON: %v", err)
			}
			if result.State != state {
				t.Fatalf("nonterminal snapshot leaked: %s", result.State)
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				t.Fatalf("progress leaked to stdout: %v", err)
			}
		})
	}
}

func TestTaskClosedWithoutTerminalFails(t *testing.T) {
	updates := make(chan app.Snapshot, 1)
	updates <- app.Snapshot{State: "exporting"}
	close(updates)
	var stdout, stderr bytes.Buffer
	if code := consumeTask(updates, true, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatal("incomplete task reported a JSON result")
	}
}

func TestMissingSelectedConfigDoesNotCreateAnotherLibrary(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--config", filepath.Join(root, "moved", "config.yaml"), "library", "list"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 {
		t.Fatalf("missing config silently accepted: code=%d stdout=%s", code, &stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "moved")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created replacement library: %v", err)
	}
}
