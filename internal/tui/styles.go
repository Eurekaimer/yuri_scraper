package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Palette — warm, muted tones
	Primary   = lipgloss.Color("#C4A35A") // warm gold
	Secondary = lipgloss.Color("#7C9A92") // sage green
	Accent    = lipgloss.Color("#D4A373") // warm amber
	Success   = lipgloss.Color("#8B9A6E") // olive green
	Warning   = lipgloss.Color("#D4A373") // amber
	Error     = lipgloss.Color("#C47C6C") // muted red
	Muted     = lipgloss.Color("#8B8378") // warm gray
	Subtle    = lipgloss.Color("#5C5650") // darker warm gray
	Bg        = lipgloss.Color("#1C1B1A") // near-black warm
	Fg        = lipgloss.Color("#D4CFC4") // warm off-white
	Border    = lipgloss.Color("#3D3935") // subtle border

	// Header
	BrandStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	TitleStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true).
			Padding(0, 1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Italic(true)

	// Content
	HighlightStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Error).
			Bold(true)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Success).
			Bold(true)

	ProgressStyle = lipgloss.NewStyle().
			Foreground(Secondary)

	HelpStyle = lipgloss.NewStyle().
			Foreground(Subtle)

	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	// Layout
	DocStyle = lipgloss.NewStyle().
			Padding(1, 2)

	// Cards / Panels
	CardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			Padding(1, 2)

	ActiveCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(1, 2)

	// List items
	ListItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	SelectedItemStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Foreground(Primary).
				Background(lipgloss.Color("#2D2B28")).
				Bold(true)

	// Input
	InputStyle = lipgloss.NewStyle().
			Foreground(Fg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			Padding(0, 1)

	FocusedInputStyle = lipgloss.NewStyle().
				Foreground(Fg).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(Primary).
				Padding(0, 1)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(Subtle).
			Padding(0, 1)
)
