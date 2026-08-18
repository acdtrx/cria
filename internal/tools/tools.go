// Package tools resolves the external programs cria drives — llama-server,
// mlx_lm.server and hf — and reports what each one's state disables
// (docs/specs/TOOLS.md). cria installs nothing: the host provides the tools, so a
// missing or unfit one degrades a feature instead of failing cria.
//
// The report is data. Naming a tool, where it resolved, what its state costs and
// the one action that clears it belongs here; deciding how to show any of that
// belongs to the TUI and the CLI.
package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"cria/internal/config"
)

// Name identifies a managed tool by the program cria would exec, which is also
// the name looked up on PATH.
type Name string

const (
	LlamaServer Name = "llama-server"
	MLXLMServer Name = "mlx_lm.server"
	HF          Name = "hf"
)

// Status is what the check concluded about one tool. Only StatusFound means cria
// may use it: an outdated or unverifiable llama-server is refused as firmly as an
// absent one, because launching by Hub reference against a private download cache
// would put model bytes where cria cannot see them (docs/specs/TOOLS.md).
type Status int

const (
	StatusFound      Status = iota // resolved, and fit for what cria asks of it
	StatusMissing                  // neither the config override nor PATH names a program cria can run
	StatusOutdated                 // llama-server only: present, but older than hubCacheBuild
	StatusUnverified               // llama-server only: present, version unreadable — treated as outdated
)

// String names the status the way a report renders it.
func (s Status) String() string {
	switch s {
	case StatusFound:
		return "found"
	case StatusMissing:
		return "missing"
	case StatusOutdated:
		return "outdated"
	case StatusUnverified:
		return "unverified"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// Tool is one managed tool's finding.
type Tool struct {
	Name     Name
	Status   Status
	Path     string // the program cria would exec; empty when the tool was not found
	Override bool   // the path came from the config.toml [tools] table, not from PATH
	Build    int    // llama-server only: the llama.cpp build number read from --version; 0 when unread
	Disables string // what this status takes away; empty while the status is StatusFound
	Fix      string // the action that clears it; empty while the status is StatusFound
}

// Usable reports whether cria may run this tool. Consumers ask this rather than
// enumerating statuses, so a new failure mode cannot silently read as usable.
func (t Tool) Usable() bool { return t.Status == StatusFound }

// Report is the whole tool check. The set is fixed at the three programs
// docs/specs/TOOLS.md names, so each is its own field: there is no lookup that
// can miss.
type Report struct {
	LlamaServer Tool
	MLXLMServer Tool
	HF          Tool
}

// All lists the findings in the order docs/specs/TOOLS.md presents them, for
// consumers that render the whole report.
func (r Report) All() []Tool { return []Tool{r.LlamaServer, r.MLXLMServer, r.HF} }

// Check resolves every managed tool and judges it. It never fails: a tool cria
// cannot find or cannot verify is a finding, not an error.
func Check(settings config.Settings) Report {
	return check(settings, runVersion)
}

// check is Check with its one exec injected, so tests can drive the llama-server
// judgement from canned --version output instead of a real llama.cpp.
func check(settings config.Settings, version versionRunner) Report {
	return Report{
		LlamaServer: checkLlamaServer(settings.Tools.LlamaServer, version),
		MLXLMServer: checkMLXLMServer(settings.Tools.MLXLMServer),
		HF:          checkHF(settings.Tools.HF),
	}
}

// checkLlamaServer resolves llama-server and, when it is there, decides whether
// its llama.cpp is recent enough to share the Hugging Face hub cache — the
// condition llama serving depends on (docs/specs/TOOLS.md).
func checkLlamaServer(override string, version versionRunner) Tool {
	found := resolve(LlamaServer, override)
	tool := Tool{Name: LlamaServer, Path: found.path, Override: found.override != ""}
	if !found.ok() {
		tool.Status = StatusMissing
		tool.Disables = unstartable("llama", "")
		tool.Fix = found.fix("llama_server", "install llama.cpp so llama-server is on PATH")
		return tool
	}

	// A program that printed its version and still exited badly has answered the
	// question, so the exec error only matters when nothing could be parsed.
	output, err := version(found.path)
	build, parsed := parseBuild(output)
	switch {
	case !parsed:
		tool.Status = StatusUnverified
		tool.Disables = unstartable("llama", versionProblem(err)+", so cria cannot confirm this build downloads into the Hugging Face hub cache")
		tool.Fix = upgradeFix
	case build < hubCacheBuild:
		tool.Status = StatusOutdated
		tool.Build = build
		tool.Disables = unstartable("llama", fmt.Sprintf("build %d downloads models into a private ~/.cache/llama.cpp instead of the Hugging Face hub cache", build))
		tool.Fix = upgradeFix
	default:
		tool.Build = build
	}
	return tool
}

// checkMLXLMServer resolves mlx_lm.server. Presence is the whole contract — no
// mlx-lm behavior cria depends on is tied to a version — and absence is normal on
// any host that is not Apple silicon.
func checkMLXLMServer(override string) Tool {
	found := resolve(MLXLMServer, override)
	tool := Tool{Name: MLXLMServer, Path: found.path, Override: found.override != ""}
	if !found.ok() {
		tool.Status = StatusMissing
		tool.Disables = unstartable("mlx", "")
		tool.Fix = found.fix("mlx_lm_server", "install mlx-lm so mlx_lm.server is on PATH (Apple silicon only)")
	}
	return tool
}

// checkHF resolves hf. cria never execs it: its job is `hf auth login`, and cria
// reads the token that writes (docs/specs/TOOLS.md). So its absence disables no
// cria feature — it only means gated repos have no credentials to download with.
func checkHF(override string) Tool {
	found := resolve(HF, override)
	tool := Tool{Name: HF, Path: found.path, Override: found.override != ""}
	if !found.ok() {
		tool.Status = StatusMissing
		tool.Disables = "no cria feature — cria never runs hf; without `hf auth login` there is no token, so gated repos fail to download"
		tool.Fix = found.fix("hf", "install the Hugging Face CLI so hf is on PATH, then run `hf auth login`")
	}
	return tool
}

// upgradeFix is the single answer to both llama-server version verdicts: the
// build has to move past the one that shares the hub cache.
var upgradeFix = fmt.Sprintf("upgrade llama.cpp to build %d or newer", hubCacheBuild)

// unstartable phrases what a backend loses, the way the degradation principle
// puts it: entries stay listed, they just cannot start (docs/specs/TOOLS.md).
// because names the cause when the tool is present but unfit.
func unstartable(backend, because string) string {
	line := "starting " + backend + " entries; they stay listed, marked unstartable"
	if because == "" {
		return line
	}
	return line + " (" + because + ")"
}

// versionProblem describes why no build number came back: the exec itself failed,
// or it succeeded and printed nothing cria recognises.
func versionProblem(err error) string {
	if err != nil {
		return "`llama-server --version` failed (" + err.Error() + ")"
	}
	return "`llama-server --version` reported no build number"
}

// resolution is one tool's lookup: where cria would exec it from, and why an
// override was refused.
type resolution struct {
	path     string // the resolved program; empty when the tool was not found
	override string // the config.toml path that was tried; empty when PATH was searched
	err      error  // why the override was refused; nil otherwise
}

// ok reports whether the lookup found a program to run.
func (r resolution) ok() bool { return r.path != "" }

// fix names the one action that would make this lookup succeed. A refused
// override is a configuration mistake with its own answer; anything else is the
// host missing a program. settingsKey is the key under [tools] that overrides
// this tool, and install is the sentence that describes getting it onto PATH.
func (r resolution) fix(settingsKey, install string) string {
	if r.err != nil {
		return fmt.Sprintf("config.toml sets tools.%s to %s, which cria cannot run (%v); correct it, or drop the key to search PATH",
			settingsKey, r.override, r.err)
	}
	return install + ", or set tools." + settingsKey + " in config.toml"
}

// resolve finds one tool: the config.toml [tools] override when the tree sets
// one, a PATH lookup otherwise (docs/specs/TOOLS.md). An override that names
// nothing runnable fails outright rather than falling back to PATH — a typo that
// silently resolved elsewhere would look like a working configuration.
func resolve(name Name, override string) resolution {
	if override != "" {
		if err := executable(override); err != nil {
			return resolution{override: override, err: err}
		}
		return resolution{path: override, override: override}
	}
	path, err := exec.LookPath(string(name))
	if err != nil {
		return resolution{}
	}
	return resolution{path: path}
}

// executable reports why path is not a program cria could run, so a bad override
// is caught by the tool check rather than at the moment of a refused start.
func executable(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("no such file")
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("it is a directory")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return errors.New("it is not executable")
	}
	return nil
}
