package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Yamibo exposes two sections and searches both until one is chosen.
func TestYamiboSearchSections(t *testing.T) {
	m := newNavigationModel(t)
	index := -1
	for i, source := range m.sources {
		if source.Name() == "百合会" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("Yamibo source is not registered")
	}
	m.sourceCursor = index
	var commands []tea.Cmd
	m.selectSource(&commands)
	if names := func() []string {
		out := make([]string, len(m.searchScopes))
		for i, scope := range m.searchScopes {
			out[i] = scope.Name
		}
		return out
	}(); len(names) != 2 || names[0] != "译文区" || names[1] != "文学区" {
		t.Fatalf("search sections = %v, want 译文区 and 文学区", names)
	}
	if ids := m.activeSearchScopeIDs(); ids != nil {
		t.Fatalf("default search is not unrestricted: %v", ids)
	}
	if got := m.activeSearchScopeName(); got != "全部分区" {
		t.Fatalf("default scope label = %q", got)
	}

	m.section = SecResults
	press := func() {
		m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF}, &commands)
	}
	press()
	if ids := m.activeSearchScopeIDs(); len(ids) != 1 || ids[0] != "55" {
		t.Fatalf("first section = %v, want 译文区", ids)
	}
	press()
	if ids := m.activeSearchScopeIDs(); len(ids) != 1 || ids[0] != "49" {
		t.Fatalf("second section = %v, want 文学区", ids)
	}
	press()
	if ids := m.activeSearchScopeIDs(); ids != nil {
		t.Fatalf("cycling did not return to all sections: %v", ids)
	}
}
