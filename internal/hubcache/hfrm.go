package hubcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// hfTimeout bounds one `hf cache rm`. It scans the cache and unlinks files, so
// it takes seconds; a run past this has hung (CODING-RULES §6).
const hfTimeout = 5 * time.Minute

// hfRunner execs hf with args and answers with what it wrote to stderr. Its
// error is the run's: a non-zero exit, or a program that could not start.
type hfRunner func(ctx context.Context, program string, args []string) (stderr []byte, err error)

// maxStderr is the most of hf's stderr a failure carries: its last words are
// the reason, and anything before them is a traceback.
const maxStderr = 4 << 10

// runHF execs hf. Its stdout is a report of what it did, which cria does not
// read: the exit status and the cache itself are the answer
// (docs/specs/CACHE.md, Deletion).
func runHF(ctx context.Context, program string, args []string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.Bytes(), err
}

// HFError is a delete hf did not finish: what it said on stderr, and what
// stopped it.
type HFError struct {
	Target Target
	Stderr string // hf's last words, on one line, credentials masked
	Err    error
}

func (e *HFError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("hf cache rm could not delete %s: %v", e.Target, e.Err)
	}
	return fmt.Sprintf("hf cache rm could not delete %s: %v: %s", e.Target, e.Err, e.Stderr)
}

func (e *HFError) Unwrap() error { return e.Err }

// throughHF carries out a plan by handing its targets to `hf cache rm`, which
// frees the shared store's payloads by its own rule, then removes the snapshot
// directories hf leaves empty — hf deletes files and keeps the directories
// that held them. What came back is measured, not taken from hf: the
// repository and the payloads it linked, before and after.
func throughHF(ctx context.Context, plan *Plan, hf string, run hfRunner) (int64, error) {
	before, err := occupied(plan)
	if err != nil {
		return 0, err
	}
	reclaimed := func() int64 {
		after, err := occupied(plan)
		if err != nil {
			// The bytes before are known and the bytes after are not: claiming
			// any would be a guess.
			return 0
		}
		// A download landing in the same repository meanwhile can only make
		// the difference smaller; it never makes the delete add bytes.
		return max(before-after, 0)
	}

	ctx, cancel := context.WithTimeout(ctx, hfTimeout)
	defer cancel()
	args := append([]string{"cache", "rm", "--cache-dir", filepath.Dir(plan.dir), "--yes"}, plan.HFTargets...)
	if stderr, err := run(ctx, hf, args); err != nil {
		return reclaimed(), &HFError{Target: plan.Target, Stderr: lastWords(stderr), Err: err}
	}

	// hf answers success for targets it could not find in the cache — a
	// repository it skips as damaged among them — so the tree is what says
	// whether it took them.
	var left []string
	for _, removal := range plan.Removes {
		if _, err := os.Lstat(removal.Path); err == nil {
			left = append(left, removal.Path)
		}
	}
	if len(left) > 0 {
		return reclaimed(), fmt.Errorf("hf cache rm left %d of the %d paths of %s in place, %s among them",
			len(left), len(plan.Removes), plan.Target, left[0])
	}
	for _, dir := range plan.Dirs {
		if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return reclaimed(), fmt.Errorf("cannot remove the emptied directory %s: %w", dir, err)
		}
	}
	return reclaimed(), nil
}

// occupied is what a plan's repository and the shared-store payloads it links
// hold on disk right now, their manifests with them: everything a delete
// through hf can take.
func occupied(plan *Plan) (int64, error) {
	total, err := regularBytes(plan.dir)
	if errors.Is(err, fs.ErrNotExist) {
		total, err = 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("cannot measure %s: %w", plan.dir, err)
	}
	var held []string
	for _, payload := range plan.payloads {
		held = append(held, payload, payload+".refs")
	}
	for _, path := range held {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("cannot measure %s: %w", path, err)
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total, nil
}

// hfToken is how a Hugging Face access token is spelled, for masking one
// should hf ever print it (CODING-RULES §9).
var hfToken = regexp.MustCompile(`hf_[A-Za-z0-9]{20,}`)

// lastWords is hf's stderr as a failure carries it: its tail, its lines joined
// into one, any token masked.
func lastWords(stderr []byte) string {
	if len(stderr) > maxStderr {
		stderr = stderr[len(stderr)-maxStderr:]
	}
	var lines []string
	for _, line := range strings.Split(string(stderr), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return hfToken.ReplaceAllString(strings.Join(lines, " · "), "hf_***")
}
