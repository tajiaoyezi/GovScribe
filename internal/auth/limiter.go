package auth

import (
	"strings"
	"sync"
	"time"
)

type LoginFailureLimiter interface {
	Blocked(username string) bool
	RecordFailure(username string) bool
	RecordSuccess(username string)
}

type WindowLoginFailureLimiter struct {
	Threshold int
	Window    time.Duration
	Now       func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
	blocked  map[string]time.Time
}

func NewWindowLoginFailureLimiter(threshold int, window time.Duration) *WindowLoginFailureLimiter {
	return &WindowLoginFailureLimiter{
		Threshold: threshold,
		Window:    window,
		Now:       time.Now,
		failures:  map[string][]time.Time{},
		blocked:   map[string]time.Time{},
	}
}

func (l *WindowLoginFailureLimiter) Blocked(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := loginKey(username)
	now := l.now()
	until, ok := l.blocked[key]
	if !ok {
		return false
	}
	if now.Before(until) {
		return true
	}
	delete(l.blocked, key)
	l.failures[key] = nil
	return false
}

func (l *WindowLoginFailureLimiter) RecordFailure(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := loginKey(username)
	now := l.now()
	windowStart := now.Add(-l.window())
	var kept []time.Time
	for _, at := range l.failures[key] {
		if at.After(windowStart) || at.Equal(windowStart) {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	l.failures[key] = kept
	if len(kept) >= l.threshold() {
		l.blocked[key] = now.Add(l.window())
		return true
	}
	return false
}

func (l *WindowLoginFailureLimiter) RecordSuccess(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := loginKey(username)
	delete(l.failures, key)
	delete(l.blocked, key)
}

func (l *WindowLoginFailureLimiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *WindowLoginFailureLimiter) threshold() int {
	if l.Threshold <= 0 {
		return 5
	}
	return l.Threshold
}

func (l *WindowLoginFailureLimiter) window() time.Duration {
	if l.Window <= 0 {
		return 15 * time.Minute
	}
	return l.Window
}

func loginKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
