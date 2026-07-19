package sites

import (
	"sort"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/PuerkitoBio/goquery"
)

func yamiboCatalogCandidates(doc *goquery.Document, siteName string) []search.ChapterCandidate {
	type scored struct {
		candidate search.ChapterCandidate
		order     int
	}
	all := make([]scored, 0)
	seen := make(map[string]bool)
	doc.Find("div.showcollapse_content, div[id*='collapse'], div.pcb, td.t_f, div#postlist").Each(func(order int, scope *goquery.Selection) {
		items, duplicateCount, noiseCount := extractYamiboCandidateLinks(scope, siteName)
		if len(items) < 2 {
			return
		}
		pids := make([]string, 0, len(items))
		chapterTitles, ordered, lastNumber := 0, 0, -1
		for _, item := range items {
			pid := item.URL
			if match := yamiboPID.FindStringSubmatch(item.URL); len(match) > 1 {
				pid = match[1]
			}
			pids = append(pids, pid)
			if yamiboChapter.MatchString(item.Title) {
				chapterTitles++
			}
			if number := chapterOrdinal(item.Title); number >= 0 {
				if lastNumber < 0 || number >= lastNumber {
					ordered++
				}
				lastNumber = number
			}
		}
		key := strings.Join(pids, ",")
		if seen[key] {
			return
		}
		seen[key] = true
		source := yamiboNodeName(scope)
		score := len(items)*4 + chapterTitles*5 + ordered*2 - duplicateCount*3 - noiseCount*4
		if strings.Contains(source, "showcollapse") || strings.Contains(source, "collapse") {
			score += 12
		}
		preview := make([]string, 0, min(3, len(items)))
		for i := 0; i < len(items) && i < 3; i++ {
			preview = append(preview, items[i].Title)
		}
		all = append(all, scored{candidate: search.ChapterCandidate{Source: source, Chapters: items, Score: score, Preview: preview}, order: order})
	})
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].candidate.Score == all[j].candidate.Score {
			return all[i].order < all[j].order
		}
		return all[i].candidate.Score > all[j].candidate.Score
	})
	result := make([]search.ChapterCandidate, len(all))
	for i := range all {
		result[i] = all[i].candidate
	}
	return result
}

func extractYamiboCandidateLinks(scope *goquery.Selection, siteName string) ([]search.Chapter, int, int) {
	items := make([]search.Chapter, 0)
	seen := make(map[string]bool)
	duplicates, noise := 0, 0
	scope.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		title := strings.TrimSpace(a.Text())
		if title == "" {
			return
		}
		if yamiboNoise.MatchString(title) || strings.HasPrefix(strings.TrimSpace(href), "javascript:") {
			noise++
			return
		}
		if !strings.Contains(href, "viewthread") && !strings.Contains(href, "findpost") && !strings.Contains(href, "pid=") {
			return
		}
		href = normalizeYamiboURL(href)
		if href == "" {
			return
		}
		key := href
		if match := yamiboPID.FindStringSubmatch(href); len(match) > 1 {
			key = match[1]
		}
		if seen[key] {
			duplicates++
			return
		}
		seen[key] = true
		items = append(items, search.Chapter{Title: title, URL: href, SiteName: siteName})
	})
	return items, duplicates, noise
}

func catalogIsConvincing(candidate search.ChapterCandidate) bool {
	return len(candidate.Chapters) >= 3 && candidate.Score >= len(candidate.Chapters)*6
}
