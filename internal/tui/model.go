package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hzg/yuri_scraper/internal/scraper"
	"github.com/hzg/yuri_scraper/internal/search"
	"github.com/hzg/yuri_scraper/internal/store"
)

// Section is the active panel.
type Section int

const (
	SecSearch Section = iota
	SecResults
	SecProgress
	SecLibrary
)

type Model struct {
	section Section
	width   int
	height  int
	ready   bool

	// Search
	input     textinput.Model
	results   []search.SearchResult
	searching bool
	searchErr error
	cursor    int

	// Progress
	progress   ProgressState
	progressCh <-chan scraper.Progress

	// Library
	entries   []store.HistoryEntry
	libCursor int

	// Services
	engine  *search.Engine
	scraper *scraper.Engine
	history *store.History

	// Context
	ctx    context.Context
	cancel context.CancelFunc

	version string
}

// ProgressState tracks a scraping session.
type ProgressState struct {
	total      int
	completed  int
	failed     int
	current    string
	done       bool
	err        error
	outputFile string
	speed      scraper.SpeedProfile
	failedChs  []scraper.FailedChapter
}

func NewModel(searchEngine *search.Engine, scraperEngine *scraper.Engine, hist *store.History, version string) *Model {
	ti := textinput.New()
	ti.Placeholder = "输入书名或作者…"
	ti.CharLimit = 100
	ti.Width = 50
	ti.Focus()
	ti.TextStyle = lipgloss.NewStyle().Foreground(Fg)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(Subtle)

	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		section: SecSearch,
		input:   ti,
		engine:  searchEngine,
		scraper: scraperEngine,
		history: hist,
		entries: hist.List(),
		ctx:     ctx,
		cancel:  cancel,
		version: version,
	}
}

func (m *Model) Init() tea.Cmd { return textinput.Blink }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = min(msg.Width-20, 60)
		m.ready = true

	case scraper.Progress:
		return m, m.handleProgress(msg)

	case SearchMsg:
		m.searching = false
		if msg.Error != nil {
			m.searchErr = msg.Error
		} else {
			m.results = msg.Results
			m.searchErr = nil
			m.cursor = 0
			if len(msg.Results) > 0 {
				m.section = SecResults
			}
		}

	case tea.KeyMsg:
		consumed := m.handleKey(msg, &cmds)
		if !consumed {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Non-key messages: let textinput process them
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *Model) handleProgress(msg scraper.Progress) tea.Cmd {
	if msg.TotalChapters > 0 && m.progress.total == 0 {
		m.progress.total = msg.TotalChapters
	}

	switch {
	case msg.ChapterIndex == -2:
		m.progress.done = true
		return nil
	case msg.ChapterIndex < 0:
		m.progress.done = true
		m.progress.err = msg.Error
		if msg.OutputFile != "" {
			m.progress.outputFile = msg.OutputFile
		}
		return nil
	case msg.Done:
		m.progress.completed++
		m.progress.current = msg.ChapterTitle
	case msg.Error != nil:
		m.progress.failed++
		m.progress.failedChs = append(m.progress.failedChs, scraper.FailedChapter{
			Index: msg.ChapterIndex, Title: msg.ChapterTitle,
			URL: msg.ChapterURL, Error: msg.Error.Error(),
		})
	}
	if !m.progress.done {
		return waitProgress(m.progressCh)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyMsg, cmds *[]tea.Cmd) bool {
	key := msg.String()
	switch key {
	case "ctrl+c":
		m.cancel()
		*cmds = append(*cmds, tea.Quit)
		return true
	case "ctrl+l":
		m.section = SecLibrary
		m.entries = m.history.List()
		return true
	case "esc":
		if m.section != SecSearch {
			m.section = SecSearch
			m.searchErr = nil
		}
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
		case "r":
			if len(m.progress.failedChs) > 0 {
				go m.scraper.RetryFailed(m.ctx, m.progress.failedChs)
				m.progress.failed = 0
				m.progress.failedChs = nil
			}
			return true
		}
	}
	switch m.section {
	case SecSearch, SecResults:
		return m.handleSearchKeys(key, cmds)
	case SecProgress:
		if key == "enter" && m.progress.done {
			m.section = SecSearch
			m.progress = ProgressState{}
		}
		return true
	case SecLibrary:
		switch key {
		case "up", "k":
			if m.libCursor > 0 { m.libCursor-- }
		case "down", "j":
			if m.libCursor < len(m.entries)-1 { m.libCursor++ }
		}
		return true
	}
	return false
}

func (m *Model) handleSearchKeys(key string, cmds *[]tea.Cmd) bool {
	switch key {
	case "enter":
		if m.section == SecResults && m.cursor < len(m.results) {
			*cmds = append(*cmds, m.startScrape(m.results[m.cursor]))
			return true
		}
		if m.input.Value() != "" && !m.searching {
			m.searching = true
			m.searchErr = nil
			*cmds = append(*cmds, m.doSearch(m.input.Value()))
		}
		return true
	case "tab":
		if len(m.results) > 0 {
			m.input.SetValue(m.results[0].Title)
		}
		return true
	}
	if m.section == SecResults {
		switch key {
		case "up", "k": if m.cursor > 0 { m.cursor-- }; return true
		case "down", "j": if m.cursor < len(m.results)-1 { m.cursor++ }; return true
		case "g": m.cursor = 0; return true
		case "G": if len(m.results) > 0 { m.cursor = len(m.results)-1 }; return true
		case "ctrl+d": m.cursor = min(m.cursor+5, max(0, len(m.results)-1)); return true
		case "ctrl+u": m.cursor = max(m.cursor-5, 0); return true
		}
	}
	return false
}

// --- Messages & Commands ---

type SearchMsg struct {
	Results []search.SearchResult
	Error   error
}

func (m *Model) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		results, err := m.engine.Search(m.ctx, query)
		return SearchMsg{Results: results, Error: err}
	}
}

func (m *Model) startScrape(result search.SearchResult) tea.Cmd {
	return func() tea.Msg {
		m.section = SecProgress
		m.progress = ProgressState{speed: scraper.SpeedNormal}
		adapter := m.engine.Registry().Get(result.SiteName)
		if adapter == nil {
			return scraper.Progress{ChapterIndex: -1, Error: fmt.Errorf("未找到站点适配器: %s", result.SiteName)}
		}
		ch, err := m.scraper.ScrapeBook(m.ctx, adapter, result.BookURL, result)
		if err != nil {
			return scraper.Progress{ChapterIndex: -1, Error: err}
		}
		m.progressCh = ch
		select {
		case p, ok := <-ch:
			if !ok {
				return scraper.Progress{ChapterIndex: -2}
			}
			return p
		case <-m.ctx.Done():
			return scraper.Progress{ChapterIndex: -1, Error: m.ctx.Err()}
		}
	}
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
