package app

import (
	"sync"
	"time"
)

// rateLimiter is a small in-memory token bucket keyed by IP or session.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, lastGC: time.Now()}
}

// Allow consumes one token. rate is tokens per minute and burst is capacity.
func (l *rateLimiter) Allow(key string, rate, burst float64) bool {
	if rate <= 0 {
		return true
	}
	if burst < 1 {
		burst = 1
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: burst, last: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * (rate / 60.0)
		if b.tokens > burst {
			b.tokens = burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, v := range l.buckets {
			if now.Sub(v.last) > 30*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	return true
}

// concurrencyGate enforces per-key and global limits without queueing.
type concurrencyGate struct {
	mu      sync.Mutex
	per     map[string]int
	global  int
}

func newConcurrencyGate() *concurrencyGate {
	return &concurrencyGate{per: map[string]int{}}
}

// TryAcquire returns a release function when both limits permit the transfer.
// A limit of 0 means unlimited.
func (g *concurrencyGate) TryAcquire(key string, perLimit, globalLimit int) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if perLimit > 0 && g.per[key] >= perLimit {
		return nil, false
	}
	if globalLimit > 0 && g.global >= globalLimit {
		return nil, false
	}
	g.per[key]++
	g.global++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.per[key]--
			if g.per[key] <= 0 {
				delete(g.per, key)
			}
			g.global--
			g.mu.Unlock()
		})
	}, true
}

// Counts returns the current global and per-key count for monitoring.
func (g *concurrencyGate) Counts() (global int, per map[string]int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]int, len(g.per))
	for k, v := range g.per {
		out[k] = v
	}
	return g.global, out
}
