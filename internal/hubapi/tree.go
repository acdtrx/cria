package hubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxPageBytes caps one page of a listing. A page holds at most a thousand file
// records; the cap keeps a body that is not what the Hub sends from becoming
// cria's memory problem.
const maxPageBytes = 4 << 20

// treeFile is one entry of the models tree API: what the file is called, what
// it weighs, and the hash the hub cache will name its bytes after. For an LFS
// file — every weight file cria cares about — size is the real size, not the
// pointer's. The xet block describes how the Hub stores a file, which is none
// of cria's business.
type treeFile struct {
	Type string `json:"type"` // "file" or "directory"
	Path string `json:"path"` // the path inside the repo, the name the cache gives it too
	Size int64  `json:"size"`
	Oid  string `json:"oid"` // the git object the Hub holds; the blob name for a file stored in git itself
	LFS  struct {
		Oid string `json:"oid"` // the content hash; the blob name for a file stored in LFS
	} `json:"lfs"`
}

// blob is the name the hub cache gives this file's bytes. huggingface_hub and
// llama-server both name a blob after the entity tag the Hub serves the file
// with: the LFS content hash where there is one, the git object id otherwise —
// which is exactly what this host's cache holds, 64-hex blobs for the weights
// and 40-hex ones for the small files beside them.
func (f treeFile) blob() string {
	if f.LFS.Oid != "" {
		return f.LFS.Oid
	}
	return f.Oid
}

// tree lists every file of a repo's main revision with its size. Pagination is
// followed to the end: a listing cut short would sum to a plausible-looking
// total that is simply wrong, so a page that cannot be followed fails the whole
// answer instead (CODING-RULES §4).
func (c *Client) tree(ctx context.Context, repo string) ([]treeFile, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("the Hub address %q is not a URL: %w", c.baseURL, err)
	}
	// recursive=true is what makes the listing every file: without it the Hub
	// returns the top level only, and repos publish quants in per-quant
	// directories.
	first := *base
	first.Path = "/api/models/" + repo + "/tree/main"
	first.RawQuery = "recursive=true"

	var files []treeFile
	for next := first.String(); next != ""; {
		page, link, err := c.page(ctx, next)
		if err != nil {
			return nil, err
		}
		files = append(files, page...)
		if next, err = nextPage(base, link); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// page fetches one page of the listing and returns its files together with the
// Link header that names the next one.
func (c *Client) page(ctx context.Context, address string) ([]treeFile, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, "", fmt.Errorf("cannot ask the Hub for %s: %w", address, err)
	}
	// The credential travels here and nowhere else — not in the URL, which gets
	// logged by every proxy in between (CODING-RULES §9).
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("the Hugging Face Hub is unreachable: %w", err)
	}
	defer response.Body.Close()

	body := io.LimitReader(response.Body, maxPageBytes)
	if response.StatusCode != http.StatusOK {
		return nil, "", refusal(response.Status, body)
	}

	var entries []treeFile
	if err := json.NewDecoder(body).Decode(&entries); err != nil {
		return nil, "", fmt.Errorf("the Hub's file listing did not parse: %w", err)
	}

	files := make([]treeFile, 0, len(entries))
	for _, entry := range entries {
		// Directories are listed alongside files and hold no bytes of their own;
		// their contents are listed too, so dropping them counts nothing twice.
		if entry.Type == "file" {
			files = append(files, entry)
		}
	}
	return files, response.Header.Get("Link"), nil
}

// refusal turns a non-200 into the reason a total is missing. A repo that is
// gated, private or misspelled all land here — none of them is fatal to cria,
// they only leave the progress display without a percentage, so the Hub's own
// words are carried through for whoever reads it.
func refusal(status string, body io.Reader) error {
	var refused struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&refused); err == nil && refused.Error != "" {
		return fmt.Errorf("the Hub answered %s: %s", status, refused.Error)
	}
	return fmt.Errorf("the Hub answered %s", status)
}

// nextPage reads the address of the next page out of a Link header, checked
// against the Hub it came from. The Authorization header rides on that request,
// so a link pointing at another host is refused rather than followed — and
// refusing fails the listing, because silently stopping there would return a
// short sum as if it were the whole model.
func nextPage(base *url.URL, link string) (string, error) {
	target := relNext(link)
	if target == "" {
		return "", nil
	}
	next, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("the Hub's next-page link %q is not a URL: %w", target, err)
	}
	next = base.ResolveReference(next)
	if next.Scheme != base.Scheme || next.Host != base.Host {
		return "", fmt.Errorf("the Hub's next-page link points at %s, not at %s", next.Host, base.Host)
	}
	return next.String(), nil
}

// relNext picks the rel="next" address out of a Link header
// (`<address>; rel="next"`, RFC 8288). A header that names no next page ends
// the listing, which is how the last page announces itself.
func relNext(link string) string {
	for _, entry := range strings.Split(link, ",") {
		address, parameters, ok := strings.Cut(strings.TrimSpace(entry), ";")
		if !ok {
			continue
		}
		address = strings.TrimSpace(address)
		if !strings.HasPrefix(address, "<") || !strings.HasSuffix(address, ">") {
			continue
		}
		for _, parameter := range strings.Split(parameters, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && name == "rel" && strings.Trim(value, `"`) == "next" {
				return address[1 : len(address)-1]
			}
		}
	}
	return ""
}
