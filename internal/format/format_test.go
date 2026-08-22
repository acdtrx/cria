package format

import "testing"

// Every scale a model size lands on, and the boundary where the unit turns
// over: a size is a number below a kibibyte and a scale above it.
func TestBytes(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 512, want: "512 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KiB"},
		{bytes: 1536, want: "1.5 KiB"},
		{bytes: 1024 * 1024, want: "1.0 MiB"},
		{bytes: 3*1024*1024*1024 + 512*1024*1024, want: "3.5 GiB"},
		{bytes: 2 * 1024 * 1024 * 1024 * 1024, want: "2.0 TiB"},
		{bytes: 5 * 1024 * 1024 * 1024 * 1024 * 1024, want: "5120.0 TiB"},
	}

	for _, test := range cases {
		if got := Bytes(test.bytes); got != test.want {
			t.Errorf("Bytes(%d) = %q, want %q", test.bytes, got, test.want)
		}
	}
}

// A combination is spelled the way `cria start` takes it, sorted so every face
// of one status writes it the same way. Nothing picked is nothing written: a
// flat entry has no combination at all.
func TestPicks(t *testing.T) {
	cases := []struct {
		name      string
		selection map[string]string
		want      string
	}{
		{name: "nothing picked", selection: nil, want: ""},
		{name: "one axis", selection: map[string]string{"quant": "q6"}, want: "quant=q6"},
		{
			name:      "sorted, not in any caller's order",
			selection: map[string]string{"quant": "q6", "layout": "coding", "context": "long"},
			want:      "context=long layout=coding quant=q6",
		},
	}

	for _, test := range cases {
		if got := Picks(test.selection); got != test.want {
			t.Errorf("%s: Picks(%v) = %q, want %q", test.name, test.selection, got, test.want)
		}
	}
}
