package cli

import (
	"strings"
	"testing"
)

// `cria --help`, `-h` and `help` are one page on stdout, and asking for help is
// not a failure: it exits zero.
func TestHelpIsPrintedForEverySpelling(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		app, out, errOut := newTestApp(testTree(), &fakeServers{})
		if code := app.run(args, "9.9.9-test"); code != exitOK {
			t.Errorf("`cria %s` exited %d, want %d", args[0], code, exitOK)
		}
		if errOut.Len() != 0 {
			t.Errorf("`cria %s` wrote %q to stderr, want the page on stdout alone", args[0], errOut)
		}
		if !strings.HasPrefix(out.String(), "cria — local LLM servers") {
			t.Errorf("`cria %s` printed %q, want the help page", args[0], out)
		}
	}
}

// The page is the whole surface: every subcommand with what it is for, every
// flag, what the exit codes mean, and where the config schema lives.
func TestHelpNamesTheWholeSurface(t *testing.T) {
	app, out, _ := newTestApp(testTree(), &fakeServers{})
	app.run([]string{"--help"}, "9.9.9-test")
	page := out.String()

	for _, subcommand := range subcommands {
		if !strings.Contains(page, subcommand) {
			t.Errorf("the help page does not mention %q", subcommand)
		}
	}
	for _, want := range []string{
		waitFlag, jsonFlag, pathsFlag, llamaFlag, mlxFlag, "--version",
		"0 the asked-for thing is true or done, 1 it is not, 2 the command line could not be routed",
		"cria start <id> [choice=option ...] [--wait]",
		"config schema and examples: cria docs",
		"agents: cria docs prints everything needed to write entries",
		"cria start <id> --wait",
		"cria status --json",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the help page does not contain %q", want)
		}
	}
}

// A subcommand cria cannot route names the set that exists and where to read
// about it.
func TestUnknownSubcommandPointsAtHelp(t *testing.T) {
	app, _, errOut := newTestApp(testTree(), &fakeServers{})

	if code := app.run([]string{"serve"}, "9.9.9-test"); code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	for _, want := range []string{`unknown subcommand "serve"`, strings.Join(subcommands, ", "), "cria --help"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
}
