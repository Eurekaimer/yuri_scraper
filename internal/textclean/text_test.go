package textclean

import "testing"

func TestParagraphRepairsPunctuationTyposAndSpacing(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"half width comma", "以后, 傅斯恬", "以后，傅斯恬"},
		{"half width colon", "时懿:", "时懿："},
		{"half width bang", "ipad!是不是", "ipad！是不是"},
		{"half width run", "怎么了?!", "怎么了？！"},
		{"decimal untouched", "1.0", "1.0"},
		{"url untouched", "http://a", "http://a"},
		{"censored dot untouched", "欲.望", "欲.望"},
		{"doubled de", "的的", "的"},
		{"doubled de word kept", "的的确确", "的的确确"},
		{"doubled de after purpose", "目的的的确", "目的的的确"},
		{"doubled de after best", "最合适的的确", "最合适的的确"},
		{"an nai", "按耐", "按捺"},
		{"zhi jie", "直接了当", "直截了当"},
		{"zuo wei", "做为", "作为"},
		{"tan xiao feng sheng", "谈笑风声", "谈笑风生"},
		{"man wu mu di", "她漫无目的的逛着。", "她漫无目的地逛着。"},
		{"han space removed", "第4节 课，我们要认真听。", "第4节课，我们要认真听。"},
		{"lyrics keep space", "我的心事 蒸发成云", "我的心事 蒸发成云"},
		{"doubled escape", "&amp;#34;再睡一会儿吧。&amp;#34;", `"再睡一会儿吧。"`},
		{"blank", " \u3000\t ", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Paragraph(test.input); got != test.want {
				t.Fatalf("Paragraph(%q) = %q; want %q", test.input, got, test.want)
			}
		})
	}
}

func TestParagraphIsIdempotent(t *testing.T) {
	for _, input := range []string{
		"以后, 傅斯恬说:走吧！",
		"她的的事，没人知道。",
		"第4节 课，我们要认真听。",
		`&amp;#34;再睡一会儿吧。&amp;#34;`,
	} {
		once := Paragraph(input)
		if twice := Paragraph(once); twice != once {
			t.Fatalf("Paragraph is not idempotent for %q: %q then %q", input, once, twice)
		}
	}
}

func TestIsAdLine(t *testing.T) {
	for _, line := range []string{
		"小贴士:找看好看得小说，就来52书库呀~www.52shuku.net",
		"目录 上一页",
		"目录\u00a0上一页",
		"目录 上一页 下一页",
		"52书库App",
		"传送门：",
		"哦豁，52书库又来了",
	} {
		if !IsAdLine(line) {
			t.Fatalf("IsAdLine(%q) = false; want true", line)
		}
	}
	for _, line := range []string{
		"她点开目录，又翻回上一页",
		"沈楝第一次见到郑汀雨，是在2009年。",
		"",
	} {
		if IsAdLine(line) {
			t.Fatalf("IsAdLine(%q) = true; want false", line)
		}
	}
}

func TestCleanTextDropsAdsAndEmptyLines(t *testing.T) {
	input := "沈楝第一次见到郑汀雨。\n\n目录\u00a0上一页\n她漫无目的的逛着, 笑了。\n  \n小贴士:就来52书库呀~www.52shuku.net\n"
	want := "沈楝第一次见到郑汀雨。\n她漫无目的地逛着，笑了。"
	if got := CleanText(input); got != want {
		t.Fatalf("CleanText = %q; want %q", got, want)
	}
}
