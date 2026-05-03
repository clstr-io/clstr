package attest

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

type latencySample struct {
	node     string
	duration time.Duration
	passed   bool
}

type latencyCollector struct {
	mu      sync.Mutex
	samples []latencySample
}

func (lc *latencyCollector) record(node string, d time.Duration, passed bool) {
	lc.mu.Lock()
	lc.samples = append(lc.samples, latencySample{node, d, passed})
	lc.mu.Unlock()
}

type nodeStats struct {
	count  int
	errPct float64
	p50    time.Duration
	p95    time.Duration
	p99    time.Duration
	max    time.Duration
}

func computeStats(samples []latencySample) nodeStats {
	var errs int
	durations := make([]time.Duration, len(samples))
	for i, s := range samples {
		durations[i] = s.duration
		if !s.passed {
			errs++
		}
	}

	slices.Sort(durations)
	n := len(durations)

	return nodeStats{
		count:  n,
		errPct: float64(errs) / float64(n) * 100,
		p50:    durations[n*50/100],
		p95:    durations[n*95/100],
		p99:    durations[n*99/100],
		max:    durations[n-1],
	}
}

func (s nodeStats) String() string {
	return fmt.Sprintf("CONCURRENTLY: %d req, %.2f%% err · p50=%s p95=%s p99=%s max=%s",
		s.count, s.errPct,
		fmtDuration(s.p50), fmtDuration(s.p95), fmtDuration(s.p99), fmtDuration(s.max),
	)
}

func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}
