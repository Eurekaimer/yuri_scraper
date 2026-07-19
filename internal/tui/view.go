package tui

import (
	"fmt"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/buildinfo"
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
		header += MutedStyle.Render(" " + buildinfo.VersionLabel(m.version))
	}

	var content string
	switch m.section {
	case SecSources:
		content = m.renderSourcesContent()
	case SecAuth:
		content = m.renderAuthContent()
	case SecSearch:
		content = m.renderSearchWorkspace(w)
	case SecResults:
		content = m.renderSearchWorkspace(w)
	case SecChapterPlan:
		content = m.renderChapterPlanContent(w)
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
	} else if m.section == SecChapterPlan {
		input = MutedStyle.Render("  下载: " + m.activeBook.Title)
	} else if m.section == SecSearch || m.section == SecResults {
		input = ""
	} else if m.section != SecSearch {
		input = MutedStyle.Render("  搜索: " + m.input.Value())
	}
	body := fmt.Sprintf("%s\n\n%s\n\n%s", header, content, m.renderStatusBar())
	if input != "" {
		body = fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s", header, input, content, m.renderStatusBar())
	}
	return DocStyle.Render(body)
}

func (m *Model) renderChapterPlanContent(w int) string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("YAMIBO PLAN // 解析策略"))
	b.WriteString(SubtitleStyle.Render("  1/2/3 选择  j/k 移动  Tab 切换参数  Enter 继续  Esc 返回\n\n"))
	modes := []struct{ title, detail string }{
		{"自动识别", "优先可信目录，否则扫描首位正文作者"},
		{"目录/电梯", "使用下方手动选择的目录候选"},
		{"作者楼层", "按页扫描指定作者发布的长正文"},
	}
	for i, mode := range modes {
		prefix := "  "
		style := ListItemStyle
		if i == m.modeCursor && m.planFocus == 0 {
			prefix, style = "▸ ", SelectedItemStyle
		}
		b.WriteString(fmt.Sprintf("%s%d  %s  %s\n", prefix, i+1, style.Render(mode.title), MutedStyle.Render(mode.detail)))
	}
	b.WriteString("\n")
	fields := []struct{ label, value string }{
		{"作者名或 UID", m.authorInput.View()},
		{"最大扫描页数", m.maxPagesInput.View()},
		{"正文最少字符", m.minCharsInput.View()},
	}
	for i, field := range fields {
		prefix := "  "
		if m.planFocus == i+1 {
			prefix = "▸ "
		}
		b.WriteString(fmt.Sprintf("%s%-16s %s\n", prefix, field.label, field.value))
	}
	b.WriteString("\n")
	if m.inspectingPlan {
		b.WriteString(MutedStyle.Render("  ◌ 正在检查目录候选…"))
	} else if len(m.candidates) > 0 {
		candidate := m.candidates[m.candidateCursor]
		b.WriteString(TitleStyle.Render(fmt.Sprintf("  目录候选 %d/%d", m.candidateCursor+1, len(m.candidates))))
		b.WriteString(MutedStyle.Render(fmt.Sprintf("  h/l 切换  节点 %s  章节 %d  评分 %d\n", candidate.Source, len(candidate.Chapters), candidate.Score)))
		for _, title := range candidate.Preview {
			b.WriteString("    · " + truncate(title, max(10, w-8)) + "\n")
		}
	} else {
		b.WriteString(MutedStyle.Render("  未发现目录候选；自动模式将回退到作者楼层。"))
	}
	if m.planErr != nil {
		b.WriteString("\n" + ErrorStyle.Render("  ⚠ "+m.planErr.Error()))
	}
	return b.String()
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

func (m *Model) renderSearchWorkspace(w int) string {
	leftWidth := min(36, max(26, w/3))
	rightWidth := max(30, w-leftWidth-3)
	leftStyle, rightStyle := CardStyle, CardStyle
	leftMark, rightMark := "  ", "  "
	if m.section == SecSearch {
		leftStyle, leftMark = ActiveCardStyle, "▸ "
	} else {
		rightStyle, rightMark = ActiveCardStyle, "▸ "
	}

	var left strings.Builder
	left.WriteString(TitleStyle.Render(leftMark + "搜索"))
	left.WriteString("\n\n" + m.input.View() + "\n\n")
	switch {
	case m.searching:
		left.WriteString(MutedStyle.Render("◌ 正在搜索…"))
	case m.searchErr != nil:
		left.WriteString(ErrorStyle.Render("⚠ " + m.searchErr.Error()))
	case len(m.results) > 0:
		left.WriteString(MutedStyle.Render(fmt.Sprintf("已找到 %d 部作品", len(m.results))))
	default:
		left.WriteString(MutedStyle.Render("输入书名或作者后按 Enter"))
	}
	left.WriteString("\n\n" + HelpStyle.Render("Tab / → / l 进入作品栏\n/ 随时回到搜索栏"))

	var right strings.Builder
	right.WriteString(TitleStyle.Render(fmt.Sprintf("%s作品  %d", rightMark, len(m.results))))
	right.WriteString("\n")
	if len(m.results) == 0 {
		right.WriteString("\n" + MutedStyle.Render("等待搜索结果"))
	} else {
		right.WriteString(m.renderResultsList(rightWidth - 4))
	}

	leftPanel := leftStyle.Width(max(20, leftWidth-4)).Render(left.String())
	rightPanel := rightStyle.Width(max(24, rightWidth-4)).Render(right.String())
	if w < 78 {
		fullWidth := max(30, w-4)
		leftPanel = leftStyle.Width(fullWidth).Render(left.String())
		rightPanel = rightStyle.Width(fullWidth).Render(right.String())
		return lipgloss.JoinVertical(lipgloss.Left, leftPanel, rightPanel)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, " ", rightPanel)
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

	b.WriteString(m.renderResultsList(w))
	return b.String()
}

func (m *Model) renderResultsList(w int) string {
	var b strings.Builder
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
	if m.progress.strategy != "" {
		b.WriteString(MutedStyle.Render("  策略: " + string(m.progress.strategy)))
	}
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
			key("Enter") + " 搜索/下载  " + key("Tab/←/→/Alt+h/l") + " 分栏切换  " + key("/") + " 搜索栏  " +
				key("Ctrl+S") + " 来源  " + key("Ctrl+L") + " 书库  " + key("Ctrl+C") + " 退出")
	case SecChapterPlan:
		return StatusBarStyle.Render(key("1/2/3") + " 模式  " + key("Tab") + " 参数  " + key("h/l") + " 候选  " + key("Enter") + " 继续  " + key("Esc") + " 返回")
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
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String()+string(r)+"…") > maxWidth {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
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
