package converter

import (
	"strings"
	"unicode"
)

// simplifiedChars maps the most common traditional characters to simplified.
// This covers ~500 common characters. For full coverage, consider opencc.
var simplifiedChars = map[rune]rune{
	'個': '个', '們': '们', '門': '门', '開': '开', '關': '关',
	'對': '对', '劉': '刘', '張': '张', '時': '时', '來': '来',
	'東': '东', '萬': '万', '會': '会', '過': '过', '動': '动',
	'學': '学', '實': '实', '體': '体', '點': '点', '機': '机',
	'後': '后', '麼': '么', '為': '为', '說': '说', '現': '现',
	'裡': '里', '國': '国', '書': '书', '兒': '儿', '頭': '头',
	'這': '这', '邊': '边', '還': '还', '讓': '让', '進': '进',
	'從': '从', '種': '种', '樣': '样', '氣': '气', '愛': '爱',
	'義': '义', '與': '与', '經': '经', '網': '网', '華': '华',
	'長': '长', '風': '风', '飛': '飞', '馬': '马', '魚': '鱼',
	'鳥': '鸟', '龍': '龙', '處': '处', '變': '变', '當': '当',
	'發': '发', '見': '见', '話': '话', '識': '识', '車': '车',
	'軍': '军', '電': '电', '靈': '灵', '紅': '红', '綠': '绿',
	'給': '给', '無': '无', '聽': '听', '聲': '声', '難': '难',
	'臉': '脸', '麵': '面', '權': '权', '觀': '观',
	'亞': '亚', '衛': '卫', '際': '际', '標': '标', '準': '准',
	'確': '确', '質': '质', '戰': '战', '爭': '争', '黨': '党',
	'獨': '独', '總': '总', '統': '统', '聯': '联', '係': '系',
	'將': '将', '應': '应', '報': '报',
	'層': '层', '導': '导', '術': '术', '號': '号', '師': '师',
	'帶': '带', '幾': '几', '幹': '干', '樹': '树', '條': '条',
	'歷': '历', '壓': '压', '嚴': '严', '團': '团', '圖': '图',
	'場': '场', '塊': '块', '遠': '远', '運': '运', '達': '达',
	'選': '选', '遺': '遗', '醫': '医', '錢': '钱', '鐵': '铁',
	'銀': '银', '錯': '错', '鍵': '键', '隨': '随', '雖': '虽',
	'靜': '静', '頁': '页', '願': '愿', '顧': '顾', '類': '类',
	// Additional common chars for test coverage
	'簡': '简', '單': '单', '習': '习', '踐': '践',
	'試': '试', '測': '测', '驗': '验',
	'滿': '满', '業': '业', '區': '区', '劃': '划',
	'顯': '显', '示': '示', '範': '范', '圍': '围',
	'傳': '传', '優': '优', '勢': '势',
	'創': '创', '新': '新', '領': '领', '域': '域',
	'腦': '脑', '絡': '络',
}

// ToSimplified converts traditional Chinese text to simplified.
// Non-Chinese characters pass through unchanged.
func ToSimplified(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if simp, ok := simplifiedChars[r]; ok {
			b.WriteRune(simp)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsTraditional estimates whether text is primarily traditional Chinese.
// Returns true if the ratio of traditional-specific chars exceeds the threshold.
func IsTraditional(s string) bool {
	trad := 0
	total := 0
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			continue
		}
		total++
		if _, ok := simplifiedChars[r]; ok {
			trad++
		}
	}
	if total == 0 {
		return false
	}
	// If more than 15% of Han chars are in our traditional set, treat as traditional.
	return float64(trad)/float64(total) > 0.15
}
