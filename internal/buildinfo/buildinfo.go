package buildinfo

import "strings"

const ProductName = "YuriScraper"

// NormalizeVersion returns a stable version value regardless of whether the
// linker supplied an optional leading "v".
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return "dev"
	}
	return version
}

func VersionLabel(version string) string {
	version = NormalizeVersion(version)
	if version == "dev" {
		return version
	}
	return "v" + version
}

func FullVersion(version string) string {
	return ProductName + " " + VersionLabel(version)
}
