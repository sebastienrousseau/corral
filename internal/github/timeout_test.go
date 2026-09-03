// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package github

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNormalizeSplitsTheDeprecatedTimeout covers the migration path: a
// caller that set only the old single knob must keep the behaviour it had,
// and a caller that sets the new fields must not have them overwritten.
func TestNormalizeSplitsTheDeprecatedTimeout(t *testing.T) {
	tests := []struct {
		name                   string
		in                     FetchOptions
		wantRequest, wantTotal time.Duration
	}{
		{
			name:        "nothing set uses both defaults",
			in:          FetchOptions{},
			wantRequest: defaultRequestTimeout,
			wantTotal:   defaultTotalTimeout,
		},
		{
			name:        "deprecated Timeout supplies both halves",
			in:          FetchOptions{Timeout: 45 * time.Second},
			wantRequest: 45 * time.Second,
			wantTotal:   45 * time.Second,
		},
		{
			name:        "explicit fields win over the deprecated one",
			in:          FetchOptions{Timeout: 45 * time.Second, RequestTimeout: 5 * time.Second, TotalTimeout: time.Hour},
			wantRequest: 5 * time.Second,
			wantTotal:   time.Hour,
		},
		{
			name:        "deprecated fills only the half left unset",
			in:          FetchOptions{Timeout: 45 * time.Second, TotalTimeout: time.Hour},
			wantRequest: 45 * time.Second,
			wantTotal:   time.Hour,
		},
		{
			name:        "request only still gets a sane total",
			in:          FetchOptions{RequestTimeout: 90 * time.Second},
			wantRequest: 90 * time.Second,
			wantTotal:   defaultTotalTimeout,
		},
		{
			name: "a total smaller than one request is raised, not accepted",
			// The first request would otherwise outlive the operation that
			// contains it, which cannot be what the caller meant.
			in:          FetchOptions{RequestTimeout: 2 * time.Minute, TotalTimeout: time.Second},
			wantRequest: 2 * time.Minute,
			wantTotal:   2 * time.Minute,
		},
		{
			name:        "negative values fall back to the defaults",
			in:          FetchOptions{RequestTimeout: -1, TotalTimeout: -1},
			wantRequest: defaultRequestTimeout,
			wantTotal:   defaultTotalTimeout,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeFetchOptions(tc.in)
			if got.RequestTimeout != tc.wantRequest {
				t.Errorf("RequestTimeout = %s, want %s", got.RequestTimeout, tc.wantRequest)
			}
			if got.TotalTimeout != tc.wantTotal {
				t.Errorf("TotalTimeout = %s, want %s", got.TotalTimeout, tc.wantTotal)
			}
		})
	}
}

// TestDefaultTotalFitsALargeListing is the regression this whole change
// exists for. The single 30s budget covered every page and every backoff,
// so an organisation needing 50 pages could not be listed at all.
func TestDefaultTotalFitsALargeListing(t *testing.T) {
	opts := normalizeFetchOptions(FetchOptions{})
	// 50 pages at, pessimistically, 2s each, plus the full retry schedule.
	const pessimistic = 50 * 2 * time.Second
	if opts.TotalTimeout <= pessimistic {
		t.Fatalf("TotalTimeout %s does not fit a 50-page listing (%s)", opts.TotalTimeout, pessimistic)
	}
	if opts.TotalTimeout <= opts.RequestTimeout {
		t.Fatalf("TotalTimeout %s must exceed RequestTimeout %s", opts.TotalTimeout, opts.RequestTimeout)
	}
}

func TestRetryBudgetErrorMessage(t *testing.T) {
	rate := &RetryBudgetError{Wait: 40 * time.Minute, Remaining: 90 * time.Second, Status: http.StatusForbidden}
	msg := rate.Error()
	for _, want := range []string{"rate-limited", "40m0s", "1m30s", "--api-total-timeout"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}

	if got := (&RetryBudgetError{Wait: time.Minute, Remaining: time.Second, Status: http.StatusTooManyRequests}).Error(); !strings.Contains(got, "rate-limited") {
		t.Errorf("429 should read as a rate limit: %q", got)
	}
	// Anything else is a generic back-off, not a rate limit.
	if got := (&RetryBudgetError{Wait: time.Minute, Remaining: time.Second, Status: http.StatusServiceUnavailable}).Error(); strings.Contains(got, "rate-limited") {
		t.Errorf("503 should not claim a rate limit: %q", got)
	}
	if got := (&RetryBudgetError{Wait: time.Minute, Remaining: time.Second}).Error(); !strings.Contains(got, "back off") {
		t.Errorf("a transport error should read as a back-off: %q", got)
	}
}

func TestStatusOf(t *testing.T) {
	if got := statusOf(nil); got != 0 {
		t.Errorf("statusOf(nil) = %d, want 0", got)
	}
	if got := statusOf(&http.Response{StatusCode: http.StatusTeapot}); got != http.StatusTeapot {
		t.Errorf("statusOf = %d, want %d", got, http.StatusTeapot)
	}
}

// rateLimitedTransport always answers with a rate-limit response whose
// reset is far in the future.
type rateLimitedTransport struct {
	resetIn time.Duration
	calls   int
}

func (r *rateLimitedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(r.resetIn).Unix(), 10))
	return &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     h,
		Body:       http.NoBody,
	}, nil
}

// TestRetryReportsAnUnwaitableRateLimit is the ARCH-2 branch: a reset
// further out than the remaining budget used to be slept on anyway, so it
// always lost the race and the caller got a bare deadline error naming no
// cause. It now reports immediately, while the reason is still known.
func TestRetryReportsAnUnwaitableRateLimit(t *testing.T) {
	rt := &rateLimitedTransport{resetIn: time.Hour}
	transport := &retryTransport{base: rt, maxRetries: 4, minBackoff: time.Millisecond, maxBackoff: time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = transport.RoundTrip(req)
	elapsed := time.Since(start)

	var budget *RetryBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("expected a *RetryBudgetError, got %v", err)
	}
	// It must return promptly rather than sleeping out the deadline.
	if elapsed > time.Second {
		t.Errorf("took %s to report an unwaitable wait; should be immediate", elapsed)
	}
	if budget.Wait < 55*time.Minute {
		t.Errorf("Wait = %s, want roughly an hour", budget.Wait)
	}
	if rt.calls != 1 {
		t.Errorf("made %d requests; should stop after the first refusal", rt.calls)
	}
}

// TestRetryWaitsWhenTheBudgetAllows is the other half: a short reset inside
// the budget is still waited out, which is the behaviour that was
// unreachable before the split.
func TestRetryWaitsWhenTheBudgetAllows(t *testing.T) {
	rt := &rateLimitedTransport{resetIn: 10 * time.Millisecond}
	transport := &retryTransport{base: rt, maxRetries: 2, minBackoff: time.Millisecond, maxBackoff: time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("a waitable reset should not error: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// maxRetries=2 means the initial attempt plus two retries.
	if rt.calls != 3 {
		t.Errorf("made %d requests, want 3 (initial + 2 retries)", rt.calls)
	}
}

// TestRetryWithoutADeadlineStillRetries covers the branch where the request
// carries no deadline at all, so there is no budget to compare against.
func TestRetryWithoutADeadlineStillRetries(t *testing.T) {
	rt := &rateLimitedTransport{resetIn: time.Millisecond}
	transport := &retryTransport{base: rt, maxRetries: 1, minBackoff: time.Millisecond, maxBackoff: time.Millisecond}

	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil) //nolint:noctx // deadline-free is the case under test
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if rt.calls != 2 {
		t.Errorf("made %d requests, want 2 (initial + 1 retry)", rt.calls)
	}
}
