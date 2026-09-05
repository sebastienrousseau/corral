// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package search

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if any test leaves a goroutine running.
//
// SearchRepo starts a worker per file up to a cap and relies on three
// separate conditions to stop them: the path list running out, the context
// being cancelled, and another worker setting hitsCap. A worker that missed
// one of those would sit in its loop forever, and the only visible symptom
// would be a test binary that used more memory than it should — nothing
// fails, because wg.Wait() would still return for every worker that did
// finish.
//
// goleak turns that into a failure. It runs after every test in the package
// has completed, so a goroutine still alive at that point had no reason to
// be.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
