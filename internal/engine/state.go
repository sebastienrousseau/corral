package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StateFileName is the basename of the per-clone sidecar file Corral writes
// next to .git/. It records the most recent push timestamp the engine has
// observed for the upstream so subsequent runs can skip a no-op `git pull`.
//
// Exported so external tooling (e.g. a future `corralctl status` command, or
// a user's .gitignore generator) can reference the same constant.
const StateFileName = ".corral-state.json"

// cloneState is the JSON shape of <repo>/.corral-state.json. New fields must
// be added with omitempty so older sidecars continue to round-trip.
type cloneState struct {
	// LastSyncedPushedAt is the upstream PushedAt value at the time of the
	// last successful clone or sync.
	LastSyncedPushedAt time.Time `json:"last_synced_pushed_at"`
	// LastSyncedAt is the local wall-clock time of the last sync attempt
	// that touched the working tree (clone or successful pull). Used for
	// human display only — never for sync-skip decisions.
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// readCloneState parses repoDir/.corral-state.json. A missing file returns
// the zero value and a nil error so callers can treat "never synced" the
// same as "no state available". A malformed file surfaces as an error so
// the caller can decide whether to fall through to a full sync or abort.
func readCloneState(repoDir string) (cloneState, error) {
	path := filepath.Join(repoDir, StateFileName)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cloneState{}, nil
		}
		return cloneState{}, err
	}
	var s cloneState
	if err := json.Unmarshal(b, &s); err != nil {
		return cloneState{}, fmt.Errorf("malformed %s: %w", path, err)
	}
	return s, nil
}

// writeCloneState serialises s to repoDir/.corral-state.json atomically by
// writing to a sibling temp file and renaming it into place. A crash mid-write
// therefore leaves the previous valid state on disk rather than a half-written
// file that would fail to parse on the next run.
func writeCloneState(repoDir string, s cloneState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	finalPath := filepath.Join(repoDir, StateFileName)
	tmp, err := os.CreateTemp(repoDir, StateFileName+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup of the temp file if anything below fails before
	// the rename succeeds.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}
