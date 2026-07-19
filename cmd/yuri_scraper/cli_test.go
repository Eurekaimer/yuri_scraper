package main

import (
	"bytes"
	"testing"
)

func TestHandleMetadataCommand(t *testing.T) {
	for _, arg := range []string{"--version", "-version", "version"} {
		var output bytes.Buffer
		if !handleMetadataCommand([]string{arg}, &output, "0.6.1") {
			t.Fatalf("%s was not handled", arg)
		}
		if got := output.String(); got != "YuriScraper v0.6.1\n" {
			t.Fatalf("output = %q", got)
		}
	}
}

func TestHandleMetadataCommandIgnoresOtherArguments(t *testing.T) {
	var output bytes.Buffer
	if handleMetadataCommand([]string{"--unknown"}, &output, "0.6.1") {
		t.Fatal("unexpectedly handled unknown argument")
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected output: %q", output.String())
	}
}
