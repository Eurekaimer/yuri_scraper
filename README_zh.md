[![Go](https://img.shields.io/badge/Go-1.26.5+-00ADD8?logo=go&style=flat)](https://go.dev/)
[![Version](https://img.shields.io/badge/v0.7.0-7C3AED?style=flat)](https://github.com/Eurekaimer/yuri_scraper/releases)
[![CI](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml/badge.svg)](https://github.com/Eurekaimer/yuri_scraper/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat)](LICENSE)

# YuriScraper

> 本地优先、多来源的终端电子书下载器。

YuriScraper 是 Go 编写的键盘流程序：Bubble Tea TUI 负责浏览，命令行负责
脚本化，两者共用同一个应用服务。v0.7.0 把下载生命周期从界面移到
`internal/app`，新增 `search`/`download`/`library`/`config`/`install`
子命令与 JSON 输出，导出改为逐章流式写入，并让配置里的并发与速率真正生效。

[English](README.md) · [架构说明](ARCHITECTURE.md)

## 内容来源

| 来源 | 访问方式 | 支持情况 |
|---|---|---|
| 52书库 | 公开访问 | 搜索、元数据、章节下载 |
| 百合会 | 账号密码 | 论坛搜索、目录/电梯识别、楼层正文下载 |

百合会账号和密码在 TUI 内或终端提示符中输入。程序读取 Discuz 登录页
`formhash`，建立只存在于内存中的会话，并立即清空输入框；账号、密码和会话
Cookie 都不会写入 YAML、下载历史、解析 checkpoint、日志或 JSON 输出。

## 安装

从 [GitHub Releases](https://github.com/Eurekaimer/yuri_scraper/releases)
下载单文件程序，或在源码目录安装用户级命令（不需要 `sudo`）：

```bash
git clone https://github.com/Eurekaimer/yuri_scraper.git
cd yuri_scraper
go run ./cmd/yuri_scraper/ install            # 构建 ~/.local/bin/yuri-scraper
```

`install` 会编译当前检出的源码、原子替换用户命令、把配置文件位置记录到平台
配置目录，并且不会修改任何 shell 启动文件或 `PATH`；请自行把提示的目录加入
`PATH`。它拒绝覆盖非普通文件，也不会覆盖已有配置。
源码安装默认使用当前目录（已安装命令可使用其嵌入的源码目录），不会根据旧配置
记录去编译另一个检出目录；有多个检出时请明确指定 `install --root DIR`。
相对 `--config` 按调用命令时的工作目录解析。配置记录指向不存在的文件时，
启动会报出路径并停止，不会悄悄新建另一套书库；用 `--config FILE` 或重新安装恢复。
源码开发构建显示 `dev`，发行版版本由构建参数注入。

直接构建：

```bash
go build -trimpath -ldflags="-s -w -X main.Version=0.7.0" -o yuri-scraper ./cmd/yuri_scraper/
```

## 命令行

不带子命令直接启动 TUI；`yuri-scraper tui` 等价。结果写入 stdout，诊断和进度
写入 stderr，因此 `--json` 可以直接管道使用。

```bash
yuri-scraper search --source 52shuku --json "书名"     # JSON 结果数组
yuri-scraper download --source 52shuku "https://www.52shuku.net/gl/1/abc.html"
yuri-scraper library list --json
yuri-scraper open 640b3c00578a003b                    # 系统默认阅读器
yuri-scraper remove 640b3c00578a003b --yes             # 移入 .trash
yuri-scraper config show --json
yuri-scraper install --bin-dir ~/.local/bin --json
```

来源名接受 `52shuku`/`52书库` 和 `yamibo`/`百合会`。`download` 可用
`--title`、`--format`、`--concurrency`、`--rate-limit`、`--retry-count`
覆盖本次运行的取值。`--base-url` 只允许 52书库使用回环 HTTP fixture，其他
来源会被拒绝。

退出码：`0` 成功，`1` 失败，`2` 参数错误，`3` 部分下载，`130` 用户中止。
`--help` 与 `--version` 不读取配置、不创建文件，无 TTY 也能正常输出。

## 终端界面

界面参考 [go-musicfox](https://github.com/go-musicfox/go-musicfox) 的键盘列表与
轻量分区：顶部品牌和导航、淡黄色圆角细框、固定状态栏和快捷键栏。
条目以 `○` / `●` 区分普通与选中状态，选中行整行高亮；中文按终端显示列宽对齐，
不依赖 Nerd Font。字体由终端设置，程序只控制颜色、粗体与间距。

终端宽度达到 100 列时可显示列表／详情双栏，`Tab` 开关预览；更窄时自动单栏，
`l` 查看详情。最小窗口为 40 × 10，低于此尺寸提示放大；调整窗口会重新计算
边框、输入宽度和可见列表行数。帮助、任务、详情与章节规划可用 `PgUp/PgDn` 翻页。

开屏依次显示白色实心花瓣、淡黄色轮廓的百合花（约 0.65 秒），再显示同色的
YURI 大字字符画（约 0.95 秒），最后进入主界面；窄窗使用简化花形。
主界面品牌前保留百合纹章 `⚜` 图标；不要求额外图标字体。
动画仍支持 `startup.animation: sweep/static/off`、`reduced_motion`、
`NO_COLOR` 和 `TERM=dumb`；任意键跳过，不会误触发下载。

维护时请同步更新本文、`ARCHITECTURE.md` 与当前版本说明，记录界面／键位变更、
状态不变量和实际验证结果，供后续 agent 接手；不要把未运行的检查写成通过。

## 键位

TUI 明确区分列表导航、普通文本输入和弹窗提示三种上下文；文本输入里输入
`j/k/h/l/q/:` 不会触发列表或全局动作。

| 上下文 | 键位 |
|---|---|
| 全局（列表） | `j/k` 或方向键、`g g`/`G`、`Ctrl+U`/`Ctrl+D`、`?` 帮助、`!` 诊断、`q`/`Ctrl+C` 退出 |
| 搜索结果 | `Enter` 下载、`p` 论坛规划、`l`/`Esc` 详情、`Tab` 预览、`/` 或 `i` 搜索、`f` 本地筛选、`r` 重试、`x` 取消搜索 |
| 书库 | `Enter`/`o` 打开、`v` 定位文件、`d` 移入回收站、`r` 重新下载、`/` 筛选、`Tab` 预览 |
| 搜索输入 | 直接输入、`Enter` 提交、`Esc` 保留草稿并返回 |
| 论坛规划 | `1/2/3` 模式、`Tab` 切换字段、`h/l` 目录候选、`Enter` 开始 |
| 任务 | `a`/`d` 减速/加速、`1/2/3` 直接选档、`x` 取消、`Esc` 浏览、`b` 书库 |
| 视图 | `s` 来源、`b` 书库、`t` 任务 |

下载期间可以继续浏览，状态栏在所有界面显示进度；从任务页返回会恢复原来的
结果列表、查询词和光标位置。

## 错误恢复与日志

- 搜索失败保留上次列表与光标；`r` 重试当前查询，`/` 修改关键词，`x` 取消搜索。
  新搜索返回不会抢走正在编辑的新草稿。
- 下载规划、正文抓取或导出失败均可在任务页按 `r` 重试；`x` 可取消尚未真正启动
  的任务。已保存的章节缓存不会被自动清空。下载期间尝试另一项不会篡改当前任务书名。
- 按 `!` 打开可滚动诊断面板，查看原始错误、恢复建议、请求／任务标识和日志路径；
  `r` 刷新日志，`Esc` 返回。HTTP 429 提示降速等待，403 提示浏览器检查访问状态，
  404 提示重新搜索，5xx／网络超时提示稍后重试；不自动绕过站点验证。
- 默认写入 `<output_dir>/.diagnostics.log`，采用 JSON Lines，记录会话、搜索结果数、
  52 书库请求失败和任务开始／结束。`yuri-scraper --debug` 额外记录成功请求的
  端点、HTTP 状态和耗时；不写搜索词、凭据、Cookie、请求／响应正文。
- `yuri-scraper logs` 输出最近最多 100 条记录（读取上限 64 KiB），路径输出到 stderr。
  日志权限为 `0600`，单文件上限 2 MiB，保留一个 `.1` 备份。无法创建日志时明确
  警告并继续运行；CLI 退回 stderr，TUI 不把日志直接刷进界面，诊断面板显示读取失败。

本轮真实书源验证：52 书库按“怦然为你”与“闵然”搜索均返回
[《怦然为你》／闵然](https://www.52shuku.net/gl/h1R2.html)。百合会未做联网验证。

## 配置

```yaml
output_dir: novels
format: epub
concurrency: 8
rate_limit: 2.0
retry_count: 3

startup:
  animation: sweep      # sweep、static 或 off
  reduced_motion: false

yamibo:
  max_concurrency: 2
  rate_limit: 1.2
  max_scan_pages: 10
  min_post_chars: 80
```

相对 `output_dir` 会相对于配置文件所在目录解析，因此无论从哪个工作目录启动，
书库位置都保持一致。配置查找顺序为：`--config`、`YURI_SCRAPER_CONFIG`、
`install` 记录的路径、包含当前可执行文件的检出目录、可执行文件同目录下的
`config.yaml`、当前目录向上找到的项目根目录或该目录的 `config.yaml`；不像
YuriScraper 配置的文件不会被静默采用。

`concurrency` 与 `rate_limit` 就是实际生效的下载预算，只会被来源声明的上限
压低。52书库在 64 并发 / 20 请求每秒的合理性上限内使用配置值，配置得更保守
时不会被抬高；百合会额外限制为最多 2 个 worker、1.2 请求每秒。下载过程中按
`1/2/3` 会用 Slow（2 worker，0.5 req/s）、Normal（8，2）、Fast（16，5）档
替换启动预算，但仍受来源上限约束。每次尝试（含重试）都经过同一个令牌桶。

`startup.animation` 接受 `sweep`、`static`、`off`；`reduced_motion`、设置
`NO_COLOR` 或 `TERM=dumb` 同样会关闭动画。

## 存储

| 路径 | 内容 |
|---|---|
| `<output_dir>/<书名>-<hash>.<ext>` | 完成的 EPUB / TXT |
| `<output_dir>/.cache/<book-hash>/` | 章节 checkpoint |
| `<output_dir>/.history.json` | 下载历史元数据 |
| `<output_dir>/<书名>-<hash>.failed.json` | 失败章节清单 |
| `<output_dir>/.trash/` | 通过书库移除的文件 |
| `<output_dir>/.library.lock` | 单写入者锁 |

成品文件名带来源与书籍 URL 派生的身份后缀，同名书不会再互相覆盖。中断的下载
保留 checkpoint，重新运行同一本书会复用已成功的章节。历史采用原子写入，按
来源 / 书籍 URL / 格式去重。

## 已知限制

- 论坛 HTML 可能变化；bundled fixtures 覆盖代表性的目录、分页、登录失效和
  楼层布局，但不能保证所有历史主题模板。
- 真实网络测试必须显式设置 `YURISCRAPER_LIVE_TEST=1`；默认测试只使用本地 HTTP
  fixture，不包含或读取个人账号密码。
- 同一个输出目录同一时间只运行一个下载任务；第二个进程会收到明确的
  library-busy 错误，而不是争抢元数据。
- Android 仍是独立客户端，见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 开发与发布

```bash
go test -short ./...
go vet ./...
make build
```

推送 `v*` 标签后，GitHub Actions 会校验标签与 `release/VERSION` 一致，构建
Windows、Linux 和 macOS 的 amd64/arm64 产物，并附带 SHA-256 清单发布到
Release。二进制产物不会提交进仓库。

## 使用边界

项目用于个人阅读整理。请使用保守的请求速率，遵守网站规则和版权要求，不要
绕过访问限制，也不要传播下载内容。

## License

MIT
