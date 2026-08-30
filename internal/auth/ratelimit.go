package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Login throttling limits. The username bucket is the one that matters: the
// administrator's name is fixed by configuration and effectively public, so an
// attacker knows exactly what to guess and only needs the password.
const (
	maxFailuresPerUsername = 5
	maxFailuresPerAddr     = 30
	failureWindow          = 15 * time.Minute

	// maxTrackedKeys bounds the memory an attacker can cost us by cycling
	// through usernames. Expired entries are dropped once the map reaches it.
	maxTrackedKeys = 4096
)

// attemptLimiter counts recent failures per key and refuses a key that has run
// through its allowance inside the window.
//
// The window is fixed rather than sliding: a key that trips the limit is shut
// out until its window expires, which is the behaviour worth having here. It
// costs an attacker the whole window per burst.
type attemptLimiter struct {
	mu      sync.Mutex
	entries map[string]*attemptEntry

	max    int
	window time.Duration

	// now is injectable so the tests do not sleep.
	now func() time.Time
}

type attemptEntry struct {
	failures  int
	expiresAt time.Time
}

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		entries: make(map[string]*attemptEntry),
		max:     max,
		window:  window,
		now:     time.Now,
	}
}

// allowed reports whether another attempt may be made for key.
func (l *attemptLimiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok {
		return true
	}
	if !l.now().Before(entry.expiresAt) {
		delete(l.entries, key)
		return true
	}
	return entry.failures < l.max
}

// recordFailure counts one failed attempt against key.
func (l *attemptLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	entry, ok := l.entries[key]
	if !ok || !now.Before(entry.expiresAt) {
		if len(l.entries) >= maxTrackedKeys {
			l.pruneExpiredLocked(now)
		}
		l.entries[key] = &attemptEntry{failures: 1, expiresAt: now.Add(l.window)}
		return
	}

	entry.failures++
}

// reset clears a key's history, called once its owner proves who they are.
func (l *attemptLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// pruneExpiredLocked drops entries whose window has passed. The caller holds
// the mutex.
func (l *attemptLimiter) pruneExpiredLocked(now time.Time) {
	for key, entry := range l.entries {
		if !now.Before(entry.expiresAt) {
			delete(l.entries, key)
		}
	}
}

// LoginLimiter throttles failed logins.
//
// It keys on both the username and the client address, and the two are not
// equally trustworthy. Behind a proxy the address is read from a request header
// that a caller can set to anything, so that bucket is advisory: it slows a
// naive attacker and nothing more. The username cannot be forged -- guessing an
// account requires sending that account's name -- so it carries the real limit.
type LoginLimiter struct {
	byUsername *attemptLimiter
	byAddr     *attemptLimiter
}

// NewLoginLimiter creates a limiter with the package's default allowances.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		byUsername: newAttemptLimiter(maxFailuresPerUsername, failureWindow),
		byAddr:     newAttemptLimiter(maxFailuresPerAddr, failureWindow),
	}
}

// Allow reports whether a login attempt may proceed.
func (l *LoginLimiter) Allow(username, addr string) bool {
	return l.byUsername.allowed(username) && l.byAddr.allowed(addr)
}

// RecordFailure counts a rejected login.
func (l *LoginLimiter) RecordFailure(username, addr string) {
	l.byUsername.recordFailure(username)
	l.byAddr.recordFailure(addr)
}

// RecordSuccess clears the username's history after a correct password.
//
// The address history is deliberately left alone: on a shared address one
// successful login must not wipe the count built up by everyone else behind it.
func (l *LoginLimiter) RecordSuccess(username, addr string) {
	l.byUsername.reset(username)
}

// clientAddr identifies the caller for rate limiting.
//
// X-Forwarded-For is honoured because the app runs behind a TLS-terminating
// proxy, where RemoteAddr is the proxy for every request and would put the whole
// internet in one bucket. The header is caller-controlled and so this value is
// only ever a secondary control -- see LoginLimiter.
func clientAddr(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
