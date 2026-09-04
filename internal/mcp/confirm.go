// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Per-call authority for destructive mutations.
//
// Until now the only gate on deletion was --enable-destructive-mutations,
// decided once when the process started. After that, every call was
// authorised purely by having been registered: no confirmation, no rate
// limit, no per-repository scoping.
//
// The refusal cascade in handleDeleteRepo is excellent at protecting
// against *mistakes* — it declines when a clone holds uncommitted,
// unpushed, stashed, gitignored or submodule work. It offers nothing
// against a *persuaded* agent that picks the one clean clone, because
// every check passes and the deletion is exactly what was asked for.
//
// The 2026 MCP security guidance is explicit that access control belongs at
// the execution layer rather than in prompt text, and the protocol provides
// the mechanism: elicitation puts the decision in front of a person.
//
// This matters more now than it did over stdio. An HTTP server accepts
// concurrent sessions, so "the user launched this" stops being a proxy for
// "the user wants this particular deletion".
//
// # How the question is asked
//
// Not by calling ServerSession.Elicit. Protocol 2026-07-28 (SEP-2322 /
// SEP-2575) forbids server-initiated requests while a call is being served,
// and the SDK enforces it: Elicit from inside a tool handler returns an
// error rather than reaching the client. The replacement is the multi
// round-trip flow — a handler that needs input returns a result carrying an
// InputRequests map and no content, the client fulfils it, and the handler
// is invoked *again* with the answers in Params.InputResponses.
//
// So confirmation is a two-pass handler, not a blocking prompt, and every
// safety check runs on both passes. That is stronger than a mutex held
// across the decision would have been: the clone is re-examined after the
// person answers, so work that appeared while they were deciding is seen.
// It also means the handler must stay free of side effects until the
// approved pass, which it is — nothing is written before beginMutation.
//
// Clients too old for the multi round-trip flow are not left out: the SDK
// installs a server-side middleware that fulfils the input requests over
// the classic elicitation request and re-invokes the handler, so the same
// code serves both.

// confirmRequestID is the server-assigned key for the deletion prompt.
//
// Namespaced because the map is shared with whatever else a future handler
// asks for in the same round trip, and the client echoes the keys back
// verbatim.
const confirmRequestID = "corral.confirm-delete"

// confirmDecision is the state of a confirmation for one call.
type confirmDecision int

const (
	// confirmApproved: a person accepted, on a previous pass.
	confirmApproved confirmDecision = iota
	// confirmDenied: a person declined, or dismissed the prompt without
	// choosing. Dismissal is not consent.
	confirmDenied
	// confirmAsk: nobody has been asked yet. The caller must return the
	// accompanying result unchanged so the client can fulfil it.
	confirmAsk
	// confirmUnavailable: there is nobody to ask, so nobody has approved.
	confirmUnavailable
)

// confirmer decides whether a destructive operation has a person behind it.
//
// An interface rather than a direct call so tests can drive every outcome —
// approved, declined, unaskable — without a live client on the other end,
// and so the two-pass flow can be asserted independently of the protocol
// plumbing that implements it.
type confirmer interface {
	// Confirm reports the state of the confirmation for this call. The
	// returned result is non-nil only for confirmAsk, and must be returned
	// to the client exactly as given.
	Confirm(req *mcp.CallToolRequest, summary, detail string) (confirmDecision, *mcp.CallToolResult)
}

// elicitConfirmer asks over the MCP elicitation channel, via the multi
// round-trip flow.
type elicitConfirmer struct{}

// noElicitationMessage explains a refusal that happened because nobody could
// be asked, rather than because somebody said no. The distinction matters:
// the first is a configuration problem with a fix, the second is an answer.
const noElicitationMessage = "this client does not support elicitation, so the deletion cannot be confirmed by a person"

// Confirm implements confirmer.
func (elicitConfirmer) Confirm(req *mcp.CallToolRequest, summary, detail string) (confirmDecision, *mcp.CallToolResult) {
	if req == nil || req.Session == nil {
		return confirmUnavailable, nil
	}
	// A client that never declared the capability cannot answer. Checking
	// here rather than asking and failing keeps the refusal a tool error
	// the model can read, instead of a protocol error raised by the
	// middleware several layers up.
	init := req.Session.InitializeParams()
	if init == nil || init.Capabilities == nil || init.Capabilities.Elicitation == nil {
		return confirmUnavailable, nil
	}

	// Second pass: the answer is in the retried call.
	if req.Params != nil {
		if resp, ok := req.Params.InputResponses[confirmRequestID]; ok {
			res, ok := resp.(*mcp.ElicitResult)
			// Only an explicit accept is approval. "cancel" — the user
			// dismissed the prompt without choosing — is not consent, and
			// neither is a response of a shape we did not ask for.
			if !ok || res == nil || res.Action != "accept" {
				return confirmDenied, nil
			}
			return confirmApproved, nil
		}
	}

	// First pass: ask. The result carries no content, which is required —
	// content and inputRequests are mutually exclusive on the wire.
	//
	// A form with no requested schema. The protocol has no dedicated
	// confirmation mode; a schemaless form is the shape that means "there
	// is nothing to fill in, only something to agree to", and its three
	// actions — accept, decline, cancel — are exactly the answers a
	// confirmation has.
	return confirmAsk, &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			confirmRequestID: &mcp.ElicitParams{
				Mode:    "form",
				Message: summary + "\n\n" + detail,
			},
		},
	}
}

// repoLocks serialises mutations per repository path.
//
// Over stdio there was one session and one request at a time, so this was
// unnecessary. Over HTTP there are neither: two sessions can reach
// handleDeleteRepo for the same clone concurrently, and the window between
// the safety checks and the removal — acknowledged as a race even in the
// single-session case — becomes wide enough to matter.
//
// Locking per path rather than globally keeps unrelated repositories
// parallel, which is the common case for an agent sweeping a workspace.
type repoLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newRepoLocks() *repoLocks {
	return &repoLocks{locks: map[string]*sync.Mutex{}}
}

// lock returns the mutex guarding path, creating it on first use.
//
// The map itself is never pruned. Entries are one mutex per repository ever
// mutated in a session, which is bounded by the workspace and orders of
// magnitude smaller than the index already held in memory; reclaiming them
// would need reference counting to avoid freeing a lock somebody holds,
// which is a great deal of machinery for a few hundred bytes.
func (r *repoLocks) lock(path string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.locks[path]
	if !ok {
		m = &sync.Mutex{}
		r.locks[path] = m
	}
	return m
}
