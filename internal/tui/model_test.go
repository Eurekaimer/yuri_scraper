package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/app"
	"github.com/Eurekaimer/yuri_scraper/internal/library"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newNavigationModel(t *testing.T) *Model {
	t.Helper()
	cfg := store.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Startup.Animation = "off"
	service, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(service, "test")
	t.Cleanup(m.cancel)
	m.ready = true
	m.width = 100
	m.height = 24
	return m
}

func TestInsertDoesNotInterpretVimKeys(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecSearch
	m.input.Focus()
	m.inputReturn = SecResults
	for _, r := range "jklq/:" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.input.Value() != "jklq/:" || m.section != SecSearch {
		t.Fatalf("navigation escaped input: %q section=%v", m.input.Value(), m.section)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.section != SecSearch {
		t.Fatal("cursor motion left input context")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.section != SecResults || m.input.Value() != "jklq/:" {
		t.Fatal("Esc lost the draft or failed to return")
	}
}

func TestLateSearchCannotReplaceNewQuery(t *testing.T) {
	m := newNavigationModel(t)
	m.requestID = 2
	m.selectedSite = "52书库"
	m.results = []search.SearchResult{{Title: "current"}}
	m.searching = true
	m.Update(SearchMsg{ID: 1, Site: m.selectedSite, Results: []search.SearchResult{{Title: "stale"}}})
	if m.results[0].Title != "current" || !m.searching {
		t.Fatal("stale result changed current query")
	}
	m.Update(SearchMsg{ID: 2, Site: m.selectedSite, Results: []search.SearchResult{{Title: "fresh"}}})
	if m.results[0].Title != "fresh" || m.searching {
		t.Fatal("current result was ignored")
	}
}

func TestResultPositionSurvivesTaskNavigation(t *testing.T) {
	m := newNavigationModel(t)
	m.results = []search.SearchResult{{Title: "A"}, {Title: "B"}}
	m.cursor = 1
	m.query = "author"
	m.section = SecProgress
	m.progress = ProgressState{state: "downloading", done: false}
	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, &cmds)
	if m.section != SecResults || m.cursor != 1 || m.query != "author" || len(m.results) != 2 {
		t.Fatal("returning from active task lost result state")
	}
}

func TestLocalFilterDoesNotStartSearch(t *testing.T) {
	m := newNavigationModel(t)
	m.allResults = []search.SearchResult{{Title: "百合 A"}, {Title: "B", Author: "百合作者"}, {Title: "C"}}
	m.results = m.allResults
	var cmds []tea.Cmd
	m.editSearch(true, &cmds)
	m.input.SetValue("百合")
	cmds = nil
	m.handleSearchKeys("enter", &cmds)
	if len(m.results) != 2 || m.searching || m.requestID != 0 {
		t.Fatal("local filtering made a network query or lost matching metadata")
	}
}

func TestPlanningRejectsInvalidBounds(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecChapterPlan
	m.maxPagesInput.SetValue("0")
	m.minCharsInput.SetValue("80")
	var cmds []tea.Cmd
	m.beginPlannedScrape(&cmds)
	if m.planErr == nil || len(cmds) != 0 {
		t.Fatal("invalid page bound started a job")
	}
}

func TestStartupKeyDoesNotStartTask(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecStartup
	m.startupNext = SecResults
	m.results = []search.SearchResult{{Title: "A", BookURL: "https://www.52shuku.net/a", SiteName: "52书库"}}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.section != SecResults || cmd != nil || m.active() || m.service.Snapshot().State != "" {
		t.Fatal("skip key was replayed as a download action")
	}
}

func TestWorkspaceFitsNarrowTerminal(t *testing.T) {
	m := newNavigationModel(t)
	m.width = 60
	m.results = []search.SearchResult{{Title: "中文标题很长但不应突破终端的宽度", Author: "作者"}}
	if got := lipgloss.Width(m.renderSearchWorkspace(56)); got > 56 {
		t.Fatalf("workspace width %d exceeds 56", got)
	}
	if got := lipgloss.Width(truncate("中文标题很长", 7)); got > 7 {
		t.Fatalf("truncate width %d exceeds 7", got)
	}
}

func TestOldTaskCannotOverwriteCurrentState(t *testing.T) {
	m := newNavigationModel(t)
	m.progress = ProgressState{id: "new", state: "planning"}
	m.Update(DownloadMsg{Snapshot: app.Snapshot{ID: "old", State: "completed", OutputFile: "wrong.epub"}})
	if m.progress.id != "new" || m.progress.state != "planning" || m.progress.outputFile != "" {
		t.Fatal("stale task overwrote active state")
	}
}

func TestRejectedDownloadKeepsActiveBook(t *testing.T) {
	m := newNavigationModel(t)
	m.activeBook = search.SearchResult{Title: "正在下载", BookURL: "https://example.test/active"}
	m.progress = ProgressState{id: "active", state: "downloading"}
	m.results = []search.SearchResult{{Title: "另一本书", BookURL: "https://example.test/other"}}
	var cmds []tea.Cmd
	m.handleSearchKeys("enter", &cmds)
	if m.activeBook.BookURL != "https://example.test/active" || m.progress.id != "active" || len(cmds) != 0 {
		t.Fatalf("rejected download changed the active task: book=%+v progress=%+v", m.activeBook, m.progress)
	}
}

func TestViewFitsTerminalAcrossContexts(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {60, 12}, {80, 24}, {120, 32}} {
		for _, section := range []Section{SecResults, SecSearch, SecSources, SecAuth, SecChapterPlan, SecProgress, SecLibrary, SecStartup} {
			m := newNavigationModel(t)
			m.width, m.height = size[0], size[1]
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.section = section
			m.results = []search.SearchResult{{Title: "一段需要按终端显示宽度截断的中文书名", Author: "作者", Description: "很长的简介。"}}
			for _, help := range []bool{false, true} {
				m.help = help
				view := m.View()
				if w, h := lipgloss.Width(view), lipgloss.Height(view); w > m.width || h > m.height {
					t.Errorf("section=%v help=%v terminal=%dx%d rendered=%dx%d", section, help, m.width, m.height, w, h)
				}
			}
		}
	}
}

func TestCancelBeforeStartCancelsPendingContext(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecProgress
	m.progress = ProgressState{state: "planning"}
	start := m.startScrape(search.SearchResult{Title: "不能开始的任务", SiteName: "52书库", BookURL: "https://www.52shuku.net/book.html"}, search.ChapterPlanOptions{})
	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}, &cmds)
	msg := start().(ScrapeStartedMsg)
	if !errors.Is(msg.Error, context.Canceled) || m.service.Snapshot().State != "" {
		t.Fatalf("canceled pending command still started a task: %+v", msg)
	}
}

func TestFailedTaskWithoutFailedChaptersCanRetry(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecProgress
	m.activeBook = search.SearchResult{Title: "规划失败的书", BookURL: "https://example.test/book"}
	m.progress = ProgressState{state: "failed", done: true}
	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}, &cmds)
	if !m.active() || m.progress.state != "planning" {
		t.Fatal("a failed plan/export cannot be retried")
	}
}

func TestSearchCompletionDoesNotInterruptNewDraft(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecSearch
	m.inputReturn = SecResults
	m.input.Focus()
	m.input.SetValue("正在编辑的新搜索")
	m.requestID = 2
	m.Update(SearchMsg{ID: 2, Site: m.selectedSite, Results: []search.SearchResult{{Title: "旧请求结果"}}})
	if m.section != SecSearch || !m.input.Focused() || m.input.Value() != "正在编辑的新搜索" {
		t.Fatal("background search stole focus from the new draft")
	}
}

func TestTrashConfirmationKeepsTargetAcrossRefresh(t *testing.T) {
	m := newNavigationModel(t)
	for _, name := range []string{"first.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(m.service.Config.OutputDir, name), []byte("book"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := m.service.Library()
	if err != nil || len(entries) != 2 {
		t.Fatalf("library: %v %v", entries, err)
	}
	m.section, m.entries = SecLibrary, entries
	var cmds []tea.Cmd
	m.handleLibraryKeys("d", &cmds)
	m.Update(LibraryMsg{Entries: []library.Entry{entries[1], entries[0]}})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, &cmds)
	if len(cmds) != 1 {
		t.Fatalf("unexpected commands: %d", len(cmds))
	}
	if msg, ok := cmds[0]().(LibraryMsg); !ok || msg.Error != nil {
		t.Fatalf("trash failed: %#v", msg)
	}
	if _, err := os.Stat(entries[0].FilePath); !os.IsNotExist(err) {
		t.Fatalf("confirmed entry not moved: %v", err)
	}
	if _, err := os.Stat(entries[1].FilePath); err != nil {
		t.Fatalf("refresh changed trash target: %v", err)
	}
}

func TestSelectedItemRemainsVisibleAfterResize(t *testing.T) {
	m := newNavigationModel(t)
	for range 20 {
		m.results = append(m.results, search.SearchResult{Title: "其他书籍"})
	}
	m.results[19].Title = "末项"
	m.cursor = 19
	for _, size := range [][2]int{{120, 32}, {40, 10}, {60, 12}, {80, 24}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if !strings.Contains(m.View(), "末项") || m.cursor != 19 {
			t.Fatalf("resize %v hid or moved selection", size)
		}
	}
}
