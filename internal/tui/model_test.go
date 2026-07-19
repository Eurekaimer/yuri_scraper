package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/Eurekaimer/yuri_scraper/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type navigationAdapter struct{ name string }

func (a navigationAdapter) Name() string    { return a.name }
func (a navigationAdapter) BaseURL() string { return "https://example.test" }
func (a navigationAdapter) Search(context.Context, string) ([]search.SearchResult, error) {
	return nil, nil
}
func (a navigationAdapter) GetChapterList(context.Context, string) ([]search.Chapter, error) {
	return nil, nil
}
func (a navigationAdapter) GetChapterContent(context.Context, string) (string, error) { return "", nil }

type navigationPlanner struct{ navigationAdapter }

func (a navigationPlanner) PlanChapters(context.Context, string, search.ChapterPlanOptions) (search.ChapterPlan, error) {
	return search.ChapterPlan{Candidates: []search.ChapterCandidate{{Source: "div#catalog", Score: 42, Chapters: []search.Chapter{{Title: "第一章"}}, Preview: []string{"第一章"}}}}, nil
}

type credentialsAdapter struct {
	navigationAdapter
	loginErr error
	loggedIn bool
}

func (a *credentialsAdapter) RequiresAuthentication() bool { return true }
func (a *credentialsAdapter) IsAuthenticated() bool        { return a.loggedIn }
func (a *credentialsAdapter) Login(_ context.Context, username, password string) error {
	if a.loginErr != nil {
		return a.loginErr
	}
	if username == "alice" && password == "secret" {
		a.loggedIn = true
		return nil
	}
	return context.Canceled
}

func newNavigationModel(t *testing.T) *Model {
	t.Helper()
	registry := search.NewRegistry()
	registry.Register(navigationAdapter{name: "test"})
	history, err := store.LoadHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewModel(search.NewEngine(registry), scraper.NewEngine(scraper.DefaultConfig()), history, "test")
}

func TestEnterYamiboResultOpensPlanScreen(t *testing.T) {
	registry := search.NewRegistry()
	registry.Register(navigationPlanner{navigationAdapter{name: "百合会"}})
	history, err := store.LoadHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(search.NewEngine(registry), scraper.NewEngine(scraper.DefaultConfig()), history, "test")
	m.SetChapterPlanDefaults(search.ChapterPlanOptions{MaxScanPages: 7, MinPostChars: 60})
	m.section = SecResults
	m.results = []search.SearchResult{{Title: "A", BookURL: "https://example.test/thread", SiteName: "百合会"}}
	var cmds []tea.Cmd
	m.handleSearchKeys("enter", &cmds)
	if m.section != SecChapterPlan || m.maxPagesInput.Value() != "7" || m.minCharsInput.Value() != "60" {
		t.Fatalf("plan screen not initialized: section=%v max=%q min=%q", m.section, m.maxPagesInput.Value(), m.minCharsInput.Value())
	}
}

func TestChapterPlanValidatesParameters(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecChapterPlan
	m.activeBook = search.SearchResult{Title: "A"}
	m.maxPagesInput.SetValue("0")
	m.minCharsInput.SetValue("80")
	var cmds []tea.Cmd
	m.beginPlannedScrape(&cmds)
	if m.planErr == nil || len(cmds) != 0 {
		t.Fatalf("invalid scan pages were accepted")
	}
}

func TestEscapeFromProgressRestoresResults(t *testing.T) {
	m := newNavigationModel(t)
	m.selectedSite = "test"
	m.results = []search.SearchResult{{Title: "A"}, {Title: "B"}}
	m.cursor = 1
	m.input.SetValue("作者名")
	m.section = SecProgress
	m.progress.done = true
	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, &cmds)
	if m.section != SecResults {
		t.Fatalf("section = %v, want SecResults", m.section)
	}
	if m.cursor != 1 || len(m.results) != 2 || m.input.Value() != "作者名" {
		t.Fatalf("results state was not restored: cursor=%d results=%d query=%q", m.cursor, len(m.results), m.input.Value())
	}
}

func TestTabSwitchesSearchAndResults(t *testing.T) {
	m := newNavigationModel(t)
	m.results = []search.SearchResult{{Title: "A"}}
	m.section = SecSearch
	var cmds []tea.Cmd
	m.handleSearchKeys("tab", &cmds)
	if m.section != SecResults {
		t.Fatalf("section = %v, want results", m.section)
	}
	m.handleSearchKeys("tab", &cmds)
	if m.section != SecSearch {
		t.Fatalf("section = %v, want search", m.section)
	}
}

func TestArrowAndVimKeysSwitchSearchColumns(t *testing.T) {
	m := newNavigationModel(t)
	m.results = []search.SearchResult{{Title: "A"}}
	m.section = SecSearch
	var cmds []tea.Cmd
	m.handleSearchKeys("right", &cmds)
	if m.section != SecResults {
		t.Fatalf("right did not focus results")
	}
	m.handleSearchKeys("h", &cmds)
	if m.section != SecSearch {
		t.Fatalf("h did not focus search")
	}
	m.handleSearchKeys("alt+l", &cmds)
	if m.section != SecResults {
		t.Fatalf("alt+l did not focus results")
	}
}

func TestSearchWorkspaceRendersBothPanes(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecResults
	m.input.SetValue("某位作者")
	m.results = []search.SearchResult{{Title: "作品甲", Author: "某位作者"}}
	wide := m.renderSearchWorkspace(100)
	if !strings.Contains(wide, "搜索") || !strings.Contains(wide, "作品甲") {
		t.Fatalf("wide workspace missed a pane: %q", wide)
	}
	narrow := m.renderSearchWorkspace(45)
	if !strings.Contains(narrow, "搜索") || !strings.Contains(narrow, "作品甲") {
		t.Fatalf("narrow workspace missed a pane: %q", narrow)
	}
}

func TestTruncateHonorsCJKCellWidth(t *testing.T) {
	got := truncate("中文标题很长", 7)
	if width := lipgloss.Width(got); width > 7 {
		t.Fatalf("truncate width = %d, want <= 7: %q", width, got)
	}
}

func TestCredentialsLoginMovesFromAuthToSearchAndClearsInputs(t *testing.T) {
	registry := search.NewRegistry()
	adapter := &credentialsAdapter{navigationAdapter: navigationAdapter{name: "百合会"}}
	registry.Register(adapter)
	history, err := store.LoadHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(search.NewEngine(registry), scraper.NewEngine(scraper.DefaultConfig()), history, "test")
	var cmds []tea.Cmd
	m.selectSource(&cmds)
	if m.section != SecAuth {
		t.Fatalf("section = %v, want auth", m.section)
	}
	m.auth.username.SetValue("alice")
	m.auth.password.SetValue("secret")
	m.auth.focus = 1
	cmds = nil
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}, &cmds)
	if !m.auth.busy || m.auth.password.Value() != "" || len(cmds) != 1 {
		t.Fatalf("login command not started securely: busy=%v password=%q cmds=%d", m.auth.busy, m.auth.password.Value(), len(cmds))
	}
	msg := cmds[0]()
	_, _ = m.Update(msg)
	if m.section != SecSearch || !adapter.loggedIn || m.auth.username.Value() != "" || m.auth.password.Value() != "" {
		t.Fatalf("login result not applied: section=%v loggedIn=%v", m.section, adapter.loggedIn)
	}
}

func TestLeavingAuthClearsPassword(t *testing.T) {
	m := newNavigationModel(t)
	m.section = SecAuth
	m.auth.username.SetValue("alice")
	m.auth.password.SetValue("secret")
	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, &cmds)
	if m.section != SecSources || m.auth.password.Value() != "" {
		t.Fatalf("password survived auth exit: section=%v password=%q", m.section, m.auth.password.Value())
	}
}
