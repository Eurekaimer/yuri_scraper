package tui

import (
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

// A partial title search such as 周次 returns several threads whose titles
// share a long prefix. Searching must only list them; download starts from the
// row the user moved to, not from the first result.
func TestSearchDoesNotImplicitlyDownloadFirstCandidate(t *testing.T) {
	m := newNavigationModel(t)
	m.selectedSite = "百合会"
	m.section = SecResults
	m.requestID = 9
	candidates := []search.SearchResult{
		{Title: "[个人翻译][长篇][羽田宇佐]一周一次买下同班同学的那些事", Author: "羽田宇佐", SiteName: "百合会", BookURL: "https://bbs.yamibo.com/forum.php?mod=viewthread&tid=521519&highlight=test"},
		{Title: "[个人翻译][番外][羽田宇佐]一周一次买下同班同学的那些事", Author: "羽田宇佐", SiteName: "百合会", BookURL: "https://bbs.yamibo.com/forum.php?mod=viewthread&tid=540704&highlight=test"},
		{Title: "【授权转载】[羽田宇佐]一周一次买下同班同学的那些事 短篇合集", Author: "转载君", SiteName: "百合会", BookURL: "https://bbs.yamibo.com/forum.php?mod=viewthread&tid=549611&highlight=test"},
	}
	m.Update(SearchMsg{ID: 9, Site: m.selectedSite, Results: candidates})
	if len(m.results) != len(candidates) {
		t.Fatalf("got %d displayed candidates, want %d", len(m.results), len(candidates))
	}
	if m.activeBook.BookURL != "" || m.progress.state != "" {
		t.Fatalf("search started a download on its own: %#v / %q", m.activeBook, m.progress.state)
	}

	var cmds []tea.Cmd
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown}, &cmds)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}, &cmds)
	if m.activeBook.BookURL != candidates[1].BookURL || m.activeBook.Title != candidates[1].Title {
		t.Fatalf("Enter used %#v, want the highlighted 番外 candidate", m.activeBook)
	}
	if m.progress.state != "planning" {
		t.Fatalf("selected candidate did not start planning: state=%q", m.progress.state)
	}
}
