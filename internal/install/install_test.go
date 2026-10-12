package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRejectsNonCheckoutRoot(t *testing.T) {
	root := t.TempDir()
	options := Options{Root: root, BinDir: filepath.Join(root, "bin"), ConfigPath: filepath.Join(root, "config.yaml")}
	if _, err := Install(context.Background(), options); err == nil || !strings.Contains(err.Error(), "source checkout") {
		t.Fatalf("install accepted a directory without the module file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin")); !os.IsNotExist(err) {
		t.Fatal("failed install created the command directory")
	}
}

func TestEnsureConfigBuildsDefaultAndPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")
	if err := ensureConfig(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "output_dir: novels") {
		t.Fatalf("generated configuration is incomplete: %s", data)
	}
	custom := []byte("output_dir: mine\n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureConfig(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(custom) {
		t.Fatalf("existing configuration was replaced: %s, %v", after, err)
	}
}

func TestCheckTargetRefusesNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := checkTarget(filepath.Join(dir, "absent")); err != nil {
		t.Fatalf("absent target must be installable: %v", err)
	}
	target := filepath.Join(dir, "directory")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkTarget(target); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("directory target was accepted: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := checkTarget(link); err == nil {
		t.Fatal("symlink target was accepted")
	}
}
