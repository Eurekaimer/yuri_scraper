package diagnostics

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Eurekaimer/yuri_scraper/internal/transport"
)

func TestFailureDoesNotPersistSensitiveErrorText(t *testing.T) {
	for _, err := range []error{
		errors.New("password=private-password Cookie=session-secret body=private-body"),
		&transport.HTTPError{URL: "https://user:private-password@example.test/search?q=private-query&token=private-token", StatusCode: 429},
	} {
		var output bytes.Buffer
		Logger(&output, true).Error("request.failed", Failure(err))
		for _, secret := range []string{"private-", "session-secret", "user:"} {
			if strings.Contains(output.String(), secret) {
				t.Fatalf("sensitive error escaped into log: %s", &output)
			}
		}
	}
}

func TestRotationKeepsRecentRecordsAndPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	writer, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	first := bytes.Repeat([]byte("a"), maxBytes-1)
	first = append(first, '\n')
	if _, err := writer.Write(first); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("latest-record\n")); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(Path(dir) + ".1")
	if err != nil || !bytes.Equal(old, first) {
		t.Fatalf("rotation lost previous log: %v", err)
	}
	recent, err := Recent(dir)
	if err != nil || recent != "latest-record" {
		t.Fatalf("recent records = %q, %v", recent, err)
	}
	for _, path := range []string{Path(dir), Path(dir) + ".1"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 || info.Size() > maxBytes {
			t.Fatalf("unsafe log: %s %v", path, info)
		}
	}
}

func TestDiagnosticTargetCannotBeDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(Path(dir), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("accepted directory as log target")
	}
	if _, err := Recent(dir); err == nil {
		t.Fatal("read directory as log")
	}
}
