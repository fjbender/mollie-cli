package cmd

import (
	"fmt"
	"runtime/debug"
)

// SetVersion sets the string shown by "mollie --version". Release builds pass
// goreleaser ldflag values; "go install" builds fall back to module build info.
func SetVersion(version, commit, date string) {
	rootCmd.Version = formatVersion(version, commit, date, readBuildInfoVersion())
}

func readBuildInfoVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return ""
}

func formatVersion(version, commit, date, buildInfoVersion string) string {
	if version == "dev" && buildInfoVersion != "" && buildInfoVersion != "(devel)" {
		return buildInfoVersion
	}
	if version == "dev" {
		return "dev"
	}
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}
