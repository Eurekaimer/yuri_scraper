package tui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg, cmds *[]tea.Cmd) bool {
	key := msg.String()
	if key == "ctrl+c" {
		m.service.Cancel()
		m.cancel()
		*cmds = append(*cmds, tea.Quit)
		return true
	}
	if m.diagnostic {
		switch key {
		case "esc", "!":
			m.diagnostic = false
			m.viewOffset = 0
		case "r":
			*cmds = append(*cmds, m.loadDiagnostics())
		case "j", "down":
			m.scrollView(1)
		case "k", "up":
			m.scrollView(-1)
		case "pgdown", "ctrl+d":
			m.scrollView(m.contentHeight())
		case "pgup", "ctrl+u":
			m.scrollView(-m.contentHeight())
		}
		return true
	}
	if m.help {
		switch key {
		case "esc", "?":
			m.help = false
			m.viewOffset = 0
		case "j", "down":
			m.scrollView(1)
		case "k", "up":
			m.scrollView(-1)
		case "pgdown", "ctrl+d":
			m.scrollView(m.contentHeight())
		case "pgup", "ctrl+u":
			m.scrollView(-m.contentHeight())
		}
		return true
	}
	if m.confirmDelete {
		if key == "y" {
			*cmds = append(*cmds, m.trashCommand(m.deleteEntry))
		}
		m.confirmDelete = false
		return true
	}
	if key == "pgdown" || key == "pgup" {
		delta := m.contentHeight()
		if key == "pgup" {
			delta = -delta
		}
		m.scrollView(delta)
		return true
	}
	// Printable navigation keys are never intercepted in text-editing contexts.
	if m.section == SecSearch {
		return m.handleSearchKeys(key, cmds)
	}
	if m.section == SecAuth {
		switch key {
		case "esc":
			m.auth.clearPassword()
			*cmds = append(*cmds, m.setSection(SecSources))
			return true
		case "tab", "shift+tab":
			*cmds = append(*cmds, m.auth.toggleFocus())
			return true
		case "enter":
			if m.auth.busy {
				return true
			}
			if m.auth.focus == 0 {
				m.auth.focus = 1
				*cmds = append(*cmds, m.auth.focusCmd())
				return true
			}
			username, password, err := m.auth.credentials()
			if err != nil {
				m.auth.err = err
				return true
			}
			auth, ok := m.sources[m.sourceCursor].(search.CredentialsAuthenticator)
			if !ok {
				m.auth.err = errors.New("此书源不支持账号登录")
				return true
			}
			m.auth.busy = true
			m.auth.err = nil
			m.auth.clearPassword()
			*cmds = append(*cmds, authenticate(m.ctx, auth, username, password))
			return true
		}
		return false
	}
	if key == "!" && (m.section != SecChapterPlan || m.planFocus == 0) {
		m.diagnostic, m.viewOffset = true, 0
		m.diagnosticLog = "正在读取日志…"
		*cmds = append(*cmds, m.loadDiagnostics())
		return true
	}
	if m.section == SecChapterPlan {
		if key == "esc" {
			*cmds = append(*cmds, m.setSection(SecResults))
			return true
		}
		return m.handleChapterPlanKeys(key, cmds)
	}
	if key == "q" {
		m.service.Cancel()
		m.cancel()
		*cmds = append(*cmds, tea.Quit)
		return true
	}
	if key == "?" {
		m.help = true
		m.viewOffset = 0
		return true
	}
	if key == "ctrl+l" || key == "b" {
		m.returnSection = m.section
		*cmds = append(*cmds, m.setSection(SecLibrary), m.loadLibrary())
		return true
	}
	if key == "s" {
		m.returnSection = m.section
		*cmds = append(*cmds, m.setSection(SecSources))
		return true
	}
	if key == "t" {
		*cmds = append(*cmds, m.setSection(SecProgress))
		return true
	}
	if key == "esc" || key == "h" {
		if m.details {
			m.details = false
			return true
		}
		target := SecResults
		if m.section == SecLibrary && m.returnSection != SecLibrary {
			target = m.returnSection
		}
		*cmds = append(*cmds, m.setSection(target))
		return true
	}
	if key == "l" {
		m.details = !m.details
		m.viewOffset = 0
		return true
	}
	switch m.section {
	case SecSources:
		switch key {
		case "up", "k":
			m.sourceCursor = max(0, m.sourceCursor-1)
		case "down", "j":
			m.sourceCursor = min(max(0, len(m.sources)-1), m.sourceCursor+1)
		case "1", "2":
			index := int(key[0] - '1')
			if index < len(m.sources) {
				m.sourceCursor = index
				return m.selectSource(cmds)
			}
		case "enter":
			return m.selectSource(cmds)
		}
		return true
	case SecResults:
		return m.handleSearchKeys(key, cmds)
	case SecLibrary:
		return m.handleLibraryKeys(key, cmds)
	case SecProgress:
		if key == "x" && m.active() {
			m.service.Cancel()
			if m.taskCancel != nil {
				m.taskCancel()
			}
			m.notice = "已请求取消；已保存章节可在重试时继续使用"
			return true
		}
		if m.active() {
			moved := false
			switch key {
			case "1":
				m.service.SetSpeed(scraper.SpeedSlow)
				moved = true
			case "2":
				m.service.SetSpeed(scraper.SpeedNormal)
				moved = true
			case "3":
				m.service.SetSpeed(scraper.SpeedFast)
				moved = true
			case "a":
				_, moved = m.service.StepSpeed(false)
				if !moved {
					m.notice = "已是最慢档位"
				}
			case "d":
				_, moved = m.service.StepSpeed(true)
				if !moved {
					m.notice = "已是最快档位"
				}
			}
			if moved {
				snapshot := m.service.Snapshot()
				m.progress.workers, m.progress.requestsPerSecond = snapshot.Workers, snapshot.RequestsPer
				m.progress.speed = snapshot.Speed
			}
		}
		if key == "r" && m.canRetryTask() {
			return m.beginDownload(m.activeBook, m.activePlanOptions, cmds)
		}
		if key == "enter" {
			*cmds = append(*cmds, m.setSection(SecResults))
		}
		return true
	}
	return true
}

func (m *Model) selectSource(cmds *[]tea.Cmd) bool {
	if len(m.sources) == 0 || m.sourceCursor >= len(m.sources) {
		return true
	}
	if m.searchCancel != nil {
		m.searchCancel()
	}
	m.requestID++
	m.planID++
	m.searching = false
	adapter := m.sources[m.sourceCursor]
	m.selectedSite = adapter.Name()
	m.searchScopes = nil
	m.scopeCursor = 0
	if scoped, ok := adapter.(search.ScopedSearcher); ok {
		m.searchScopes = scoped.SearchScopes()
	}
	m.results = nil
	m.allResults = nil
	m.cursor = 0
	m.searchErr = nil
	if auth, ok := adapter.(search.CredentialsAuthenticator); ok && auth.RequiresAuthentication() && !auth.IsAuthenticated() {
		m.auth.err = nil
		m.auth.busy = false
		*cmds = append(*cmds, m.setSection(SecAuth))
	} else {
		*cmds = append(*cmds, m.setSection(SecResults))
	}
	return true
}

func (m *Model) editSearch(filter bool, cmds *[]tea.Cmd) {
	m.inputReturn = m.section
	m.filtering = filter
	if filter {
		m.input.SetValue("")
		m.input.Placeholder = "筛选当前列表"
	} else {
		m.input.SetValue(m.query)
		m.input.Placeholder = "搜索书名或作者"
	}
	*cmds = append(*cmds, m.setSection(SecSearch))
}

func (m *Model) move(key string, cursor, total int) int {
	if key == "g" {
		if m.gPending {
			m.gPending = false
			return 0
		}
		m.gPending = true
		return cursor
	}
	m.gPending = false
	switch key {
	case "j", "down":
		return min(max(0, total-1), cursor+1)
	case "k", "up":
		return max(0, cursor-1)
	case "G":
		return max(0, total-1)
	case "ctrl+d":
		return min(max(0, total-1), cursor+max(1, m.visibleRows()/2))
	case "ctrl+u":
		return max(0, cursor-max(1, m.visibleRows()/2))
	}
	return cursor
}

func (m *Model) handleSearchKeys(key string, cmds *[]tea.Cmd) bool {
	if m.section == SecSearch {
		switch key {
		case "esc":
			target := m.inputReturn
			if target == SecSearch || target == SecStartup {
				target = SecResults
			}
			*cmds = append(*cmds, m.setSection(target))
			return true
		case "enter":
			query := strings.TrimSpace(m.input.Value())
			if m.filtering {
				needle := strings.ToLower(query)
				if m.inputReturn == SecLibrary {
					m.entries = nil
					for _, e := range m.allEntries {
						if strings.Contains(strings.ToLower(e.Title+" "+e.Author+" "+e.Format), needle) {
							m.entries = append(m.entries, e)
						}
					}
					m.libCursor = 0
					*cmds = append(*cmds, m.setSection(SecLibrary))
				} else {
					m.results = nil
					for _, r := range m.allResults {
						if strings.Contains(strings.ToLower(r.Title+" "+r.Author), needle) {
							m.results = append(m.results, r)
						}
					}
					m.cursor = 0
					*cmds = append(*cmds, m.setSection(SecResults))
				}
			} else if query != "" {
				m.searching = true
				m.searchErr = nil
				*cmds = append(*cmds, m.doSearch(query), m.setSection(SecResults))
			}
			return true
		}
		return false
	}
	if key == "x" && m.searching {
		if m.searchCancel != nil {
			m.searchCancel()
		}
		m.requestID++
		m.searching = false
		m.notice = "已取消搜索；上次结果仍保留，按 r 重新搜索"
		return true
	}
	if key == "r" && !m.searching && m.query != "" {
		m.searching, m.searchErr = true, nil
		*cmds = append(*cmds, m.doSearch(m.query))
		return true
	}
	if key == "/" || key == "i" {
		m.editSearch(false, cmds)
		return true
	}
	if key == "f" {
		m.editSearch(true, cmds)
		return true
	}
	if key == "ctrl+f" && len(m.searchScopes) > 0 {
		m.scopeCursor = (m.scopeCursor + 1) % (len(m.searchScopes) + 1)
		if m.query != "" {
			m.searching = true
			*cmds = append(*cmds, m.doSearch(m.query))
		}
		return true
	}
	m.cursor = m.move(key, m.cursor, len(m.results))
	if (key == "enter" || key == "p") && m.cursor < len(m.results) {
		if m.active() {
			m.notice = "已有下载任务；按 t 查看或取消"
			return true
		}
		book := m.results[m.cursor]
		if key == "p" {
			adapter := m.engine.Registry().Get(book.SiteName)
			if _, ok := adapter.(search.ChapterPlanner); ok {
				m.activeBook = book
				m.resetChapterPlan()
				*cmds = append(*cmds, m.setSection(SecChapterPlan), m.inspectChapterPlan(book))
			} else {
				m.notice = "此书源使用标准章节目录，无需额外规划"
			}
			return true
		}
		return m.beginDownload(book, m.chapterOptions, cmds)
	}
	if key == "tab" {
		m.preview = !m.preview
	}
	return true
}

func (m *Model) beginDownload(book search.SearchResult, options search.ChapterPlanOptions, cmds *[]tea.Cmd) bool {
	if m.active() {
		m.notice = "已有下载任务；按 t 查看或取消"
		return true
	}
	m.activeBook = book
	m.activePlanOptions = options
	m.progress = ProgressState{state: "planning"}
	*cmds = append(*cmds, m.setSection(SecProgress), m.startScrape(book, options))
	return true
}

func (m *Model) handleLibraryKeys(key string, cmds *[]tea.Cmd) bool {
	if key == "/" || key == "f" || key == "i" {
		m.editSearch(true, cmds)
		return true
	}
	m.libCursor = m.move(key, m.libCursor, len(m.entries))
	if m.libCursor >= len(m.entries) {
		return true
	}
	entry := m.entries[m.libCursor]
	switch key {
	case "enter", "o":
		*cmds = append(*cmds, readerCommand(entry, false))
	case "v":
		*cmds = append(*cmds, readerCommand(entry, true))
	case "d":
		m.confirmDelete = true
		m.deleteEntry = entry
	case "r":
		if entry.BookURL == "" {
			m.notice = "本地导入文件没有书源信息，无法重试下载"
			return true
		}
		return m.beginDownload(search.SearchResult{Title: entry.Title, Author: entry.Author, SiteName: entry.SiteName, BookURL: entry.BookURL}, m.chapterOptions, cmds)
	case "tab":
		m.preview = !m.preview
	}
	return true
}

func (m *Model) handleChapterPlanKeys(key string, cmds *[]tea.Cmd) bool {
	if key == "tab" || key == "shift+tab" {
		delta := 1
		if key == "shift+tab" {
			delta = -1
		}
		m.planFocus = (m.planFocus + delta + 4) % 4
		*cmds = append(*cmds, m.setSection(SecChapterPlan))
		return true
	}
	if m.planFocus == 0 {
		switch key {
		case "k", "up":
			m.modeCursor = max(0, m.modeCursor-1)
		case "j", "down":
			m.modeCursor = min(2, m.modeCursor+1)
		case "1", "2", "3":
			m.modeCursor = int(key[0] - '1')
		case "h", "left":
			m.candidateCursor = max(0, m.candidateCursor-1)
		case "l", "right":
			m.candidateCursor = min(max(0, len(m.candidates)-1), m.candidateCursor+1)
		case "enter":
			return m.beginPlannedScrape(cmds)
		}
		return true
	}
	if key == "enter" {
		return m.beginPlannedScrape(cmds)
	}
	return false
}

func (m *Model) beginPlannedScrape(cmds *[]tea.Cmd) bool {
	pages, err := strconv.Atoi(strings.TrimSpace(m.maxPagesInput.Value()))
	if err != nil || pages <= 0 {
		m.planErr = errors.New("扫描页数必须是正整数")
		return true
	}
	chars, err := strconv.Atoi(strings.TrimSpace(m.minCharsInput.Value()))
	if err != nil || chars < 20 {
		m.planErr = errors.New("最少正文长度不能小于 20 字")
		return true
	}
	modes := []search.ChapterResolveMode{search.ChapterModeAuto, search.ChapterModeCatalog, search.ChapterModeAuthor}
	options := search.ChapterPlanOptions{Mode: modes[m.modeCursor], Author: strings.TrimSpace(m.authorInput.Value()), MaxScanPages: pages, MinPostChars: chars, CatalogCandidate: m.candidateCursor}
	return m.beginDownload(m.activeBook, options, cmds)
}
func (m *Model) blurPlanInputs() {
	m.authorInput.Blur()
	m.maxPagesInput.Blur()
	m.minCharsInput.Blur()
}
func (m *Model) focusPlanInput() tea.Cmd {
	switch m.planFocus {
	case 1:
		return m.authorInput.Focus()
	case 2:
		return m.maxPagesInput.Focus()
	case 3:
		return m.minCharsInput.Focus()
	}
	return nil
}
func (m *Model) updatePlanInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.planFocus {
	case 1:
		m.authorInput, cmd = m.authorInput.Update(msg)
	case 2:
		m.maxPagesInput, cmd = m.maxPagesInput.Update(msg)
	case 3:
		m.minCharsInput, cmd = m.minCharsInput.Update(msg)
	}
	return cmd
}

func (m *Model) scrollView(delta int) {
	content := ""
	switch {
	case m.diagnostic:
		content = m.renderDiagnostics(m.contentWidth())
	case m.help:
		content = m.renderHelp(m.contentWidth())
	case m.section == SecChapterPlan:
		content = m.renderChapterPlanContent(m.contentWidth())
	case m.section == SecProgress:
		content = m.renderProgressContent(m.contentWidth())
	case m.details && m.section == SecResults && m.cursor < len(m.results):
		content = m.bookPreview(m.results[m.cursor], m.contentWidth())
	case m.details && m.section == SecLibrary && m.libCursor < len(m.entries):
		content = libraryPreview(m.entries[m.libCursor], m.contentWidth())
	default:
		return
	}
	m.viewOffset = min(max(0, m.viewOffset+delta), max(0, strings.Count(content, "\n")+1-m.contentHeight()))
}
