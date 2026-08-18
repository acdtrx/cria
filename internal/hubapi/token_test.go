package hubapi

import (
	"os"
	"path/filepath"
	"testing"
)

// The token comes from the environment first and from the file `hf auth login`
// writes otherwise, located the way huggingface_hub locates it. A host with no
// token at all is a normal host.
func TestTokenResolution(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
		want  string
	}{
		{
			name: "the environment names it",
			setup: func(t *testing.T) {
				t.Setenv("HF_TOKEN", "hf_from_env")
			},
			want: "hf_from_env",
		},
		{
			name: "the environment wins over the token file",
			setup: func(t *testing.T) {
				t.Setenv("HF_TOKEN", "hf_from_env")
				t.Setenv("HF_HOME", writeToken(t, "hf_from_file"))
			},
			want: "hf_from_env",
		},
		{
			name: "HF_HOME holds the token file",
			setup: func(t *testing.T) {
				t.Setenv("HF_HOME", writeToken(t, "hf_from_hf_home\n"))
			},
			want: "hf_from_hf_home",
		},
		{
			name: "the XDG cache location holds it",
			setup: func(t *testing.T) {
				cache := t.TempDir()
				t.Setenv("XDG_CACHE_HOME", cache)
				writeTokenAt(t, filepath.Join(cache, "huggingface"), "hf_from_xdg")
			},
			want: "hf_from_xdg",
		},
		{
			name: "the home directory holds it",
			setup: func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				writeTokenAt(t, filepath.Join(home, ".cache", "huggingface"), "hf_from_home")
			},
			want: "hf_from_home",
		},
		{
			name:  "no token anywhere",
			setup: func(t *testing.T) {},
		},
		{
			name: "HF_HOME names a tree with no token in it",
			setup: func(t *testing.T) {
				// huggingface_hub does not fall back to the cache location
				// either: HF_HOME names the whole Hugging Face home.
				t.Setenv("HF_HOME", t.TempDir())
				writeTokenAt(t, filepath.Join(t.TempDir(), "huggingface"), "hf_elsewhere")
			},
		},
		{
			name: "the token file holds only whitespace",
			setup: func(t *testing.T) {
				t.Setenv("HF_HOME", writeToken(t, "\n"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolate(t)
			test.setup(t)

			if got := Token(); got != test.want {
				t.Errorf("token is %q, want %q", got, test.want)
			}
		})
	}
}

// isolate cuts the test off from the developer's own Hugging Face setup: every
// variable the resolution reads is cleared and the home directory is an empty
// one, so a token on this machine cannot answer for the case under test.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HF_TOKEN", "")
	t.Setenv("HF_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", t.TempDir())
}

// writeToken writes a token file into a fresh Hugging Face home and returns it.
func writeToken(t *testing.T, content string) string {
	t.Helper()
	return writeTokenAt(t, t.TempDir(), content)
}

// writeTokenAt writes a token file into the given Hugging Face home, creating
// it, and returns that home.
func writeTokenAt(t *testing.T, home, content string) string {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "token"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}
