package cli

// update runs `cria update`: it replaces this binary with the latest GitHub
// release (docs/specs/CLI.md).
//
// The comparison is plain equality. A release binary carries the bare tag it
// was built from, so matching the latest release means there is nothing to do;
// a dev build's version ("dev (<commit>, …)") matches no tag at all, so a dev
// binary always updates — which is deliberate: it is how a machine deployed by
// hand rejoins the release train.
func (a *app) update(args []string, version string) int {
	if len(args) != 0 {
		return a.usage("update: no arguments; usage: cria update")
	}

	updater := a.updater()
	latest, err := updater.LatestVersion()
	if err != nil {
		return a.fail("update: %v", err)
	}
	if version == latest {
		a.printf("cria %s is the latest release\n", latest)
		return exitOK
	}

	a.waiting("downloading cria %s", latest)
	replaced, err := updater.Install(latest)
	if err != nil {
		return a.fail("update: %v", err)
	}
	a.printf("updated %s: %s → %s\n", replaced, version, latest)
	return exitOK
}
