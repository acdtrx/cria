package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// fakeUpdater is the update subcommand's whole outside world: what the latest
// release is, and what installing it did.
type fakeUpdater struct {
	latest     string
	latestErr  error
	replaced   string // the path Install answers
	installErr error

	installed string // the version Install was asked for
}

func (f *fakeUpdater) LatestVersion() (string, error) { return f.latest, f.latestErr }
func (f *fakeUpdater) Install(version string) (string, error) {
	f.installed = version
	return f.replaced, f.installErr
}

// updateApp is a test app whose updater is the fake.
func updateApp(fake *fakeUpdater) (*app, *bytes.Buffer, *bytes.Buffer) {
	app, answer, aside := newTestApp(testTree(), &fakeServers{})
	app.updater = func() updater { return fake }
	return app, answer, aside
}

// A binary already at the latest release has nothing to do, and that is a true
// answer: exit 0, and nothing installed.
func TestUpdateReportsTheLatestReleaseAsAlreadyThere(t *testing.T) {
	fake := &fakeUpdater{latest: "0.3.0"}
	app, answer, _ := updateApp(fake)

	if code := app.update(nil, "0.3.0"); code != exitOK {
		t.Fatalf("exit code %d, want %d", code, exitOK)
	}
	if !strings.Contains(answer.String(), "cria 0.3.0 is the latest release") {
		t.Errorf("cria printed %q, want the already-latest answer", answer)
	}
	if fake.installed != "" {
		t.Errorf("Install was asked for %q, want no install at all", fake.installed)
	}
}

// A release binary behind the latest is updated to it, and the answer names
// the path and both versions.
func TestUpdateInstallsTheLatestRelease(t *testing.T) {
	fake := &fakeUpdater{latest: "0.3.0", replaced: "/home/u/.local/bin/cria"}
	app, answer, _ := updateApp(fake)

	if code := app.update(nil, "0.2.2"); code != exitOK {
		t.Fatalf("exit code %d, want %d", code, exitOK)
	}
	if fake.installed != "0.3.0" {
		t.Errorf("Install was asked for %q, want the latest release", fake.installed)
	}
	if !strings.Contains(answer.String(), "updated /home/u/.local/bin/cria: 0.2.2 → 0.3.0") {
		t.Errorf("cria printed %q, want the path and both versions", answer)
	}
}

// A dev build matches no tag, so it updates too — deliberately: that is how a
// hand-deployed machine rejoins the release train. The answer shows exactly
// what was replaced.
func TestUpdateReplacesADevBuild(t *testing.T) {
	fake := &fakeUpdater{latest: "0.3.0", replaced: "/home/u/.local/bin/cria"}
	app, answer, _ := updateApp(fake)

	if code := app.update(nil, "dev (353b777, 2026-08-21)"); code != exitOK {
		t.Fatalf("exit code %d, want %d", code, exitOK)
	}
	if fake.installed != "0.3.0" {
		t.Errorf("Install was asked for %q, want the latest release", fake.installed)
	}
	if !strings.Contains(answer.String(), "dev (353b777, 2026-08-21) → 0.3.0") {
		t.Errorf("cria printed %q, want the dev identity it replaced", answer)
	}
}

// The download is narrated on stderr while the caller waits; stdout stays the
// answer alone.
func TestUpdateNarratesTheDownloadAsAnAside(t *testing.T) {
	fake := &fakeUpdater{latest: "0.3.0", replaced: "/home/u/.local/bin/cria"}
	app, answer, aside := updateApp(fake)

	if code := app.update(nil, "0.2.2"); code != exitOK {
		t.Fatalf("exit code %d, want %d", code, exitOK)
	}
	if !strings.Contains(aside.String(), "downloading cria 0.3.0") {
		t.Errorf("stderr held %q, want the download narration", aside)
	}
	if strings.Contains(answer.String(), "downloading") {
		t.Errorf("stdout held %q, want the answer alone", answer)
	}
}

// A lookup that failed is a failure, and nothing gets installed on top of it.
func TestUpdateReportsALookupFailure(t *testing.T) {
	fake := &fakeUpdater{latestErr: errors.New("the latest-release lookup failed: no route to host")}
	app, _, aside := updateApp(fake)

	if code := app.update(nil, "0.2.2"); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(aside.String(), "the latest-release lookup failed") {
		t.Errorf("cria printed %q, want the lookup failure", aside)
	}
	if fake.installed != "" {
		t.Errorf("Install was asked for %q, want no install after a failed lookup", fake.installed)
	}
}

// An install that failed says why, exit 1.
func TestUpdateReportsAnInstallFailure(t *testing.T) {
	fake := &fakeUpdater{latest: "0.3.0", installErr: errors.New("cannot replace /usr/local/bin/cria: permission denied")}
	app, _, aside := updateApp(fake)

	if code := app.update(nil, "0.2.2"); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(aside.String(), "cannot replace /usr/local/bin/cria") {
		t.Errorf("cria printed %q, want the install failure", aside)
	}
}

// Update takes nothing at all.
func TestUpdateRefusesWhatItCannotRoute(t *testing.T) {
	for _, args := range [][]string{{"0.3.0"}, {"--check"}} {
		app, _, aside := updateApp(&fakeUpdater{latest: "0.3.0"})
		if code := app.update(args, "0.2.2"); code != exitUsage {
			t.Errorf("`cria update %v` exited %d, want %d", args, code, exitUsage)
		}
		if !strings.Contains(aside.String(), "usage: cria update") {
			t.Errorf("cria printed %q, want the usage line", aside)
		}
	}
}
