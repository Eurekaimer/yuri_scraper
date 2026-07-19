package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/search/sites"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	"github.com/Eurekaimer/yuri_scraper/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	cfg, err := store.LoadConfig("config.yaml")
	if err != nil {
		log.Printf("警告: 读取配置失败: %v (使用默认)", err)
		cfg = store.DefaultConfig()
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		log.Fatalf("创建输出目录失败 %s: %v", cfg.OutputDir, err)
	}

	hist, err := store.LoadHistory(cfg.OutputDir)
	if err != nil {
		log.Printf("警告: 读取历史失败: %v", err)
	}

	// 注册站点适配器
	registry := search.NewRegistry()
	registry.Register(sites.New52Shuku())
	registry.Register(sites.NewYamiboWithPolicy(int64(cfg.Yamibo.MaxConcurrency), cfg.Yamibo.RateLimit))
	engine := search.NewEngine(registry)

	scraperEngine := scraper.NewEngine(scraper.Config{
		Concurrency:  int64(cfg.Concurrency),
		RateLimit:    cfg.RateLimit,
		OutputDir:    cfg.OutputDir,
		Format:       cfg.Format,
		RetryCount:   cfg.RetryCount,
		RetryDelay:   2 * time.Second,
		SpeedProfile: scraper.SpeedNormal,
	})
	m := tui.NewModel(engine, scraperEngine, hist, Version)
	m.SetChapterPlanDefaults(search.ChapterPlanOptions{
		MaxScanPages: cfg.Yamibo.MaxScanPages,
		MinPostChars: cfg.Yamibo.MinPostChars,
	})
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("错误: %v\n", err)
		os.Exit(1)
	}
}
