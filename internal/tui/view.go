package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Eurekaimer/yuri_scraper/internal/buildinfo"
	"github.com/Eurekaimer/yuri_scraper/internal/library"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func (m *Model) contentWidth() int  { return max(1, m.width-6) }
func (m *Model) contentHeight() int { return max(1, m.height-8) }

func (m *Model) View() string {
	if !m.ready {
		return "正在初始化终端…"
	}
	if m.width < 40 || m.height < 10 {
		return fitLines("窗口过小，请放大至 40 × 10\nCtrl+C 退出", max(1, m.width), max(1, m.height), 0)
	}
	if m.section == SecStartup {
		return m.renderStartup()
	}
	w, h := m.contentWidth(), m.contentHeight()
	header := BrandStyle.Render("⚜ YURI") + MutedStyle.Render(" / ") + TitleStyle.Render("百合书房")
	meta := clean(m.selectedSite) + " · " + buildinfo.VersionLabel(m.version)
	header = edgeLine(header, MutedStyle.Render(meta), m.width-2)
	var content string
	switch m.section {
	case SecSources:
		content = m.renderSourcesContent()
	case SecAuth:
		content = m.renderAuthContent()
	case SecSearch, SecResults:
		content = m.renderSearchWorkspace(w)
	case SecChapterPlan:
		content = m.renderChapterPlanContent(w)
	case SecProgress:
		content = m.renderProgressContent(w)
	case SecLibrary:
		content = m.renderLibraryContent(w)
	}
	offset := m.viewOffset
	if m.diagnostic {
		content = m.renderDiagnostics(w)
	} else if m.help {
		content = m.renderHelp(w)
	} else if m.confirmDelete {
		content = TitleStyle.Render("移入回收站？") + "\n" + truncate(clean(m.deleteEntry.Title), w) + "\n\n文件将移入 .trash，可手动恢复。\ny 确认 · 其他键取消"
		offset = 0
	} else if m.section == SecAuth && h < 6 {
		offset = max(0, 2+m.auth.focus-h+1)
	} else if m.section == SecSources {
		offset = max(0, m.sourceCursor+2-h+1)
	} else if m.section == SecChapterPlan && m.planFocus > 0 {
		offset = max(offset, 4+m.planFocus-h+1)
	}
	frame := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Primary).
		Padding(0, 1).Render(fitLines(content, w, h, offset))
	status := "就绪 · / 搜索书名或作者"
	if m.searching {
		status = "正在搜索 · x 取消 · 可继续浏览"
	}
	if m.searchErr != nil {
		status = "搜索失败；保留上次结果 · r 重试 · ! 诊断"
	}
	if m.progress.state != "" {
		status = fmt.Sprintf("%s · %s · %d/%d 章 · 失败 %d", stateLabel(m.progress.state), clean(m.activeBook.Title), m.progress.completed, m.progress.total, m.progress.failed)
	}
	if m.notice != "" {
		status = clean(m.notice)
	}
	body := header + "\n" + fitLines(m.renderNavigation(), m.width-2, 1, 0) + "\n" + frame + "\n" +
		MutedStyle.Render(fitLines(status, m.width-2, 1, 0)) + "\n" + fitLines(m.renderStatusBar(), m.width-2, 1, 0)
	return DocStyle.Render(body)
}

func (m *Model) renderNavigation() string {
	items := []struct {
		title   string
		section Section
	}{{"搜书 /", SecResults}, {"书源 s", SecSources}, {"书库 b", SecLibrary}, {"任务 t", SecProgress}}
	current := m.section
	if current == SecSearch {
		current = m.inputReturn
	}
	if current == SecAuth {
		current = SecSources
	}
	if current == SecChapterPlan {
		current = SecResults
	}
	var tabs []string
	for _, item := range items {
		if current == item.section {
			tabs = append(tabs, HighlightStyle.Render("● "+item.title))
		} else {
			tabs = append(tabs, MutedStyle.Render("○ "+item.title))
		}
	}
	return strings.Join(tabs, "  ")
}

func (m *Model) renderSearchWorkspace(w int) string {
	if m.contentHeight() <= 4 && m.section != SecSearch {
		return m.resultPanels(w)
	}
	prompt := MutedStyle.Render("/ 输入书名或作者")
	if m.query != "" {
		prompt = MutedStyle.Render("搜索 · " + truncate(clean(m.query), max(1, w-7)))
	}
	if m.section == SecSearch {
		label := "搜索 "
		if m.filtering {
			label = "筛选 "
		}
		prompt = HighlightStyle.Render(label) + m.input.View()
	}
	if m.searching {
		prompt += ProgressStyle.Render(" · 搜索中…")
	}
	if m.searchErr != nil && m.section != SecSearch {
		prompt = ErrorStyle.Render(truncate("搜索失败 · r 重试 · ! 诊断（保留上次结果）", w))
	}
	if m.section == SecSearch && m.inputReturn == SecLibrary && m.filtering {
		return fitLines(prompt, w, 1, 0) + "\n\n" + m.renderLibraryList(w)
	}
	return fitLines(prompt, w, 1, 0) + "\n\n" + m.resultPanels(w)
}

func (m *Model) resultPanels(w int) string {
	if len(m.results) == 0 {
		if m.query != "" && !m.searching {
			return MutedStyle.Render("没有匹配结果\n试试更短的关键词，或按 s 切换书源。")
		}
		return TitleStyle.Render("在这里，遇见下一段故事") + "\n\n" + MutedStyle.Render("○ 按 / 搜索书名或作者\n○ 按 s 选择书源\n○ 按 b 打开本地书库\n\n? 查看全部快捷键")
	}
	if m.details {
		return m.bookPreview(m.results[m.cursor], w)
	}
	if w < 94 || !m.preview {
		return m.renderResultsList(w)
	}
	left := (w - 3) * 3 / 5
	right := w - left - 3
	h := max(1, m.contentHeight()-2)
	return lipgloss.JoinHorizontal(lipgloss.Top, fitLines(m.renderResultsList(left), left, h, 0),
		MutedStyle.Render(strings.TrimSuffix(strings.Repeat(" │ \n", h), "\n")), fitLines(m.bookPreview(m.results[m.cursor], right), right, h, 0))
}

func (m *Model) bookPreview(book search.SearchResult, w int) string {
	lines := []string{TitleStyle.Render(truncate(clean(book.Title), w)), MutedStyle.Render("作者 · " + clean(book.Author)), "", "书源 · " + clean(book.SiteName)}
	if book.Status != "" {
		lines = append(lines, "状态 · "+clean(book.Status))
	}
	if len(book.Tags) > 0 {
		lines = append(lines, truncate(clean(strings.Join(book.Tags, " · ")), w))
	}
	if book.ChapterCount > 0 {
		lines = append(lines, fmt.Sprintf("章节 · %d 章", book.ChapterCount))
	}
	lines = append(lines, "", HelpStyle.Render("Enter 下载 · p 章节规划"))
	if book.Description != "" {
		lines = append(lines, "", lipgloss.NewStyle().Width(w).Render(clean(book.Description)))
	}
	return strings.Join(lines, "\n")
}

func listRow(text string, selected bool, w int) string {
	prefix := "○ "
	style := lipgloss.NewStyle()
	if selected {
		prefix, style = "● ", SelectedItemStyle
	}
	line := truncate(prefix+text, w)
	return style.Render(line + strings.Repeat(" ", max(0, w-lipgloss.Width(line))))
}

func (m *Model) renderResultsList(w int) string {
	var b strings.Builder
	start, end := visibleWindow(m.cursor, len(m.results), m.visibleRows())
	b.WriteString(MutedStyle.Render(fmt.Sprintf("搜索结果 · %d/%d · %s", m.cursor+1, len(m.results), clean(m.activeSearchScopeName()))) + "\n")
	for i := start; i < end; i++ {
		r := m.results[i]
		text := fmt.Sprintf("%02d  %s", i+1, clean(r.Title))
		if w >= 50 {
			authorWidth := min(18, w/4)
			text = edgeLine(truncate(text, w-authorWidth-4), truncate(clean(r.Author), authorWidth), w-2)
		}
		b.WriteString(listRow(text, i == m.cursor, w))
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *Model) renderSourcesContent() string {
	w := m.contentWidth()
	var b strings.Builder
	b.WriteString(TitleStyle.Render("选择书源") + "\n\n")
	for i, source := range m.sources {
		label := fmt.Sprintf("%d  %s", i+1, clean(source.Name()))
		if source.Name() == m.selectedSite {
			label += " · 当前"
		}
		b.WriteString(listRow(label, i == m.sourceCursor, w) + "\n")
	}
	b.WriteString("\n" + MutedStyle.Render("公开书源可直接搜索；百合会需登录。\n账号与密码仅用于当前会话。"))
	return b.String()
}

func (m *Model) renderAuthContent() string {
	status := "凭据仅用于本次会话，不会写入磁盘。"
	if m.auth.busy {
		status = "正在登录…"
	} else if m.auth.err != nil {
		status = clean(m.auth.err.Error())
	}
	return TitleStyle.Render("书源登录") + "\n\n账号  " + m.auth.username.View() + "\n密码  " + m.auth.password.View() + "\n\n" + MutedStyle.Render(status)
}

func (m *Model) renderChapterPlanContent(w int) string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("章节规划") + "\n")
	for i, mode := range []string{"自动识别目录或作者正文", "目录模式 · 选择候选目录", "作者模式 · 限页扫描正文"} {
		b.WriteString(listRow(mode, i == m.modeCursor && m.planFocus == 0, w) + "\n")
	}
	b.WriteString("\n作者 / UID  " + m.authorInput.View() + "\n扫描页数    " + m.maxPagesInput.View() + "\n最少字数    " + m.minCharsInput.View() + "\n\n")
	if m.inspectingPlan {
		b.WriteString("正在识别目录…")
	} else if len(m.candidates) > 0 {
		c := m.candidates[m.candidateCursor]
		b.WriteString(fmt.Sprintf("候选 %d/%d · %d 章 · 评分 %d\n", m.candidateCursor+1, len(m.candidates), len(c.Chapters), c.Score))
		for _, title := range c.Preview {
			b.WriteString("  " + truncate(clean(title), w-2) + "\n")
		}
	} else {
		b.WriteString("未找到目录；自动模式可尝试作者正文。")
	}
	b.WriteString("\n" + MutedStyle.Render("扫描受页数上限约束，不保证收录全帖。"))
	if m.planErr != nil {
		b.WriteString("\n" + ErrorStyle.Render(clean(m.planErr.Error())))
	}
	return b.String()
}

func (m *Model) renderProgressContent(w int) string {
	if m.progress.state == "" {
		return TitleStyle.Render("下载任务") + "\n\n" + MutedStyle.Render("暂无任务\n搜索后按 Enter 下载，完成后按 b 查看书库。")
	}
	var b strings.Builder
	b.WriteString(TitleStyle.Render(truncate(clean(m.activeBook.Title), w)) + "\n\n")
	b.WriteString(HighlightStyle.Render(stateLabel(m.progress.state)) + "\n")
	if m.progress.total > 0 {
		ratio := float64(m.progress.completed+m.progress.failed) / float64(m.progress.total)
		b.WriteString(renderBar(ratio, max(1, w-7)) + fmt.Sprintf(" %3.0f%%\n", ratio*100))
	}
	b.WriteString(fmt.Sprintf("\n章节 %d/%d · 失败 %d · 缓存 %d\n", m.progress.completed, m.progress.total, m.progress.failed, m.progress.cached))
	if m.progress.workers > 0 {
		b.WriteString(MutedStyle.Render(fmt.Sprintf("%s · %d 并发 · 每秒 %g 请求\n", speedLabel(m.progress.speed), m.progress.workers, m.progress.requestsPerSecond)))
	}
	if m.progress.current != "" {
		b.WriteString("\n" + truncate(clean(m.progress.current), w))
	}
	if m.progress.outputFile != "" {
		b.WriteString("\n\n" + truncate(clean(m.progress.outputFile), w))
	}
	if m.progress.err != nil {
		b.WriteString("\n\n" + ErrorStyle.Render(clean(m.progress.err.Error())))
	}
	if m.active() {
		b.WriteString("\n\n" + HelpStyle.Render("Esc 返回浏览，下载继续 · x 取消任务"))
	}
	return b.String()
}

func (m *Model) renderLibraryList(w int) string {
	if len(m.entries) == 0 {
		return MutedStyle.Render("书库暂无匹配文件\n下载书籍，或在下载目录放入 EPUB / PDF / TXT。")
	}
	var b strings.Builder
	start, end := visibleWindow(m.libCursor, len(m.entries), m.visibleRows())
	b.WriteString(MutedStyle.Render(fmt.Sprintf("本地书籍 · %d/%d", m.libCursor+1, len(m.entries))) + "\n")
	for i := start; i < end; i++ {
		e := m.entries[i]
		state := stateLabel(e.State)
		if e.Missing {
			state = "文件缺失"
		}
		meta := clean(e.Format) + " · " + state
		line := edgeLine(truncate(clean(e.Title), max(1, w-lipgloss.Width(meta)-4)), meta, w-2)
		b.WriteString(listRow(line, i == m.libCursor, w))
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func libraryPreview(entry library.Entry, w int) string {
	return TitleStyle.Render(truncate(clean(entry.Title), w)) + "\n\n作者 · " + clean(entry.Author) + "\n" + clean(entry.Format) + " · " + stateLabel(entry.State) + "\n\n" + HelpStyle.Render("Enter 打开 · v 定位 · d 回收") + "\n\n" + lipgloss.NewStyle().Width(w).Render(clean(entry.FilePath))
}

func (m *Model) renderLibraryContent(w int) string {
	if m.details && m.libCursor < len(m.entries) {
		return libraryPreview(m.entries[m.libCursor], w)
	}
	body := m.renderLibraryList(w)
	if m.contentHeight() <= 4 {
		return body
	}
	if m.preview && w >= 94 && m.libCursor < len(m.entries) {
		left, h := (w-3)*3/5, max(1, m.contentHeight()-2)
		body = lipgloss.JoinHorizontal(lipgloss.Top, fitLines(m.renderLibraryList(left), left, h, 0), MutedStyle.Render(strings.TrimSuffix(strings.Repeat(" │ \n", h), "\n")), fitLines(libraryPreview(m.entries[m.libCursor], w-left-3), w-left-3, h, 0))
	}
	return TitleStyle.Render("本地书库") + "\n\n" + body
}

func (m *Model) renderStatusBar() string {
	if m.diagnostic {
		return StatusBarStyle.Render("↑↓ 滚动 · r 刷新 · Esc 返回")
	}
	if m.help {
		return StatusBarStyle.Render("↑↓ 滚动 · PgUp/PgDn 翻页 · Esc 关闭")
	}
	if m.section == SecResults && m.searchErr != nil {
		return StatusBarStyle.Render("r 重试 · / 修改关键词 · ! 诊断")
	}
	if m.section == SecProgress && m.progress.err != nil {
		return StatusBarStyle.Render("r 重试 · ! 诊断 · Esc 返回")
	}
	if m.confirmDelete {
		return StatusBarStyle.Render("y 确认回收 · 其他键取消")
	}
	if m.width < 72 {
		switch m.section {
		case SecSearch:
			return StatusBarStyle.Render("Enter 提交 · Esc 返回")
		case SecAuth:
			return StatusBarStyle.Render("Tab 切换 · Enter 登录 · Esc 返回")
		case SecChapterPlan:
			return StatusBarStyle.Render("Tab 字段 · Enter 开始 · PgDn 下翻")
		case SecProgress:
			if m.active() {
				return StatusBarStyle.Render("a/d 调速 · x 取消 · Esc 返回")
			}
			if m.canRetryTask() {
				return StatusBarStyle.Render("r 重试 · Esc 返回 · ! 诊断")
			}
			return StatusBarStyle.Render("Esc 返回 · b 书库 · ! 诊断")
		case SecLibrary:
			return StatusBarStyle.Render("↑↓ 选择 · Enter 打开 · ? 帮助")
		case SecSources:
			return StatusBarStyle.Render("↑↓ 选择 · Enter 确认 · Esc 返回")
		}
		return StatusBarStyle.Render("↑↓ 选择 · Enter 下载 · ? 帮助")
	}
	switch m.section {
	case SecSearch:
		return StatusBarStyle.Render("输入模式 · Enter 提交 · Esc 保留草稿并返回")
	case SecAuth:
		return StatusBarStyle.Render("Tab 切换字段 · Enter 登录 · Esc 返回")
	case SecChapterPlan:
		return StatusBarStyle.Render("j/k 模式 · h/l 候选 · Tab 字段 · Enter 开始 · PgUp/PgDn 翻页")
	case SecProgress:
		if m.active() {
			return StatusBarStyle.Render("a/d 调速 · 1/2/3 档位 · x 取消 · Esc 浏览 · b 书库")
		}
		if m.canRetryTask() {
			return StatusBarStyle.Render("r 重试 · Esc 浏览 · b 书库 · ! 诊断")
		}
		return StatusBarStyle.Render("Esc 浏览 · b 书库 · ! 诊断 · ? 帮助")
	case SecLibrary:
		return StatusBarStyle.Render("j/k 选择 · Enter 打开 · / 筛选 · v 定位 · d 回收 · ? 帮助")
	case SecSources:
		return StatusBarStyle.Render("j/k 选择 · Enter 确认 · Esc 返回 · ? 帮助")
	}
	return StatusBarStyle.Render("j/k 选择 · Enter 下载 · / 搜索 · f 筛选 · ? 帮助 · ! 诊断")
}

func (m *Model) renderHelp(w int) string {
	return TitleStyle.Render("键盘操作") + "\n\n" +
		"j/k 或 ↑↓      选择条目\ngg / G         首项 / 末项\nCtrl+U/D       上 / 下半页\n/ 或 i         搜索（输入模式）\nf              筛选已有结果\nCtrl+F         切换搜索分区\nl / Esc        详情 / 返回\nTab            开关宽屏预览\ns / b / t      书源 / 书库 / 任务\np              论坛章节规划\nEnter          下载 / 打开\nv / d          定位 / 移入回收站\nr              重试搜索或失败任务\n!              错误诊断与最近日志\na / d          任务页减速 / 加速\n1 / 2 / 3      慢速 / 标准 / 快速\nx              取消搜索或下载\nq / Ctrl+C     退出并取消任务\nPgUp/PgDn      长页面翻页\n\n输入模式下 j/k/h/l/q 均为普通文字。\n浏览其他页面时，下载仍会继续。\n实际并发与速率受配置和书源上限约束。\n\nEsc 或 ? 关闭帮助。"
}

func stateLabel(state string) string {
	switch state {
	case "planning":
		return "正在规划"
	case "downloading":
		return "正在下载"
	case "exporting":
		return "正在导出"
	case "completed":
		return "已完成"
	case "partial":
		return "部分完成"
	case "failed":
		return "失败"
	case "canceled":
		return "已取消"
	case "imported":
		return "本地导入"
	case "missing":
		return "文件缺失"
	case "":
		return "就绪"
	default:
		return clean(state)
	}
}
func speedLabel(speed string) string {
	switch strings.ToLower(speed) {
	case "slow":
		return "慢速"
	case "fast":
		return "快速"
	case "", "normal":
		return "标准"
	default:
		return clean(speed)
	}
}
func (m *Model) activeSearchScopeName() string {
	if m.scopeCursor <= 0 || m.scopeCursor > len(m.searchScopes) {
		return "全部分区"
	}
	return m.searchScopes[m.scopeCursor-1].Name
}
func visibleWindow(cursor, total, visible int) (int, int) {
	visible = max(1, visible)
	start := max(0, cursor-visible/2)
	end := min(total, start+visible)
	return max(0, end-visible), end
}
func renderBar(ratio float64, width int) string {
	width = max(0, width)
	ratio = min(1.0, max(0.0, ratio))
	filled := int(ratio * float64(width))
	return ProgressStyle.Render(strings.Repeat("━", filled)) + MutedStyle.Render(strings.Repeat("─", width-filled))
}
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// All measurements are terminal cells, not bytes or rune counts. Reserve the
// frame and footer before cropping so resizing never pushes controls offscreen.
func fitLines(text string, w, h, offset int) string {
	lines := strings.Split(text, "\n")
	offset = min(max(0, offset), max(0, len(lines)-h))
	var b strings.Builder
	for row := range h {
		line := ""
		if row+offset < len(lines) {
			line = truncate(lines[row+offset], w)
		}
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", max(0, w-lipgloss.Width(line))))
		if row+1 < h {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func edgeLine(left, right string, w int) string {
	left = truncate(left, w)
	remaining := w - lipgloss.Width(left)
	if remaining < lipgloss.Width(right)+2 {
		return left
	}
	return left + strings.Repeat(" ", remaining-lipgloss.Width(right)) + right
}
