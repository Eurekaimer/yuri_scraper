package sites

import (
	"os"
	"testing"
)

func requireLiveTest(t *testing.T) {
	t.Helper()
	if testing.Short() || os.Getenv("YURISCRAPER_LIVE_TEST") != "1" {
		t.Skip("set YURISCRAPER_LIVE_TEST=1 and omit -short to run live network tests")
	}
}
