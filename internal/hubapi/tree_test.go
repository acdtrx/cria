package hubapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
)

// The Hub pages long listings and points at the next page with a Link header.
// Every page has to be read: a listing cut short sums to a total that looks
// right and is not.
func TestEveryPageOfTheListingIsRead(t *testing.T) {
	hub := newHub(t, "mlx-community/Qwen3-30B-A3B-4bit", mlxRepo...)
	hub.pageSize = 2

	total := totalOf(t, hub, "", config.Entry{Backend: config.BackendMLX, Repo: hub.repo})

	if total.Bytes != 10100 || !total.Known || len(total.Blobs) != 5 {
		t.Fatalf("total is %+v, want 10100 bytes over 5 blobs — the whole listing, not one page", total)
	}
	asked := hub.requests()
	if len(asked) != 3 {
		t.Fatalf("the Hub was asked %d times for %d entries at 2 a page, want 3: %v", len(asked), len(mlxRepo), asked)
	}
	if strings.Contains(asked[0], "cursor") {
		t.Errorf("the first request %q carries a cursor, want the plain listing", asked[0])
	}
	for _, request := range asked[1:] {
		if !strings.Contains(request, "cursor=") {
			t.Errorf("request %q carries no cursor, want the one the Link header named", request)
		}
	}
}

// The credential travels on every page, not only the first: the Hub
// re-authenticates each request.
func TestTheTokenTravelsOnEveryPage(t *testing.T) {
	hub := newHub(t, "mlx-community/Qwen3-30B-A3B-4bit", mlxRepo...)
	hub.pageSize = 2

	totalOf(t, hub, "hf_secret", config.Entry{Backend: config.BackendMLX, Repo: hub.repo})

	credentials := hub.credentials()
	if len(credentials) != 3 {
		t.Fatalf("the Hub saw %d requests, want 3", len(credentials))
	}
	for i, credential := range credentials {
		if credential != "Bearer hf_secret" {
			t.Errorf("page %d carried Authorization %q, want the bearer token", i+1, credential)
		}
	}
}

// A next-page link pointing somewhere else is refused: the Authorization header
// would ride along. Refusing fails the whole listing rather than returning the
// pages read so far, which would be a short total passed off as the model.
func TestANextPageLinkOffTheHubIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://example.invalid/api/models/org/name/tree/main?cursor=1>; rel="next"`)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"type":"file","path":"model.safetensors","size":10}]`))
	}))
	t.Cleanup(server.Close)

	entry := config.Entry{Backend: config.BackendMLX, Repo: "org/name"}
	total := newClient(server.URL, "hf_secret", time.Second).Total(t.Context(), entry)

	if total.Known {
		t.Fatalf("total is %+v, want the short listing refused", total)
	}
	if !strings.Contains(total.Reason, "example.invalid") {
		t.Errorf("reason is %q, want it to name the host it refused", total.Reason)
	}
}

// The Link header comes in the shape RFC 8288 defines and the Hub sends.
func TestRelNextReadsTheLinkHeader(t *testing.T) {
	tests := []struct {
		name string
		link string
		want string
	}{
		{
			name: "the Hub's own shape",
			link: `<https://huggingface.co/api/models/unsloth/Qwen3-30B-A3B-GGUF/tree/main?expand=false&recursive=true&limit=3&cursor=ZXlK%3D>; rel="next"`,
			want: "https://huggingface.co/api/models/unsloth/Qwen3-30B-A3B-GGUF/tree/main?expand=false&recursive=true&limit=3&cursor=ZXlK%3D",
		},
		{
			name: "next among several relations",
			link: `<https://hub/first>; rel="first", <https://hub/second>; rel="next"`,
			want: "https://hub/second",
		},
		{
			name: "a relation that is not next",
			link: `<https://hub/first>; rel="prev"`,
		},
		{
			name: "no link at all",
		},
		{
			name: "a link that names no relation",
			link: `<https://hub/second>`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := relNext(test.link); got != test.want {
				t.Errorf("next page is %q, want %q", got, test.want)
			}
		})
	}
}

// A relative next-page link resolves against the Hub it came from.
func TestARelativeNextPageLinkResolvesAgainstTheHub(t *testing.T) {
	base, err := url.Parse("https://huggingface.co")
	if err != nil {
		t.Fatal(err)
	}

	next, err := nextPage(base, `</api/models/org/name/tree/main?cursor=2>; rel="next"`)
	if err != nil {
		t.Fatalf("nextPage refused a relative link: %v", err)
	}
	if want := "https://huggingface.co/api/models/org/name/tree/main?cursor=2"; next != want {
		t.Errorf("next page is %q, want %q", next, want)
	}
}
