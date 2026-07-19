package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type authenticationMsg struct{ err error }

type authForm struct {
	username textinput.Model
	password textinput.Model
	focus    int
	busy     bool
	err      error
}

func newAuthForm() authForm {
	username := textinput.New()
	username.Placeholder = "百合会账号"
	username.CharLimit = 100
	username.Width = 42

	password := textinput.New()
	password.Placeholder = "百合会密码（输入不回显）"
	password.CharLimit = 256
	password.Width = 42
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '•'
	return authForm{username: username, password: password}
}

func (f *authForm) focusCmd() tea.Cmd {
	if f.focus == 0 {
		f.password.Blur()
		return f.username.Focus()
	}
	f.username.Blur()
	return f.password.Focus()
}

func (f *authForm) blur() {
	f.username.Blur()
	f.password.Blur()
}

func (f *authForm) setWidth(width int) {
	width = min(max(width, 20), 60)
	f.username.Width = width
	f.password.Width = width
}

func (f *authForm) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if f.focus == 0 {
		f.username, cmd = f.username.Update(msg)
	} else {
		f.password, cmd = f.password.Update(msg)
	}
	return cmd
}

func (f *authForm) toggleFocus() tea.Cmd {
	f.focus = (f.focus + 1) % 2
	return f.focusCmd()
}

func (f *authForm) credentials() (string, string, error) {
	username := strings.TrimSpace(f.username.Value())
	password := f.password.Value()
	if username == "" || password == "" {
		return "", "", errors.New("请输入完整的百合会账号和密码")
	}
	return username, password, nil
}

func (f *authForm) clearPassword() { f.password.SetValue("") }

func authenticate(ctx context.Context, auth search.CredentialsAuthenticator, username, password string) tea.Cmd {
	return func() tea.Msg {
		err := auth.Login(ctx, username, password)
		password = ""
		return authenticationMsg{err: err}
	}
}
