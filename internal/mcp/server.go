package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

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
	// EnableMutations gates the write-tools (Phase 3 of the design doc).
	// In Phase 1 no mutation tools are registered, so this field is
	// reserved for forward-compatibility and ignored today.
	EnableMutations bool
}

// Server wraps an mcp-go MCPServer with the corral-specific configuration.
// Exposed as a struct (rather than handing back the bare *server.MCPServer)
// so future phases can attach per-server state (search backends, audit
// logger, etc.) without breaking the cmd-layer call site.
type Server struct {
	mcp  *server.MCPServer
	opts ServerOptions
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
		server.WithRecovery(),
	)

	s := &Server{mcp: mcpSrv, opts: opts}
	s.registerTools()
	s.registerResources()
	return s, nil
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
