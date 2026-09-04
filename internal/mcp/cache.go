// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Cache hints (protocol 2026-07-28).
//
// The protocol lets a server tell a client how long a result stays fresh
// and who may keep it — `ttlMs` and `cacheScope`, with the same meaning as
// HTTP's max-age and public/private. Without them a client re-fetches
// everything on every turn, which for this server means re-listing tools
// that cannot have changed and re-reading a workspace index that is
// itself already cached behind a five-second TTL.
//
// # Why this is a middleware rather than a field on each handler
//
// The list results — tools, prompts, resources, resource templates — are
// built by the SDK, not by any handler here, so there is no per-handler
// place to set them. A receiving middleware sees every result on its way
// out and is the one location where the policy can be stated once and
// audited.
//
// Receiving, not sending: in this SDK "sending" middleware wraps requests
// the *server* initiates, while a client's tools/list arrives through the
// receiving chain. It also has to be receiving for a second reason — the
// SDK stamps its own defaults ("public", no TTL) inside those handlers,
// so a middleware that did not wrap them would be overwritten by them.
//
// # The two policies, and why they differ
//
// Listings describe this server's own surface. It is fixed when the
// process starts — tools are registered in NewServer and never added or
// removed — so a client that caches a listing for a minute cannot be
// wrong, and "public" is honest because the answer does not depend on who
// asked.
//
// Resource reads describe the user's machine: which repositories exist,
// what is in them, what branch they are on. Two things follow. The TTL
// matches the workspace scan's own, because promising freshness the
// server does not itself maintain would be a lie a client acts on. And
// the scope is "private", because the content is one developer's
// workspace and an intermediary caching it for anyone else is exactly
// what that flag exists to prevent.

// listingTTL is how long a client may treat a listing as fresh.
//
// A minute rather than an hour: the tool set is fixed for the process, but
// the process is not immortal, and a client holding a stale listing across
// a restart that changed the flags would call a tool that no longer
// exists. A minute is long enough to remove the per-turn re-listing, short
// enough that a restart converges quickly.
const listingTTL = time.Minute

// cacheHintMiddleware stamps freshness hints on the results that carry
// them.
//
// Results that are not cacheable pass through untouched — a tool call
// among them, deliberately: a call can have side effects, and there is no
// TTL at which it is safe to reuse the answer to one.
func cacheHintMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || res == nil {
				return res, err
			}
			switch r := res.(type) {
			case *mcp.ListToolsResult:
				r.Cacheable = listingCacheable()
			case *mcp.ListPromptsResult:
				r.Cacheable = listingCacheable()
			case *mcp.ListResourcesResult:
				r.Cacheable = listingCacheable()
			case *mcp.ListResourceTemplatesResult:
				r.Cacheable = listingCacheable()
			case *mcp.ReadResourceResult:
				r.Cacheable = workspaceCacheable()
			}
			return res, err
		}
	}
}

// listingCacheable is the hint for this server's own surface.
func listingCacheable() mcp.Cacheable {
	return mcp.Cacheable{
		TTLMs: int(listingTTL / time.Millisecond),
		// The tool set does not depend on who asked, so anyone may keep
		// it.
		CacheScope: "public",
	}
}

// workspaceCacheable is the hint for anything describing the user's
// machine.
func workspaceCacheable() mcp.Cacheable {
	return mcp.Cacheable{
		// Exactly the server's own scan TTL. Claiming longer would promise
		// a freshness this server does not maintain, and a client would
		// act on the promise.
		TTLMs: int(scanTTL / time.Millisecond),
		// A developer's workspace. An intermediary caching this and
		// serving it to somebody else is the thing "private" exists to
		// prevent.
		CacheScope: "private",
	}
}
