package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// hubCacheBuild is the first llama.cpp build whose `-hf` downloads land in the
// standard Hugging Face hub cache instead of a private ~/.cache/llama.cpp:
// PR ggml-org/llama.cpp#20775, "common : add standard Hugging Face cache
// support", released unchanged as build b8498 (2026-03-24). Below it the bytes a
// server fetches are invisible to cria's cache view, download progress and cache
// surgery, which breaks the single-source-of-truth principle — so an older build
// disables llama serving outright rather than warning (docs/specs/TOOLS.md).
const hubCacheBuild = 8498

// versionTimeout bounds the only program this package runs: a llama-server that
// hangs on --version must not hang cria's startup check. Generous on purpose —
// the first exec after a brew upgrade revalidates signatures over ~15 MB of
// dylibs, and a machine busy serving a model pages them in slowly; both were
// seen pushing an otherwise-40ms run over an earlier 3s budget, whose kill was
// then misread as an unverifiable build.
const versionTimeout = 10 * time.Second

// The two positions llama.cpp has printed its build number in. See parseBuild.
const (
	versionPrefix = "version:"
	buildLabel    = "build"
)

// versionRunner runs a tool's --version and returns everything it printed. It is
// this package's one seam: the rest of the check reads the filesystem, which a
// test can build for real.
type versionRunner func(path string) (string, error)

// runVersion is the real runner. llama.cpp writes its version banner to stderr,
// so both streams are captured, and the output comes back alongside any failure —
// a build number the caller can read is worth having even from a program that
// exited badly.
func runVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "--version")
	// Without WaitDelay a child that leaked its output pipe to a grandchild keeps
	// the read alive past the kill, and the timeout would not bound this call.
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		// The kill was cria's own timeout: say that, not "signal: killed" — the
		// reader is deciding whether the binary is broken or the machine busy.
		err = fmt.Errorf("took longer than %s: %w", versionTimeout, err)
	}
	return string(output), err
}

// parseBuild reads the llama.cpp build number out of `llama-server --version`.
// llama.cpp prints it on the version line, in one of the two shapes it has used:
//
//	version: 8498 (8c7957ca3)
//	version: 0.1.0-dev (build 10450, commit ece963f41)
//
// Only those two positions are read. Scanning the rest of the banner for
// something that looks like a build number would be silent-and-plausible; a shape
// cria does not know is reported absent instead (CODING-RULES §4), and the caller
// treats that as too old.
func parseBuild(output string) (int, bool) {
	for _, line := range strings.Split(output, "\n") {
		rest, isVersion := strings.CutPrefix(strings.TrimSpace(line), versionPrefix)
		if !isVersion {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false
		}
		// The older shape puts the build number straight after the label.
		if build, err := strconv.Atoi(unbracket(fields[0])); err == nil {
			return build, true
		}
		// The current shape puts a version string there and labels the build.
		for i := 0; i+1 < len(fields); i++ {
			if unbracket(fields[i]) != buildLabel {
				continue
			}
			if build, err := strconv.Atoi(unbracket(fields[i+1])); err == nil {
				return build, true
			}
		}
		return 0, false
	}
	return 0, false
}

// unbracket strips the punctuation llama.cpp wraps its version fields in, so
// "(build" and "10450," read as the tokens they are.
func unbracket(field string) string { return strings.Trim(field, "(),") }
