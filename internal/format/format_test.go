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
