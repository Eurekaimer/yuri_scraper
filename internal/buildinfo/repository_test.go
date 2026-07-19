package buildinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryBuildEntrypointsUseReleaseVersion(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	versionData, err := os.ReadFile(filepath.Join(root, "release", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionData))
	if version != "0.6.1" {
		t.Fatalf("release/VERSION = %q, want 0.6.1", version)
	}

	checks := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{"build.bat", []string{"release\\VERSION", "cmd\\yuri_scraper", "yuri_scraper-v%VERSION%.exe"}, []string{"cmd\\52scraper", "set VERSION=0.6.0"}},
		{"build.sh", []string{"release/VERSION", "yuri-scraper-v${VERSION}"}, []string{"VERSION=\"${1:-0.6.0}"}},
		{"Makefile", []string{"release/VERSION", "yuri_scraper-v$(VERSION)"}, []string{"VERSION ?= 0.6.0"}},
		{filepath.Join(".github", "workflows", "release.yml"), []string{"yuri-scraper-v${{ steps.version.outputs.value }}"}, nil},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(root, check.path))
		if err != nil {
			t.Fatalf("read %s: %v", check.path, err)
		}
		text := string(data)
		for _, required := range check.required {
			if !strings.Contains(text, required) {
				t.Errorf("%s missing %q", check.path, required)
			}
		}
		for _, forbidden := range check.forbidden {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s still contains %q", check.path, forbidden)
			}
		}
	}
}
