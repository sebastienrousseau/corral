// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// MIME types the corral resources advertise. Pinned constants keep the
// resource registration and the handlers in sync without typos drifting.
const (
	mimeJSON     = "application/json"
	mimeMarkdown = "text/markdown"
	mimePlain    = "text/plain"
)

// maxFileBytes caps the size of any single file the file-resource
// will return. A misconfigured agent that asks for a multi-GB log
// file shouldn't be able to OOM the host. 1 MiB matches the upper
// bound documented for VS Code MCP clients and is plenty for source.
const maxFileBytes = 1 << 20

// registerResources attaches the v0 resource set (one static index +
// three URI templates) to the underlying MCP server. URI scheme is
// `corral://` per the design doc; templated paths use RFC 6570
// expansion (handled by mcp-go via the github.com/yosida95/uritemplate
// dependency it already pulls in).
func (s *Server) registerResources() {
	s.mcp.AddResource(s.workspaceIndexResource())
	s.mcp.AddResourceTemplate(s.repoStateResource())
	s.mcp.AddResourceTemplate(s.repoTreeResource())
	s.mcp.AddResourceTemplate(s.repoFileResource())
}

// workspaceIndexResource is the only static resource. It mirrors the
// corral_workspace_index tool, but exposed as a resource so clients
// that prefer to subscribe (rather than call tools) get the same data.
func (s *Server) workspaceIndexResource() (mcp.Resource, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)) {
	r := mcp.NewResource(
		"corral://workspace/index",
		"Workspace index",
		mcp.WithResourceDescription("Full JSON index of every clone in the Corral workspace. Mirrors the corral_workspace_index tool output."),
		mcp.WithMIMEType(mimeJSON),
	)
	handler := func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		idx, err := s.scan()
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		b, err := json.MarshalIndent(idx, "", "  ")
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: mimeJSON,
				Text:     string(b),
			},
		}, nil
	}
	return r, handler
}

// repoStateResource exposes the on-disk .corral-state.json sidecar for
// a single clone via a URI template. Returns 404-equivalent (error)
// when the repo or sidecar isn't found, so clients can distinguish
// "no such repo" from "no sync yet" by the error text.
func (s *Server) repoStateResource() (mcp.ResourceTemplate, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)) {
	r := mcp.NewResourceTemplate(
		"corral://repo/{owner}/{name}/state",
		"Repository sync state",
		mcp.WithTemplateDescription("Parsed .corral-state.json sidecar for a single clone: last upstream pushed_at and last local sync timestamp."),
		mcp.WithTemplateMIMEType(mimeJSON),
	)
	handler := func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		repo, err := s.resolveURIRepo(req.Params.URI)
		if err != nil {
			return nil, err
		}
		state, ok := readState(repo.Path)
		if !ok {
			return nil, fmt.Errorf("no .corral-state.json sidecar found in %s", repo.RelPath)
		}
		b, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: mimeJSON,
				Text:     string(b),
			},
		}, nil
	}
	return r, handler
}

// repoTreeResource returns a top-level file/directory listing for one
// clone, scoped to two-deep entries (the agent's first orientation pass
// rarely needs more than that, and a deep listing of a large repo would
// blow the response budget). Bigger walks go through follow-up tool
// calls in later phases.
func (s *Server) repoTreeResource() (mcp.ResourceTemplate, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)) {
	r := mcp.NewResourceTemplate(
		"corral://repo/{owner}/{name}/tree",
		"Repository top-level tree",
		mcp.WithTemplateDescription("Shallow (two levels) directory listing for a single clone. Use the corral://repo/{owner}/{name}/file/{path} resource to read individual file contents."),
		mcp.WithTemplateMIMEType(mimePlain),
	)
	handler := func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		repo, err := s.resolveURIRepo(req.Params.URI)
		if err != nil {
			return nil, err
		}
		var lines []string
		err = filepath.WalkDir(repo.Path, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			rel, _ := filepath.Rel(repo.Path, path)
			if rel == "." {
				return nil
			}
			depth := strings.Count(rel, string(filepath.Separator))
			if depth >= 2 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			// Hide the .git internals — the agent doesn't need them and
			// they overwhelm the listing.
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			suffix := ""
			if d.IsDir() {
				suffix = "/"
			}
			lines = append(lines, filepath.ToSlash(rel)+suffix)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: mimePlain,
				Text:     strings.Join(lines, "\n"),
			},
		}, nil
	}
	return r, handler
}

// repoFileResource reads one file inside a clone. This is the highest-
// security-impact resource in v0: a path-traversal bug here would let
// an agent escape the workspace root. The handler validates the
// resolved path is still under the configured Root via Index.SafePath
// before opening the file, and bounds the read at maxFileBytes.
func (s *Server) repoFileResource() (mcp.ResourceTemplate, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)) {
	r := mcp.NewResourceTemplate(
		"corral://repo/{owner}/{name}/file/{path}",
		"Repository file contents",
		mcp.WithTemplateDescription("Read a single file from a clone, bounded at 1 MiB. The {path} segment is relative to the repository root and is validated against the configured server root to prevent directory traversal."),
		mcp.WithTemplateMIMEType(mimePlain),
	)
	handler := func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		repo, err := s.resolveURIRepo(req.Params.URI)
		if err != nil {
			return nil, err
		}
		path, err := extractFilePath(req.Params.URI)
		if err != nil {
			return nil, err
		}
		idx := &Index{Root: s.opts.Root}
		// Compose the candidate from the repo path so the traversal check
		// uses the workspace root, not the per-repo root.
		candidate := filepath.Join(repo.Path, path)
		safe, err := idx.SafePath(candidate)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(safe) // #nosec G304 -- SafePath enforces the workspace sandbox
		if err != nil {
			return nil, fmt.Errorf("open file: %w", err)
		}
		defer func() { _ = f.Close() }()

		limited := io.LimitReader(f, maxFileBytes+1)
		body, err := io.ReadAll(limited)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		truncated := false
		if int64(len(body)) > maxFileBytes {
			body = body[:maxFileBytes]
			truncated = true
		}
		mime := guessMIME(safe)
		text := string(body)
		if truncated {
			text += "\n\n[corral-mcp: truncated at 1 MiB]\n"
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: mime,
				Text:     text,
			},
		}, nil
	}
	return r, handler
}

// resolveURIRepo parses owner+name out of a corral:// URI and returns
// the matching RepoEntry. Returns an error when the URI is malformed
// or the repo isn't in the index — both are surfaced to the agent.
//
// URIs are expected to look like:
//
//	corral://repo/{owner}/{name}/state
//	corral://repo/{owner}/{name}/tree
//	corral://repo/{owner}/{name}/file/{path}
//
// url.Parse treats "repo" as the Host and the rest as Path, so this
// concatenates them via path.Join semantics to avoid the double-slash
// pitfall that broke the first naive implementation.
func (s *Server) resolveURIRepo(uri string) (*RepoEntry, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid uri: %w", err)
	}
	if u.Scheme != "corral" {
		return nil, fmt.Errorf("unsupported scheme %q (want corral)", u.Scheme)
	}
	combined := strings.Trim(u.Host, "/") + "/" + strings.TrimPrefix(u.Path, "/")
	combined = strings.Trim(combined, "/")
	parts := strings.Split(combined, "/")
	if len(parts) > 0 && parts[0] == "repo" {
		parts = parts[1:]
	}
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("uri %q is missing owner/name segments", uri)
	}
	owner, name := parts[0], parts[1]

	idx, err := s.scan()
	if err != nil {
		return nil, err
	}
	for i := range idx.Repos {
		r := &idx.Repos[i]
		if r.Name != name {
			continue
		}
		if strings.EqualFold(r.Visibility, owner) ||
			strings.EqualFold(firstSegment(r.RelPath), owner) ||
			ownerMatchesURL(r.RemoteURL, owner) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("no repository %s/%s in workspace", owner, name)
}

// ownerMatchesURL reports whether owner equals ANY namespace segment
// preceding the repository name in remoteURL. This matters for
// GitLab-style / Gitea-style / self-hosted layouts with nested groups
// where an origin URL like https://git.example.com/parent/team/repo.git
// should match agent queries against both "parent" and "team". For
// standard GitHub URLs (https://github.com/owner/repo) the namespace
// list is a single element and behaviour is unchanged.
func ownerMatchesURL(remoteURL, owner string) bool {
	if remoteURL == "" || owner == "" {
		return false
	}
	for _, seg := range parseOwnerFromURL(remoteURL) {
		if strings.EqualFold(seg, owner) {
			return true
		}
	}
	return false
}

// parseOwnerFromURL returns every namespace segment that precedes the
// final repository segment in remoteURL, in order (root-most first).
// Empty slice when the URL can't be parsed. Handles both the HTTPS
// scheme://host/A/B/…/repo form and the SSH user@host:A/B/…/repo form.
// Returned as []string (rather than the previous single-segment form)
// so callers can match against deep hierarchies without losing the
// intermediate names.
func parseOwnerFromURL(remoteURL string) []string {
	if remoteURL == "" {
		return nil
	}
	remoteURL = strings.TrimSuffix(remoteURL, ".git")

	// HTTPS style: https://host/A/B/.../repo
	if strings.Contains(remoteURL, "://") {
		if u, err := url.Parse(remoteURL); err == nil {
			parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
			if len(parts) >= 2 {
				return parts[:len(parts)-1]
			}
		}
	}

	// SSH style: user@host:A/B/.../repo — everything after the first ':'
	// is the path.
	if idx := strings.Index(remoteURL, ":"); idx >= 0 {
		parts := strings.Split(remoteURL[idx+1:], "/")
		if len(parts) >= 2 {
			return parts[:len(parts)-1]
		}
	}
	return nil
}

// extractFilePath pulls the {path} portion out of a
// corral://repo/{owner}/{name}/file/{path} URI. mcp-go's URI template
// matcher doesn't expose captured groups to the handler, so we re-parse.
func extractFilePath(uri string) (string, error) {
	const marker = "/file/"
	idx := strings.Index(uri, marker)
	if idx < 0 {
		return "", fmt.Errorf("uri %q is not a file resource", uri)
	}
	raw := uri[idx+len(marker):]
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("decoding path segment: %w", err)
	}
	if decoded == "" {
		return "", fmt.Errorf("file resource requires a non-empty path")
	}
	return decoded, nil
}

// firstSegment returns the first path component, used as a fallback
// match when a custom layout doesn't carry an explicit Visibility.
func firstSegment(rel string) string {
	if i := strings.Index(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return rel
}

// guessMIME picks a content type from the file extension. Mostly to
// help clients render code with syntax highlighting; not security-
// relevant. Defaults to text/plain for anything unrecognised.
func guessMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return mimeMarkdown
	case ".json":
		return mimeJSON
	default:
		return mimePlain
	}
}
