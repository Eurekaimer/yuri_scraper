# YuriScraper v0.6.0 — PC multi-source preview

v0.6.0 is the first PC release that begins merging the original 52shuku and
Yamibo download workflows into one keyboard-driven Go TUI.

## 主要变化

### 多来源 PC TUI

- 启动后可使用 `1/2` 选择 **52书库** 或 **百合会**；
- 百合会 Cookie 直接在 TUI 中输入，只保存在本次运行的内存里；
- 初步支持百合会论坛搜索、目录/电梯识别和楼层正文提取；
- 百合会固定使用更保守的上限：最多 2 个 worker、1.2 请求/秒。

### 搜索结果与键盘流修复

- 修复下载一本小说后按 `Esc` 无法返回原搜索结果的问题；
- 作者搜索得到的多本小说会继续保留，光标不会被强制卡回输入框；
- `Tab/Shift+Tab` 在搜索栏与结果栏之间切换；
- `/` 随时聚焦搜索栏，`↓` 可直接从输入栏进入结果；
- `Ctrl+S` 打开来源选择，`Ctrl+L` 打开本地书库。

### 下载可靠性与速度控制

- 将“每章一个 goroutine”改为有界 worker pool；
- `1/2/3` 现在会同时改变并发数和请求速率；
- 每一次重试同样经过令牌桶限速；
- 重试使用指数退避和抖动，不再发生零间隔连续请求；
- 成功章节保存为 checkpoint，重新运行时不会重复下载；
- 某些章节失败时仍会生成部分 TXT/EPUB，并写入 `书名.failed.json`；
- 下载完成后按 `R` 只重新请求失败章节。

### 工程与发布

- 下载历史按书籍 URL 去重并采用原子写入；
- TXT/EPUB 先写入临时文件，再原子替换最终文件；
- 修正 Go module 和 README 中的仓库地址；
- 加入单元测试、竞态检查、`go vet` 和跨平台 Release workflow；
- 删除仓库中提交的二进制文件，改由本 Release 提供下载。

## 下载文件

| 文件 | 平台 |
|---|---|
| `yuri-scraper-windows-amd64.exe` | Windows x86-64 |
| `yuri-scraper-linux-amd64` | Linux / NixOS x86-64 |
| `yuri-scraper-linux-arm64` | Linux ARM64 |
| `yuri-scraper-darwin-amd64` | Intel macOS |
| `yuri-scraper-darwin-arm64` | Apple Silicon macOS |

下载对应平台的单文件程序即可运行。`config.yaml` 为可选配置；缺失时会
使用默认下载目录和 EPUB 输出设置。

## 已知限制

- v0.6.0 的百合会支持优先面向带目录/电梯的帖子；
- 无目录帖子已经有首屏作者正文回退，但完整跨页作者楼层扫描仍在开发；
- 百合会页面结构变化或 Cookie 失效时，需要重新输入 Cookie；
- 请将工具用于个人阅读整理，并保持保守请求速率。

## Validation

- `go test -short ./...`
- `go test -race -short ./internal/scraper ./internal/tui`
- `go vet ./...`
- Windows/Linux `CGO_ENABLED=0` cross-build
- interactive TUI source/authentication smoke test
