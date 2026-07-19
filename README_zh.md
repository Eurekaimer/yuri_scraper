[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.5.0-7C3AED?style=flat)](https://github.com/hzg/yuri_scraper)
[![License](https://img.shields.io/badge/MIT-green?style=flat)](LICENSE)
[![Platform](https://img.shields.io/badge/Windows%7CLinux%7CmacOS-blue?style=flat)](https://github.com/hzg/yuri_scraper/releases)

> 终端里的电子书管家 —— 搜索、抓取、精校，一气呵成。

**YuriScraper** 是一个基于终端的 TUI 小说抓取工具，专为 [52书库](https://www.52shuku.net/) 设计。支持模糊搜索、多级并发下载、EPUB/TXT 输出，内置繁简转换与自动标签系统。

[English](README.md)

---

## 功能

- **TUI 界面** — 基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea)，全键盘操作，单屏布局不跳转
- **模糊搜索** — 字符重叠 + N-gram + Levenshtein 编辑距离，中文匹配精准
- **三级速度控制** — 慢速/正常/快速，抓取中按 `1/2/3` 实时切换
- **并发下载** — Worker Pool + 令牌桶限流，兼顾速度与礼貌
- **元数据抓取** — 自动提取作者、简介、主角、标签、完结状态
- **EPUB 3 输出** — 完整元数据（`dc:subject`、`dc:description`、主角信息）
- **繁简自动转换** — 检测并转为简体中文，统一阅读体验
- **智能标签** — 从书名和简介自动识别 20+ 分类（总裁、修仙、GL、百合…）
- **失败重试** — 失败章节记录到 `.failed.json`，按 `R` 一键重试
- **下载历史** — 本地 `.history.json`，避免重复下载

---

## 快速开始

### 前置要求

- [Go](https://go.dev/dl/) 1.26+

### 安装 & 运行

```bash
git clone https://github.com/hzg/yuri_scraper.git
cd yuri_scraper
go mod tidy
go build -o yuri_scraper ./cmd/yuri_scraper/
./yuri_scraper
```

或者直接 `go run ./cmd/yuri_scraper/`。

### 操作说明

| 场景 | 按键 |
|------|------|
| 搜索输入 | 直接输入 → `Enter` 搜索 → `Tab` 补全 |
| 结果列表 | `j/k` 移动 `g/G` 首尾 `Ctrl+D/U` 翻页 `Enter` 选择 |
| 抓取中 | `1/2/3` 切换速度 `R` 重试失败章节 |
| 书库 | `Ctrl+L` 打开 `Esc` 返回 |
| 全局 | `Ctrl+C` 退出 |

### 配置

编辑 `config.yaml`：

```yaml
output_dir: novels      # 下载目录
format: epub            # 输出格式：txt / epub
concurrency: 8          # 并发数（0 = 用速度档位默认值）
rate_limit: 2.0         # 每秒请求数（0 = 用速度档位默认值）
retry_count: 3          # 失败重试次数
```

速度档位：

| 档位 | 并发 | 限速 | 热键 |
|------|------|------|------|
| 慢速 | 2 | 0.5 req/s | `1` |
| 正常 | 8 | 2 req/s | `2` |
| 快速 | 16 | 5 req/s | `3` |

---

## 开发

```bash
go test -short ./...     # 单元测试
go test ./...            # 全部测试（含网络）
go vet ./...             # 代码检查
```

### 项目结构

```
cmd/yuri_scraper/        # 程序入口
internal/
  tui/                # Bubble Tea 界面（单屏布局）
  search/             # 模糊搜索 + 站点适配器
    sites/            # 52书库适配器
  scraper/            # 并发抓取引擎 + 速度控制
  converter/          # EPUB/TXT + 繁简转换 + 标签
  store/              # 配置 + 下载历史
```

---

## Release

预编译二进制在 [Releases](https://github.com/hzg/yuri_scraper/releases)。

```bash
make release    # 全平台交叉编译
# 或 ./build.sh
```

产物：

- `yuri_scraper-windows-amd64.exe`
- `yuri_scraper-linux-amd64`（NixOS 可用）
- `yuri_scraper-linux-arm64`
- `yuri_scraper-darwin-amd64`
- `yuri_scraper-darwin-arm64`

---

## License

MIT
