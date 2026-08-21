package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// releaseArchive is what the release workflow ships: a tar.gz holding the one
// file `cria`.
func releaseArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	files := tar.NewWriter(compressed)
	if err := files.WriteHeader(&tar.Header{Name: "cria", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatalf("cannot write the archive header: %v", err)
	}
	if _, err := files.Write([]byte(binary)); err != nil {
		t.Fatalf("cannot write the archive body: %v", err)
	}
	if err := files.Close(); err != nil {
		t.Fatalf("cannot close the archive: %v", err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatalf("cannot close the archive: %v", err)
	}
	return buffer.Bytes()
}

// assetName is this test host's asset — the same expression Install builds.
func assetName() string {
	return fmt.Sprintf("cria_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// checksumLine is one sha256sum-format line for an asset.
func checksumLine(asset string, archive []byte) string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:]) + "  " + asset + "\n"
}

// githubStandIn serves the three URLs an update touches, from one local server:
// the latest-release lookup, a release's checksums file, and its asset.
func githubStandIn(t *testing.T, tag string, checksums string, archive []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acdtrx/cria/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": %q, "name": "cria %s"}`, tag, tag)
	})
	mux.HandleFunc("/acdtrx/cria/releases/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	mux.HandleFunc("/acdtrx/cria/releases/download/"+tag+"/"+assetName(), func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// installedBinary is a stand-in for the running executable: a file holding the
// old build, in its own directory so leftovers are visible.
func installedBinary(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "cria")
	if err := os.WriteFile(target, []byte("the old build"), 0o755); err != nil {
		t.Fatalf("cannot write the stand-in binary: %v", err)
	}
	return target
}

func testClient(server *httptest.Server, target string) *Client {
	return &Client{
		apiURL:      server.URL,
		downloadURL: server.URL,
		http:        &http.Client{},
		target:      func() (string, error) { return target, nil },
	}
}

// The version a release embeds is the tag without its v prefix, and that is
// what LatestVersion answers — the two sides of the comparison speak one form.
func TestLatestVersionIsTheTagWithoutItsPrefix(t *testing.T) {
	archive := releaseArchive(t, "the new build")
	server := githubStandIn(t, "v0.3.0", checksumLine(assetName(), archive), archive)

	got, err := testClient(server, "").LatestVersion()
	if err != nil {
		t.Fatalf("LatestVersion failed: %v", err)
	}
	if got != "0.3.0" {
		t.Errorf("LatestVersion answered %q, want 0.3.0", got)
	}
}

// A lookup GitHub refuses is an error naming the status, not a version.
func TestLatestVersionReportsARefusedLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	_, err := testClient(server, "").LatestVersion()
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("LatestVersion answered %v, want an error naming the status", err)
	}
}

// The happy path: the asset is fetched, verified against checksums.txt, and the
// binary inside it lands at the target's own path, executable, with nothing
// staged left behind.
func TestInstallReplacesTheBinary(t *testing.T) {
	archive := releaseArchive(t, "the new build")
	server := githubStandIn(t, "v0.3.0", checksumLine(assetName(), archive), archive)
	target := installedBinary(t)

	replaced, err := testClient(server, target).Install("0.3.0")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if replaced != target {
		t.Errorf("Install answered %q, want the target path %q", replaced, target)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "the new build" {
		t.Errorf("the target holds %q (%v), want the release's binary", content, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the target's mode is %v (%v), want 0755", info.Mode(), err)
	}
	assertNothingStaged(t, target)
}

// A download that does not match checksums.txt never reaches the binary.
func TestInstallRefusesAChecksumMismatch(t *testing.T) {
	archive := releaseArchive(t, "the new build")
	tampered := checksumLine(assetName(), []byte("something else entirely"))
	server := githubStandIn(t, "v0.3.0", tampered, archive)
	target := installedBinary(t)

	_, err := testClient(server, target).Install("0.3.0")
	if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Fatalf("Install answered %v, want a checksum refusal", err)
	}
	assertUntouched(t, target)
}

// An asset checksums.txt does not list is a platform the release has no build
// for, and the refusal names the platform.
func TestInstallRefusesAPlatformWithNoBuild(t *testing.T) {
	archive := releaseArchive(t, "the new build")
	server := githubStandIn(t, "v0.3.0", checksumLine("cria_plan9_386.tar.gz", archive), archive)
	target := installedBinary(t)

	_, err := testClient(server, target).Install("0.3.0")
	want := fmt.Sprintf("no %s/%s build", runtime.GOOS, runtime.GOARCH)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Install answered %v, want it to name the missing platform", err)
	}
	assertUntouched(t, target)
}

// An archive with no cria file in it is refused, not installed.
func TestInstallRefusesAnArchiveWithoutTheBinary(t *testing.T) {
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	files := tar.NewWriter(compressed)
	files.WriteHeader(&tar.Header{Name: "README", Size: 5})
	files.Write([]byte("hello"))
	files.Close()
	compressed.Close()
	archive := buffer.Bytes()

	server := githubStandIn(t, "v0.3.0", checksumLine(assetName(), archive), archive)
	target := installedBinary(t)

	_, err := testClient(server, target).Install("0.3.0")
	if err == nil || !strings.Contains(err.Error(), "no cria binary") {
		t.Fatalf("Install answered %v, want the missing-binary refusal", err)
	}
	assertUntouched(t, target)
}

// assertUntouched proves a failed update left the installed binary exactly as
// it was — old content, no staged leftovers.
func assertUntouched(t *testing.T, target string) {
	t.Helper()
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "the old build" {
		t.Errorf("the target holds %q (%v), want the old build untouched", content, err)
	}
	assertNothingStaged(t, target)
}

// assertNothingStaged proves no temp file survived next to the binary.
func assertNothingStaged(t *testing.T, target string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".cria-update-*"))
	if err != nil || len(leftovers) != 0 {
		t.Errorf("staged files left behind: %v (%v)", leftovers, err)
	}
}
