package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Section int

const (
	SecSources Section = iota
	SecAuth
	SecSearch
	SecResults
	SecChapterPlan
	SecProgress
	SecLibrary
)

type Model struct {
	section       Section
	returnSection Section
	width, height int
	ready         bool

	input, authInput                  textinput.Model
	authorInput, maxPagesInput        textinput.Model
	minCharsInput                     textinput.Model
	results                           []search.SearchResult
	searching                         bool
	searchErr                         error
	cursor                            int
	sources                           []search.SiteAdapter
	sourceCursor                      int
	selectedSite                      string
	chapterOptions, activePlanOptions search.ChapterPlanOptions
	modeCursor, planFocus             int
	candidates                        []search.ChapterCandidate
	candidateCursor                   int
	inspectingPlan                    bool
	planErr                           error

	progress   ProgressState
	progressCh <-chan scraper.Progress
	activeBook search.SearchResult
	entries    []store.HistoryEntry
	libCursor  int

	engine  *search.Engine
	scraper *scraper.Engine
	history *store.History
	ctx     context.Context
	cancel  context.CancelFunc
	version string
}

type ProgressState struct {
	total, completed, failed int
	current                  string
	done                     bool
	err                      error
	outputFile               string
	speed                    scraper.SpeedProfile
	failedChs                []scraper.FailedChapter
	strategy                 search.ChapterResolveMode
}

func NewModel(searchEngine *search.Engine, scraperEngine *scraper.Engine, hist *store.History, version string) *Model {
	ti := textinput.New()
	ti.Placeholder, ti.CharLimit, ti.Width = "输入书名或作者…", 100, 50
	ti.TextStyle = lipgloss.NewStyle().Foreground(Fg)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(Subtle)
	auth := textinput.New()
	auth.Placeholder, auth.CharLimit, auth.Width = "粘贴百合会 Cookie（仅保存在本次运行内存中）", 8192, 70
	auth.EchoMode, auth.EchoCharacter = textinput.EchoPassword, '•'
	author := textinput.New()
	author.Placeholder, author.CharLimit, author.Width = "自动识别（可填作者名或 UID）", 100, 32
	maxPages := textinput.New()
	maxPages.CharLimit, maxPages.Width = 4, 8
	minimum := textinput.New()
	minimum.CharLimit, minimum.Width = 6, 8
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		section: SecSources, input: ti, authInput: auth, authorInput: author,
		maxPagesInput: maxPages, minCharsInput: minimum,
		sources: searchEngine.Registry().All(), engine: searchEngine,
		scraper: scraperEngine, history: hist, entries: hist.List(),
		ctx: ctx, cancel: cancel, version: version,
	}
}

func (m *Model) SetChapterPlanDefaults(options search.ChapterPlanOptions) {
	if options.MaxScanPages <= 0 {
		options.MaxScanPages = 10
	}
	if options.MinPostChars <= 0 {
		options.MinPostChars = 80
	}
	m.chapterOptions = options
	m.maxPagesInput.SetValue(strconv.Itoa(options.MaxScanPages))
	m.minCharsInput.SetValue(strconv.Itoa(options.MinPostChars))
}

func (m *Model) setSection(section Section) tea.Cmd {
	m.section = section
	m.blurPlanInputs()
	if section == SecSearch {
		m.authInput.Blur()
		return m.input.Focus()
	}
	m.input.Blur()
	if section == SecAuth {
		return m.authInput.Focus()
	}
	if section == SecChapterPlan && m.planFocus > 0 {
		return m.focusPlanInput()
	}
	m.authInput.Blur()
	return nil
}

func (m *Model) Init() tea.Cmd { return textinput.Blink }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		m.input.Width, m.authInput.Width = min(msg.Width-20, 60), min(msg.Width-20, 70)
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
			m.results, m.searchErr, m.cursor = msg.Results, nil, 0
			if len(msg.Results) > 0 {
				cmds = append(cmds, m.setSection(SecResults))
			}
		}
	case ChapterPlanMsg:
		m.inspectingPlan, m.candidates, m.planErr = false, msg.Plan.Candidates, msg.Error
		if m.candidateCursor >= len(m.candidates) {
			m.candidateCursor = max(0, len(m.candidates)-1)
		}
	case tea.KeyMsg:
		consumed := m.handleKey(msg, &cmds)
		if !consumed && m.section == SecAuth {
			var cmd tea.Cmd
			m.authInput, cmd = m.authInput.Update(msg)
			cmds = append(cmds, cmd)
		} else if !consumed && m.section == SecChapterPlan {
			cmds = append(cmds, m.updatePlanInput(msg))
		} else if !consumed {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	if m.section == SecAuth {
		m.authInput, cmd = m.authInput.Update(msg)
	} else if m.section == SecChapterPlan {
		cmd = m.updatePlanInput(msg)
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
		m.progress.done, m.progress.err = true, msg.Error
		if msg.OutputFile != "" {
			m.progress.outputFile = msg.OutputFile
			format := strings.TrimPrefix(filepath.Ext(msg.OutputFile), ".")
			if err := m.history.Add(store.HistoryEntry{Title: m.activeBook.Title, Author: m.activeBook.Author, SiteName: m.activeBook.SiteName, BookURL: m.activeBook.BookURL, Format: format, FilePath: msg.OutputFile, Chapters: m.progress.completed}); err != nil && m.progress.err == nil {
				m.progress.err = fmt.Errorf("保存下载历史: %w", err)
			}
		}
		return nil
	case msg.Done:
		m.progress.completed++
		m.progress.current = msg.ChapterTitle
	case msg.Error != nil:
		m.progress.failed++
		m.progress.failedChs = append(m.progress.failedChs, scraper.FailedChapter{Index: msg.ChapterIndex, Title: msg.ChapterTitle, URL: msg.ChapterURL, Error: msg.Error.Error()})
	}
	if !m.progress.done {
		return waitProgress(m.progressCh)
	}
	return nil
}
