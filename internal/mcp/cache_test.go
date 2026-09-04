// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestListingsCarryAFreshnessHint: without one, a client re-lists tools on
// every turn, and this server's tool set is fixed when the process starts.
func TestListingsCarryAFreshnessHint(t *testing.T) {
	h := newHarness(t, ServerOptions{Root: t.TempDir()})
	ctx := context.Background()

	tools, err := h.session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tools.TTLMs != int(listingTTL/time.Millisecond) {
		t.Errorf("tools TTL = %d ms, want %d", tools.TTLMs, int(listingTTL/time.Millisecond))
	}
	// The tool set does not depend on who asked.
	if tools.CacheScope != "public" {
		t.Errorf("tools cache scope = %q, want public", tools.CacheScope)
	}

	prompts, err := h.session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prompts.TTLMs == 0 {
		t.Error("a prompt listing is as stable as a tool listing")
	}

	resources, err := h.session.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resources.TTLMs == 0 {
		t.Error("a resource listing is as stable as a tool listing")
	}

	templates, err := h.session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if templates.TTLMs == 0 {
		t.Error("a resource-template listing is as stable as a tool listing")
	}
}

// TestWorkspaceReadsArePrivateAndMatchTheScanTTL is the honesty property:
// the server may not promise a freshness it does not itself maintain, and
// may not invite an intermediary to share one developer's workspace.
func TestWorkspaceReadsArePrivateAndMatchTheScanTTL(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/acme/alpha.git", "")
	h := newHarness(t, ServerOptions{Root: base})

	res, err := h.session.ReadResource(context.Background(),
		&mcp.ReadResourceParams{URI: "corral://workspace/index"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TTLMs != int(scanTTL/time.Millisecond) {
		t.Errorf("TTL = %d ms, want the scan TTL of %d ms", res.TTLMs, int(scanTTL/time.Millisecond))
	}
	// A developer's workspace is not something an intermediary may serve
	// to somebody else.
	if res.CacheScope != "private" {
		t.Errorf("cache scope = %q, want private", res.CacheScope)
	}
}

// TestToolCallsAreNotCacheable: a call can have side effects, and there is
// no TTL at which reusing the answer to one is safe. The protocol does not
// offer the fields on a call result, and the middleware must not invent
// them.
func TestToolCallsAreNotCacheable(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/acme/alpha.git", "")
	h := newHarness(t, ServerOptions{Root: base})

	res, err := h.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "corral_status_summary",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	// A CallToolResult has no cache fields at all; this asserts the type
	// has not grown them behind a middleware that would then be silently
	// stamping calls.
	var _ mcp.Result = res
	if _, ok := any(res).(mcp.CacheableResult); ok {
		t.Error("a tool result must not advertise itself as cacheable")
	}
}

// TestCacheMiddlewarePassesErrorsThrough: a middleware that swallowed an
// error, or that stamped a hint on a failed result, would turn a transient
// failure into a cached one.
func TestCacheMiddlewarePassesErrorsThrough(t *testing.T) {
	want := errors.New("downstream failed")
	mw := cacheHintMiddleware()
	handler := mw(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, want
	})
	res, err := handler(context.Background(), "tools/list", nil)
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
	if res != nil {
		t.Errorf("res = %v, want nil", res)
	}
}

// TestCacheMiddlewareIgnoresResultsWithoutTheFields covers the default arm:
// a result type the protocol does not make cacheable passes through
// untouched rather than being special-cased somewhere else later.
func TestCacheMiddlewareIgnoresResultsWithoutTheFields(t *testing.T) {
	mw := cacheHintMiddleware()
	original := &mcp.CallToolResult{}
	handler := mw(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return original, nil
	})
	res, err := handler(context.Background(), "tools/call", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res != mcp.Result(original) {
		t.Error("a non-cacheable result should pass through as-is")
	}

	// And a nil result must not be dereferenced.
	handler = mw(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	if res, err = handler(context.Background(), "tools/call", nil); err != nil || res != nil {
		t.Errorf("nil should pass through: res=%v err=%v", res, err)
	}
}
