package converter

import (
	"strings"
)

// tagPatterns maps genre keywords to canonical tags.
var tagPatterns = []struct {
	keywords []string
	tag      string
}{
	{[]string{"总裁", "豪门", "霸总"}, "豪门总裁"},
	{[]string{"重生", "穿越", "转世", "轮回"}, "重生穿越"},
	{[]string{"修仙", "修真", "仙侠", "飞升", "渡劫", "仙尊"}, "仙侠修真"},
	{[]string{"玄幻", "异世", "异界", "魔法", "斗气"}, "玄幻奇幻"},
	{[]string{"都市", "现代", "职场", "商战"}, "都市现代"},
	{[]string{"言情", "恋爱", "甜宠", "虐恋", "宠文"}, "言情恋爱"},
	{[]string{"悬疑", "推理", "侦探", "刑侦"}, "悬疑推理"},
	{[]string{"科幻", "星际", "未来", "末世", "机甲"}, "科幻未来"},
	{[]string{"武侠", "江湖", "武林", "侠客"}, "武侠江湖"},
	{[]string{"历史", "古代", "宫廷", "王朝", "帝王"}, "历史古代"},
	{[]string{"游戏", "网游", "电竞", "虚拟"}, "游戏竞技"},
	{[]string{"恐怖", "惊悚", "灵异", "鬼怪", "诡异"}, "恐怖灵异"},
	{[]string{"系统", "金手指", "升级"}, "系统升级"},
	{[]string{"女频", "女主", "女强"}, "女频"},
	{[]string{"男频", "男主", "男强"}, "男频"},
	{[]string{"种田", "田园", "农场"}, "种田生活"},
	{[]string{"后宫", "多女主"}, "后宫"},
	{[]string{"纯爱", "耽美", "BL", "百合", "GL"}, "纯爱"},
	{[]string{"校园", "青春", "学生"}, "校园青春"},
	{[]string{"军旅", "军事", "战争", "特种兵"}, "军旅战争"},
}

// AutoDetectTags scans title and description text to infer genre tags.
func AutoDetectTags(title, description string) []string {
	text := strings.ToLower(title + " " + description)
	seen := map[string]bool{}
	var tags []string
	for _, p := range tagPatterns {
		for _, kw := range p.keywords {
			if strings.Contains(text, strings.ToLower(kw)) {
				if !seen[p.tag] {
					seen[p.tag] = true
					tags = append(tags, p.tag)
				}
				break
			}
		}
	}
	return tags
}

// MergeTags combines site-provided tags with auto-detected tags, deduplicating.
func MergeTags(siteTags, autoTags []string) []string {
	seen := map[string]bool{}
	var merged []string
	for _, t := range siteTags {
		if !seen[t] {
			seen[t] = true
			merged = append(merged, t)
		}
	}
	for _, t := range autoTags {
		if !seen[t] {
			seen[t] = true
			merged = append(merged, t)
		}
	}
	return merged
}
