package sites

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestNormalizeYamiboURL(t *testing.T) {
	got := normalizeYamiboURL("forum.php?mod=viewthread&tid=42&amp;pid=7")
	want := "https://bbs.yamibo.com/forum.php?mod=viewthread&tid=42&pid=7"
	if got != want {
		t.Fatalf("normalizeYamiboURL() = %q, want %q", got, want)
	}
}

func TestExtractYamiboLinksPrefersUniquePosts(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
        <div id="catalog">
          <a href="forum.php?mod=viewthread&amp;tid=42&amp;pid=7">第一章 开始</a>
          <a href="forum.php?mod=viewthread&amp;tid=42&amp;pid=7">第一章重复</a>
          <a href="forum.php?mod=viewthread&amp;tid=42&amp;pid=8">第二章 后续</a>
          <a href="javascript:void(0)">复制链接</a>
        </div>`))
	if err != nil {
		t.Fatal(err)
	}
	items := extractYamiboLinks(doc.Find("#catalog"), "百合会")
	if len(items) != 2 {
		t.Fatalf("got %d chapters, want 2", len(items))
	}
	if items[0].Title != "第一章 开始" || items[1].Title != "第二章 后续" {
		t.Fatalf("unexpected titles: %#v", items)
	}
}

func TestCleanYamiboContent(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
        <table><tr><td id="postmessage_7"><div class="quote">quoted noise</div>
        第一章 开始<br>这是足够长的正文内容，用来确认清洗器能够保留小说正文而不是论坛控件，
        并且不会错误地把真正需要保存的段落当成界面噪声删除。</td></tr></table>`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := cleanYamiboContent(doc.Find("#postmessage_7"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "quoted noise") || !strings.Contains(text, "第一章") {
		t.Fatalf("unexpected cleaned content: %q", text)
	}
}
