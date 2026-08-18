package tools

import "testing"

// llama.cpp has printed its version banner two ways, and both are still in the
// wild: the bare-number shape covers every build from before the threshold up to
// late 2026, the labelled shape covers current builds. Anything else must fail to
// parse rather than yield a number scavenged from the banner (CODING-RULES §4).
func TestParseBuild(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
		wantOK bool
	}{
		{
			name:   "current shape, labelled build",
			output: "version: 0.1.0-dev (build 10450, commit ece963f41)\nbuilt with AppleClang 21.0.0.21000101 for Darwin arm64\n",
			want:   10450,
			wantOK: true,
		},
		{
			name:   "older shape, bare build number",
			output: "version: 8498 (8c7957ca3)\nbuilt with Apple clang version 17.0.0 for arm64-apple-darwin24.6.0\n",
			want:   8498,
			wantOK: true,
		},
		{
			name:   "older shape, pre-threshold build",
			output: "version: 8497 (2b6c8e1d1)\nbuilt with Apple clang version 17.0.0 for arm64-apple-darwin24.6.0\n",
			want:   8497,
			wantOK: true,
		},
		{
			name:   "carriage returns survive",
			output: "version: 9001 (deadbeef)\r\nbuilt with gcc for x86_64\r\n",
			want:   9001,
			wantOK: true,
		},
		{
			name:   "banner noise before the version line",
			output: "warning: no GPU backend found\nversion: 10450 (ece963f41)\n",
			want:   10450,
			wantOK: true,
		},
		{
			name:   "garbage",
			output: "zsh: command not found: llama-server\n",
			wantOK: false,
		},
		{
			name:   "empty",
			output: "",
			wantOK: false,
		},
		{
			name:   "version line without a build number anywhere",
			output: "version: unknown\nbuilt with clang for arm64\n",
			wantOK: false,
		},
		{
			// A shape cria does not know: the build is genuinely absent, and no
			// number is invented from the commit hash or the compiler version.
			name:   "labelled shape with the build label missing",
			output: "version: 0.1.0-dev (commit ece963f41)\nbuilt with AppleClang 21.0.0.21000101 for Darwin arm64\n",
			wantOK: false,
		},
		{
			name:   "no version line at all",
			output: "built with AppleClang 21.0.0.21000101 for Darwin arm64\n",
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			build, ok := parseBuild(test.output)
			if ok != test.wantOK {
				t.Fatalf("parseBuild(%q) ok is %v, want %v", test.output, ok, test.wantOK)
			}
			if ok && build != test.want {
				t.Errorf("parseBuild(%q) is %d, want %d", test.output, build, test.want)
			}
		})
	}
}
