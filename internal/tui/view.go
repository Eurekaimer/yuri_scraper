package tui

import (
	"fmt"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) View() string {
	if !m.ready {
		return "loading..."
	}

	w := max(m.width-4, 40)

	header := BrandStyle.Render(" YuriScraper")
	if m.version != "dev" {
		header += MutedStyle.Render(" v" + m.version)
	}

	var content string
	switch m.section {
	case SecSources:
		content = m.renderSourcesContent()
	case SecAuth:
		content = m.renderAuthContent()
	case SecSearch:
		content = m.renderSearchContent(w)
	case SecResults:
		content = m.renderResultsContent(w)
	case SecProgress:
		content = m.renderProgressContent(w)
	case SecLibrary:
		content = m.renderLibraryContent(w)
	}

	input := m.input.View()
	if m.section == SecSources {
		input = MutedStyle.Render("  先选择内容来源")
	} else if m.section == SecAuth {
		input = m.authInput.View()
	} else if m.section != SecSearch {
		input = MutedStyle.Render("  搜索: " + m.input.Value())
	}
	body := fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s",
		header, input, content, m.renderStatusBar())
	return DocStyle.Render(body)
}

func (m *Model) renderSourcesContent() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("SOURCE SELECT // 内容来源"))
	b.WriteString(SubtitleStyle.Render("  j/k 移动  Enter 选择  1/2 快速选择\n\n"))
	for i, source := range m.sources {
		prefix, style := "  ", ListItemStyle
		if i == m.sourceCursor {
			prefix, style = "▸ ", SelectedItemStyle
		}
		access := "PUBLIC"
		if auth, ok := source.(search.CookieAuthenticator); ok && auth.RequiresCookie() {
			access = "COOKIE"
		}
		b.WriteString(fmt.Sprintf("%s%02d  %s  %s\n", prefix, i+1, style.Render(source.Name()), MutedStyle.Render(access)))
	}
	return b.String()
}

func (m *Model) renderAuthContent() string {
	return TitleStyle.Render("AUTH // 百合会会话") + "\n\n" +
		MutedStyle.Render("  请从已登录的浏览器复制 bbs.yamibo.com Cookie。Cookie 不会写入配置或历史文件。") + "\n" +
		SubtitleStyle.Render("  Enter 确认  Esc 返回来源选择")
}

func (m *Model) renderSearchContent(w int) string {
	switch {
	case m.searching:
		return MutedStyle.Render("  ⏳ 正在搜索…")
	case m.searchErr != nil:
		return ErrorStyle.Render(fmt.Sprintf("  ⚠ %v", m.searchErr))
	case len(m.results) == 0 && m.input.Value() != "":
		return MutedStyle.Render("  无结果，试试缩短关键词")
	case len(m.results) == 0:
		return MutedStyle.Render("  输入关键词搜索小说…")
	default:
		return m.renderResultsContent(w)
	}
}

func (m *Model) renderResultsContent(w int) string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render(fmt.Sprintf("▸ %d 个结果", len(m.results))))
	b.WriteString(SubtitleStyle.Render("  j/k移动  g/G首尾  Ctrl+D/U翻页  Enter下载  Tab或/切回输入\n"))

	start, end := visibleWindow(m.cursor, len(m.results), 12)
	for i := start; i < end; i++ {
		r := m.results[i]
		prefix, style := "  ", ListItemStyle
		if i == m.cursor {
			prefix, style = "▸ ", SelectedItemStyle
		}
		line := prefix + style.Render(r.Title)
		if r.Author != "" {
			line += MutedStyle.Render(" — " + r.Author)
		}
		if r.Status != "" {
			line += MutedStyle.Render(" [" + r.Status + "]")
		}
		if len(r.Tags) > 0 {
			line += MutedStyle.Render(" " + r.Tags[0])
		}
		b.WriteString(truncate(line, w))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) renderProgressContent(w int) string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("▸ 抓取进度"))
	b.WriteString(MutedStyle.Render(speedIndicator(m.progress.speed)))
	b.WriteString("\n\n")

	if m.progress.done {
		if m.progress.err != nil {
			b.WriteString(ErrorStyle.Render(fmt.Sprintf("  ✗ %v", m.progress.err)))
		} else if m.progress.failed > 0 {
			b.WriteString(ErrorStyle.Render("  ⚠ 部分完成，失败章节已写入清单"))
			b.WriteString(fmt.Sprintf("\n  文件: %s\n", m.progress.outputFile))
			b.WriteString(fmt.Sprintf("  共 %d 章 | 成功 %d | 失败 %d\n",
				m.progress.total, m.progress.completed, m.progress.failed))
		} else {
			b.WriteString(SuccessStyle.Render("  ✓ 抓取完成"))
			b.WriteString(fmt.Sprintf("\n  文件: %s\n", m.progress.outputFile))
			b.WriteString(fmt.Sprintf("  共 %d 章 | 成功 %d | 失败 %d\n",
				m.progress.total, m.progress.completed, m.progress.failed))
		}
	} else if m.progress.total > 0 {
		ratio := float64(m.progress.completed+m.progress.failed) / float64(m.progress.total)
		b.WriteString(renderBar(ratio, w-4))
		b.WriteString(fmt.Sprintf("\n  %d/%d 章  (%d 失败)", m.progress.completed, m.progress.total, m.progress.failed))
		if m.progress.current != "" {
			t := m.progress.current
			if r := []rune(t); len(r) > 40 {
				t = string(r[:40]) + "…"
			}
			b.WriteString("\n  " + MutedStyle.Render("当前: "+t))
		}
	} else {
		b.WriteString(ProgressStyle.Render("  ⏳ 获取章节列表中…"))
	}

	if len(m.progress.failedChs) > 0 {
		b.WriteString(fmt.Sprintf("\n\n  %s 完成后按 R 重试 %d 个失败章节",
			ErrorStyle.Render("⚠"), len(m.progress.failedChs)))
	}
	return b.String()
}

func (m *Model) renderLibraryContent(w int) string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("▸ 已下载书库"))
	b.WriteString(SubtitleStyle.Render("  j/k 移动  Esc 返回\n"))

	if len(m.entries) == 0 {
		return b.String() + MutedStyle.Render("  还没有下载过任何小说")
	}

	start, end := visibleWindow(m.libCursor, len(m.entries), 12)
	for i := start; i < end; i++ {
		e := m.entries[i]
		prefix, style := "  ", ListItemStyle
		if i == m.libCursor {
			prefix, style = "▸ ", SelectedItemStyle
		}
		line := fmt.Sprintf("%s%s", prefix, style.Render(e.Title))
		if e.Author != "" {
			line += MutedStyle.Render(" — " + e.Author)
		}
		line += MutedStyle.Render(fmt.Sprintf("  [%s] [%d章] %s",
			e.Format, e.Chapters, e.Downloaded.Format("01-02")))
		b.WriteString(truncate(line, w))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) renderStatusBar() string {
	switch m.section {
	case SecSources:
		return StatusBarStyle.Render(key("1/2") + " 来源  " + key("Enter") + " 选择  " + key("Ctrl+C") + " 退出")
	case SecAuth:
		return StatusBarStyle.Render(key("Enter") + " 使用 Cookie  " + key("Esc") + " 返回  " + key("Ctrl+C") + " 退出")
	case SecSearch, SecResults:
		return StatusBarStyle.Render(
			key("Enter") + " 搜索/下载  " + key("Tab") + " 输入⇄结果  " + key("/") + " 搜索栏  " +
				key("Ctrl+S") + " 来源  " + key("Ctrl+L") + " 书库  " + key("Ctrl+C") + " 退出")
	case SecProgress:
		if m.progress.done {
			return StatusBarStyle.Render(
				key("Enter/Esc") + " 返回结果  " + key("R") + " 重试失败  " + key("Ctrl+C") + " 退出")
		}
		return StatusBarStyle.Render(
			key("1/2/3") + " 速度档  " + key("Ctrl+C") + " 退出")
	case SecLibrary:
		return StatusBarStyle.Render(key("Esc") + " 返回  " + key("Ctrl+C") + " 退出")
	}
	return ""
}

// --- helpers ---

func speedIndicator(sp scraper.SpeedProfile) string {
	sel := func(p scraper.SpeedProfile) string {
		if sp == p {
			return "▶"
		}
		return " "
	}
	return fmt.Sprintf("  速度: [%s慢速%s | %s正常%s | %s快速%s]  按1/2/3切换",
		sel(scraper.SpeedSlow), selIf(sp == scraper.SpeedSlow, "◀", ""),
		sel(scraper.SpeedNormal), selIf(sp == scraper.SpeedNormal, "◀", ""),
		sel(scraper.SpeedFast), selIf(sp == scraper.SpeedFast, "◀", ""),
	)
}

func visibleWindow(cursor, total, visible int) (start, end int) {
	start = cursor - visible/2
	if start < 0 {
		start = 0
	}
	end = start + visible
	if end > total {
		end = total
		start = max(0, end-visible)
	}
	return
}

func renderBar(ratio float64, width int) string {
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	return ProgressStyle.Render(strings.Repeat("█", filled) + strings.Repeat("░", width-filled))
}

func truncate(s string, maxWidth int) string {
	r := []rune(s)
	if len(r) <= maxWidth {
		return s
	}
	return string(r[:maxWidth-1]) + "…"
}

func key(label string) string {
	return lipgloss.NewStyle().Foreground(Accent).Render(label)
}

func selIf(cond bool, t, f string) string {
	if cond {
		return t
	}
	return f
}
