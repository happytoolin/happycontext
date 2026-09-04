package hc

// P8 goleak integration: every test in this package runs under
// goleak.VerifyTestMain, failing the suite on any leaked goroutine —
// the lifecycle and straggler suites exercise exactly the paths that
// would leak (panicking sinks, armed events, future watchdog).
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
