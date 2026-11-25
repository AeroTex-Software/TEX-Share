package app

import (
	"sync/atomic"
	"time"
)

// startMetrics samples host CPU utilisation in the background. Memory and disk
// are read on demand; only CPU needs two samples spaced in time, so it is polled
// here. Platforms that cannot report CPU leave cpuKnown at zero and the UI shows
// the gauge as unavailable instead of inventing a value.
func (a *App) startMetrics() {
	a.jobWG.Add(1)
	go func() {
		defer a.jobWG.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		var lastIdle, lastTotal uint64
		have := false
		for {
			if idle, total, ok := cpuTimes(); ok {
				atomic.StoreInt32(&a.cpuKnown, 1)
				if have && total > lastTotal {
					dTotal := total - lastTotal
					dIdle := idle - lastIdle
					if dTotal > 0 {
						milli := int64(dTotal-dIdle) * 1000 / int64(dTotal)
						if milli < 0 {
							milli = 0
						}
						if milli > 1000 {
							milli = 1000
						}
						atomic.StoreInt64(&a.cpuMilli, milli)
					}
				}
				lastIdle, lastTotal, have = idle, total, true
			}
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
			}
		}
	}()
}

// cpuPercent returns the most recent CPU utilisation in percent. The second
// result is false when the platform cannot report it.
func (a *App) cpuPercent() (float64, bool) {
	if atomic.LoadInt32(&a.cpuKnown) == 0 {
		return 0, false
	}
	return float64(atomic.LoadInt64(&a.cpuMilli)) / 10, true
}
