// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withSmallAuditLog shrinks the rotation threshold so the rotation path is
// reachable without writing 8 MiB, restoring it when the test ends.
func withSmallAuditLog(t *testing.T, limit int64) {
	t.Helper()
	original := maxAuditLogBytes
	maxAuditLogBytes = limit
	t.Cleanup(func() { maxAuditLogBytes = original })
}

// restoreAuditSeams puts the filesystem seams back after a test replaces
// them, so an injected failure cannot leak into another test.
func restoreAuditSeams(t *testing.T) {
	t.Helper()
	stat, rename, remove := statAuditFile, renameAuditFile, removeAuditFile
	t.Cleanup(func() {
		statAuditFile, renameAuditFile, removeAuditFile = stat, rename, remove
	})
}

func TestAuditLogRotatesAtThreshold(t *testing.T) {
	withSmallAuditLog(t, 200)
	path := filepath.Join(t.TempDir(), "mutations.log")
	auditor := NewAuditor(path)

	// Each record is well under the limit, so several land in the active
	// log before it is rotated aside.
	for i := 0; i < 12; i++ {
		if err := auditor.Write(AuditRecord{Tool: "corral_sync_repo", Target: "repo", Result: "ok"}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active log missing after rotation: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected a rotated generation at %s.1: %v", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxAuditLogBytes {
		t.Fatalf("active log is %d bytes, above the %d-byte bound", info.Size(), maxAuditLogBytes)
	}
}

func TestAuditLogKeepsBoundedGenerations(t *testing.T) {
	withSmallAuditLog(t, 120)
	path := filepath.Join(t.TempDir(), "mutations.log")
	auditor := NewAuditor(path)

	// Enough writes to rotate well past the retention window.
	for i := 0; i < 60; i++ {
		if err := auditor.Write(AuditRecord{Tool: "corral_delete_repo", Target: "repo", Result: "ok"}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	for gen := 1; gen <= auditLogGenerations; gen++ {
		name := path + "." + string(rune('0'+gen))
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("expected retained generation %s: %v", name, err)
		}
	}
	beyond := path + "." + string(rune('0'+auditLogGenerations+1))
	if _, err := os.Stat(beyond); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation %s should have been discarded, stat err = %v", beyond, err)
	}
}

func TestAuditLogRotationPreservesRecords(t *testing.T) {
	withSmallAuditLog(t, 150)
	path := filepath.Join(t.TempDir(), "mutations.log")
	auditor := NewAuditor(path)

	for _, target := range []string{"alpha", "beta", "gamma", "delta"} {
		if err := auditor.Write(AuditRecord{Tool: "corral_clone_repo", Target: target, Result: "ok"}); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}

	// The rotated generations plus the active log must still hold every
	// record: rotation moves history aside, it never drops it inside the
	// retention window.
	var combined strings.Builder
	for gen := auditLogGenerations; gen >= 1; gen-- {
		if body, err := os.ReadFile(path + "." + string(rune('0'+gen))); err == nil {
			combined.Write(body)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	combined.Write(body)

	for _, target := range []string{"alpha", "beta", "gamma", "delta"} {
		if !strings.Contains(combined.String(), `"target":"`+target+`"`) {
			t.Fatalf("record for %s lost across rotation; log was:\n%s", target, combined.String())
		}
	}
}

func TestAuditLogNoRotationBelowThreshold(t *testing.T) {
	withSmallAuditLog(t, 1<<20)
	path := filepath.Join(t.TempDir(), "mutations.log")
	auditor := NewAuditor(path)

	if err := auditor.Write(AuditRecord{Tool: "corral_sync_repo", Target: "repo", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rotated a log that was below the threshold, stat err = %v", err)
	}
}

func TestAuditLogRotationStatFailureIsReported(t *testing.T) {
	restoreAuditSeams(t)
	path := filepath.Join(t.TempDir(), "mutations.log")
	statAuditFile = func(string) (os.FileInfo, error) { return nil, errors.New("stat exploded") }

	err := NewAuditor(path).Write(AuditRecord{Tool: "corral_sync_repo", Result: "ok"})
	if err == nil || !strings.Contains(err.Error(), "stat audit log") {
		t.Fatalf("expected a stat failure to surface, got %v", err)
	}
}

func TestAuditLogRotationRenameFailureIsReported(t *testing.T) {
	restoreAuditSeams(t)
	withSmallAuditLog(t, 1)
	path := filepath.Join(t.TempDir(), "mutations.log")
	auditor := NewAuditor(path)

	// Seed a log so the next write must rotate.
	if err := auditor.Write(AuditRecord{Tool: "corral_sync_repo", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	renameAuditFile = func(string, string) error { return errors.New("rename exploded") }

	err := auditor.Write(AuditRecord{Tool: "corral_sync_repo", Result: "ok"})
	if err == nil || !strings.Contains(err.Error(), "rotate audit log") {
		t.Fatalf("expected a rename failure to surface, got %v", err)
	}
}

func TestAuditLogRotationRemoveFailureIsReported(t *testing.T) {
	restoreAuditSeams(t)
	withSmallAuditLog(t, 1)
	path := filepath.Join(t.TempDir(), "mutations.log")
	auditor := NewAuditor(path)

	if err := auditor.Write(AuditRecord{Tool: "corral_sync_repo", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	removeAuditFile = func(string) error { return errors.New("remove exploded") }

	err := auditor.Write(AuditRecord{Tool: "corral_sync_repo", Result: "ok"})
	if err == nil || !strings.Contains(err.Error(), "remove oldest audit log") {
		t.Fatalf("expected a remove failure to surface, got %v", err)
	}
}
