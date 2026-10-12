package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Lily palette: ivory fill, pale yellow contours, terminal-native background.
	Primary   = lipgloss.Color("#EAD99A")
	Secondary = lipgloss.Color("#B4C7A3")
	Accent    = lipgloss.Color("#FFF0BB")
	Error     = lipgloss.Color("#C47C6C") // muted red
	Muted     = lipgloss.Color("#8B8378") // warm gray
	Subtle    = lipgloss.Color("#5C5650") // darker warm gray
	Fg        = lipgloss.Color("#F5F3E8")

	// Header
	BrandStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	TitleStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	// Content
	HighlightStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Error).
			Bold(true)

	ProgressStyle = lipgloss.NewStyle().
			Foreground(Secondary)

	HelpStyle = lipgloss.NewStyle().
			Foreground(Muted)

	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	// Layout
	DocStyle = lipgloss.NewStyle().
			Padding(1, 1)

	// List items
	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(Primary).
				Background(lipgloss.Color("#33322B")).
				Bold(true)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(Secondary)

	LilyFillStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	LilyEdgeStyle  = lipgloss.NewStyle().Foreground(Primary)
	LilyDimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#47453E"))
	LilySweepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF9C4"))
)
