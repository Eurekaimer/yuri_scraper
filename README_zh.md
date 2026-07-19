[![Go](https://img.shields.io/badge/Go-1.26.5+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.6.1-7C3AED?style=flat)](https://github.com/Eurekaimer/yuri_scraper/releases)
[![CI](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml/badge.svg)](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat)](LICENSE)

# YuriScraper

> 一个本地优先、支持多来源的终端电子书下载器。

YuriScraper 是使用 Go 与 Bubble Tea 编写的键盘流 TUI。v0.6.1 继续稳定
52书库和百合会，并加入断点缓存、失败后部分输出、指数退避，以及可以在
下载过程中真正改变并发数的有界 worker pool。

[English](README.md) · [架构说明](ARCHITECTURE.md)

## 内容来源

| 来源 | 访问方式 | v0.6 支持情况 |
|---|---|---|
| 52书库 | 公开访问 | 搜索、元数据、章节下载 |
| 百合会 | Cookie | 论坛搜索、目录/电梯识别、楼层正文下载 |

百合会 Cookie 直接在 TUI 内粘贴，只保留在本次运行的内存中，不会写入
YAML、下载历史、解析 checkpoint 或日志。下载前可明确选择自动识别、
目录/电梯或跨页作者楼层模式。

## v0.6.1 重点改动

- 目录候选综合 PID、章节标题、重复链接、论坛控件、折叠节点和顺序评分；
- TUI 展示候选来源、章节数、评分及前三个标题，并允许手动切换；
- 作者楼层模式自动识别首个长正文作者，也可填写作者名或 UID；
- 按页限速扫描、PID 去重、保持原顺序，并支持扫描 checkpoint 续传；
- 已在解析阶段取得的楼层正文直接写入章节缓存，不再重复请求；
- 新增离线 HTML fixture 测试，默认测试不会访问百合会；
- 修复低并发下未启用 worker 在任务结束后无法退出的问题。

## v0.6.0 重点改动

- 启动时使用 `1/2` 选择 52书库或百合会；
- 修复下载一本书后无法回到原搜索结果列表的问题；
- `Tab/Shift+Tab` 在输入栏和结果栏之间切换，`/` 随时聚焦搜索栏；
- 速度档位同时控制 worker 数与请求速率；
- 每次重试同样经过限速，并使用指数退避和随机抖动；
- 成功章节写入 `novels/.cache/`，避免整本正文长期堆在内存中；
- 某些章节失败时仍然生成可阅读文件，并保存 `书名.failed.json`；
- 下载完成后按 `R` 只重新请求失败章节，成功章节直接命中缓存；
- 下载历史去重并采用原子写入；
- 二进制改由 GitHub Releases 构建，不再提交到 Git 仓库。

## 安装

推荐从 [GitHub Releases](https://github.com/Eurekaimer/yuri_scraper/releases)
下载单文件程序。Windows 使用 `yuri-scraper-windows-amd64.exe`，不需要
安装 Python、Java 或额外运行时。

从源码构建：

```bash
git clone https://github.com/Eurekaimer/yuri_scraper.git
cd yuri_scraper
go build -trimpath -ldflags="-s -w -X main.Version=0.6.1" -o yuri-scraper ./cmd/yuri_scraper/
./yuri-scraper
```

## 键位

| 界面 | 键位 |
|---|---|
| 来源选择 | `1/2`、`j/k`、`Enter` |
| 百合会认证 | 粘贴 Cookie、`Enter` 确认、`Esc` 返回 |
| 搜索栏 | 输入关键词、`Enter` 搜索、`Tab` 或 `↓` 进入结果 |
| 结果栏 | `j/k`、`g/G`、`Ctrl+D/U`、`Enter` 下载 |
| 百合会规划 | `1/2/3` 模式、`Tab` 参数、`h/l` 目录候选、`Enter` 继续 |
| 搜索栏 ⇄ 作品栏 | `Tab/Shift+Tab`、方向键、`Alt+h/l`；`/` 聚焦搜索栏 |
| 下载中 | `1/2/3` 切换速度，`R` 重试失败章节 |
| 全局 | `Ctrl+S` 来源、`Ctrl+L` 书库、`Ctrl+C` 退出 |

下载结束后按 `Esc` 或 `Enter` 会返回原来的搜索结果，不会清空作者搜索
得到的多本小说，也不会把焦点卡在输入栏。查询词、完整作品列表和原光标
位置都会保留；宽终端使用左右双栏，窄终端自动改为上下排列。

## 配置

```yaml
output_dir: novels
format: epub
concurrency: 8
rate_limit: 2.0
retry_count: 3

yamibo:
  max_concurrency: 2
  rate_limit: 1.2
  max_scan_pages: 10
  min_post_chars: 80
```

配置决定启动时的默认值；下载过程中按 `1/2/3` 会同时调整并发和速率。
百合会会额外限制为最多 2 个 worker、1.2 请求/秒，避免把 52书库的快速档
直接套到论坛上。
v0.6.0 配置仍可直接使用；缺少的 `yamibo` 字段会合并默认值。Cookie 和
密码不是配置字段。

## 已知限制

- 论坛 HTML 可能变化；fixtures 覆盖代表性的目录、分页、登录失效和楼层
  布局，但不能保证所有历史主题模板。
- 真实网络测试必须显式设置 `YURISCRAPER_LIVE_TEST=1`，Cookie 只能通过
  环境变量提供。本次候选版本未使用真实百合会 Cookie 验证。

## 开发与发布

```bash
go test -short ./...
go vet ./...
make build
```

推送 `v*` 标签后，GitHub Actions 会构建 Windows、Linux 和 macOS 的
amd64/arm64 产物，并自动附加到 Release。

## 使用边界

项目用于个人阅读整理。请使用保守的请求速率，遵守网站规则和版权要求，
不要绕过访问限制，也不要传播下载内容。

## License

MIT
