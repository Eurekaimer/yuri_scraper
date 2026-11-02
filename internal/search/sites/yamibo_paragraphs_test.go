package sites

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/scraper"
	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"golang.org/x/time/rate"
)

func TestYamiboDownloadPreservesParagraphsInEPUB(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		pid := r.URL.Query().Get("pid")
		if pid == "" {
			fmt.Fprint(w, `<div id="postlist"><table><tr><td class="t_f">`)
			for i := 1; i <= 3; i++ {
				fmt.Fprintf(w, `<a href="%s/forum.php?mod=viewthread&amp;tid=42&amp;pid=%d">第%d章</a>`, server.URL, 100+i, i)
			}
			fmt.Fprint(w, `</td></tr></table></div>`)
			return
		}
		fmt.Fprintf(w, `<div id="postlist"><table><tr><td id="postmessage_%s"><div>第一段文字<span>和行内内容</span>在一起。</div><p>第二段来自独立段落标签。</p>第三段以缩进结束。&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;第四段以缩进开始。</td></tr></table></div>`, pid)
	}))
	defer server.Close()

	y := NewYamibo()
	y.baseURL, y.client = server.URL+"/", server.Client()
	y.limiter = rate.NewLimiter(rate.Inf, 1)
	y.SetCookie("test=session")
	defer y.Close()
	cfg := scraper.DefaultConfig()
	cfg.OutputDir, cfg.Format, cfg.Concurrency, cfg.RateLimit, cfg.RetryCount = t.TempDir(), "epub", 2, 1000, 0
	bookURL := server.URL + "/forum.php?mod=viewthread&tid=42"
	engine := scraper.NewEngine(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	progress, err := engine.ScrapeBook(ctx, y, bookURL, search.SearchResult{Title: "段落测试"})
	if err != nil {
		t.Fatal(err)
	}
	var final scraper.Progress
	for update := range progress {
		final = update
	}
	if final.Phase != "completed" || final.Failed != 0 {
		t.Fatalf("download failed: %+v", final)
	}
	book, err := zip.OpenReader(final.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for _, file := range book.File {
		if file.Name != "OEBPS/chapter_001.xhtml" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		var chapter struct {
			Paragraphs []string `xml:"body>p"`
		}
		if err := xml.Unmarshal(data, &chapter); err != nil {
			t.Fatal(err)
		}
		want := []string{"第一段文字和行内内容在一起。", "第二段来自独立段落标签。", "第三段以缩进结束。", "第四段以缩进开始。"}
		if strings.Join(chapter.Paragraphs, "\n") != strings.Join(want, "\n") {
			t.Fatalf("EPUB lost paragraph boundaries: %#v", chapter.Paragraphs)
		}
		return
	}
	t.Fatal("downloaded EPUB has no first chapter")
}
