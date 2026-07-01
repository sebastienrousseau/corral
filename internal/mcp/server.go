package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ServerName is the public identifier the MCP server advertises to clients.
// Kept stable across versions so clients can match by name rather than
// version. Matches the registry entry slug.
const ServerName = "corral"

// ServerOptions configures a Server. All zero values are valid except Root,
// which must be a non-empty absolute path the server will sandbox itself to.
type ServerOptions struct {
	// Root is the absolute path the server treats as the workspace.
	// All tools and resources reject paths outside this root.
	Root string
	// Version is injected at build time (see cmd.Version); surfaced to
	// MCP clients in the server-info handshake.
	Version string
	// EnableMutations, when true, registers the write-side tools
	// (corral_sync_repo, corral_clone_repo). The read-only tool set is
	// always registered; this flag only unlocks the ones that touch
	// the filesystem or the network.
	EnableMutations bool
	// EnableDestructiveMutations gates corral_delete_repo specifically.
	// A misfiring agent that could delete workspace repos is a class of
	// harm distinct from clone/sync mistakes, so it earns its own opt-in.
	// Ignored unless EnableMutations is also true.
	EnableDestructiveMutations bool
	// AuditLogPath is where the JSONL audit log for every mutation is
	// appended. Empty means use the XDG default
	// ($XDG_STATE_HOME/corral/mutations.log). Only consulted when at
	// least one mutation gate is enabled.
	AuditLogPath string
}

// Server wraps an mcp-go MCPServer with the corral-specific configuration.
// Exposed as a struct (rather than handing back the bare *server.MCPServer)
// so future phases can attach per-server state (search backends, audit
// logger, etc.) without breaking the cmd-layer call site.
type Server struct {
	mcp     *server.MCPServer
	opts    ServerOptions
	auditor *Auditor

	// scanMu guards the in-memory workspace-index cache below.
	// Every tool and resource handler goes through Server.scan(),
	// which walks the filesystem at most once every scanTTL and
	// returns the cached snapshot in between. This trades a small
	// amount of staleness (see scanTTL) for O(1) amortised cost on
	// bursty client sessions where an agent fires 5-10 tool calls
	// in quick succession.
	scanMu      sync.Mutex
	scanIndex   *Index
	scanExpires time.Time
}

// scanTTL is how long a workspace scan is considered fresh. 5s is short
// enough that an agent noticing a just-cloned repo can always retry and
// see it, and long enough to amortise a burst of tool calls from a
// single session. The value is deliberately in-package rather than a
// ServerOptions field: this is v0 policy, not a user knob, and giving
// users control over it invites confusion about staleness bugs.
const scanTTL = 5 * time.Second

// scan returns a cached workspace Index, walking the filesystem only
// when the previous snapshot has expired. Safe for concurrent callers
// (single mutex; the walk itself is not parallel so there is no gain
// from a RWMutex here). On error the cache is not populated and the
// error is propagated so the caller can surface it to the agent.
func (s *Server) scan() (*Index, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if s.scanIndex != nil && time.Now().Before(s.scanExpires) {
		return s.scanIndex, nil
	}
	idx, err := Scan(s.opts.Root)
	if err != nil {
		return nil, err
	}
	s.scanIndex = idx
	s.scanExpires = time.Now().Add(scanTTL)
	return idx, nil
}

// invalidateScanCache drops the cached workspace index so the next
// call to scan() re-walks the filesystem. Used by tests to make
// consecutive assertions against different tree states deterministic
// without waiting scanTTL between them.
func (s *Server) invalidateScanCache() {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	s.scanIndex = nil
	s.scanExpires = time.Time{}
}

// NewServer constructs and configures a Corral MCP server. It registers
// every read-only tool and resource defined in this package. Returns an
// error when ServerOptions are invalid (notably an unset or non-absolute
// Root) so the cmd layer fails fast instead of starting a server that
// would reject every call.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("Root must not be empty")
	}
	if !isAbsolutePath(opts.Root) {
		return nil, fmt.Errorf("Root %q must be an absolute path", opts.Root)
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}

	mcpSrv := server.NewMCPServer(
		ServerName,
		opts.Version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(true, false),
		server.WithPromptCapabilities(false),
		server.WithRecovery(),
	)

	s := &Server{mcp: mcpSrv, opts: opts}
	if opts.EnableMutations || opts.EnableDestructiveMutations {
		s.auditor = NewAuditor(opts.AuditLogPath)
	}
	s.registerTools()
	s.registerResources()
	s.registerPrompts()
	if s.opts.EnableMutations {
		s.registerMutationTools()
	}
	if s.opts.EnableDestructiveMutations && s.opts.EnableMutations {
		s.registerDestructiveTools()
	}
	return s, nil
}

// AuditLogPath returns the audit log path when mutations are enabled;
// empty otherwise. Exposed for the cmd-layer startup banner.
func (s *Server) AuditLogPath() string {
	if s.auditor == nil {
		return ""
	}
	return s.auditor.Path()
}

// ServeStdio runs the server on the stdio transport (the MCP standard
// for local servers). Blocks until stdin closes or the server errors.
// Stdout is reserved for the JSON-RPC protocol stream — any debug logging
// the cmd layer wants to emit must go to stderr.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcp)
}

// Root returns the sandbox root the server was configured with. Useful
// for the cmd layer's startup-log line and for tests.
func (s *Server) Root() string {
	return s.opts.Root
}

// MutationsEnabled reports whether write-tools are unlocked. Read by the
// tool registry at construction time; surfaced via this accessor so
// future phases can also gate behaviour outside the registry path.
func (s *Server) MutationsEnabled() bool {
	return s.opts.EnableMutations
}

// isAbsolutePath checks for an absolute filesystem path without
// importing path/filepath into a hot accessor — the cmd layer also
// validates upstream, so this is belt-and-braces.
func isAbsolutePath(p string) bool {
	if len(p) == 0 {
		return false
	}
	// POSIX-style absolute paths.
	if p[0] == '/' {
		return true
	}
	// Windows-style "C:\..." — accepted so the server can run on
	// developer laptops cross-platform.
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return false
}

// describeRepo is a small formatter used by several tool handlers to
// render a RepoEntry as a human-readable bullet for the text-content
// fallback in CallToolResult. JSON content carries the full structure;
// the text is for clients that surface tool output verbatim.
func describeRepo(r RepoEntry) string {
	parts := []string{r.RelPath}
	if r.Visibility != "" || r.Language != "" {
		parts = append(parts, fmt.Sprintf("[%s/%s]", strings.ToLower(r.Visibility), r.Language))
	}
	if r.RemoteURL != "" {
		parts = append(parts, fmt.Sprintf("(%s)", r.RemoteURL))
	}
	if r.State != nil && r.State.LastSyncedAt != "" {
		parts = append(parts, fmt.Sprintf("last_synced=%s", r.State.LastSyncedAt))
	}
	return strings.Join(parts, " ")
}

// jsonResult marshals payload as the structured content block of a tool
// result, falling back to NewToolResultError if marshalling fails.
// Returns a result + nil error — mcp-go's convention is that tool
// failures travel through the result's IsError flag, not via a Go error.
func jsonResult(payload any) *mcp.CallToolResult {
	b, err := jsonMarshalIndent(payload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("internal: marshal: %v", err))
	}
	return mcp.NewToolResultText(string(b))
}

// jsonMarshalIndent is split out so test code can stub the marshaller if
// it ever needs to assert error-path coverage; otherwise it is just a
// thin wrapper around encoding/json with the indent corral uses
// everywhere else.
var jsonMarshalIndent = func(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
