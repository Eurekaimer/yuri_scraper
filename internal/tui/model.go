package tui

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/app"
	"github.com/Eurekaimer/yuri_scraper/internal/library"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	SecStartup
)

type Model struct {
	section, returnSection                                     Section
	width, height                                              int
	ready                                                      bool
	input                                                      textinput.Model
	auth                                                       authForm
	authorInput, maxPagesInput, minCharsInput                  textinput.Model
	results, allResults                                        []search.SearchResult
	searching                                                  bool
	searchErr                                                  error
	cursor                                                     int
	sources                                                    []search.SiteAdapter
	sourceCursor                                               int
	selectedSite                                               string
	searchScopes                                               []search.SearchScope
	scopeCursor                                                int
	chapterOptions, activePlanOptions                          search.ChapterPlanOptions
	modeCursor, planFocus                                      int
	candidates                                                 []search.ChapterCandidate
	candidateCursor                                            int
	inspectingPlan                                             bool
	planErr                                                    error
	taskCancel                                                 context.CancelFunc
	progress                                                   ProgressState
	progressCh                                                 <-chan app.Snapshot
	activeBook                                                 search.SearchResult
	entries, allEntries                                        []library.Entry
	libCursor                                                  int
	service                                                    *app.Service
	engine                                                     *search.Engine
	ctx                                                        context.Context
	cancel                                                     context.CancelFunc
	searchCancel                                               context.CancelFunc
	requestID, planID                                          uint64
	version                                                    string
	startedAt                                                  time.Time
	startupNext                                                Section
	preview, details, help, filtering, gPending, confirmDelete bool
	inputReturn                                                Section
	query, notice                                              string
	viewOffset                                                 int
	deleteEntry                                                library.Entry
	diagnostic                                                 bool
	diagnosticLog                                              string
}

type ProgressState struct {
	total, completed, failed, cached int
	current                          string
	done                             bool
	err                              error
	outputFile, state, id            string
	speed                            string
	workers                          int64
	requestsPerSecond                float64
}

func NewModel(service *app.Service, version string) *Model {
	ti := textinput.New()
	ti.Placeholder = "搜索书名或作者"
	ti.CharLimit = 200
	ti.Width = 50
	author := textinput.New()
	author.Placeholder = "作者名或 UID"
	author.CharLimit = 100
	author.Width = 30
	pages := textinput.New()
	pages.CharLimit = 4
	pages.Width = 8
	chars := textinput.New()
	chars.CharLimit = 6
	chars.Width = 8
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{section: SecResults, startupNext: SecResults, input: ti, auth: newAuthForm(), authorInput: author, maxPagesInput: pages, minCharsInput: chars, service: service, engine: service.Search, sources: service.Search.Registry().All(), ctx: ctx, cancel: cancel, version: version, preview: true}
	if len(m.sources) > 0 {
		m.selectedSite = m.sources[0].Name()
	}
	m.SetChapterPlanDefaults(search.ChapterPlanOptions{MaxScanPages: service.Config.Yamibo.MaxScanPages, MinPostChars: service.Config.Yamibo.MinPostChars})
	if service.Config.Startup.Animation != "off" && !service.Config.Startup.ReducedMotion && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == "" {
		m.section = SecStartup
	}
	return m
}

func (m *Model) ShowLibrary() { m.section = SecLibrary; m.startupNext = SecLibrary }

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
	m.gPending = false
	m.details = false
	m.viewOffset = 0
	m.notice = ""
	m.blurPlanInputs()
	m.input.Blur()
	m.auth.blur()
	if section == SecSearch {
		return m.input.Focus()
	}
	if section == SecAuth {
		return m.auth.focusCmd()
	}
	if section == SecChapterPlan && m.planFocus > 0 {
		return m.focusPlanInput()
	}
	return nil
}

func (m *Model) Init() tea.Cmd {
	if m.section == SecStartup {
		m.startedAt = time.Now()
		return tea.Batch(textinput.Blink, tickStartup())
	}
	if m.section == SecLibrary {
		return m.loadLibrary()
	}
	return textinput.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		m.input.Width = max(1, m.contentWidth()-7)
		m.auth.setWidth(max(1, m.contentWidth()-8))
		m.authorInput.Width = min(30, max(1, m.contentWidth()-14))
		m.viewOffset = 0
	case startupTick:
		if m.section == SecStartup {
			if time.Since(m.startedAt) >= startupDuration {
				m.finishStartup()
				if m.section == SecLibrary {
					return m, m.loadLibrary()
				}
			} else {
				return m, tickStartup()
			}
		}
	case DownloadMsg:
		if msg.Snapshot.ID != m.progress.id && m.progress.id != "" {
			return m, nil
		}
		p := msg.Snapshot
		m.progress.id = p.ID
		m.progress.state = p.State
		m.progress.total = p.Total
		m.progress.completed = p.Completed
		m.progress.failed = p.Failed
		m.progress.cached = p.Cached
		m.progress.current = p.Current
		m.progress.outputFile = p.OutputFile
		m.progress.workers, m.progress.requestsPerSecond = p.Workers, p.RequestsPer
		m.progress.speed = p.Speed
		m.progress.done = p.Terminal()
		if p.Error != "" {
			m.progress.err = errors.New(p.Error)
		}
		if !m.progress.done {
			return m, waitProgress(m.progressCh)
		}
		if m.taskCancel != nil {
			m.taskCancel()
			m.taskCancel = nil
		}
		return m, m.loadLibrary()
	case ScrapeStartedMsg:
		if msg.Error != nil {
			m.progress.done = true
			m.progress.err = msg.Error
			m.progress.state = "failed"
			if errors.Is(msg.Error, context.Canceled) {
				m.progress.state = "canceled"
			}
			if m.taskCancel != nil {
				m.taskCancel()
				m.taskCancel = nil
			}
			return m, nil
		}
		m.progressCh = msg.Progress
		return m, waitProgress(msg.Progress)
	case SearchMsg:
		if msg.ID != m.requestID || msg.Site != m.selectedSite {
			return m, nil
		}
		m.searching = false
		m.searchErr = msg.Error
		if msg.Error == nil {
			m.results = msg.Results
			m.allResults = msg.Results
			m.cursor = 0
		}
	case ChapterPlanMsg:
		if msg.ID != m.planID {
			return m, nil
		}
		m.inspectingPlan = false
		m.candidates = msg.Plan.Candidates
		m.planErr = msg.Error
		m.candidateCursor = min(m.candidateCursor, max(0, len(m.candidates)-1))
	case LibraryMsg:
		if msg.Error != nil {
			m.notice = msg.Error.Error()
		} else {
			m.entries = msg.Entries
			m.allEntries = msg.Entries
			m.libCursor = min(m.libCursor, max(0, len(m.entries)-1))
		}
	case diagnosticMsg:
		m.diagnosticLog = msg.text
	case NoticeMsg:
		m.notice = msg.Text
	case authenticationMsg:
		m.auth.busy = false
		m.auth.err = msg.err
		if msg.err == nil {
			m.auth.username.SetValue("")
			m.auth.clearPassword()
			cmds = append(cmds, m.setSection(SecResults))
		} else {
			m.auth.focus = 1
			cmds = append(cmds, m.auth.focusCmd())
		}
	case tea.KeyMsg:
		if m.section == SecStartup && msg.String() != "ctrl+c" {
			m.finishStartup()
			if m.section == SecLibrary {
				return m, m.loadLibrary()
			}
			return m, nil
		}
		consumed := m.handleKey(msg, &cmds)
		if !consumed {
			switch m.section {
			case SecAuth:
				cmds = append(cmds, m.auth.update(msg))
			case SecChapterPlan:
				cmds = append(cmds, m.updatePlanInput(msg))
			case SecSearch:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	}
	if m.section == SecSearch {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.section == SecAuth {
		cmds = append(cmds, m.auth.update(msg))
	}
	if m.section == SecChapterPlan {
		cmds = append(cmds, m.updatePlanInput(msg))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) active() bool { return m.progress.state != "" && !m.progress.done }
func (m *Model) canRetryTask() bool {
	return m.progress.done && (m.progress.failed > 0 || m.progress.state == "canceled" || m.progress.state == "failed" || m.progress.state == "partial")
}
func (m *Model) visibleRows() int {
	if m.contentHeight() <= 4 {
		return max(1, m.contentHeight()-1)
	}
	return max(1, m.contentHeight()-3)
}
