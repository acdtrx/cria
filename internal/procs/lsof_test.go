package procs

import "testing"

// The outputs here are real `lsof -a -p PID -d cwd -F n` from macOS 26.6: one
// field per line, the first byte naming the field. `lsof` emits the process (p)
// and file-descriptor (f) fields whatever else is asked for, and the working
// directory is the name that follows the cwd slot — not simply the first name in
// the output.
func TestParseWorkingDir(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		want   string
		wantOK bool
	}{
		{
			name:   "a real answer",
			out:    "p79831\nfcwd\nn/Users/acdtrx/projects/cria\n",
			want:   "/Users/acdtrx/projects/cria",
			wantOK: true,
		},
		{
			// A pid that is gone, or one whose working directory this user may
			// not read: nothing on stdout, and an exit status the caller reads as
			// "no answer" rather than as a failure.
			name:   "nothing came back",
			out:    "",
			wantOK: false,
		},
		{
			name:   "a directory containing spaces and unicode",
			out:    "p900\nfcwd\nn/Users/acdtrx/Модели/日本語 models\n",
			want:   "/Users/acdtrx/Модели/日本語 models",
			wantOK: true,
		},
		{
			// The reason the cwd slot is looked for rather than the first name:
			// another file's name must never be reported as the directory.
			name:   "another file listed ahead of the cwd",
			out:    "p900\nf3\nn/var/log/llama.log\nfcwd\nn/Users/acdtrx/models\n",
			want:   "/Users/acdtrx/models",
			wantOK: true,
		},
		{
			name:   "a process set with no cwd in it",
			out:    "p900\nf3\nn/var/log/llama.log\n",
			wantOK: false,
		},
		{
			name:   "a cwd slot with no name",
			out:    "p900\nfcwd\n",
			wantOK: false,
		},
		{
			name:   "the root directory",
			out:    "p1\nfcwd\nn/\n",
			want:   "/",
			wantOK: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, ok := parseWorkingDir(test.out)
			if ok != test.wantOK {
				t.Fatalf("parseWorkingDir(%q) ok is %v, want %v", test.out, ok, test.wantOK)
			}
			if ok && dir != test.want {
				t.Errorf("working directory is %q, want %q", dir, test.want)
			}
		})
	}
}

// The outputs here are real `lsof -nP -iTCP:PORT -sTCP:LISTEN -F pn` from macOS
// 26.6. One process can hold a port through several sockets, and a forked server
// shares them with its children, so the same port comes back as several p
// sections — the port has holders, and each is named once.
func TestParseListeners(t *testing.T) {
	tests := []struct {
		name string
		out  string
		port int
		want []int
	}{
		{
			name: "one listener",
			out:  "p79838\nf3\nn127.0.0.1:18777\n",
			port: 18777,
			want: []int{79838},
		},
		{
			name: "a server bound to every address",
			out:  "p801\nf3\nn*:8080\n",
			port: 8080,
			want: []int{801},
		},
		{
			// Both address families in one process: two sockets, one holder.
			name: "the same pid on two sockets",
			out:  "p79888\nf3\nn*:18777\nf4\nn*:18777\n",
			port: 18777,
			want: []int{79888},
		},
		{
			// A server that forked: `lsof` reports the shared sockets again under
			// each child.
			name: "a forked server",
			out:  "p79888\nf3\nn*:18777\nf4\nn*:18777\np79890\nf3\nn*:18777\nf4\nn*:18777\n",
			port: 18777,
			want: []int{79888, 79890},
		},
		{
			name: "an IPv6 listener",
			out:  "p801\nf4\nn[::1]:8080\n",
			port: 8080,
			want: []int{801},
		},
		{
			// A free port: nothing on stdout and an exit status.
			name: "nobody is listening",
			out:  "",
			port: 8080,
			want: nil,
		},
		{
			// The port is read back off the name field, so a socket that is not
			// on the port asked about is not an attribution. A port that merely
			// ends in the same digits is not the port either.
			name: "a socket on a port that ends the same way",
			out:  "p801\nf3\nn127.0.0.1:18080\n",
			port: 8080,
			want: nil,
		},
		{
			name: "a process set with no socket named",
			out:  "p801\nf3\n",
			port: 8080,
			want: nil,
		},
		{
			name: "a pid that is not a number",
			out:  "pPID\nf3\nn*:8080\n",
			port: 8080,
			want: nil,
		},
		{
			name: "a name field before any process",
			out:  "n*:8080\np801\nf3\nn*:8080\n",
			port: 8080,
			want: []int{801},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pids := parseListeners(test.out, test.port)
			if len(pids) != len(test.want) {
				t.Fatalf("parseListeners(%q, %d) is %v, want %v", test.out, test.port, pids, test.want)
			}
			for i, want := range test.want {
				if pids[i] != want {
					t.Errorf("listener %d is pid %d, want %d", i, pids[i], want)
				}
			}
		})
	}
}
