package sites

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/search"
	"github.com/PuerkitoBio/goquery"
)

var (
	yamiboSearchNumber = regexp.MustCompile(`\d[\d,]*`)
	yamiboForumID      = regexp.MustCompile(`(?:forum-|[?&]fid=)(\d+)`)
)

type yamiboSearchHit struct {
	result    search.SearchResult
	replyRank int
	viewRank  int
}

func (y *Yamibo) SearchScopes() []search.SearchScope {
	return []search.SearchScope{{ID: "55", Name: "译文区"}, {ID: "49", Name: "文学区"}}
}

func (y *Yamibo) Search(ctx context.Context, query string) ([]search.SearchResult, error) {
	return y.SearchScoped(ctx, query, nil)
}

func (y *Yamibo) SearchScoped(ctx context.Context, query string, scopeIDs []string) ([]search.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	forums := y.searchForumIDs(scopeIDs)
	variants := search.QueryVariants(query, 4)
	hits := make(map[string]*yamiboSearchHit)
	var requestErrors []error
	successfulRequests := 0

	for _, order := range []string{"replies", "views"} {
		for _, fid := range forums {
			for _, variant := range variants {
				before := len(hits)
				for _, searchURL := range y.searchURLs(fid, order, variant) {
					doc, err := y.document(ctx, searchURL)
					if err != nil {
						requestErrors = append(requestErrors, fmt.Errorf("%s/%s: %w", mapYamiboForum(fid), order, err))
						continue
					}
					successfulRequests++
					collectYamiboSearchResults(doc, hits, forums, fid, order, y.baseURL)
					if len(hits) > before {
						break
					}
				}
				if len(hits) > before {
					break
				}
			}
		}
	}
	if len(hits) == 0 && successfulRequests == 0 && len(requestErrors) > 0 {
		return nil, errors.Join(requestErrors...)
	}

	results := make([]search.SearchResult, 0, len(hits))
	for _, hit := range hits {
		replyRankScore := 0.0
		if hit.replyRank > 0 {
			replyRankScore = max(0, 150-float64(hit.replyRank))
		}
		viewRankScore := 0.0
		if hit.viewRank > 0 {
			viewRankScore = max(0, 120-float64(hit.viewRank))
		}
		hit.result.Popularity = float64(hit.result.Replies*3+hit.result.Views) + replyRankScore*100 + viewRankScore*60
		results = append(results, hit.result)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Popularity > results[j].Popularity })
	if len(results) > 40 {
		results = results[:40]
	}
	return results, nil
}

func (y *Yamibo) searchForumIDs(scopeIDs []string) []int {
	if len(scopeIDs) == 0 {
		return []int{55, 49}
	}
	allowed := map[int]bool{55: true, 49: true}
	seen := make(map[int]bool)
	forums := make([]int, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		fid, err := strconv.Atoi(id)
		if err == nil && allowed[fid] && !seen[fid] {
			seen[fid] = true
			forums = append(forums, fid)
		}
	}
	if len(forums) == 0 {
		return []int{55, 49}
	}
	return forums
}

func (y *Yamibo) searchURLs(fid int, order, query string) []string {
	values := url.Values{
		"mod":          {"forum"},
		"searchsubmit": {"yes"},
		"srchfid[]":    {strconv.Itoa(fid)},
		"orderby":      {order},
		"ascdesc":      {"desc"},
		"srchtxt":      {query},
		"kw":           {query},
	}
	withKeyword := resolveYamiboURL(y.baseURL, "search.php?"+values.Encode())
	values.Del("kw")
	return []string{withKeyword, resolveYamiboURL(y.baseURL, "search.php?"+values.Encode())}
}

func collectYamiboSearchResults(doc *goquery.Document, hits map[string]*yamiboSearchHit, allowedForums []int, fallbackFID int, order, baseURL string) {
	allowed := make(map[int]bool, len(allowedForums))
	for _, fid := range allowedForums {
		allowed[fid] = true
	}
	rank := 0
	doc.Find("a.xst, a.s.xst, a[href*='mod=viewthread'], a[href*='viewthread']").Each(func(_ int, anchor *goquery.Selection) {
		href, ok := anchor.Attr("href")
		if !ok {
			return
		}
		href = resolveYamiboURL(baseURL, href)
		match := yamiboTID.FindStringSubmatch(href)
		if len(match) < 2 {
			return
		}
		title := strings.TrimSpace(anchor.Text())
		if title == "" {
			return
		}
		row := anchor.Closest("tr")
		fid, forumName := yamiboSearchForum(row, fallbackFID)
		if len(allowed) > 0 && fid != 0 && !allowed[fid] {
			return
		}
		rank++
		replies, views := yamiboSearchStats(row)
		key := match[1]
		hit := hits[key]
		if hit == nil {
			hit = &yamiboSearchHit{result: search.SearchResult{BookURL: href, SiteName: "百合会"}}
			hits[key] = hit
		}
		hit.result.Title = title
		hit.result.BookURL = href
		hit.result.Tags = []string{forumName}
		hit.result.Replies = max(hit.result.Replies, replies)
		hit.result.Views = max(hit.result.Views, views)
		if hit.result.Author == "" {
			hit.result.Author = strings.TrimSpace(row.Find("td.by cite a, td.by a[href*='space-uid'], td.by a[href*='uid=']").First().Text())
		}
		if order == "replies" && (hit.replyRank == 0 || rank < hit.replyRank) {
			hit.replyRank = rank
		}
		if order == "views" && (hit.viewRank == 0 || rank < hit.viewRank) {
			hit.viewRank = rank
		}
	})
}

func yamiboSearchForum(row *goquery.Selection, fallback int) (int, string) {
	fid, name := fallback, mapYamiboForum(fallback)
	forum := row.Find("a[href*='forum-'], a[href*='fid=']").First()
	if forum.Length() == 0 {
		return fid, name
	}
	if text := strings.TrimSpace(forum.Text()); text != "" {
		name = text
	}
	href, _ := forum.Attr("href")
	match := yamiboForumID.FindStringSubmatch(href)
	if len(match) > 1 {
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			fid = parsed
			name = mapYamiboForum(fid)
		}
	}
	return fid, name
}

func yamiboSearchStats(row *goquery.Selection) (int, int) {
	cell := row.Find("td.num, td[class*='num']").First()
	values := make([]int, 0, 2)
	cell.Find("a, em, span").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		raw := yamiboSearchNumber.FindString(strings.TrimSpace(node.Text()))
		if raw != "" {
			values = append(values, parseYamiboSearchNumber(raw))
		}
		return len(values) < 2
	})
	if len(values) >= 2 {
		return values[0], values[1]
	}
	text := strings.TrimSpace(cell.Text())
	numbers := yamiboSearchNumber.FindAllString(text, 2)
	if len(numbers) == 0 {
		return 0, 0
	}
	replies := parseYamiboSearchNumber(numbers[0])
	if len(numbers) == 1 {
		return replies, 0
	}
	return replies, parseYamiboSearchNumber(numbers[1])
}

func parseYamiboSearchNumber(value string) int {
	value = yamiboSearchNumber.FindString(value)
	parsed, _ := strconv.Atoi(strings.ReplaceAll(value, ",", ""))
	return parsed
}
