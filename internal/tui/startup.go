package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const lilyLogo = `             ╭╮
            ╱██╲
     ╭──╮   │██│   ╭──╮
      ╲██╲  │██│  ╱██╱
       ╲██╲ │██│ ╱██╱
   ╭────╲██╲│██│╱██╱────╮
    ╲████╲██╲██╱██╱████╱
     ╰────╲██╲╱██╱────╯
           ╲█││█╱
            ╲││╱
        ╭──╮ ││
         ╲██╲││
          ╰──╯│`

const compactLily = `   ╲█╲│█│╱█╱
    ╲█╲█╱█╱
     ╲█│█╱
       │`

const wordmarkLogo = `██╗   ██╗██╗   ██╗██████╗ ██╗
╚██╗ ██╔╝██║   ██║██╔══██╗██║
 ╚████╔╝ ██║   ██║██████╔╝██║
  ╚██╔╝  ██║   ██║██╔══██╗██║
   ██║   ╚██████╔╝██║  ██║██║
   ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═╝`

const flowerDuration = 650 * time.Millisecond
const startupDuration = 1600 * time.Millisecond

type startupGlyph struct {
	normal, dim, glow string
	column            int
}

var lilyRows = startupRows(lilyLogo)
var compactRows = startupRows(compactLily)
var wordmarkRows = startupRows(wordmarkLogo)

func startupRows(logo string) [][]startupGlyph {
	lines := strings.Split(logo, "\n")
	rows := make([][]startupGlyph, len(lines))
	for y, line := range lines {
		column := 0
		for _, r := range line {
			text := string(r)
			style := LilyFillStyle
			if r != '█' && r != ' ' {
				style = LilyEdgeStyle
			}
			glyph := startupGlyph{normal: style.Render(text), dim: LilyDimStyle.Render(text), glow: LilySweepStyle.Render(text), column: column}
			if r == '█' {
				glyph.dim, glyph.glow = glyph.normal, glyph.normal
			}
			if r == ' ' {
				glyph.normal, glyph.dim, glyph.glow = " ", " ", " "
			}
			rows[y] = append(rows[y], glyph)
			column += lipgloss.Width(text)
		}
	}
	return rows
}

type startupTick time.Time

func tickStartup() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg { return startupTick(t) })
}

func (m *Model) finishStartup() {
	m.section = m.startupNext
	m.startedAt = time.Time{}
}

func (m *Model) renderStartup() string {
	rows, width := lilyRows, lipgloss.Width(lilyLogo)
	elapsed := time.Since(m.startedAt)
	duration := flowerDuration
	if elapsed >= flowerDuration {
		rows, width = wordmarkRows, lipgloss.Width(wordmarkLogo)
		elapsed -= flowerDuration
		duration = startupDuration - flowerDuration
	} else if m.width < 46 || m.height < 22 {
		rows, width = compactRows, lipgloss.Width(compactLily)
	}
	progress := min(1.0, max(0.0, float64(elapsed)/float64(duration)))
	motion := m.service.Config.Startup.Animation == "sweep" && !m.service.Config.Startup.ReducedMotion
	var b strings.Builder
	sweep := ((progress-0.28)/0.62)*float64(width+6) - 3
	for y, row := range rows {
		for _, glyph := range row {
			text := glyph.normal
			x := float64(glyph.column)
			if motion {
				if progress < 0.28 && x/float64(width) > progress/0.28 {
					text = glyph.dim
				}
				if progress >= 0.28 && progress < 0.9 && x > sweep-2 && x < sweep+2 {
					text = glyph.glow
				}
			}
			b.WriteString(text)
		}
		if y < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	flower := fitLines(b.String(), width, len(rows), 0)
	content := flower + "\n\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, BrandStyle.Render("YURI · 百合书房")) +
		"\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, MutedStyle.Render("让故事，在终端绽放"))
	if m.height >= 12 {
		content += "\n\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, HelpStyle.Render("任意键继续 · Ctrl+C 退出"))
	}
	content = fitLines(content, max(width, lipgloss.Width(content)), lipgloss.Height(content), 0)
	return lipgloss.Place(max(1, m.width), max(1, m.height), lipgloss.Center, lipgloss.Center, content)
}
