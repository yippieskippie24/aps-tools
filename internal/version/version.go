// Package version reports which build of the tool is running.
//
// Release builds have these values stamped in by GoReleaser (see the
// "ldflags" section of .goreleaser.yaml). Builds made with `go install`
// fall back to the module version Go records in the binary.
package version

import "runtime/debug"

// These are overwritten at build time by GoReleaser. Do not rename them
// without also updating .goreleaser.yaml.
var (
	// Version is the release tag, e.g. "v0.1.0".
	Version = "dev"
	// Commit is the git commit the build came from.
	Commit = "none"
	// Date is when the build was made.
	Date = "unknown"
)

// String returns a one-line, human-friendly description of this build.
func String() string {
	v := Version
	if v == "dev" {
		// `go install ...@v0.1.0` records the version here even without ldflags.
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	return "aps-tools " + v + " (commit " + Commit + ", built " + Date + ")"
}
