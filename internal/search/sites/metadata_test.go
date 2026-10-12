package sites

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
)

func TestGetBookMeta_Integration(t *testing.T) {
	requireLiveTest(t)

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bookURL := "https://www.52shuku.net/gl/30_b/bjYp6.html"
	detail, err := s.GetBookMeta(ctx, bookURL)
	if err != nil {
		t.Fatalf("GetBookMeta: %v", err)
	}
	if detail == nil {
		t.Fatal("GetBookMeta returned nil")
	}

	// Verify description contains expected content
	if detail.Description == "" {
		t.Error("expected non-empty description")
	} else {
		if !strings.Contains(detail.Description, "年下") && !strings.Contains(detail.Description, "忠犬") {
			t.Logf("description may not contain expected content: %s", detail.Description)
		}
	}

	// Verify protagonist detection
	if detail.Protagonist == "" {
		t.Log("protagonist not detected (may need improvement)")
	} else {
		if !strings.Contains(detail.Protagonist, "秦青晏") {
			t.Logf("expected protagonist 秦青晏, got %q", detail.Protagonist)
		}
	}

	// Verify status
	if detail.Status == "" {
		t.Log("status not detected")
	} else if detail.Status != "完结" {
		t.Logf("status = %q, want '完结'", detail.Status)
	}

	// Verify tags
	if len(detail.Tags) == 0 {
		t.Error("expected at least some tags")
	} else {
		hasTag := func(want string) bool {
			for _, tag := range detail.Tags {
				if strings.Contains(tag, want) {
					return true
				}
			}
			return false
		}
		if !hasTag("GL") && !hasTag("百合") {
			t.Logf("expected GL/百合 in tags: %v", detail.Tags)
		}
		if !hasTag("年下") {
			t.Logf("expected 年下 in tags: %v", detail.Tags)
		}
	}

	t.Logf("Title info - Status: %q", detail.Status)
	t.Logf("Description: %s", detail.Description[:min(len(detail.Description), 100)])
	t.Logf("Protagonist: %q", detail.Protagonist)
	t.Logf("Tags: %v", detail.Tags)
	t.Logf("LastUpdated: %q", detail.LastUpdated)
}

func TestGetBookMeta_InvalidURL(t *testing.T) {
	requireLiveTest(t)

	s := New52Shuku()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := s.GetBookMeta(ctx, "https://www.52shuku.net/nonexistent.html")
	if err == nil {
		t.Error("expected error for nonexistent URL")
	}
}

func Test52BookMetaSeparatesGenresFromAuthor(t *testing.T) {
	// Real book pages link the author under "所属专题" before the genre tags, and
	// the breadcrumb ends with a "…小说在线阅读" entry that is not a genre.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h1 class="article-title">怦然为你_闵然【完结】</h1>
<div class="breadcrumbs"><a href="/">52书库</a> &gt; <a href="/DanMei/">耽美小说</a> &gt; <a href="/gl/">GL百合</a> &gt; <a href="/gl/h1R2.html">怦然为你_闵然【完结】小说在线阅读</a></div>
<article class="article-content">
<p>小说简介：</p>
<p>[ＧＬ百合] 《怦然为你（gl）》作者：闵然【完结】 文案： 时懿的人生，从笔直的高岭之花到弯成回形针，只差了一个傅斯恬。 所有人都以为，傅斯恬会是时懿心上的一根刺。</p>
<p>所属专题： <a href="/zuozhe/minran.html" title="闵然">闵然</a>&nbsp;&nbsp;&nbsp;<a href="/Tags_Nan/XianDaiGL.html" title="现代百合">现代百合</a>&nbsp;&nbsp;&nbsp;<a href="/Tags_Nan/QingYouDuZhong.html" title="情有独钟">情有独钟</a></p>
<p>Tips：如果觉得52不错，记得收藏唯一网址 https://www.52shuku.net/ 或推荐给朋友哦~拜托啦 (&gt;.&lt;)</p>
</article>`)
	}))
	defer server.Close()
	source := fixture52(t, server)
	detail, err := source.GetBookMeta(context.Background(), server.URL+"/gl/h1R2.html")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "完结" {
		t.Fatalf("status was not read from the heading: %q", detail.Status)
	}
	for _, tag := range detail.Tags {
		if tag == "闵然" {
			t.Fatalf("author leaked into genre tags: %v", detail.Tags)
		}
		if strings.Contains(tag, "在线阅读") {
			t.Fatalf("breadcrumb title leaked into genre tags: %v", detail.Tags)
		}
	}
	want := map[string]bool{"现代百合": true, "情有独钟": true, "GL百合": true}
	for _, tag := range detail.Tags {
		delete(want, tag)
	}
	if len(want) != 0 {
		t.Fatalf("missing genre tags %v in %v", want, detail.Tags)
	}
	if strings.HasPrefix(detail.Description, "文案") {
		t.Fatalf("synopsis label was kept in the description: %q", detail.Description)
	}
	if !strings.Contains(detail.Description, "高岭之花") {
		t.Fatalf("synopsis body was lost: %q", detail.Description)
	}
}

// Verify Site52Shuku implements BookMetaProvider
var _ search.BookMetaProvider = (*Site52Shuku)(nil)
