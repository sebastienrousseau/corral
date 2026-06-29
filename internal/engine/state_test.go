package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloneStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	want := cloneState{
		LastSyncedPushedAt: now,
		LastSyncedAt:       now.Add(time.Minute),
	}
	if err := writeCloneState(dir, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readCloneState(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !got.LastSyncedPushedAt.Equal(want.LastSyncedPushedAt) {
		t.Errorf("pushed_at: got %v, want %v", got.LastSyncedPushedAt, want.LastSyncedPushedAt)
	}
	if !got.LastSyncedAt.Equal(want.LastSyncedAt) {
		t.Errorf("synced_at: got %v, want %v", got.LastSyncedAt, want.LastSyncedAt)
	}
}

func TestReadCloneStateMissingReturnsZero(t *testing.T) {
	dir := t.TempDir() // no state file inside
	s, err := readCloneState(dir)
	if err != nil {
		t.Fatalf("expected nil error for missing state, got %v", err)
	}
	if !s.LastSyncedPushedAt.IsZero() || !s.LastSyncedAt.IsZero() {
		t.Errorf("expected zero state, got %+v", s)
	}
}

func TestReadCloneStateMalformedSurfacesError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StateFileName), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := readCloneState(dir)
	if err == nil {
		t.Fatal("expected error for malformed state")
	}
}

func TestWriteCloneStateAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	first := cloneState{LastSyncedPushedAt: time.Unix(1, 0).UTC()}
	if err := writeCloneState(dir, first); err != nil {
		t.Fatalf("write first: %v", err)
	}
	second := cloneState{LastSyncedPushedAt: time.Unix(2, 0).UTC()}
	if err := writeCloneState(dir, second); err != nil {
		t.Fatalf("write second: %v", err)
	}
	got, err := readCloneState(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !got.LastSyncedPushedAt.Equal(second.LastSyncedPushedAt) {
		t.Errorf("expected second write to replace first, got %v", got.LastSyncedPushedAt)
	}
	// No leftover tmp files in the dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != StateFileName {
			t.Errorf("unexpected entry in dir: %s", e.Name())
		}
	}
}

func TestWriteCloneStateMissingDirError(t *testing.T) {
	err := writeCloneState("/no/such/path/anywhere", cloneState{})
	if err == nil {
		t.Fatal("expected error writing to nonexistent dir")
	}
}
