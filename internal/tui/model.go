package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Section is the active panel.
type Section int

const (
	SecSources Section = iota
	SecAuth
	SecSearch
	SecResults
	SecProgress
	SecLibrary
)

type Model struct {
	section       Section
	returnSection Section
	width         int
	height        int
	ready         bool

	// Search
	input        textinput.Model
	authInput    textinput.Model
	results      []search.SearchResult
	searching    bool
	searchErr    error
	cursor       int
	sources      []search.SiteAdapter
	sourceCursor int
	selectedSite string

	// Progress
	progress   ProgressState
	progressCh <-chan scraper.Progress
	activeBook search.SearchResult

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
	ti.TextStyle = lipgloss.NewStyle().Foreground(Fg)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(Subtle)

	auth := textinput.New()
	auth.Placeholder = "粘贴百合会 Cookie（仅保存在本次运行内存中）"
	auth.CharLimit = 8192
	auth.Width = 70
	auth.EchoMode = textinput.EchoPassword
	auth.EchoCharacter = '•'

	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		section:   SecSources,
		input:     ti,
		authInput: auth,
		sources:   searchEngine.Registry().All(),
		engine:    searchEngine,
		scraper:   scraperEngine,
		history:   hist,
		entries:   hist.List(),
		ctx:       ctx,
		cancel:    cancel,
		version:   version,
	}
}

func (m *Model) setSection(section Section) tea.Cmd {
	m.section = section
	if section == SecSearch {
		m.authInput.Blur()
		return m.input.Focus()
	}
	m.input.Blur()
	if section == SecAuth {
		return m.authInput.Focus()
	}
	m.authInput.Blur()
	return nil
}

func (m *Model) Init() tea.Cmd { return textinput.Blink }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = min(msg.Width-20, 60)
		m.authInput.Width = min(msg.Width-20, 70)
		m.ready = true

	case scraper.Progress:
		return m, m.handleProgress(msg)

	case ScrapeStartedMsg:
		if msg.Error != nil {
			return m, m.handleProgress(scraper.Progress{ChapterIndex: -1, Error: msg.Error})
		}
		m.progressCh = msg.Progress
		return m, waitProgress(m.progressCh)

	case SearchMsg:
		m.searching = false
		if msg.Error != nil {
			m.searchErr = msg.Error
		} else {
			m.results = msg.Results
			m.searchErr = nil
			m.cursor = 0
			if len(msg.Results) > 0 {
				cmds = append(cmds, m.setSection(SecResults))
			}
		}

	case tea.KeyMsg:
		consumed := m.handleKey(msg, &cmds)
		if !consumed && m.section == SecAuth {
			var cmd tea.Cmd
			m.authInput, cmd = m.authInput.Update(msg)
			cmds = append(cmds, cmd)
		} else if !consumed {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Non-key messages: let textinput process them
	var cmd tea.Cmd
	if m.section == SecAuth {
		m.authInput, cmd = m.authInput.Update(msg)
	} else {
		m.input, cmd = m.input.Update(msg)
	}
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
			format := strings.TrimPrefix(filepath.Ext(msg.OutputFile), ".")
			if err := m.history.Add(store.HistoryEntry{
				Title: m.activeBook.Title, Author: m.activeBook.Author,
				SiteName: m.activeBook.SiteName, BookURL: m.activeBook.BookURL,
				Format: format, FilePath: msg.OutputFile, Chapters: m.progress.completed,
			}); err != nil && m.progress.err == nil {
				m.progress.err = fmt.Errorf("保存下载历史: %w", err)
			}
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
	case SecProgress:
		if key == "r" && m.progress.done && len(m.progress.failedChs) > 0 {
			m.progress = ProgressState{speed: scraper.SpeedNormal}
			*cmds = append(*cmds, m.startScrape(m.activeBook))
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
			m.progress = ProgressState{speed: scraper.SpeedNormal}
			*cmds = append(*cmds, m.setSection(SecProgress))
			*cmds = append(*cmds, m.startScrape(m.results[m.cursor]))
			return true
		}
		if m.input.Value() != "" && !m.searching {
			m.searching = true
			m.searchErr = nil
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
	case "/":
		*cmds = append(*cmds, m.setSection(SecSearch))
		return true
	}
	if m.section == SecSearch && len(m.results) > 0 {
		switch key {
		case "down", "ctrl+n":
			*cmds = append(*cmds, m.setSection(SecResults))
			return true
		}
	}
	if m.section == SecResults {
		switch key {
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

// --- Messages & Commands ---

type SearchMsg struct {
	Results []search.SearchResult
	Error   error
}

type ScrapeStartedMsg struct {
	Progress <-chan scraper.Progress
	Error    error
}

func (m *Model) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		results, err := m.engine.SearchSite(m.ctx, m.selectedSite, query)
		return SearchMsg{Results: results, Error: err}
	}
}

func (m *Model) startScrape(result search.SearchResult) tea.Cmd {
	return func() tea.Msg {
		adapter := m.engine.Registry().Get(result.SiteName)
		if adapter == nil {
			return ScrapeStartedMsg{Error: fmt.Errorf("未找到站点适配器: %s", result.SiteName)}
		}
		ch, err := m.scraper.ScrapeBook(m.ctx, adapter, result.BookURL, result)
		if err != nil {
			return ScrapeStartedMsg{Error: err}
		}
		return ScrapeStartedMsg{Progress: ch}
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
