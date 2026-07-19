package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds user configuration.
type Config struct {
	OutputDir   string       `yaml:"output_dir"`
	Format      string       `yaml:"format"`      // "txt" or "epub"
	Concurrency int          `yaml:"concurrency"` // parallel chapter downloads
	RateLimit   float64      `yaml:"rate_limit"`  // requests/sec per site
	RetryCount  int          `yaml:"retry_count"`
	Yamibo      YamiboConfig `yaml:"yamibo"`
}

type YamiboConfig struct {
	MaxConcurrency int     `yaml:"max_concurrency"`
	RateLimit      float64 `yaml:"rate_limit"`
	MaxScanPages   int     `yaml:"max_scan_pages"`
	MinPostChars   int     `yaml:"min_post_chars"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		OutputDir:   "novels",
		Format:      "epub",
		Concurrency: 8,
		RateLimit:   2.0,
		RetryCount:  3,
		Yamibo: YamiboConfig{
			MaxConcurrency: 2,
			RateLimit:      1.2,
			MaxScanPages:   10,
			MinPostChars:   80,
		},
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
	defaults := DefaultConfig()
	if cfg.Yamibo.MaxConcurrency == 0 {
		cfg.Yamibo.MaxConcurrency = defaults.Yamibo.MaxConcurrency
	}
	if cfg.Yamibo.RateLimit == 0 {
		cfg.Yamibo.RateLimit = defaults.Yamibo.RateLimit
	}
	if cfg.Yamibo.MaxScanPages == 0 {
		cfg.Yamibo.MaxScanPages = defaults.Yamibo.MaxScanPages
	}
	if cfg.Yamibo.MinPostChars == 0 {
		cfg.Yamibo.MinPostChars = defaults.Yamibo.MinPostChars
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	// Resolve relative paths
	if !filepath.IsAbs(cfg.OutputDir) {
		cfg.OutputDir = filepath.Clean(cfg.OutputDir)
	}

	return cfg, nil
}

func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.OutputDir) == "" {
		return fmt.Errorf("output_dir 不能为空")
	}
	if cfg.Format != "txt" && cfg.Format != "epub" {
		return fmt.Errorf("format 必须是 txt 或 epub")
	}
	if cfg.Concurrency <= 0 || cfg.RateLimit <= 0 || cfg.RetryCount < 0 {
		return fmt.Errorf("concurrency、rate_limit 必须大于 0，retry_count 不能为负数")
	}
	if cfg.Yamibo.MaxConcurrency < 1 || cfg.Yamibo.MaxConcurrency > 2 {
		return fmt.Errorf("yamibo.max_concurrency 必须在 1 到 2 之间")
	}
	if cfg.Yamibo.RateLimit <= 0 || cfg.Yamibo.RateLimit > 1.2 {
		return fmt.Errorf("yamibo.rate_limit 必须大于 0 且不超过 1.2")
	}
	if cfg.Yamibo.MaxScanPages <= 0 {
		return fmt.Errorf("yamibo.max_scan_pages 必须大于 0")
	}
	if cfg.Yamibo.MinPostChars < 20 {
		return fmt.Errorf("yamibo.min_post_chars 不能小于 20")
	}
	return nil
}

// SaveConfig writes config to path.
func SaveConfig(path string, cfg Config) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
