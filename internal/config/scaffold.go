package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// agentsFile is the page the scaffold drops in the tree root: the entry point
	// for a coding agent asked to write an entry (docs/cria.md, principle 5).
	agentsFile = "AGENTS.md"
	// dirPerm and filePerm are the modes the scaffold creates with — a config tree
	// is the user's own, readable by them, written by nobody else.
	dirPerm  = 0o755
	filePerm = 0o644
)

// agentsPage is the AGENTS.md content, carried in the binary so a first run needs
// nothing on disk. The source file is lowercase: an AGENTS.md inside the repo
// would read as instructions for agents working on cria itself.
//
//go:embed agents.md
var agentsPage []byte

// Scaffold creates the parts of the config tree that are missing: the root, the
// models/ directory and AGENTS.md. It never touches anything that already exists —
// the files in the tree belong to whoever wrote them (docs/specs/CONFIG.md) — so
// running it on every invocation costs one stat and changes nothing after the
// first. It creates no config.toml and no entries: those are written from
// `cria docs`, by hand or by an agent.
func Scaffold(root string) error {
	entries := filepath.Join(root, entriesDir)
	if err := os.MkdirAll(entries, dirPerm); err != nil {
		return fmt.Errorf("cannot create the config tree at %s: %w", entries, err)
	}
	return createMissingFile(filepath.Join(root, agentsFile), agentsPage)
}

// createMissingFile writes content to path, and leaves a file that is already
// there exactly as it is — including one the user has rewritten. O_EXCL makes
// "does it exist" and "create it" one step, so two cria processes starting at once
// cannot have one overwrite the other's file.
func createMissingFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", path, err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}
