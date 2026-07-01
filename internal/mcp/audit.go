package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditRecord is one entry in the mutation audit log. It captures enough
// to reconstruct what an agent did after the fact — what tool was
// invoked, what arguments it received, which repository was affected,
// what the outcome was, and when. The fields are deliberately flat and
// JSON-line encoded so `jq` and grep-style tools work naturally.
type AuditRecord struct {
	// Timestamp is the moment the audited operation completed, RFC 3339 UTC.
	Timestamp string `json:"ts"`
	// Tool is the MCP tool name that triggered the mutation
	// (e.g. "corral_clone_repo").
	Tool string `json:"tool"`
	// Target is the repo path or clone URL the operation acted on. The
	// exact meaning depends on the tool — sync/delete write a path,
	// clone writes the source URL — but each record documents its scope.
	Target string `json:"target"`
	// Args captures the tool's structured input so a reviewer can replay
	// the mutation. Kept as a raw map to avoid pinning a specific schema
	// per tool at this layer.
	Args map[string]any `json:"args,omitempty"`
	// Result is "ok" on success or a short human-readable reason on
	// refusal/failure. The full error message goes to Message.
	Result string `json:"result"`
	// Message is a human-readable outcome; blank on clean success.
	Message string `json:"message,omitempty"`
}

// Auditor writes AuditRecord entries to an append-only JSONL log. The
// log path defaults to $XDG_STATE_HOME/corral/mutations.log (falling
// back to ~/.local/state/corral/mutations.log per the XDG spec).
// Concurrent Write calls are serialised by an internal mutex — the
// mutation tools are called at most a few times per second in
// practice, so the lock contention is negligible.
type Auditor struct {
	path string
	mu   sync.Mutex
}

// NewAuditor constructs an Auditor writing to the given path. When path
// is empty, DefaultAuditLogPath is used. The parent directory is
// created with 0o700 on first write so the log file itself is only
// visible to the running user; audit records may contain repo paths
// that leak intent about a developer's workspace layout.
func NewAuditor(path string) *Auditor {
	if path == "" {
		path = DefaultAuditLogPath()
	}
	return &Auditor{path: path}
}

// DefaultAuditLogPath returns the platform-default audit log location.
// Follows the XDG Base Directory spec: $XDG_STATE_HOME/corral/mutations.log
// with the documented fallback to ~/.local/state/corral/mutations.log
// when XDG_STATE_HOME is unset. Rooted at HOME so a system-wide
// deployment doesn't accidentally share audit logs across users.
func DefaultAuditLogPath() string {
	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "corral", "mutations.log")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Rare — home dir is always resolvable on any supported platform.
		// If it truly isn't, tempdir is a defensible last resort that at
		// least keeps the log local to the running session.
		return filepath.Join(os.TempDir(), "corral", "mutations.log")
	}
	return filepath.Join(home, ".local", "state", "corral", "mutations.log")
}

// Write appends one AuditRecord to the log. Any error is returned so
// callers can decide whether to fail the tool call or continue — the
// mutation tools in this package treat audit failures as fatal because
// a mutation without a durable record defeats the purpose of the
// audit mechanism.
func (a *Auditor) Write(r AuditRecord) error {
	if r.Timestamp == "" {
		r.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}
	line = append(line, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write audit record: %w", err)
	}
	return nil
}

// Path returns the log path the Auditor is writing to. Exposed for the
// server startup banner and for tests that need to read the log back.
func (a *Auditor) Path() string { return a.path }
