# Step 1 — `groups` in prefs

Status: pending

## Intent

Extend the `ui.json` schema with entry groups, keeping the file's existing
contract: strict decode, loud reset on a broken file, atomic write.

## Files likely touched

- `internal/tui/prefs.go`
- `internal/tui/prefs_test.go`

## Decisions (from planning)

- Shape:

  ```go
  type entryGroup struct {
      Name    string   `json:"name"`
      Entries []string `json:"entries"`
  }
  // on prefs:
  Groups []entryGroup `json:"groups,omitempty"`
  ```

  Array order **is** display order. `Entries` order carries no meaning
  (display is alphabetical); an empty `Entries` list is legal.
- Validation in `decodePrefs`, beside the existing backend check, each
  rejection making the whole file invalid (existing loadPrefs behavior:
  report + defaults):
  - a group name must be non-empty and unique (exact match) across groups;
  - an entry id may appear in at most one group, once.
- Ids are **not** checked against the config tree here — prefs decoding knows
  nothing about the tree; dangling ids are a render/write concern (step 2).
- No migration concerns: feature-building mode; an old cria reading a new
  `ui.json` resets loudly to defaults, which is the documented behavior.

## Acceptance criteria

- Round-trip test: prefs with two groups (one empty) survive save/load intact,
  order preserved.
- Rejection tests: empty name, duplicate name, id in two groups — each loads
  as defaults with the loud message, matching the existing broken-file test's
  pattern (`prefs_test.go:68`).
- `omitempty`: a prefs value with no groups serializes byte-identical to
  today's file.
- Suite green (`go test ./...`), `gofmt -l .` clean, `go vet ./...` clean —
  pure addition, no expected reds.

## Result

(recorded on completion)
