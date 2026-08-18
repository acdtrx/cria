package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cria/internal/config"
)

// fakeBin writes a runnable stand-in for a managed tool into dir and returns its
// path. The body matters only for the one test that runs a tool for real.
func fakeBin(t *testing.T, dir string, name Name, body string) string {
	t.Helper()
	path := filepath.Join(dir, string(name))
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	return path
}

// pathWith points PATH at a temp directory holding exactly the named tools, so a
// lookup finds those and nothing the developer's own machine happens to have.
func pathWith(t *testing.T, names ...Name) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		fakeBin(t, dir, name, "#!/bin/sh\nexit 0\n")
	}
	t.Setenv("PATH", dir)
	return dir
}

// versionOutput is the banner a llama.cpp of the given build prints, in the shape
// current builds use.
func versionOutput(build int) string {
	return fmt.Sprintf("version: 0.1.0-dev (build %d, commit ece963f41)\nbuilt with AppleClang 21.0.0.21000101 for Darwin arm64\n", build)
}

// modernVersion stands in for a llama.cpp new enough to share the hub cache, for
// the tests that are about resolution rather than about the build judgement.
func modernVersion(string) (string, error) { return versionOutput(hubCacheBuild), nil }

func TestCheckResolvesEveryToolFromPath(t *testing.T) {
	dir := pathWith(t, LlamaServer, MLXLMServer, HF)

	report := check(config.Settings{}, modernVersion)

	for _, tool := range report.All() {
		if !tool.Usable() {
			t.Errorf("%s is %s (%s), want found", tool.Name, tool.Status, tool.Disables)
		}
		if want := filepath.Join(dir, string(tool.Name)); tool.Path != want {
			t.Errorf("%s resolved to %q, want %q", tool.Name, tool.Path, want)
		}
		if tool.Override {
			t.Errorf("%s is marked as a config override, but it was found on PATH", tool.Name)
		}
		if tool.Disables != "" || tool.Fix != "" {
			t.Errorf("%s is found yet reports disables=%q fix=%q, want both empty", tool.Name, tool.Disables, tool.Fix)
		}
	}
	if report.LlamaServer.Build != hubCacheBuild {
		t.Errorf("llama-server build is %d, want %d", report.LlamaServer.Build, hubCacheBuild)
	}
}

// The report lists the tools in the order docs/specs/TOOLS.md presents them.
func TestReportAllIsInSpecOrder(t *testing.T) {
	pathWith(t)
	var names []string
	for _, tool := range check(config.Settings{}, modernVersion).All() {
		names = append(names, string(tool.Name))
	}
	if got, want := strings.Join(names, ","), "llama-server,mlx_lm.server,hf"; got != want {
		t.Errorf("All() lists %s, want %s", got, want)
	}
}

// Absence disables features and is reported; it never fails the check. Each
// finding has to say what is lost and what the user would do about it.
func TestCheckReportsMissingTools(t *testing.T) {
	pathWith(t)

	report := check(config.Settings{}, modernVersion)

	for _, tool := range report.All() {
		if tool.Status != StatusMissing {
			t.Errorf("%s is %s, want missing", tool.Name, tool.Status)
		}
		if tool.Path != "" {
			t.Errorf("%s resolved to %q, want no path", tool.Name, tool.Path)
		}
		if tool.Disables == "" {
			t.Errorf("%s is missing but names nothing it disables", tool.Name)
		}
		if tool.Fix == "" {
			t.Errorf("%s is missing but names no fix", tool.Name)
		}
	}

	// The two server tools leave their entries listed and unstartable; hf costs no
	// cria feature at all, only credentials for gated repos.
	if !strings.Contains(report.LlamaServer.Disables, "llama entries") {
		t.Errorf("llama-server disables %q, want it to name llama entries", report.LlamaServer.Disables)
	}
	if !strings.Contains(report.MLXLMServer.Disables, "mlx entries") {
		t.Errorf("mlx_lm.server disables %q, want it to name mlx entries", report.MLXLMServer.Disables)
	}
	if !strings.Contains(report.HF.Disables, "gated repos") {
		t.Errorf("hf disables %q, want it to name gated repos", report.HF.Disables)
	}

	// Every fix points at the [tools] key that would settle it without touching PATH.
	for tool, key := range map[Tool]string{
		report.LlamaServer: "tools.llama_server",
		report.MLXLMServer: "tools.mlx_lm_server",
		report.HF:          "tools.hf",
	} {
		if !strings.Contains(tool.Fix, key) {
			t.Errorf("%s fix is %q, want it to name %s", tool.Name, tool.Fix, key)
		}
	}
}

// The override exists to bypass PATH, so it wins even when PATH would have found
// something.
func TestCheckPrefersTheConfigOverride(t *testing.T) {
	pathWith(t, LlamaServer, MLXLMServer, HF)
	elsewhere := t.TempDir()
	settings := config.Settings{Tools: config.Tools{
		LlamaServer: fakeBin(t, elsewhere, LlamaServer, "#!/bin/sh\nexit 0\n"),
		MLXLMServer: fakeBin(t, elsewhere, MLXLMServer, "#!/bin/sh\nexit 0\n"),
		HF:          fakeBin(t, elsewhere, HF, "#!/bin/sh\nexit 0\n"),
	}}

	for _, tool := range check(settings, modernVersion).All() {
		if !tool.Usable() {
			t.Errorf("%s is %s, want found", tool.Name, tool.Status)
		}
		if want := filepath.Join(elsewhere, string(tool.Name)); tool.Path != want {
			t.Errorf("%s resolved to %q, want the override %q", tool.Name, tool.Path, want)
		}
		if !tool.Override {
			t.Errorf("%s resolved from the override but is not marked as one", tool.Name)
		}
	}
}

// An override that names nothing runnable is a configuration mistake, not a
// reason to search PATH: falling back would make a typo look like a working
// setup. The tool is on PATH throughout, so a fallback would be visible.
func TestCheckRefusesAnUnusableOverride(t *testing.T) {
	tests := []struct {
		name       string
		override   func(t *testing.T, dir string) string
		wantReason string
	}{
		{
			name:       "dangling path",
			override:   func(_ *testing.T, dir string) string { return filepath.Join(dir, "not-installed") },
			wantReason: "no such file",
		},
		{
			name: "a directory",
			override: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "a-directory")
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("cannot create %s: %v", path, err)
				}
				return path
			},
			wantReason: "it is a directory",
		},
		{
			name: "a file without the executable bit",
			override: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "not-executable")
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
					t.Fatalf("cannot write %s: %v", path, err)
				}
				return path
			},
			wantReason: "it is not executable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pathWith(t, LlamaServer, MLXLMServer, HF)
			override := test.override(t, t.TempDir())
			settings := config.Settings{Tools: config.Tools{LlamaServer: override}}

			report := check(settings, modernVersion)
			llama := report.LlamaServer

			if llama.Status != StatusMissing {
				t.Fatalf("llama-server is %s (path %q), want missing", llama.Status, llama.Path)
			}
			if llama.Path != "" {
				t.Errorf("llama-server resolved to %q, want no path", llama.Path)
			}
			if !strings.Contains(llama.Fix, override) || !strings.Contains(llama.Fix, test.wantReason) {
				t.Errorf("llama-server fix is %q, want it to name %q and %q", llama.Fix, override, test.wantReason)
			}
			// The bad override disables its own tool only.
			if !report.MLXLMServer.Usable() || !report.HF.Usable() {
				t.Errorf("a bad llama_server override also disabled %s / %s", report.MLXLMServer.Status, report.HF.Status)
			}
		})
	}
}

// The cache check: a llama.cpp older than the build that shares the hub cache
// disables llama serving outright, and a version cria cannot read counts as too
// old (docs/specs/TOOLS.md).
func TestCheckJudgesTheLlamaServerBuild(t *testing.T) {
	execFailed := errors.New("signal: killed")

	tests := []struct {
		name       string
		output     string
		err        error
		wantStatus Status
		wantBuild  int
		wantCause  string
	}{
		{
			name:       "a current build",
			output:     versionOutput(10450),
			wantStatus: StatusFound,
			wantBuild:  10450,
		},
		{
			name:       "exactly the threshold build",
			output:     fmt.Sprintf("version: %d (8c7957ca3)\n", hubCacheBuild),
			wantStatus: StatusFound,
			wantBuild:  hubCacheBuild,
		},
		{
			name:       "one build below the threshold",
			output:     fmt.Sprintf("version: %d (2b6c8e1d1)\n", hubCacheBuild-1),
			wantStatus: StatusOutdated,
			wantBuild:  hubCacheBuild - 1,
			wantCause:  "~/.cache/llama.cpp",
		},
		{
			name:       "an ancient build",
			output:     "version: 4000 (0f1e2d3c4)\n",
			wantStatus: StatusOutdated,
			wantBuild:  4000,
			wantCause:  "~/.cache/llama.cpp",
		},
		{
			name:       "output cria cannot read",
			output:     "llama-server: unrecognized option '--version'\n",
			wantStatus: StatusUnverified,
			wantCause:  "reported no build number",
		},
		{
			name:       "no output at all",
			wantStatus: StatusUnverified,
			wantCause:  "reported no build number",
		},
		{
			name:       "the version command itself failed",
			err:        execFailed,
			wantStatus: StatusUnverified,
			wantCause:  execFailed.Error(),
		},
		{
			// A program that answered and still exited badly has answered.
			name:       "a readable version despite a failed exit",
			output:     versionOutput(10450),
			err:        execFailed,
			wantStatus: StatusFound,
			wantBuild:  10450,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pathWith(t, LlamaServer)
			version := func(string) (string, error) { return test.output, test.err }

			llama := check(config.Settings{}, version).LlamaServer

			if llama.Status != test.wantStatus {
				t.Fatalf("llama-server is %s, want %s", llama.Status, test.wantStatus)
			}
			if llama.Build != test.wantBuild {
				t.Errorf("llama-server build is %d, want %d", llama.Build, test.wantBuild)
			}
			if llama.Path == "" {
				t.Error("llama-server was found, so the report must still carry its path")
			}
			if test.wantStatus == StatusFound {
				if llama.Disables != "" || llama.Fix != "" {
					t.Errorf("a fit llama-server reports disables=%q fix=%q, want both empty", llama.Disables, llama.Fix)
				}
				return
			}
			if !strings.Contains(llama.Disables, "llama entries") || !strings.Contains(llama.Disables, test.wantCause) {
				t.Errorf("llama-server disables %q, want it to name llama entries and %q", llama.Disables, test.wantCause)
			}
			// Both verdicts have the same answer: move the build past the threshold.
			if !strings.Contains(llama.Fix, "upgrade llama.cpp") || !strings.Contains(llama.Fix, strconv.Itoa(hubCacheBuild)) {
				t.Errorf("llama-server fix is %q, want it to name the upgrade and build %d", llama.Fix, hubCacheBuild)
			}
		})
	}
}

// The exported entry point runs the real version command. llama.cpp prints its
// banner on stderr, so a runner that only read stdout would report every install
// as unverified.
func TestCheckReadsAVersionPrintedOnStderr(t *testing.T) {
	dir := t.TempDir()
	fakeBin(t, dir, LlamaServer, "#!/bin/sh\necho 'version: 9999 (abcdef123)' >&2\necho 'built with cc for test' >&2\n")
	t.Setenv("PATH", dir)

	llama := Check(config.Settings{}).LlamaServer

	if !llama.Usable() {
		t.Fatalf("llama-server is %s (%s), want found", llama.Status, llama.Disables)
	}
	if llama.Build != 9999 {
		t.Errorf("llama-server build is %d, want 9999", llama.Build)
	}
}
