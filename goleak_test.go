package hc

// P8 goleak integration: every test in this package runs under
// goleak.VerifyTestMain, failing the suite on any leaked goroutine —
// the lifecycle and straggler suites exercise exactly the paths that
// would leak (panicking sinks, armed events, future watchdog).
//
// Why goleak AND testing/synctest (see the Agent N section of
// crash_test.go) — deliberately both, not either:
//
//   - goleak is the blanket net: one line covers every test, including
//     tests that forget to opt in, package init, and TestMain itself.
//     It works with t.Run/t.Parallel and with goroutines blocked on
//     real I/O (integration-style tests).
//   - synctest bubbles are opt-in per test: they add per-test exit
//     proofs, durable-deadlock detection, and a virtual clock — but
//     forbid t.Run/t.Parallel, cannot wrap init/TestMain, and cannot
//     host goroutines blocked on non-durable operations (I/O, mutexes).
//     Replacing goleak with bubbles would require wrapping every test,
//     rewriting subtest-style ones, and would still lose coverage.
//   - goleak.VerifyNone(t) exists for per-test goleak checks; prefer a
//     bubble when virtual time or deadlock detection is the point.
//
// goleak is TEST-ONLY: nothing outside _test.go imports it, and the
// root module's production build stays dependency-free.

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
