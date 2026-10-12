package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/app"
	"github.com/Eurekaimer/yuri_scraper/internal/diagnostics"
	"github.com/Eurekaimer/yuri_scraper/internal/library"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

type SearchMsg struct {
	ID      uint64
	Site    string
	Results []search.SearchResult
	Error   error
}
type ScrapeStartedMsg struct {
	Progress <-chan app.Snapshot
	Error    error
}
type ChapterPlanMsg struct {
	ID    uint64
	Plan  search.ChapterPlan
	Error error
}
type DownloadMsg struct{ Snapshot app.Snapshot }
type LibraryMsg struct {
	Entries []library.Entry
	Error   error
}
type NoticeMsg struct{ Text string }
type diagnosticMsg struct{ text string }

func (m *Model) loadDiagnostics() tea.Cmd {
	dir := m.service.Config.OutputDir
	return func() tea.Msg {
		text, err := diagnostics.Recent(dir)
		if err != nil {
			text = "无法读取日志：" + err.Error()
		}
		if text == "" {
			text = "日志暂无记录。"
		}
		return diagnosticMsg{text: text}
	}
}

func (m *Model) doSearch(query string) tea.Cmd {
	if m.searchCancel != nil {
		m.searchCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.searchCancel = cancel
	m.requestID++
	id := m.requestID
	engine, site, scopes := m.engine, m.selectedSite, m.activeSearchScopeIDs()
	m.query = query
	m.notice = ""
	return func() tea.Msg {
		started := time.Now()
		results, err := engine.SearchSiteScoped(ctx, site, query, scopes)
		if err != nil {
			slog.Error("search.failed", "request_id", id, "source", site, "elapsed_ms", time.Since(started).Milliseconds(), diagnostics.Failure(err))
		} else {
			slog.Info("search.completed", "request_id", id, "source", site, "results", len(results), "elapsed_ms", time.Since(started).Milliseconds())
		}
		return SearchMsg{ID: id, Site: site, Results: results, Error: err}
	}
}

func (m *Model) activeSearchScopeIDs() []string {
	if m.scopeCursor <= 0 || m.scopeCursor > len(m.searchScopes) {
		return nil
	}
	return []string{m.searchScopes[m.scopeCursor-1].ID}
}

func (m *Model) startScrape(result search.SearchResult, options search.ChapterPlanOptions) tea.Cmd {
	service := m.service
	ctx, cancel := context.WithCancel(m.ctx)
	m.taskCancel = cancel
	return func() tea.Msg {
		ch, err := service.Start(ctx, result, options)
		return ScrapeStartedMsg{Progress: ch, Error: err}
	}
}

func (m *Model) inspectChapterPlan(result search.SearchResult) tea.Cmd {
	adapter := m.engine.Registry().Get(result.SiteName)
	planner, ok := adapter.(search.ChapterPlanner)
	opts, ctx := m.chapterOptions, m.ctx
	m.planID++
	id := m.planID
	return func() tea.Msg {
		if !ok {
			return ChapterPlanMsg{ID: id, Error: fmt.Errorf("此书源不支持章节规划")}
		}
		opts.Mode = search.ChapterModeCatalog
		opts.CatalogCandidate = 0
		plan, err := planner.PlanChapters(ctx, result.BookURL, opts)
		return ChapterPlanMsg{ID: id, Plan: plan, Error: err}
	}
}

func (m *Model) resetChapterPlan() {
	m.modeCursor, m.planFocus, m.candidateCursor = 0, 0, 0
	m.authorInput.SetValue("")
	m.maxPagesInput.SetValue(strconv.Itoa(m.chapterOptions.MaxScanPages))
	m.minCharsInput.SetValue(strconv.Itoa(m.chapterOptions.MinPostChars))
	m.candidates, m.planErr = nil, nil
	m.inspectingPlan = true
}

func waitProgress(ch <-chan app.Snapshot) tea.Cmd {
	return func() tea.Msg {
		snapshot, ok := <-ch
		if !ok {
			return NoticeMsg{Text: "任务消息流已关闭"}
		}
		return DownloadMsg{Snapshot: snapshot}
	}
}

func (m *Model) loadLibrary() tea.Cmd {
	service := m.service
	return func() tea.Msg { entries, err := service.Library(); return LibraryMsg{Entries: entries, Error: err} }
}

func readerCommand(entry library.Entry, reveal bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if reveal {
			err = library.Reveal(entry)
		} else {
			err = library.Open(entry)
		}
		if err != nil {
			return NoticeMsg{Text: err.Error()}
		}
		return NoticeMsg{Text: "已请求系统默认应用打开"}
	}
}

func (m *Model) trashCommand(entry library.Entry) tea.Cmd {
	service := m.service
	return func() tea.Msg {
		if err := service.Remove(entry.ID); err != nil {
			return NoticeMsg{Text: err.Error()}
		}
		entries, err := service.Library()
		return LibraryMsg{Entries: entries, Error: err}
	}
}
