// Package diagnostics keeps bounded local logs without request bodies or credentials.
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Eurekaimer/yuri_scraper/internal/transport"
)

const maxBytes = 2 << 20

func Path(outputDir string) string { return filepath.Join(outputDir, ".diagnostics.log") }

// Writer keeps the current log and one backup, each at most 2 MiB. A process
// owns its descriptor; rotation is serialized among that process's goroutines.
type Writer struct {
	mu   sync.Mutex
	file *os.File
	path string
	size int64
}

func Open(outputDir string) (*Writer, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, err
	}
	w := &Writer{path: Path(outputDir)}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *Writer) open() error {
	if info, err := os.Lstat(w.path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("日志目标不是普通文件: %s", w.path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	if len(p) > maxBytes {
		return 0, fmt.Errorf("诊断记录超过 %d 字节", maxBytes)
	}
	if w.size+int64(len(p)) > maxBytes {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		w.file = nil
		if err := os.Rename(w.path, w.path+".1"); err != nil {
			_ = w.open()
			return 0, err
		}
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
func Logger(writer io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))
}

// Failure uses an allowlist, never err.Error(): third-party errors can contain
// passwords, cookies, URL queries or response bodies. Raw errors remain local
// to the interactive error panel, not persistent logs.
func Failure(err error) slog.Attr {
	kind := "operation"
	status := 0
	var httpErr *transport.HTTPError
	var netErr net.Error
	var pathErr *os.PathError
	switch {
	case errors.Is(err, context.Canceled):
		kind = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "timeout"
	case errors.As(err, &httpErr):
		kind, status = "http", httpErr.StatusCode
	case errors.As(err, &netErr):
		kind = "network"
		if netErr.Timeout() {
			kind = "timeout"
		}
	case errors.As(err, &pathErr):
		kind = "filesystem"
	}
	return slog.Group("failure", "kind", kind, "status", status, "type", fmt.Sprintf("%T", err), "retryable", transport.CanRetry(err), "guidance", Guidance(err))
}

func Guidance(err error) string {
	var httpErr *transport.HTTPError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "操作已取消；已保存的章节可在重试时复用。"
	case errors.Is(err, os.ErrPermission):
		return "请检查下载目录的写入权限，不要以管理员身份强行覆盖文件。"
	case errors.As(err, &httpErr):
		switch httpErr.StatusCode {
		case 429:
			return "书源限流，请稍后重试，并在任务页降低速度。"
		case 401, 403:
			return "书源拒绝访问，请在浏览器确认访问状态；不会绕过验证。"
		case 404:
			return "页面不存在，请重新搜索并选择有效书籍。"
		}
		if httpErr.StatusCode >= 500 {
			return "书源暂时异常，请稍后重试；已有结果与缓存保留。"
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return "网络连接失败或超时，请检查网络、DNS 与代理后重试。"
	}
	return "查看下方原始错误与日志；修复配置或页面解析问题后重试，缓存不会被自动清空。"
}

// Recent reads a bounded tail; diagnostics must not load a long-running log
// into memory or create files just because the user wants to inspect them.
func Recent(outputDir string) (string, error) {
	info, err := os.Lstat(Path(outputDir))
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("日志目标不是普通文件")
	}
	file, err := os.Open(Path(outputDir))
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("日志目标不是普通文件")
	}
	const limit = 64 << 10
	start := max(int64(0), info.Size()-limit)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return "", err
	}
	text := string(data)
	if start > 0 {
		_, text, _ = strings.Cut(text, "\n")
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	return strings.Join(lines, "\n"), nil
}
