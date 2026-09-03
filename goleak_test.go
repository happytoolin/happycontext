package hc

// P8 goleak integration (action plan P8): every test in this package
// runs under goleak.VerifyTestMain, which fails the suite if any
// goroutine is still running after the tests finish. This catches
// leaks from panicking sinks, armed-event machinery, or any future
// async work (e.g. a watchdog) — the lifecycle and straggler suites
// exercise exactly the paths that would leak.
//
// goleak is a TEST-ONLY dependency: nothing outside _test.go imports
// it, and the root module's production build stays dependency-free.

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
