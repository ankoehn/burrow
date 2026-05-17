// Package testutil holds shared test helpers.
package testutil

import (
	"runtime"
	"testing"
	"time"
)

// AssertNoGoroutineLeak fails t if goroutine count has not returned to within
// +1 of baseline after a short settle. Call: defer testutil.AssertNoGoroutineLeak(t)()
func AssertNoGoroutineLeak(t *testing.T) func() {
	t.Helper()
	before := runtime.NumGoroutine()
	return func() {
		for i := 0; i < 50; i++ {
			if runtime.NumGoroutine() <= before+1 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Errorf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
	}
}
