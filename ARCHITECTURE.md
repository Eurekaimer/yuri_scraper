# YuriScraper Go 重构 — 架构路线图

> 从 Python 单线程脚本 → Go 多级并发 TUI 小说抓取工具

---

## 一、整体架构

```
┌─────────────────────────────────────────────────────────┐
│                      TUI Layer                          │
│              (Bubble Tea + Bubbles)                     │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐ │
│  │ Search   │ │ Results  │ │ Progress │ │ Library   │ │
│  │ Input    │ │ List     │ │ Bar      │ │ Manager   │ │
│  └──────────┘ └──────────┘ └──────────┘ └───────────┘ │
├─────────────────────────────────────────────────────────┤
│                    Core Engine                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐ │
│  │ Search   │ │ Scraper  │ │ Converter│ │ Exporter  │ │
│  │ Engine   │ │ Engine   │ │ Engine   │ │ Manager   │ │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └─────┬─────┘ │
│       │             │             │              │       │
│  ┌────┴─────────────┴─────────────┴──────────────┴───┐ │
│  │              Concurrency Scheduler                 │ │
│  │    (semaphore + worker pool + rate limiter)        │ │
│  └───────────────────────────────────────────────────┘ │
├─────────────────────────────────────────────────────────┤
│                   Data Layer                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐ │
│  │ Site     │ │ Cache    │ │ Config   │ │ History   │ │
│  │ Registry │ │ (HTTP)   │ │ (YAML)   │ │ (JSON)    │ │
│  └──────────┘ └──────────┘ └──────────┘ └───────────┘ │
└─────────────────────────────────────────────────────────┘
```

---

## 二、模块划分

### 1. `cmd/yuri_scraper/` — 入口
- `main.go`: 解析 CLI flag，启动 TUI

### 2. `internal/tui/` — TUI 层 (Bubble Tea)
| 文件 | 职责 |
|------|------|
| `model.go` | 顶层 Model，管理视图切换 |
| `search.go` | 搜索输入框 + 智能建议 |
| `results.go` | 搜索结果列表（书名、站点、章节数） |
| `progress.go` | 抓取/转换进度条 |
| `library.go` | 已下载小说浏览与管理 |
| `styles.go` | Lipgloss 样式定义 |

**视图流程:**
```
SearchInput → ResultsList → ProgressBar → Complete
                │                            │
                └── Library (随时可切) ←──────┘
```

### 3. `internal/search/` — 搜索与站点注册
| 文件 | 职责 |
|------|------|
| `engine.go` | 搜索引擎接口 + 聚合多源结果 |
| `adapter.go` | 站点适配器接口 (统一抓取协议) |
| `registry.go` | 内置站点注册表 |
| `sites/52shuku.go` | 52书库适配器（搜索 + 元数据 + 章节） |
| `sites/utils.go` | HTML 解码等共享工具 |
| `fuzzy.go` | 模糊搜索：Levenshtein + 字符重叠 + N-gram |

**站点适配器接口:**
```go
type SiteAdapter interface {
    Name() string
    BaseURL() string
    Search(query string) ([]SearchResult, error)
    GetChapterList(bookURL string) ([]Chapter, error)
    GetChapterContent(chapterURL string) (string, error)
}
```

### 4. `internal/scraper/` — 多级并发抓取引擎
| 文件 | 职责 |
|------|------|
| `engine.go` | 调度器：任务队列 + Worker Pool |
| `worker.go` | 单个 worker：抓取章节内容 |
| `ratelimit.go` | 令牌桶限流器（per-site） |
| `pipeline.go` | 三级流水线：章节列表 → 内容抓取 → 文本清洗 |

**三级并发模型:**
```
Level 1: 多个站点并行搜索
Level 2: 单本书内章节列表并行抓取（分页）
Level 3: 章节内容并行下载（受控并发数 + per-site 限速）
```

### 5. `internal/converter/` — 格式转换
| 文件 | 职责 |
|------|------|
| `txt.go` | TXT 输出（章节合并 + 格式化） |
| `epub.go` | EPUB 3 输出（目录 + 分章 + 元数据） |
| `cleaner.go` | HTML → 纯文本清洗流水线 |

### 6. `internal/store/` — 存储
| 文件 | 职责 |
|------|------|
| `config.go` | 用户配置：下载目录、并发数、默认格式 |
| `history.go` | 下载历史（已下载、断点续传） |
| `cache.go` | HTTP 缓存（章节列表、站点元数据） |

### 7. `internal/ai/` — 智能搜索增强（可选，Phase 2）
- 中文分词 + 模糊匹配
- 搜索结果排序（热度、匹配度、完整性）

---

## 三、数据流

```
用户输入 "秦寺"
        │
        ▼
┌───────────────────┐
│  Search Engine    │ ── 52书库搜索 API + 模糊匹配排序
│  (模糊排序)        │
└───────┬───────────┘
        │ []SearchResult{书名, 作者, URL, 站点, 章节数}
        ▼
┌───────────────────┐
│  用户选择一本书      │
└───────┬───────────┘
        │
        ▼
┌───────────────────┐
│  获取章节列表       │ ── 第1章, 第2章, ..., 第N章
└───────┬───────────┘
        │
        ▼
┌───────────────────┐
│  多级并发下载       │ ── Worker Pool (默认8并发)
│  (Pipeline)       │     每个 worker: GET → HTML 清洗 → 写入缓冲
└───────┬───────────┘
        │
        ▼
┌───────────────────┐
│  格式转换          │ ── TXT / EPUB
└───────┬───────────┘
        │
        ▼
┌───────────────────┐
│  保存到 ./novels/  │
└───────────────────┘
```

---

## 四、目录结构

```
yuri_scraper/
├── cmd/
│   └── yuri_scraper/
│       └── main.go              # 程序入口
├── internal/
│   ├── tui/
│   │   ├── model.go             # Bubble Tea 顶层模型
│   │   ├── search.go            # 搜索输入视图
│   │   ├── results.go           # 结果列表视图
│   │   ├── progress.go          # 进度视图
│   │   ├── library.go           # 书库视图
│   │   └── styles.go            # 主题/样式
│   ├── search/
│   │   ├── engine.go            # 搜索聚合引擎
│   │   ├── adapter.go           # 站点适配器接口
│   │   ├── registry.go          # 站点注册
│   │   └── sites/
│   │       ├── 52shuku.go       # 52书库
│   │       ├── biquge.go        # 笔趣阁
│   │       └── ...              # 更多站点
│   ├── scraper/
│   │   ├── engine.go            # 调度器
│   │   ├── worker.go            # 抓取 worker
│   │   ├── ratelimit.go         # 限流器
│   │   └── pipeline.go          # 流水线
│   ├── converter/
│   │   ├── txt.go               # TXT 输出
│   │   ├── epub.go              # EPUB 输出
│   │   └── cleaner.go           # HTML 清洗
│   └── store/
│       ├── config.go            # 配置管理
│       ├── history.go           # 下载历史
│       └── cache.go             # HTTP 缓存
├── novels/                      # [gitignored] 下载的小说
├── go.mod
├── go.sum
├── config.yaml                  # 默认配置
├── ARCHITECTURE.md              # 本文件
└── .gitignore
```

---

## 五、实施阶段

### Phase 1: 核心引擎 (Foundation)
- [x] 架构设计
- [ ] Go module 初始化 + 依赖安装
- [ ] 站点适配器接口定义
- [ ] 52书库适配器实现（搜索 + 目录 + 内容）
- [ ] 多级并发抓取引擎
- [ ] HTML 清洗 + TXT/EPUB 输出
- [ ] 单元测试

### Phase 2: TUI (Interface)
- [ ] Bubble Tea 项目骨架
- [ ] 搜索输入界面 + 即时建议
- [ ] 搜索结果列表
- [ ] 实时进度条
- [ ] 书库浏览
- [ ] 主题/样式

### Phase 3: 智能增强 (Intelligence)
- [ ] 笔趣阁 + 更多站点适配器
- [ ] 搜索结果排序（匹配度 + 热度）
- [ ] 断点续传
- [ ] 配置 YAML 管理

### Phase 4: 打磨 (Polish)
- [ ] .gitignore 重写
- [ ] README 更新
- [ ] 交叉编译脚本
- [ ] 打包发布

---

## 六、关键技术选型

| 需求 | 方案 | 原因 |
|------|------|------|
| TUI 框架 | `charmbracelet/bubbletea` | 最成熟的 Go TUI，Elm 架构 |
| TUI 组件 | `charmbracelet/bubbles` | 输入框、列表、进度条等 |
| 样式 | `charmbracelet/lipgloss` | 终端样式，ANSI |
| HTML 解析 | `PuerkitoBio/goquery` | jQuery 风格，易用 |
| EPUB 生成 | 自实现 (XML 模板) | Go 无成熟 EPUB 库，EPUB 本质是 ZIP+XML |
| HTTP 客户端 | `net/http` + `hashicorp/go-retryablehttp` | 标准库 + 重试 |
| 并发控制 | `golang.org/x/sync/semaphore` | 官方扩展库 |
| 配置 | `gopkg.in/yaml.v3` | YAML 可读性好 |
| 中文分词 | `go-ego/gse` (可选) | 搜索智能匹配 |
| 限流 | 自实现令牌桶 | `golang.org/x/time/rate` |

---

## 七、并发模型详解

```
                    ┌─────────────┐
                    │  Task Queue  │
                    │  (channel)   │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
         ┌────▼───┐   ┌───▼────┐  ┌───▼────┐
         │Worker 1│   │Worker 2│  │Worker N│  (默认 N=8)
         └────┬───┘   └───┬────┘  └───┬────┘
              │            │            │
              │     ┌──────┴──────┐     │
              │     │ Rate Limiter│     │
              │     │ (per-site)  │     │
              │     │ 2 req/s     │     │
              │     └─────────────┘     │
              │            │            │
         ┌────▼────────────▼────────────▼────┐
         │       Result Collector             │
         │       (ordered by chapter)         │
         └────────────────────────────────────┘
```

- **Worker 数量**: 可配置，默认 8
- **Per-site 限流**: 令牌桶，默认 2 req/s，可配
- **章节排序**: 抓取完成后按章节序号重排
- **内存控制**: 流式写入，不缓存全文
- **优雅中断**: Ctrl+C → 保存进度 → 下次断点续传

---

## 八、搜索智能增强策略

1. **多站点并行搜索**: 同时查询 52书库、笔趣阁等站内搜索
2. **本地缓存**: 站点搜索结果缓存 1 小时
3. **中文分词**: 对用户输入分词 → 匹配书名/作者
4. **排序算法**: 
   - 精确匹配 > 部分匹配
   - 热门站点权重更高
   - 有完整章节列表的优先
5. **即时建议**: 输入 2 字后触发搜索，debounce 300ms
