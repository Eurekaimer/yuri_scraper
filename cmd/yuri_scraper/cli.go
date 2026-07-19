package main

import (
	"fmt"
	"io"

	"github.com/Eurekaimer/yuri_scraper/internal/buildinfo"
)

func handleMetadataCommand(args []string, output io.Writer, version string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "--version", "-version", "version":
		_, _ = fmt.Fprintln(output, buildinfo.FullVersion(version))
		return true
	default:
		return false
	}
}
