// Package selfupdate replaces the running cria binary with a GitHub release
// (docs/specs/CLI.md, `cria update`). It owns the two halves of that: asking
// GitHub what the newest release is, and installing one release's binary over
// the running executable — verified against the release's checksums file
// before a byte lands near the binary, and swapped in by an atomic rename so
// the executable on disk is never half-written. Running servers are detached
// processes holding the old inode; nothing here touches them.
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// repo is the one place cria releases from; install.sh names the same one.
const repo = "acdtrx/cria"

// The two timeouts bound what each request actually is. The release lookup and
// the checksums file are a few hundred bytes of metadata — an answer that has
// not arrived in ten seconds is a network problem worth reporting. The asset
// is a compressed binary of a few megabytes: two minutes lets it arrive over a
// genuinely slow line without letting a dead connection hang forever.
const (
	metadataTimeout = 10 * time.Second
	assetTimeout    = 2 * time.Minute
)

// maxAssetBytes rejects an absurd download before buffering it. The asset is a
// gzipped Go binary — single-digit megabytes — so a hundred times that is not
// a plausible release, it is a broken or hostile response.
const maxAssetBytes = 512 << 20

// Client updates the binary from one repository's releases. The two base URLs
// exist so the package tests can stand in for GitHub with a local server; New
// wires the real ones.
type Client struct {
	apiURL      string // GitHub REST API root
	downloadURL string // release-asset root
	http        *http.Client
	target      func() (string, error) // the running executable, symlinks resolved
}

// New is a client over the real GitHub and the real running executable.
func New() *Client {
	return &Client{
		apiURL:      "https://api.github.com",
		downloadURL: "https://github.com",
		http:        &http.Client{},
		target:      executablePath,
	}
}

// releaseDocument is the one field cria reads from GitHub's release answer.
type releaseDocument struct {
	TagName string `json:"tag_name"`
}

// LatestVersion is the newest release's version, bare — the tag without its v
// prefix, which is the form release builds embed as main.version.
func (c *Client) LatestVersion() (string, error) {
	address := fmt.Sprintf("%s/repos/%s/releases/latest", c.apiURL, repo)
	body, err := c.fetch(address, metadataTimeout)
	if err != nil {
		return "", fmt.Errorf("the latest-release lookup failed: %w", err)
	}
	defer body.Close()

	var release releaseDocument
	if err := json.NewDecoder(body).Decode(&release); err != nil {
		return "", fmt.Errorf("the latest-release answer did not parse: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("the latest-release answer names no tag")
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}

// Install downloads release <version>'s binary for this platform, verifies it
// against the release's checksums file, and atomically replaces the running
// executable. It returns the path it replaced. The version pins every URL to
// the release that was compared against, so a release published mid-update
// cannot mix assets.
func (c *Client) Install(version string) (string, error) {
	target, err := c.target()
	if err != nil {
		return "", fmt.Errorf("cannot locate the running binary: %w", err)
	}

	asset := fmt.Sprintf("cria_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("%s/%s/releases/download/v%s", c.downloadURL, repo, version)

	wantSum, err := c.releaseChecksum(base, asset, version)
	if err != nil {
		return "", err
	}
	archive, err := c.fetchAsset(base + "/" + asset)
	if err != nil {
		return "", fmt.Errorf("downloading %s failed: %w", asset, err)
	}
	// The checksum passes before any extraction happens — which is why the
	// asset is buffered rather than streamed: it is a few megabytes, and
	// nothing unverified gets written next to the binary.
	if gotSum := sha256.Sum256(archive); hex.EncodeToString(gotSum[:]) != wantSum {
		return "", fmt.Errorf("%s does not match the release's checksums.txt; run cria update again", asset)
	}

	binary, err := extractBinary(archive)
	if err != nil {
		return "", fmt.Errorf("%s is not the archive a release ships: %w", asset, err)
	}
	if err := replace(target, binary); err != nil {
		return "", err
	}
	return target, nil
}

// releaseChecksum is the asset's expected sha256 from the release's checksums
// file. The file doubles as the release's build manifest: an asset it does not
// list is a platform the release has no build for.
func (c *Client) releaseChecksum(base, asset, version string) (string, error) {
	body, err := c.fetch(base+"/checksums.txt", metadataTimeout)
	if err != nil {
		return "", fmt.Errorf("fetching release v%s's checksums failed: %w", version, err)
	}
	defer body.Close()
	sums, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("fetching release v%s's checksums failed: %w", version, err)
	}

	for _, line := range strings.Split(string(sums), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("release v%s has no %s/%s build", version, runtime.GOOS, runtime.GOARCH)
}

// fetchAsset is the whole asset, bounded by maxAssetBytes.
func (c *Client) fetchAsset(address string) ([]byte, error) {
	body, err := c.fetch(address, assetTimeout)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	archive, err := io.ReadAll(io.LimitReader(body, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(archive) > maxAssetBytes {
		return nil, fmt.Errorf("the download exceeds %d MB, which no cria release is", maxAssetBytes>>20)
	}
	return archive, nil
}

// fetch is one GET with its timeout; the caller owns closing the body. A
// non-200 answer is an error naming the status — GitHub answers 404 for both a
// missing release and a missing asset, and the caller's wrapping says which
// was asked for.
func (c *Client) fetch(address string, timeout time.Duration) (io.ReadCloser, error) {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "cria")

	client := *c.http
	client.Timeout = timeout
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GitHub answered %s", response.Status)
	}
	return response.Body, nil
}

// extractBinary is the one file a release archive holds.
func extractBinary(archive []byte) ([]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	files := tar.NewReader(compressed)
	for {
		header, err := files.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("it holds no cria binary")
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(header.Name) == "cria" {
			return io.ReadAll(files)
		}
	}
}

// replace writes the new binary next to the target and renames it into place:
// same directory, so the rename is atomic on one filesystem, and the running
// process keeps its old inode. macOS also demands the rename — rewriting an
// executable in place invalidates its code signature.
func replace(target string, binary []byte) error {
	staged, err := os.CreateTemp(filepath.Dir(target), ".cria-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", target, err)
	}
	defer os.Remove(staged.Name()) // a no-op once the rename has claimed it

	if _, err := staged.Write(binary); err != nil {
		staged.Close()
		return fmt.Errorf("cannot write the new binary: %w", err)
	}
	if err := staged.Chmod(0o755); err != nil {
		staged.Close()
		return fmt.Errorf("cannot mark the new binary executable: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("cannot write the new binary: %w", err)
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		return fmt.Errorf("cannot replace %s: %w", target, err)
	}
	return nil
}

// executablePath is the file the running process was started from, symlinks
// resolved so the file that gets replaced is the real one, not a link to it.
func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}
