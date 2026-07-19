package store

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds user configuration.
type Config struct {
	OutputDir   string  `yaml:"output_dir"`
	Format      string  `yaml:"format"`      // "txt" or "epub"
	Concurrency int     `yaml:"concurrency"` // parallel chapter downloads
	RateLimit   float64 `yaml:"rate_limit"`  // requests/sec per site
	RetryCount  int     `yaml:"retry_count"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		OutputDir:   "novels",
		Format:      "epub",
		Concurrency: 8,
		RateLimit:   2.0,
		RetryCount:  3,
	}
}

// LoadConfig reads config from path, merging with defaults.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	// Resolve relative paths
	if !filepath.IsAbs(cfg.OutputDir) {
		cfg.OutputDir = filepath.Clean(cfg.OutputDir)
	}

	return cfg, nil
}

// SaveConfig writes config to path.
func SaveConfig(path string, cfg Config) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
