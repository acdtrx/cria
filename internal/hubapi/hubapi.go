// Package hubapi asks the Hugging Face Hub how big a model is. The cache counts
// the bytes that have landed (internal/hubcache); the Hub says how many there
// will be — together they are the download progress docs/specs/SERVE.md
// renders, numerator and denominator.
//
// The Hub is optional: cria never blocks a start on it. Every answer here is a
// Total that either carries bytes or carries the reason it does not, so an
// unreachable Hub costs the display its percentage and nothing else.
//
// This package also resolves the Hugging Face token — the one credential cria
// touches. It is read here, sent to the Hub in an Authorization header, and
// handed to the servers cria launches through their environment
// (docs/specs/SERVE.md). Never a URL, never an argv, never a log line
// (CODING-RULES §9).
package hubapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cria/internal/config"
	"cria/internal/hubcache"
)

// hubURL is the Hugging Face Hub's API root. cria talks to the public Hub only;
// the address is a constant rather than a setting because a wrong one would
// silently send the host's token somewhere else.
const hubURL = "https://huggingface.co"

// hubTimeout bounds a whole answer, pagination included. The Hub is consulted
// while a server is starting, so a Hub that is slow or gone has to become "no
// total" quickly rather than hold the display.
const hubTimeout = 5 * time.Second

// Total is what one entry's model comes to when it is complete — the
// denominator of downloading progress (docs/specs/SERVE.md). A total cria could
// not obtain is not an error: Known is false, Reason says why, and progress
// falls back to showing bytes without a total.
type Total struct {
	Bytes  int64  // the entry's model when complete; meaningful only when Known
	Known  bool   // the Hub answered and the answer covers this entry
	Reason string // why there is no total; empty exactly when Known

	// Blobs names the files this total summed, as the cache stores them: one
	// hash per file of the entry's model at the revision the Hub serves now.
	// The cache names every blob — and every unfinished download — after
	// exactly these strings, which is what lets an observation tell a file of
	// this model landing right now from some other file of the same repo
	// landing (docs/specs/SERVE.md). Empty exactly when there is no total.
	Blobs []string
}

// Client talks to one Hub API root with one credential.
type Client struct {
	baseURL string
	token   string        // sent as a bearer credential; empty means anonymous
	timeout time.Duration // bounds one Total, every page of it
	http    *http.Client
}

// New builds the client cria uses: the real Hub, the host's resolved token, and
// a timeout short enough that an unreachable Hub never delays a start.
func New() *Client {
	return newClient(hubURL, Token(), hubTimeout)
}

// newClient is New with its address, credential and timeout injected, so tests
// can drive the whole package against a local server.
func newClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{baseURL: baseURL, token: token, timeout: timeout, http: &http.Client{}}
}

// Total answers for one entry: the bytes of the quantization a llama entry
// names, or the whole repo for an mlx entry, whose quantization is its repo
// (docs/cria.md, principle 2).
func (c *Client) Total(ctx context.Context, entry config.Entry) Total {
	if entry.Backend == config.BackendLlama && entry.Quant == "" {
		// llama-server picks a quantization out of the repo and cria cannot know
		// which. Naming the whole repo instead would be a denominator many times
		// the file that is actually downloading, so there is no total — the one
		// case where the cache's numerator (the repo's bytes) and a total would
		// not be measuring the same thing.
		return unknown("the entry names no quantization, so which file llama-server downloads is known only once it starts")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	files, err := c.tree(ctx, entry.Repo)
	if err != nil {
		return unknown(err.Error())
	}
	if entry.Backend == config.BackendLlama {
		return quantTotal(files, entry.Repo, entry.Quant)
	}
	return repoTotal(files, entry.Repo)
}

// quantTotal sums the files that make up one quantization, shards included.
// Which files those are is hubcache's rule, called rather than restated: the
// bytes on disk and the bytes the Hub promises have to be counted over the same
// set of files or the progress they form is nonsense.
func quantTotal(files []treeFile, repo, quant string) Total {
	// The listing, reduced to the question being asked: which item each GGUF
	// file belongs to, what it weighs, and the blob it lands in.
	labels := make([]string, 0, len(files))
	quantFiles := make([]treeFile, 0, len(files))
	for _, file := range files {
		label, isGGUF := hubcache.GGUFItem(file.Path)
		if !isGGUF {
			continue
		}
		labels = append(labels, label)
		quantFiles = append(quantFiles, file)
	}

	match, found := hubcache.MatchQuant(labels, quant)
	if !found {
		return unknown(fmt.Sprintf("the Hub lists no %s file in %s", quant, repo))
	}
	total := Total{Known: true}
	for i, label := range labels {
		if label == labels[match] {
			total.Bytes += quantFiles[i].Size
			total.Blobs = append(total.Blobs, quantFiles[i].blob())
		}
	}
	return total
}

// repoTotal sums a whole repo — every file, weights and tokenizer alike, since
// an MLX server fetches the repo entire.
func repoTotal(files []treeFile, repo string) Total {
	total := Total{Known: true}
	for _, file := range files {
		total.Bytes += file.Size
		total.Blobs = append(total.Blobs, file.blob())
	}
	if total.Bytes == 0 {
		return unknown(fmt.Sprintf("the Hub lists no files in %s", repo))
	}
	return total
}

// unknown builds the answer that carries no bytes: the reason, for display next
// to the progress that has no percentage.
func unknown(reason string) Total {
	return Total{Reason: reason}
}
