package sites

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/PuerkitoBio/goquery"
)

const (
	defaultYamiboScanPages = 10
	defaultYamiboMinChars  = 80
)

var yamiboUID = regexp.MustCompile(`(?:uid[-=]|[?&]uid=)(\d+)`)

type yamiboPost struct {
	PID     string
	Author  string
	UID     string
	Title   string
	Content string
}

type yamiboScanCheckpoint struct {
	ThreadURL    string           `json:"thread_url"`
	Author       string           `json:"author"`
	MinPostChars int              `json:"min_post_chars"`
	TotalPages   int              `json:"total_pages"`
	NextPage     int              `json:"next_page"`
	Chapters     []search.Chapter `json:"chapters"`
}

// PlanChapters keeps catalog and author-post resolution as explicit
// strategies. Auto mode uses a convincing catalog first, then falls back to
// paginated posts by the first author with substantial prose.
func (y *Yamibo) PlanChapters(ctx context.Context, threadURL string, opts search.ChapterPlanOptions) (search.ChapterPlan, error) {
	if opts.Mode == "" {
		opts.Mode = search.ChapterModeAuto
	}
	if opts.MaxScanPages <= 0 {
		opts.MaxScanPages = defaultYamiboScanPages
	}
	if opts.MinPostChars <= 0 {
		opts.MinPostChars = defaultYamiboMinChars
	}

	doc, err := y.document(ctx, threadURL)
	if err != nil {
		return search.ChapterPlan{}, err
	}
	totalPages := yamiboTotalPages(doc)
	candidates := yamiboCatalogCandidates(doc, y.Name())
	plan := search.ChapterPlan{Candidates: candidates, TotalPages: totalPages}

	if opts.Mode != search.ChapterModeAuthor {
		candidate := opts.CatalogCandidate
		if candidate < 0 || candidate >= len(candidates) {
			candidate = 0
		}
		if len(candidates) > 0 && (opts.Mode == search.ChapterModeCatalog || catalogIsConvincing(candidates[candidate])) {
			plan.Strategy = search.ChapterModeCatalog
			plan.Chapters = renumberChapters(append([]search.Chapter(nil), candidates[candidate].Chapters...))
			return plan, nil
		}
		if opts.Mode == search.ChapterModeCatalog {
			return plan, fmt.Errorf("未发现可信目录候选，可改用作者楼层模式")
		}
	}

	author := strings.TrimSpace(opts.Author)
	if author == "" {
		author = yamiboFirstSubstantialAuthor(doc, opts.MinPostChars)
	}
	if author == "" {
		return plan, fmt.Errorf("当前帖子没有足够长的正文楼层")
	}
	chapters, scanned, hits, err := y.scanAuthorPosts(ctx, doc, threadURL, author, totalPages, opts)
	plan.Strategy = search.ChapterModeAuthor
	plan.Author = author
	plan.ScannedPages = scanned
	plan.CheckpointHits = hits
	plan.Chapters = renumberChapters(chapters)
	if err != nil {
		return plan, err
	}
	if len(plan.Chapters) == 0 {
		return plan, fmt.Errorf("未找到作者 %q 的足够长正文楼层", author)
	}
	return plan, nil
}

func (y *Yamibo) scanAuthorPosts(ctx context.Context, firstDoc *goquery.Document, threadURL, author string, totalPages int, opts search.ChapterPlanOptions) ([]search.Chapter, int, int, error) {
	maxPages := min(max(1, totalPages), opts.MaxScanPages)
	checkpoint := yamiboScanCheckpoint{
		ThreadURL: canonicalYamiboThreadURL(threadURL), Author: author,
		MinPostChars: opts.MinPostChars, TotalPages: totalPages, NextPage: 1,
	}
	checkpointHits := 0
	if saved, err := loadYamiboScanCheckpoint(opts.CheckpointPath); err == nil &&
		saved.ThreadURL == checkpoint.ThreadURL && saved.Author == author && saved.MinPostChars == opts.MinPostChars {
		checkpoint = saved
		checkpointHits = len(saved.Chapters)
	}

	chapters := append([]search.Chapter(nil), checkpoint.Chapters...)
	seen := make(map[string]bool, len(chapters))
	for _, chapter := range chapters {
		seen[yamiboChapterKey(chapter.URL)] = true
	}
	startPage := max(1, checkpoint.NextPage)
	started := time.Now()
	for page := startPage; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return chapters, page - 1, checkpointHits, err
		}
		doc := firstDoc
		if page != 1 {
			var err error
			doc, err = y.document(ctx, yamiboThreadPage(threadURL, page))
			if err != nil {
				return chapters, page - 1, checkpointHits, fmt.Errorf("扫描作者楼层第 %d 页: %w", page, err)
			}
		}
		for _, post := range yamiboPosts(doc, opts.MinPostChars) {
			if !yamiboAuthorMatches(post, author) || seen[post.PID] {
				continue
			}
			chapter := search.Chapter{Title: post.Title, URL: yamiboPostURL(threadURL, post.PID), SiteName: y.Name()}
			seen[post.PID] = true
			chapters = append(chapters, chapter)
			if opts.ContentSink != nil {
				if err := opts.ContentSink(chapter, post.Content); err != nil {
					return chapters, page - 1, checkpointHits, fmt.Errorf("保存作者楼层正文 checkpoint: %w", err)
				}
			}
		}
		checkpoint.Chapters = chapters
		checkpoint.NextPage = page + 1
		if err := saveYamiboScanCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
			return chapters, page, checkpointHits, err
		}
		if opts.OnProgress != nil {
			opts.OnProgress(search.ChapterPlanProgress{
				Strategy: search.ChapterModeAuthor, ScannedPages: page,
				FoundChapters: len(chapters), CheckpointHits: checkpointHits, StartedAt: started,
			})
		}
	}
	return chapters, maxPages, checkpointHits, nil
}

func yamiboPosts(doc *goquery.Document, minChars int) []yamiboPost {
	posts := make([]yamiboPost, 0)
	doc.Find("[id^='post_']").Each(func(_ int, wrapper *goquery.Selection) {
		node := wrapper.Find("td[id^='postmessage_'], div[id^='postmessage_']").First()
		if node.Length() == 0 {
			return
		}
		id, _ := node.Attr("id")
		pid := strings.TrimPrefix(id, "postmessage_")
		content, err := cleanYamiboContent(node)
		if err != nil || yamiboTextLength(content) < minChars {
			return
		}
		authorNode := wrapper.Find(".authi a.xw1, .authi a[href*='uid'], a[href*='space-uid']").First()
		author := strings.TrimSpace(authorNode.Text())
		href, _ := authorNode.Attr("href")
		uid := ""
		if match := yamiboUID.FindStringSubmatch(href); len(match) > 1 {
			uid = match[1]
		}
		posts = append(posts, yamiboPost{PID: pid, Author: author, UID: uid, Title: yamiboPostTitle(content, len(posts)+1), Content: content})
	})
	// Some fixtures and older mobile pages expose postmessage nodes without a
	// post wrapper. Preserve the old fallback, but only when no structured post
	// was found; manual author filtering cannot be guaranteed in that layout.
	if len(posts) == 0 {
		doc.Find("td[id^='postmessage_'], div[id^='postmessage_']").Each(func(_ int, node *goquery.Selection) {
			id, _ := node.Attr("id")
			content, err := cleanYamiboContent(node)
			if err != nil || yamiboTextLength(content) < minChars {
				return
			}
			posts = append(posts, yamiboPost{PID: strings.TrimPrefix(id, "postmessage_"), Title: yamiboPostTitle(content, len(posts)+1), Content: content})
		})
	}
	return posts
}

func yamiboFirstSubstantialAuthor(doc *goquery.Document, minChars int) string {
	posts := yamiboPosts(doc, minChars)
	if len(posts) == 0 {
		return ""
	}
	if posts[0].UID != "" {
		return posts[0].UID
	}
	return posts[0].Author
}

func yamiboAuthorMatches(post yamiboPost, author string) bool {
	author = strings.TrimSpace(author)
	return author == "" || strings.EqualFold(post.Author, author) || post.UID == author
}

func yamiboPostTitle(content string, fallback int) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if yamiboChapter.MatchString(line) && len([]rune(line)) <= 80 {
			return line
		}
	}
	return fmt.Sprintf("作者楼层 %d", fallback)
}

func yamiboTextLength(text string) int {
	return len([]rune(strings.ReplaceAll(text, "\n", "")))
}

func yamiboTotalPages(doc *goquery.Document) int {
	total := 1
	doc.Find(".pg a[href], .pg label span, a[href*='page='], a[href*='thread-']").Each(func(_ int, s *goquery.Selection) {
		values := []string{strings.TrimSpace(s.Text())}
		if href, ok := s.Attr("href"); ok {
			values = append(values, href)
		}
		for _, value := range values {
			for _, re := range []*regexp.Regexp{yamiboPage, yamiboPageArg} {
				if match := re.FindStringSubmatch(value); len(match) > 1 {
					if page, _ := strconv.Atoi(match[1]); page > total {
						total = page
					}
				}
			}
			if page, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && page > total {
				total = page
			}
		}
	})
	return total
}

func yamiboPostURL(threadURL, pid string) string {
	if match := yamiboTID.FindStringSubmatch(threadURL); len(match) > 1 && pid != "" {
		base, err := url.Parse(threadURL)
		if err == nil && base.Scheme != "" && base.Host != "" {
			return fmt.Sprintf("%s://%s/forum.php?mod=viewthread&tid=%s&pid=%s", base.Scheme, base.Host, match[1], pid)
		}
		return fmt.Sprintf("%sforum.php?mod=viewthread&tid=%s&pid=%s", yamiboBaseURL, match[1], pid)
	}
	return threadURL
}

func yamiboChapterKey(rawURL string) string {
	if match := yamiboPID.FindStringSubmatch(rawURL); len(match) > 1 {
		return match[1]
	}
	return rawURL
}

func canonicalYamiboThreadURL(rawURL string) string {
	if match := yamiboTID.FindStringSubmatch(rawURL); len(match) > 1 {
		base, err := url.Parse(rawURL)
		if err == nil && base.Scheme != "" && base.Host != "" {
			return fmt.Sprintf("%s://%s/forum.php?mod=viewthread&tid=%s", base.Scheme, base.Host, match[1])
		}
		return yamiboBaseURL + "forum.php?mod=viewthread&tid=" + match[1]
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.Fragment = ""
	return parsed.String()
}

func yamiboNodeName(scope *goquery.Selection) string {
	if scope.Length() == 0 {
		return "unknown"
	}
	name := goquery.NodeName(scope)
	if id, ok := scope.Attr("id"); ok && id != "" {
		name += "#" + id
	}
	if class, ok := scope.Attr("class"); ok && class != "" {
		name += "." + strings.Join(strings.Fields(class), ".")
	}
	return name
}

func chapterOrdinal(title string) int {
	match := regexp.MustCompile(`\d+`).FindString(title)
	if match == "" {
		return -1
	}
	n, _ := strconv.Atoi(match)
	return n
}
