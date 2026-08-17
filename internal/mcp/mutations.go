// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Mutation tools for the corral MCP server. Only registered when the
// caller opts in via ServerOptions.EnableMutations (and, for
// corral_delete_repo, ServerOptions.EnableDestructiveMutations).
//
// Every handler:
//   1. Validates all inputs against the configured Root sandbox.
//   2. Writes an AuditRecord to the JSONL log BEFORE returning any
//      structured result, so a crash mid-tool still leaves a durable
//      trail of what the agent tried.
//   3. Uses the shared gitClone/gitPull vars from internal/git so the
//      same non-interactive env + auth pipeline that classic corralctl
//      relies on covers the MCP path too.
//
// Refusal is preferred over silent no-op or partial success: the tool
// response's IsError field is set with a concrete reason so the agent
// can adapt its next action rather than assuming the mutation went
// through.

package mcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sebastienrousseau/corral/internal/git"
)

// gitPull / gitClone are indirected through package vars so tests can
// stub the "actually shell out and hit the network" branch. Production
// callers get the real git package functions.
var (
	gitPull            = git.Pull
	gitClone           = git.Clone
	hasUnpushedCommits = git.HasUnpublishedWork
	statMutation       = os.Stat
	mkdirMutation      = os.MkdirAll
	removeMutation     = os.RemoveAll
	markSynced         = markStateSynced
)

var mutationSequence atomic.Uint64

func mutationID() string {
	return fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), mutationSequence.Add(1))
}

func redactCloneURL(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		if parsed.User != nil {
			parsed.User = url.User("REDACTED")
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		return "REDACTED@" + raw[at+1:]
	}
	return raw
}

// registerMutationTools attaches the non-destructive write tools
// (corral_sync_repo, corral_clone_repo) to the underlying MCP server.
// corral_delete_repo lives in registerDestructiveTools so callers can
// grant "may pull and clone" without also granting "may delete."
func (s *Server) registerMutationTools() {
	s.mcp.AddTool(s.syncRepoTool())
	s.mcp.AddTool(s.cloneRepoTool())
}

// registerDestructiveTools attaches corral_delete_repo. Kept in its
// own function so a future grep for "destructive tool registration"
// finds one and only one call site.
func (s *Server) registerDestructiveTools() {
	s.mcp.AddTool(s.deleteRepoTool())
}

// audit writes a mutation record. Failure to audit is a fatal error
// for the surrounding tool call: an unlogged mutation defeats the
// mechanism, so callers should propagate this back to the agent as
// an IsError result.
func (s *Server) audit(rec AuditRecord) error {
	if s.auditor == nil {
		// Should be unreachable — the mutation tools are only registered
		// when the auditor is configured — but returning a clear error
		// beats a silent nil-deref if the wiring ever regresses.
		return fmt.Errorf("mutation attempted with no auditor configured")
	}
	return s.auditor.Write(rec)
}

func (s *Server) beginMutation(rec AuditRecord) (AuditRecord, error) {
	rec.OperationID = mutationID()
	rec.Phase = "intent"
	rec.Result = "pending"
	if err := s.audit(rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func (s *Server) completeMutation(rec AuditRecord, result, message string) error {
	rec.Phase = "completion"
	rec.Result = result
	rec.Message = message
	rec.Timestamp = ""
	return s.audit(rec)
}

func (s *Server) auditRefusal(rec AuditRecord, message string) error {
	rec.OperationID = mutationID()
	return s.completeMutation(rec, "refused", message)
}

// syncRepoTool returns corral_sync_repo. Wraps git.Pull for a resolved
// repo. Preserves the same smart-sync sidecar semantics as the classic
// `corralctl <owner>` sync path — no separate write-through cache to
// maintain. Refuses when the repo is not in the workspace index or
// when the operation cannot be sandboxed to the configured Root.
func (s *Server) syncRepoTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_sync_repo",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithDescription("Run `git pull --rebase --autostash` against one clone in the Corral workspace. Requires --enable-mutations. Reuses the same non-interactive git environment the classic corralctl uses (no credential prompts, no signing pinentry) and honours smart-sync via the .corral-state.json sidecar. Refuses when the repo isn't in the index or resolves outside the configured sandbox root."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Repository identifier: bare name ('corral'), relative path ('Public/go/corral'), or any path suffix that uniquely identifies a repo."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		repo, err := idx.Find(query)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// Belt-and-braces sandbox check — Index.Find already returns
		// only Root-relative repos, but a future refactor might not.
		safe, err := idx.SafeMutationPath(repo.Path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rec := AuditRecord{
			Tool:   "corral_sync_repo",
			Target: safe,
			Args:   map[string]any{"query": query},
		}
		rec, err = s.beginMutation(rec)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("audit intent failed: %v", err)), nil
		}
		pullErr := gitPull(ctx, safe, git.PullOptions{})
		if pullErr != nil {
			if auditErr := s.completeMutation(rec, "error", pullErr.Error()); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("git pull failed: %v; audit completion failed: %v", pullErr, auditErr)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("git pull failed: %v", pullErr)), nil
		}
		if err := markSynced(safe); err != nil {
			if auditErr := s.completeMutation(rec, "error", err.Error()); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("state update failed: %v; audit completion failed: %v", err, auditErr)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("state update failed: %v", err)), nil
		}
		if err := s.completeMutation(rec, "ok", ""); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("audit completion failed: %v", err)), nil
		}
		s.invalidateScanCache()
		return jsonResult(map[string]any{
			"tool":   "corral_sync_repo",
			"repo":   repo.RelPath,
			"result": "synced",
		}), nil
	}
	return tool, handler
}

// cloneRepoTool returns corral_clone_repo. Wraps git.Clone into the
// layout-templated target directory. Refuses if the target already
// exists (never silently overwrites) or if the destination would
// escape the sandbox root.
func (s *Server) cloneRepoTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_clone_repo",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithDescription("Clone a repository into the Corral workspace at a caller-provided path relative to the sandbox root. Requires --enable-mutations. Uses the shared non-interactive git environment; supports optional shallow / single-branch / blobless clones. Refuses when the target already exists or when the resolved path escapes the sandbox root."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The clone URL. HTTPS or SSH; the same auth pipeline classic corralctl uses."),
		),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description("Destination directory relative to the sandbox root, e.g. 'Public/go/mytool'. Must not exist yet."),
		),
		mcp.WithNumber("depth",
			mcp.Description("Optional shallow-clone depth. 0 or unset = full history."),
		),
		mcp.WithBoolean("blobless",
			mcp.Description("Optional partial clone with --filter=blob:none."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := req.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		target, err := req.RequireString("target")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		depth := int(req.GetFloat("depth", 0))
		blobless := req.GetBool("blobless", false)

		idx := &Index{Root: s.opts.Root}
		safeTarget, err := idx.SafeMutationPath(target)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if _, err := statMutation(safeTarget); err == nil {
			return mcp.NewToolResultError(fmt.Sprintf("target %s already exists", safeTarget)), nil
		}

		rec := AuditRecord{
			Tool:   "corral_clone_repo",
			Target: safeTarget,
			Args:   map[string]any{"url": redactCloneURL(url), "target": target, "depth": depth, "blobless": blobless},
		}
		rec, err = s.beginMutation(rec)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("audit intent failed: %v", err)), nil
		}
		if err := mkdirMutation(filepath.Dir(safeTarget), 0o750); err != nil {
			if auditErr := s.completeMutation(rec, "error", err.Error()); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("create target parent: %v; audit completion failed: %v", err, auditErr)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("create target parent: %v", err)), nil
		}
		cloneErr := gitClone(ctx, url, safeTarget, git.CloneOptions{
			Depth:    depth,
			Blobless: blobless,
		})
		if cloneErr != nil {
			if auditErr := s.completeMutation(rec, "error", cloneErr.Error()); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("git clone failed: %v; audit completion failed: %v", cloneErr, auditErr)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("git clone failed: %v", cloneErr)), nil
		}
		if err := s.completeMutation(rec, "ok", ""); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("audit completion failed: %v", err)), nil
		}
		s.invalidateScanCache()
		return jsonResult(map[string]any{
			"tool":   "corral_clone_repo",
			"target": safeTarget,
			"result": "cloned",
		}), nil
	}
	return tool, handler
}

// deleteRepoTool returns corral_delete_repo. This is the highest-risk
// operation the MCP server exposes: it removes a local clone from
// disk. The safeguards are deliberately paranoid:
//
//  1. Requires BOTH EnableMutations and EnableDestructiveMutations
//     to be registered at all.
//  2. Resolves the target via SafeMutationPath so path traversal cannot
//     escape the sandbox.
//  3. Refuses if the working tree has uncommitted changes.
//  4. Refuses if there are unpushed commits on any branch.
//  5. Refuses if the target isn't a git repository at all
//     (defence against typos deleting an unrelated directory).
//  6. Always writes an audit record before removing anything, so a
//     race between the check and the removal is still logged.
func (s *Server) deleteRepoTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_delete_repo",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithDescription("Permanently remove one clone from the Corral workspace. Requires BOTH --enable-mutations and --enable-destructive-mutations. Refuses when uncommitted changes exist, unpushed commits exist, or the target isn't a git repository. Every attempt (successful or refused) is logged to the mutation audit trail."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Repository identifier: bare name, relative path, or any path suffix that uniquely identifies a repo."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		repo, err := idx.Find(query)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		safe, err := idx.SafeMutationPath(repo.Path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rec := AuditRecord{
			Tool:   "corral_delete_repo",
			Target: safe,
			Args:   map[string]any{"query": query},
		}

		// Refusal cascade: any single check failing aborts, audits,
		// and returns to the agent with a specific reason.
		if !git.IsRepository(safe) {
			rec.Message = fmt.Sprintf("target %s is not a git repository", safe)
			if auditErr := s.auditRefusal(rec, rec.Message); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("%s; audit failed: %v", rec.Message, auditErr)), nil
			}
			return mcp.NewToolResultError(rec.Message), nil
		}
		if dirty, out := hasDirtyWorkingTree(ctx, safe); dirty {
			rec.Message = fmt.Sprintf("uncommitted changes present: %s", out)
			if auditErr := s.auditRefusal(rec, rec.Message); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("%s; audit failed: %v", rec.Message, auditErr)), nil
			}
			return mcp.NewToolResultError(rec.Message), nil
		}
		if ahead, out := hasUnpushedCommits(ctx, safe); ahead {
			rec.Message = fmt.Sprintf("unpublished git state present: %s", out)
			if auditErr := s.auditRefusal(rec, rec.Message); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("%s; audit failed: %v", rec.Message, auditErr)), nil
			}
			return mcp.NewToolResultError(rec.Message), nil
		}
		// Gitignored content is the least recoverable thing in a clone — local
		// .env files, databases, caches — and `git status --porcelain` hides it
		// by design, so "clean" was never sufficient grounds to rm -rf.
		if ignored, out := hasIgnoredContent(ctx, safe); ignored {
			rec.Message = fmt.Sprintf("gitignored content present: %s", out)
			if auditErr := s.auditRefusal(rec, rec.Message); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("%s; audit failed: %v", rec.Message, auditErr)), nil
			}
			return mcp.NewToolResultError(rec.Message), nil
		}

		rec, err = s.beginMutation(rec)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("audit intent failed: %v", err)), nil
		}
		if err := removeMutation(safe); err != nil {
			if auditErr := s.completeMutation(rec, "error", err.Error()); auditErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("remove failed: %v; audit completion failed: %v", err, auditErr)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("remove failed: %v", err)), nil
		}
		if err := s.completeMutation(rec, "ok", ""); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("audit completion failed: %v", err)), nil
		}
		s.invalidateScanCache()
		return jsonResult(map[string]any{
			"tool":   "corral_delete_repo",
			"target": safe,
			"result": "deleted",
		}), nil
	}
	return tool, handler
}

// hasDirtyWorkingTree reports tracked, staged or untracked modifications in the
// target repo. Indirected through a package var so tests can stub the dangerous
// "actually run git" path.
//
// This delegates to git.HasLocalChanges rather than shelling out itself. The
// local copy ran `git status --porcelain` a second time (HasUnpublishedWork
// already calls HasLocalChanges first) and, more importantly, skipped
// withGitEnv — so it lacked the non-interactive hardening that stops git
// prompting for credentials and hanging a stdio MCP session.
var hasDirtyWorkingTree = git.HasLocalChanges

// hasIgnoredContent reports gitignored files in the target repo. Indirected for
// the same reason as hasDirtyWorkingTree.
var hasIgnoredContent = git.HasIgnoredContent
