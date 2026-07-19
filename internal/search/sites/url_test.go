package sites

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSearchURL(t *testing.T) {
	s := New52Shuku()
	q := url.QueryEscape("秦寺")
	u := fmt.Sprintf("%s/so/search.php?q=%s", s.BaseURL(), q)
	t.Logf("Search URL: %s", u)

	if !strings.HasPrefix(u, "https://www.52shuku.net/so/search.php?q=") || strings.Contains(u, "秦寺") {
		t.Fatalf("search URL is not safely encoded: %s", u)
	}
}
