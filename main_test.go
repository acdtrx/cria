package main

import (
	"runtime/debug"
	"testing"
)

// A dev build names its commit; a release passes through; a build the toolchain
// recorded nothing about stays plain dev.
func TestIdentified(t *testing.T) {
	full := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "f7d44e6a1b2c3d4e5f60718293a4b5c6d7e8f901"},
		{Key: "vcs.time", Value: "2026-08-20T07:14:02Z"},
		{Key: "vcs.modified", Value: "false"},
	}
	dirty := append(append([]debug.BuildSetting{}, full[:2]...),
		debug.BuildSetting{Key: "vcs.modified", Value: "true"})

	cases := []struct {
		name     string
		version  string
		settings []debug.BuildSetting
		want     string
	}{
		{"release passes through", "0.3.0", full, "0.3.0"},
		{"dev names its commit", "dev", full, "dev (f7d44e6, 2026-08-20)"},
		{"a dirty checkout says so", "dev", dirty, "dev (f7d44e6, 2026-08-20, dirty)"},
		{"no VCS facts stays plain", "dev", nil, "dev"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := identified(test.version, test.settings); got != test.want {
				t.Errorf("identified(%q) = %q, want %q", test.version, got, test.want)
			}
		})
	}
}
