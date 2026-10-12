package store

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds user configuration.
type Config struct {
	OutputDir   string        `yaml:"output_dir" json:"output_dir"`
	Format      string        `yaml:"format" json:"format"`
	Concurrency int           `yaml:"concurrency" json:"concurrency"`
	RateLimit   float64       `yaml:"rate_limit" json:"rate_limit"`
	RetryCount  int           `yaml:"retry_count" json:"retry_count"`
	Yamibo      YamiboConfig  `yaml:"yamibo" json:"yamibo"`
	Startup     StartupConfig `yaml:"startup" json:"startup"`
}

type StartupConfig struct {
	Animation     string `yaml:"animation" json:"animation"` // sweep, static, or off
	ReducedMotion bool   `yaml:"reduced_motion" json:"reduced_motion"`
}

type YamiboConfig struct {
	MaxConcurrency int     `yaml:"max_concurrency" json:"max_concurrency"`
	RateLimit      float64 `yaml:"rate_limit" json:"rate_limit"`
	MaxScanPages   int     `yaml:"max_scan_pages" json:"max_scan_pages"`
	MinPostChars   int     `yaml:"min_post_chars" json:"min_post_chars"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		OutputDir:   "novels",
		Format:      "epub",
		Concurrency: 8,
		RateLimit:   2.0,
		RetryCount:  3,
		Startup:     StartupConfig{Animation: "sweep"},
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
		return fmt.Errorf("output_dir must not be empty")
	}
	if cfg.Format != "txt" && cfg.Format != "epub" {
		return fmt.Errorf("format must be txt or epub")
	}
	if cfg.Concurrency <= 0 || cfg.Concurrency > 64 || cfg.RateLimit <= 0 || cfg.RateLimit > 20 || math.IsNaN(cfg.RateLimit) || math.IsInf(cfg.RateLimit, 0) || cfg.RetryCount < 0 || cfg.RetryCount > 10 {
		return fmt.Errorf("concurrency must be 1..64, rate_limit must be >0 and <=20, retry_count must be 0..10")
	}
	if cfg.Yamibo.MaxConcurrency < 1 || cfg.Yamibo.MaxConcurrency > 2 {
		return fmt.Errorf("yamibo.max_concurrency must be 1..2")
	}
	if cfg.Yamibo.RateLimit <= 0 || cfg.Yamibo.RateLimit > 1.2 || math.IsNaN(cfg.Yamibo.RateLimit) || math.IsInf(cfg.Yamibo.RateLimit, 0) {
		return fmt.Errorf("yamibo.rate_limit must be >0 and <=1.2")
	}
	if cfg.Yamibo.MaxScanPages <= 0 {
		return fmt.Errorf("yamibo.max_scan_pages must be positive")
	}
	if cfg.Yamibo.MinPostChars < 20 {
		return fmt.Errorf("yamibo.min_post_chars must be at least 20")
	}
	switch cfg.Startup.Animation {
	case "", "sweep", "static", "off":
	default:
		return fmt.Errorf("startup.animation must be sweep, static, or off")
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
