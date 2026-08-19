// Command cria serves local LLMs from a config tree: bare `cria` opens the TUI,
// subcommands drive the same subsystems scriptably (docs/specs/CLI.md).
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"cria/internal/cli"
	"cria/internal/config"
	"cria/internal/tui"
)

// version identifies this binary; it is what `cria --version` prints. Release
// builds inject the tag (-ldflags "-X main.version=<tag>",
// .github/workflows/release.yml); a local build honestly says dev rather than
// claiming a release it may have drifted from — and names its commit, so a dev
// binary in a bug report identifies itself (identified below).
var version = "dev"

func main() {
	scaffoldConfigTree()
	os.Exit(cli.Dispatch(os.Args[1:], identified(version, buildSettings()), tui.Run))
}

// identified turns the bare "dev" into "dev (<commit>, <date>[, dirty])" from
// the VCS facts the Go toolchain embeds in any binary built inside a checkout.
// A release version is already an identity and passes through untouched; a dev
// build with no VCS facts (built outside git) stays plain dev.
func identified(version string, settings []debug.BuildSetting) string {
	if version != "dev" {
		return version
	}
	var revision, date, dirty string
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				revision = s.Value[:7]
			}
		case "vcs.time":
			if len(s.Value) >= 10 {
				date = s.Value[:10] // the commit's date, not the build's
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = ", dirty"
			}
		}
	}
	if revision == "" {
		return version
	}
	return fmt.Sprintf("%s (%s, %s%s)", version, revision, date, dirty)
}

// buildSettings is what the toolchain recorded about this build, if anything.
func buildSettings() []debug.BuildSetting {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Settings
	}
	return nil
}

// scaffoldConfigTree gives every invocation — TUI or subcommand — a config tree to
// read, creating only what is missing (docs/specs/CONFIG.md). A tree cria cannot
// create is reported rather than fatal: `cria docs` still prints the schema, which
// is exactly what someone facing an unwritable config directory needs next, and
// the subcommands that do need the tree fail naming the same path.
func scaffoldConfigTree() {
	root, err := config.Root()
	if err == nil {
		err = config.Scaffold(root)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "cria: %v\n", err)
	}
}
