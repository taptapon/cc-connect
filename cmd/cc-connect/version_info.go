package main

import "runtime/debug"

// effectiveBuildInfo returns the version/commit/build-time triple for
// display, falling back to the VCS metadata the Go toolchain embeds at
// build time when the Makefile's -ldflags stamps are absent.
//
// Why: the Windows build flow uses a bare `go build` (no -ldflags), which
// left `cc-connect --version` reporting "dev / none / unknown" and made the
// per-machine patch-commit table unverifiable on that platform. vcs.revision
// / vcs.time are embedded by default when building from a git checkout
// (Go 1.18+), so even a bare build reports the real commit.
func effectiveBuildInfo() (ver, com, built string) {
	ver, com, built = version, commit, buildTime
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if com == "none" && len(s.Value) >= 7 {
				com = s.Value[:7]
			}
		case "vcs.time":
			if built == "unknown" && s.Value != "" {
				built = s.Value
			}
		}
	}
	return
}
