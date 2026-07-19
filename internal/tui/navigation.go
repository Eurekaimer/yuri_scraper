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
	switch key {
	case "ctrl+c":
		m.cancel()
		*cmds = append(*cmds, tea.Quit)
		return true
	case "ctrl+l":
		if m.section == SecProgress && !m.progress.done {
			return true
		}
		m.returnSection = m.section
		*cmds = append(*cmds, m.setSection(SecLibrary))
		m.entries = m.history.List()
		return true
	case "esc":
		switch m.section {
		case SecAuth:
			*cmds = append(*cmds, m.setSection(SecSources))
		case SecLibrary:
			target := m.returnSection
			if target == SecLibrary {
				target = SecSources
			}
			*cmds = append(*cmds, m.setSection(target))
		case SecProgress:
			if !m.progress.done {
				return true
			}
			if len(m.results) > 0 {
				*cmds = append(*cmds, m.setSection(SecResults))
			} else {
				*cmds = append(*cmds, m.setSection(SecSearch))
			}
		case SecResults:
			*cmds = append(*cmds, m.setSection(SecSearch))
		case SecChapterPlan:
			*cmds = append(*cmds, m.setSection(SecResults))
		case SecSearch:
			*cmds = append(*cmds, m.setSection(SecSources))
		}
		m.searchErr = nil
		return true
	}
	if key == "ctrl+s" {
		if m.section == SecProgress && !m.progress.done {
			return true
		}
		*cmds = append(*cmds, m.setSection(SecSources))
		return true
	}
	if m.section == SecProgress && !m.progress.done {
		switch key {
		case "1":
			m.scraper.SetSpeed(scraper.SpeedSlow)
			m.progress.speed = scraper.SpeedSlow
			return true
		case "2":
			m.scraper.SetSpeed(scraper.SpeedNormal)
			m.progress.speed = scraper.SpeedNormal
			return true
		case "3":
			m.scraper.SetSpeed(scraper.SpeedFast)
			m.progress.speed = scraper.SpeedFast
			return true
		}
	}
	switch m.section {
	case SecSources:
		switch key {
		case "up", "k":
			if m.sourceCursor > 0 {
				m.sourceCursor--
			}
		case "down", "j":
			if m.sourceCursor < len(m.sources)-1 {
				m.sourceCursor++
			}
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
	case SecAuth:
		if key == "enter" && len(m.sources) > 0 {
			if auth, ok := m.sources[m.sourceCursor].(search.CookieAuthenticator); ok && strings.TrimSpace(m.authInput.Value()) != "" {
				auth.SetCookie(m.authInput.Value())
				m.authInput.SetValue("")
				*cmds = append(*cmds, m.setSection(SecSearch))
			}
			return true
		}
		return false
	case SecSearch, SecResults:
		return m.handleSearchKeys(key, cmds)
	case SecChapterPlan:
		return m.handleChapterPlanKeys(key, cmds)
	case SecProgress:
		if key == "r" && m.progress.done && len(m.progress.failedChs) > 0 {
			m.progress = ProgressState{speed: scraper.SpeedNormal, strategy: m.activePlanOptions.Mode}
			*cmds = append(*cmds, m.startScrape(m.activeBook, m.activePlanOptions))
			return true
		}
		if key == "enter" && m.progress.done {
			*cmds = append(*cmds, m.setSection(SecResults))
			m.progress = ProgressState{}
		}
		return true
	case SecLibrary:
		switch key {
		case "up", "k":
			if m.libCursor > 0 {
				m.libCursor--
			}
		case "down", "j":
			if m.libCursor < len(m.entries)-1 {
				m.libCursor++
			}
		}
		return true
	}
	return false
}

func (m *Model) selectSource(cmds *[]tea.Cmd) bool {
	if len(m.sources) == 0 || m.sourceCursor >= len(m.sources) {
		return true
	}
	adapter := m.sources[m.sourceCursor]
	m.selectedSite = adapter.Name()
	m.results = nil
	m.cursor = 0
	if auth, ok := adapter.(search.CookieAuthenticator); ok && auth.RequiresCookie() && !auth.HasCookie() {
		*cmds = append(*cmds, m.setSection(SecAuth))
	} else {
		*cmds = append(*cmds, m.setSection(SecSearch))
	}
	return true
}

func (m *Model) handleSearchKeys(key string, cmds *[]tea.Cmd) bool {
	switch key {
	case "enter":
		if m.section == SecResults && m.cursor < len(m.results) {
			m.activeBook = m.results[m.cursor]
			if adapter := m.engine.Registry().Get(m.activeBook.SiteName); adapter != nil {
				if _, ok := adapter.(search.ChapterPlanner); ok {
					m.resetChapterPlan()
					*cmds = append(*cmds, m.setSection(SecChapterPlan), m.inspectChapterPlan(m.activeBook))
					return true
				}
			}
			m.progress = ProgressState{speed: scraper.SpeedNormal}
			m.activePlanOptions = search.ChapterPlanOptions{}
			*cmds = append(*cmds, m.setSection(SecProgress), m.startScrape(m.results[m.cursor], m.activePlanOptions))
			return true
		}
		if m.input.Value() != "" && !m.searching {
			m.searching, m.searchErr = true, nil
			*cmds = append(*cmds, m.doSearch(m.input.Value()))
		}
		return true
	case "tab", "shift+tab":
		if m.section == SecSearch && len(m.results) > 0 {
			*cmds = append(*cmds, m.setSection(SecResults))
		} else if m.section == SecResults {
			*cmds = append(*cmds, m.setSection(SecSearch))
		}
		return true
	case "right", "alt+l":
		if len(m.results) > 0 {
			*cmds = append(*cmds, m.setSection(SecResults))
		}
		return true
	case "left", "alt+h":
		*cmds = append(*cmds, m.setSection(SecSearch))
		return true
	case "/":
		*cmds = append(*cmds, m.setSection(SecSearch))
		return true
	}
	if m.section == SecSearch && len(m.results) > 0 && (key == "down" || key == "ctrl+n") {
		*cmds = append(*cmds, m.setSection(SecResults))
		return true
	}
	if m.section == SecResults {
		switch key {
		case "h":
			*cmds = append(*cmds, m.setSection(SecSearch))
			return true
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return true
		case "down", "j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return true
		case "g":
			m.cursor = 0
			return true
		case "G":
			if len(m.results) > 0 {
				m.cursor = len(m.results) - 1
			}
			return true
		case "ctrl+d":
			m.cursor = min(m.cursor+5, max(0, len(m.results)-1))
			return true
		case "ctrl+u":
			m.cursor = max(m.cursor-5, 0)
			return true
		}
	}
	return false
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
		case "up", "k":
			m.modeCursor = max(0, m.modeCursor-1)
			return true
		case "down", "j":
			m.modeCursor = min(2, m.modeCursor+1)
			return true
		case "1", "2", "3":
			m.modeCursor = int(key[0] - '1')
			return true
		case "left", "h":
			m.candidateCursor = max(0, m.candidateCursor-1)
			return true
		case "right", "l":
			m.candidateCursor = min(max(0, len(m.candidates)-1), m.candidateCursor+1)
			return true
		case "enter":
			if m.modeCursor == 2 {
				m.planFocus = 1
				*cmds = append(*cmds, m.setSection(SecChapterPlan))
				return true
			}
			return m.beginPlannedScrape(cmds)
		}
		return true
	}
	if key == "enter" {
		if m.planFocus < 3 {
			m.planFocus++
			*cmds = append(*cmds, m.setSection(SecChapterPlan))
			return true
		}
		return m.beginPlannedScrape(cmds)
	}
	return false
}

func (m *Model) beginPlannedScrape(cmds *[]tea.Cmd) bool {
	maxPages, err := strconv.Atoi(strings.TrimSpace(m.maxPagesInput.Value()))
	if err != nil || maxPages <= 0 {
		m.planErr = errors.New("最大扫描页数必须是正整数")
		return true
	}
	minChars, err := strconv.Atoi(strings.TrimSpace(m.minCharsInput.Value()))
	if err != nil || minChars < 20 {
		m.planErr = errors.New("正文最少字符数必须不小于 20")
		return true
	}
	modes := []search.ChapterResolveMode{search.ChapterModeAuto, search.ChapterModeCatalog, search.ChapterModeAuthor}
	opts := search.ChapterPlanOptions{Mode: modes[m.modeCursor], Author: strings.TrimSpace(m.authorInput.Value()), MaxScanPages: maxPages, MinPostChars: minChars, CatalogCandidate: m.candidateCursor}
	m.activePlanOptions = opts
	m.progress = ProgressState{speed: scraper.SpeedNormal, strategy: opts.Mode}
	*cmds = append(*cmds, m.setSection(SecProgress), m.startScrape(m.activeBook, opts))
	return true
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
