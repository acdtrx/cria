package hubapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
)

// ggufRepo is a GGUF repo shaped like the ones cria serves: one quant per file,
// one split across shards in a directory of its own, a projector that pairs
// with any of them, and the repo furniture that is not a model.
var ggufRepo = []hubEntry{
	{path: "BF16", dir: true},
	{path: ".gitattributes", size: 3313},
	{path: "BF16/Qwen3-30B-A3B-BF16-00001-of-00002.gguf", size: 4000, lfs: true},
	{path: "BF16/Qwen3-30B-A3B-BF16-00002-of-00002.gguf", size: 1000, lfs: true},
	{path: "Qwen3-30B-A3B-UD-Q4_K_XL.gguf", size: 2000, lfs: true},
	{path: "Qwen3-30B-A3B-UD-Q2_K_XL.gguf", size: 1500, lfs: true},
	{path: "Qwen3-30B-A3B-Q8_0.gguf", size: 3000, lfs: true},
	{path: "mmproj-BF16.gguf", size: 400, lfs: true},
	{path: "README.md", size: 500},
	{path: "config.json", size: 100},
}

// mlxRepo is an MLX repo: the quantization is the repo, so every file in it is
// part of the download.
var mlxRepo = []hubEntry{
	{path: "config.json", size: 100},
	{path: "model-00001-of-00002.safetensors", size: 5000, lfs: true},
	{path: "model-00002-of-00002.safetensors", size: 4000, lfs: true},
	{path: "tokenizer.json", size: 700, lfs: true},
	{path: "README.md", size: 300},
	// The Hub reports zero bytes for a directory. This one carries bytes so the
	// sum proves directories are skipped rather than merely adding nothing.
	{path: "original", size: 999999, dir: true},
}

// A llama entry serves one quantization out of a repo that holds several, so
// the total is that quant's files — every shard of it, and nothing else.
func TestTotalOfALlamaEntry(t *testing.T) {
	hub := newHub(t, "unsloth/Qwen3-30B-A3B-GGUF", ggufRepo...)

	tests := []struct {
		name  string
		quant string
		want  Total
	}{
		{
			name:  "one file",
			quant: "Q8_0",
			want:  Total{Bytes: 3000, Known: true, Blobs: []string{lfsOid("Qwen3-30B-A3B-Q8_0.gguf")}},
		},
		{
			// The tag as unsloth documents it and as the repo spells it — the
			// value a cria entry carries for the quants this host serves.
			name:  "the documented UD- spelling",
			quant: "UD-Q2_K_XL",
			want:  Total{Bytes: 1500, Known: true, Blobs: []string{lfsOid("Qwen3-30B-A3B-UD-Q2_K_XL.gguf")}},
		},
		{
			// llama.cpp's -hf repo:TAG resolution ignores case, so the answer
			// must too — the one difference in spelling that still resolves.
			name:  "spelled in another case",
			quant: "ud-q4_k_xl",
			want:  Total{Bytes: 2000, Known: true, Blobs: []string{lfsOid("Qwen3-30B-A3B-UD-Q4_K_XL.gguf")}},
		},
		{
			// The projector's name carries BF16 too and it is not part of the
			// quant, on disk or here.
			name:  "shards in a directory of their own",
			quant: "BF16",
			want: Total{Bytes: 5000, Known: true, Blobs: []string{
				lfsOid("BF16/Qwen3-30B-A3B-BF16-00001-of-00002.gguf"),
				lfsOid("BF16/Qwen3-30B-A3B-BF16-00002-of-00002.gguf"),
			}},
		},
		{
			name:  "the projector is its own item",
			quant: "mmproj-BF16.gguf",
			want:  Total{Bytes: 400, Known: true, Blobs: []string{lfsOid("mmproj-BF16.gguf")}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := config.Entry{Backend: config.BackendLlama, Repo: hub.repo, Quant: test.quant}
			if got := totalOf(t, hub, "", entry); !sameTotal(got, test.want) {
				t.Errorf("total is %+v, want %+v", got, test.want)
			}
		})
	}
}

// The blobs a total names are the ones the cache stores those files under: the
// LFS content hash where the Hub has one, the git object id where it does not.
// An observation matches an unfinished download against these strings, so a
// wrong one would read as "this model is not the one landing"
// (docs/specs/SERVE.md).
func TestATotalNamesTheBlobsItsFilesLandIn(t *testing.T) {
	hub := newHub(t, "mlx-community/Qwen3-30B-A3B-4bit", mlxRepo...)

	total := totalOf(t, hub, "", config.Entry{Backend: config.BackendMLX, Repo: hub.repo})

	want := []string{
		gitOid("config.json"),
		lfsOid("model-00001-of-00002.safetensors"),
		lfsOid("model-00002-of-00002.safetensors"),
		lfsOid("tokenizer.json"),
		gitOid("README.md"),
	}
	if !slices.Equal(total.Blobs, want) {
		t.Errorf("the total names blobs %v, want %v", total.Blobs, want)
	}
}

// A quant the repo does not publish under that exact tag has no total. Zero
// would read as a finished download; the reason names what was looked for
// instead.
func TestTotalOfAQuantTheRepoDoesNotHave(t *testing.T) {
	hub := newHub(t, "unsloth/Qwen3-30B-A3B-GGUF", ggufRepo...)

	tests := []struct {
		name  string
		quant string
	}{
		{name: "a tag the repo does not publish", quant: "Q2_K"},
		// The repo publishes UD-Q2_K_XL. cria does not add or strip a
		// provider's prefix to make an entry fit what is there.
		{name: "the tag without the prefix the repo carries", quant: "Q2_K_XL"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := config.Entry{Backend: config.BackendLlama, Repo: hub.repo, Quant: test.quant}

			total := totalOf(t, hub, "", entry)

			if total.Known || total.Bytes != 0 {
				t.Fatalf("total is %+v, want no total", total)
			}
			if !strings.Contains(total.Reason, test.quant) || !strings.Contains(total.Reason, hub.repo) {
				t.Errorf("reason is %q, want it to name %s and the repo", total.Reason, test.quant)
			}
		})
	}
}

// An MLX quantization is its own repo, so the whole repo is the download.
func TestTotalOfAnMLXEntry(t *testing.T) {
	hub := newHub(t, "mlx-community/Qwen3-30B-A3B-4bit", mlxRepo...)

	total := totalOf(t, hub, "", config.Entry{Backend: config.BackendMLX, Repo: hub.repo})

	if total.Bytes != 10100 || !total.Known {
		t.Errorf("total is %+v, want 10100 bytes, known", total)
	}
}

// A llama entry that names no quant leaves the server to pick one, and cria
// cannot know which — the repo's own size would be a denominator several times
// the file that is actually downloading. There is no total, and no reason to
// ask the Hub for one.
func TestTotalOfALlamaEntryWithoutAQuant(t *testing.T) {
	hub := newHub(t, "unsloth/Qwen3-30B-A3B-GGUF", ggufRepo...)

	total := totalOf(t, hub, "", config.Entry{Backend: config.BackendLlama, Repo: hub.repo})

	if total.Known || total.Reason == "" {
		t.Fatalf("total is %+v, want no total with a reason", total)
	}
	if asked := hub.requests(); len(asked) != 0 {
		t.Errorf("the Hub was asked %v, want no request at all", asked)
	}
}

// The token is a credential: it rides in the Authorization header of every
// request and appears nowhere else. A host with no token asks anonymously,
// which is all a public repo needs.
func TestTheTokenTravelsInTheAuthorizationHeaderOnly(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "a token resolved", token: "hf_secret", want: "Bearer hf_secret"},
		{name: "no token on this host", token: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub := newHub(t, "unsloth/Qwen3-30B-A3B-GGUF", ggufRepo...)
			entry := config.Entry{Backend: config.BackendLlama, Repo: hub.repo, Quant: "Q8_0"}

			if total := totalOf(t, hub, test.token, entry); !total.Known {
				t.Fatalf("total is %+v, want the listing to have been read", total)
			}
			credentials := hub.credentials()
			if len(credentials) != 1 || credentials[0] != test.want {
				t.Errorf("the Hub saw Authorization %q, want %q", credentials, test.want)
			}
			for _, asked := range hub.requests() {
				if test.token != "" && strings.Contains(asked, test.token) {
					t.Errorf("the request URI %q carries the token", asked)
				}
			}
		})
	}
}

// A Hub that cannot answer is not a failure cria propagates: the entry still
// starts and its progress simply shows bytes without a total
// (docs/specs/SERVE.md). Every one of these carries the reason it has none.
func TestATotalTheHubCannotGiveIsNotAnError(t *testing.T) {
	tests := []struct {
		name   string
		hub    func(t *testing.T) (address string, timeout time.Duration)
		reason string
	}{
		{
			name: "the Hub is unreachable",
			hub: func(t *testing.T) (string, time.Duration) {
				dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				address := dead.URL
				dead.Close()
				return address, time.Second
			},
			reason: "unreachable",
		},
		{
			name: "the Hub does not answer in time",
			hub: func(t *testing.T) (string, time.Duration) {
				hub := newHub(t, "unsloth/Qwen3-30B-A3B-GGUF", ggufRepo...)
				hub.delay = 250 * time.Millisecond
				return hub.URL, 20 * time.Millisecond
			},
			reason: "unreachable",
		},
		{
			name: "the repo is private, gated or misspelled",
			hub: func(t *testing.T) (string, time.Duration) {
				hub := newHub(t, "some/other-repo")
				return hub.URL, time.Second
			},
			// The Hub's own words, carried through for display.
			reason: "Invalid username or password.",
		},
		{
			name:   "the Hub refuses the listing",
			hub:    refusingHub(http.StatusForbidden, `{"error":"Access to model is restricted."}`),
			reason: "Access to model is restricted.",
		},
		{
			name:   "the Hub refuses without saying why",
			hub:    refusingHub(http.StatusNotFound, ""),
			reason: "404",
		},
		{
			name:   "the answer is not a file listing",
			hub:    refusingHub(http.StatusOK, "<!doctype html>"),
			reason: "did not parse",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, timeout := test.hub(t)
			entry := config.Entry{Backend: config.BackendLlama, Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "Q8_0"}

			total := newClient(address, "", timeout).Total(t.Context(), entry)

			if total.Known || total.Bytes != 0 {
				t.Fatalf("total is %+v, want no total", total)
			}
			if !strings.Contains(total.Reason, test.reason) {
				t.Errorf("reason is %q, want it to carry %q", total.Reason, test.reason)
			}
		})
	}
}

// refusingHub answers every request with one status and body.
func refusingHub(status int, body string) func(t *testing.T) (string, time.Duration) {
	return func(t *testing.T) (string, time.Duration) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, body)
		}))
		t.Cleanup(server.Close)
		return server.URL, time.Second
	}
}

// totalOf asks a fake hub for one entry's total.
func totalOf(t *testing.T, hub *fakeHub, token string, entry config.Entry) Total {
	t.Helper()
	return newClient(hub.URL, token, time.Second).Total(t.Context(), entry)
}

// sameTotal reports whether two totals are the same answer, blobs included: a
// Total carries a slice, so the tests compare it field by field.
func sameTotal(got, want Total) bool {
	return got.Bytes == want.Bytes && got.Known == want.Known &&
		got.Reason == want.Reason && slices.Equal(got.Blobs, want.Blobs)
}
