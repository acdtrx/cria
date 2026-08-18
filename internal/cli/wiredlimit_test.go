package cli

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

// wiredLimitApp is an invocation on the test's 16 GiB machine.
func wiredLimitApp(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	app, out, errOut := newTestApp(testTree(), &fakeServers{})
	code := app.run(append([]string{"wired-limit"}, args...), "test")
	return code, out.String(), errOut.String()
}

// The plist goes to stdout whole and alone — a redirect is a clean file — and
// every instruction, sudo steps included, goes to stderr for the user to run:
// cria generates, it never installs (docs/specs/CLI.md).
func TestWiredLimitGeneratesThePlist(t *testing.T) {
	code, plist, help := wiredLimitApp(t, "13312")
	if code != exitOK {
		t.Fatalf("a valid limit exited %d, want %d\nstderr: %s", code, exitOK, help)
	}

	if !strings.HasPrefix(plist, "<?xml") || !strings.HasSuffix(strings.TrimSpace(plist), "</plist>") {
		t.Errorf("stdout is not one whole plist:\n%s", plist)
	}
	var parsed struct {
		Dict struct {
			Strings []string `xml:"array>string"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal([]byte(plist), &parsed); err != nil {
		t.Fatalf("the plist does not parse as XML: %v", err)
	}
	if want := []string{"/usr/sbin/sysctl", "iogpu.wired_limit_mb=13312"}; len(parsed.Dict.Strings) != 2 ||
		parsed.Dict.Strings[0] != want[0] || parsed.Dict.Strings[1] != want[1] {
		t.Errorf("ProgramArguments are %q, want %q", parsed.Dict.Strings, want)
	}

	for _, step := range []string{
		"16.0 GiB of memory", "13.0 GiB", "leaves 3.0 GiB for macOS",
		"sudo cp", "chown root:wheel", "launchctl bootstrap system",
		"sysctl iogpu.wired_limit_mb", "launchctl bootout",
	} {
		if !strings.Contains(help, step) {
			t.Errorf("the instructions never say %q:\n%s", step, help)
		}
	}
	// The file keeps its own uninstall steps — whoever finds it in
	// /Library/LaunchDaemons should know how to remove it — but installing is
	// the instructions' business, never the file's.
	if strings.Contains(plist, "sudo cp") || strings.Contains(plist, "bootstrap") {
		t.Error("an install step leaked into the plist on stdout")
	}
	if !strings.Contains(plist, "launchctl bootout") {
		t.Error("the plist does not carry its own uninstall steps")
	}
}

// The argument rules: the count is usage, the value is validation.
func TestWiredLimitRefusals(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		says string
	}{
		{name: "no value", args: nil, code: exitUsage, says: "usage:"},
		{name: "two values", args: []string{"1", "2"}, code: exitUsage, says: "usage:"},
		{name: "not a number", args: []string{"13GB"}, code: exitUsage, says: "not a whole number"},
		{name: "zero", args: []string{"0"}, code: exitFailure, says: "pins no memory"},
		{name: "negative", args: []string{"-4096"}, code: exitFailure, says: "pins no memory"},
		{name: "all of the memory", args: []string{"16384"}, code: exitFailure, says: "must leave macOS room"},
		{name: "more than the memory", args: []string{"99999"}, code: exitFailure, says: "must leave macOS room"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			code, plist, help := wiredLimitApp(t, test.args...)
			if code != test.code {
				t.Errorf("exited %d, want %d", code, test.code)
			}
			if plist != "" {
				t.Errorf("a refusal still printed a plist:\n%s", plist)
			}
			if !strings.Contains(help, test.says) {
				t.Errorf("the refusal reads %q, want it to say %q", help, test.says)
			}
		})
	}
}

// Off macOS the knob does not exist, and the refusal says so rather than
// generating a plist that lies about the host (the darwin build reads the real
// sysctl; this is the other build's answer, injected).
func TestWiredLimitRefusesWhereTheKnobIsMissing(t *testing.T) {
	app, out, errOut := newTestApp(testTree(), &fakeServers{})
	app.memoryMB = func() (int, error) {
		return 0, errors.New("iogpu.wired_limit_mb is an Apple-silicon knob; this host runs linux")
	}

	if code := app.run([]string{"wired-limit", "13312"}, "test"); code != exitFailure {
		t.Errorf("exited %d, want %d", code, exitFailure)
	}
	if out.Len() != 0 {
		t.Errorf("a refusal still printed a plist:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "Apple-silicon knob") {
		t.Errorf("the refusal reads %q, want the knob named", errOut.String())
	}
}
