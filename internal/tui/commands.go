package tui

import (
	"fmt"
	"strconv"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

type SearchMsg struct {
	Results []search.SearchResult
	Error   error
}
type ScrapeStartedMsg struct {
	Progress <-chan scraper.Progress
	Error    error
}
type ChapterPlanMsg struct {
	Plan  search.ChapterPlan
	Error error
}

func (m *Model) doSearch(query string) tea.Cmd {
	engine, ctx, site, scopes := m.engine, m.ctx, m.selectedSite, m.activeSearchScopeIDs()
	return func() tea.Msg {
		results, err := engine.SearchSiteScoped(ctx, site, query, scopes)
		return SearchMsg{Results: results, Error: err}
	}
}

func (m *Model) activeSearchScopeIDs() []string {
	if m.scopeCursor <= 0 || m.scopeCursor > len(m.searchScopes) {
		return nil
	}
	return []string{m.searchScopes[m.scopeCursor-1].ID}
}

func (m *Model) startScrape(result search.SearchResult, options search.ChapterPlanOptions) tea.Cmd {
	engine, scraperEngine, ctx := m.engine, m.scraper, m.ctx
	return func() tea.Msg {
		adapter := engine.Registry().Get(result.SiteName)
		if adapter == nil {
			return ScrapeStartedMsg{Error: fmt.Errorf("未找到站点适配器: %s", result.SiteName)}
		}
		ch, err := scraperEngine.ScrapeBookWithOptions(ctx, adapter, result.BookURL, result, options)
		if err != nil {
			return ScrapeStartedMsg{Error: err}
		}
		return ScrapeStartedMsg{Progress: ch}
	}
}

func (m *Model) inspectChapterPlan(result search.SearchResult) tea.Cmd {
	adapter := m.engine.Registry().Get(result.SiteName)
	planner, ok := adapter.(search.ChapterPlanner)
	opts, ctx := m.chapterOptions, m.ctx
	return func() tea.Msg {
		if !ok {
			return ChapterPlanMsg{Error: fmt.Errorf("当前来源不支持章节规划")}
		}
		opts.Mode, opts.CatalogCandidate = search.ChapterModeCatalog, 0
		plan, err := planner.PlanChapters(ctx, result.BookURL, opts)
		return ChapterPlanMsg{Plan: plan, Error: err}
	}
}

func (m *Model) resetChapterPlan() {
	if m.chapterOptions.MaxScanPages <= 0 {
		m.SetChapterPlanDefaults(search.ChapterPlanOptions{})
	}
	m.modeCursor, m.planFocus, m.candidateCursor = 0, 0, 0
	m.authorInput.SetValue("")
	m.maxPagesInput.SetValue(strconv.Itoa(m.chapterOptions.MaxScanPages))
	m.minCharsInput.SetValue(strconv.Itoa(m.chapterOptions.MinPostChars))
	m.candidates, m.planErr = nil, nil
	m.inspectingPlan = true
}

func waitProgress(ch <-chan scraper.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return scraper.Progress{ChapterIndex: -2}
		}
		return p
	}
}
