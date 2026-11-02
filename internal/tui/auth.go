package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type authenticationMsg struct {
	err     error
	attempt uint64
}

// authErrorLine is the first line of the authentication error text inside
// renderAuthContent, used to scroll straight to the actionable message.
const authErrorLine = 5

type authForm struct {
	username textinput.Model
	password textinput.Model
	focus    int
	busy     bool
	err      error
	attempt  uint64
	cancel   context.CancelFunc
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
	width = min(max(width, 1), 60)
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

// beginLogin starts one cancellable attempt and invalidates every earlier
// result, including a login that is still running in the background.
func (f *authForm) beginLogin(ctx context.Context) (context.Context, uint64) {
	if f.cancel != nil {
		f.cancel()
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.attempt++
	f.busy = true
	f.err = nil
	return attemptCtx, f.attempt
}

// cancelLogin stops the in-flight attempt, clears its password and releases
// the context. Any late result carries an outdated attempt number and is
// ignored by the caller.
func (f *authForm) cancelLogin() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.attempt++
	f.busy = false
	f.err = nil
	f.clearPassword()
}

// finishLogin accepts only the current attempt.
func (f *authForm) finishLogin(attempt uint64) bool {
	if attempt != f.attempt {
		return false
	}
	f.busy = false
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	return true
}

func authenticate(ctx context.Context, auth search.CredentialsAuthenticator, username, password string, attempt uint64) tea.Cmd {
	return func() tea.Msg {
		err := auth.Login(ctx, username, password)
		password = ""
		return authenticationMsg{err: err, attempt: attempt}
	}
}

// errorGuidance exposes user-facing advice carried by authentication errors
// without printing raw causes that may contain request details.
func errorGuidance(err error) string {
	var guided interface{ Guidance() string }
	if errors.As(err, &guided) {
		return strings.TrimSpace(guided.Guidance())
	}
	return ""
}
