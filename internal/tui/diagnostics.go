package tui

import (
	"fmt"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/diagnostics"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) renderDiagnostics(w int) string {
	lines := []string{TitleStyle.Render("诊断与恢复"), "", "日志 · " + diagnostics.Path(m.service.Config.OutputDir),
		"命令 · yuri-scraper --debug", "查看 · yuri-scraper logs", "", "r 刷新日志 · ↑↓ 滚动 · Esc 返回", ""}
	if m.searchErr != nil {
		lines = append(lines, ErrorStyle.Render("搜索错误"), diagnostics.Guidance(m.searchErr), clean(m.searchErr.Error()), "")
	}
	if m.progress.err != nil {
		lines = append(lines, ErrorStyle.Render("下载错误"), "任务标识 · "+clean(m.progress.id), diagnostics.Guidance(m.progress.err), clean(m.progress.err.Error()), "")
	}
	if m.planErr != nil {
		lines = append(lines, ErrorStyle.Render("规划错误"), clean(m.planErr.Error()), "")
	}
	if m.notice != "" {
		lines = append(lines, "提示 · "+clean(m.notice), "")
	}
	lines = append(lines, fmt.Sprintf("书源 · %s  |  搜索请求 · %d", clean(m.selectedSite), m.requestID),
		"", TitleStyle.Render("最近日志（最多 100 条）"), "不记录搜索词、账号、密码、Cookie 或响应正文。", "")
	for _, line := range strings.Split(m.diagnosticLog, "\n") {
		lines = append(lines, clean(line))
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}
