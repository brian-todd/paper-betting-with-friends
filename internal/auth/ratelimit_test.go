package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeClock drives the limiter without sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter(max int, window time.Duration) (*attemptLimiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	limiter := newAttemptLimiter(max, window)
	limiter.now = clock.now
	return limiter, clock
}

func TestAttemptLimiterBlocksAfterMaxFailures(t *testing.T) {
	limiter, _ := newTestLimiter(3, time.Minute)

	for i := range 3 {
		if !limiter.allowed("alice") {
			t.Fatalf("attempt %d was blocked before the limit was reached", i+1)
		}
		limiter.recordFailure("alice")
	}

	if limiter.allowed("alice") {
		t.Error("attempt 4 was allowed, want blocked")
	}
	// One key tripping its limit must not affect another.
	if !limiter.allowed("bob") {
		t.Error("an unrelated key was blocked")
	}
}

func TestAttemptLimiterRecoversAfterWindow(t *testing.T) {
	limiter, clock := newTestLimiter(2, 15*time.Minute)

	limiter.recordFailure("alice")
	limiter.recordFailure("alice")
	if limiter.allowed("alice") {
		t.Fatal("key was not blocked at the limit")
	}

	clock.advance(14 * time.Minute)
	if limiter.allowed("alice") {
		t.Error("key recovered before its window expired")
	}

	clock.advance(2 * time.Minute)
	if !limiter.allowed("alice") {
		t.Error("key did not recover after its window expired")
	}
}

// A burst that trips the limit must cost the full window, not slide forward as
// earlier failures age out. Otherwise an attacker paces themselves at the limit
// and keeps guessing indefinitely.
func TestAttemptLimiterWindowDoesNotSlide(t *testing.T) {
	limiter, clock := newTestLimiter(2, 15*time.Minute)

	limiter.recordFailure("alice")
	clock.advance(14 * time.Minute)
	limiter.recordFailure("alice")

	if limiter.allowed("alice") {
		t.Fatal("key was not blocked at the limit")
	}

	// The window is anchored to the first failure of the run.
	clock.advance(2 * time.Minute)
	if !limiter.allowed("alice") {
		t.Error("key did not recover once the window from the first failure passed")
	}
}

func TestAttemptLimiterResetClearsHistory(t *testing.T) {
	limiter, _ := newTestLimiter(2, time.Minute)

	limiter.recordFailure("alice")
	limiter.recordFailure("alice")
	limiter.reset("alice")

	if !limiter.allowed("alice") {
		t.Error("reset did not clear the key's history")
	}
}

// An attacker cycling usernames must not be able to grow the map without bound.
func TestAttemptLimiterPrunesExpiredEntries(t *testing.T) {
	limiter, clock := newTestLimiter(1, time.Minute)

	for i := range maxTrackedKeys {
		limiter.recordFailure(string(rune(i%256)) + string(rune(i/256)))
	}
	if got := len(limiter.entries); got != maxTrackedKeys {
		t.Fatalf("tracked %d keys, want %d", got, maxTrackedKeys)
	}

	// Every entry is now stale, so the next write clears them out.
	clock.advance(2 * time.Minute)
	limiter.recordFailure("one-more")

	if got := len(limiter.entries); got != 1 {
		t.Errorf("after pruning, tracked %d keys, want 1", got)
	}
}

// A correct password clears the account's history but must not clear the
// address's: on a shared address one success would otherwise wipe the count
// built up by everyone else behind it.
func TestLoginLimiterSuccessDoesNotClearTheAddress(t *testing.T) {
	limiter := NewLoginLimiter()

	for range maxFailuresPerAddr {
		limiter.RecordFailure("guessing", "203.0.113.5")
	}

	limiter.RecordSuccess("alice", "203.0.113.5")

	if limiter.Allow("alice", "203.0.113.5") {
		t.Error("a success cleared the address history built up by other accounts")
	}
	if !limiter.Allow("alice", "198.51.100.7") {
		t.Error("the account itself should be clear after a success")
	}
}

func TestLoginLimiterBlocksOnUsernameAlone(t *testing.T) {
	limiter := NewLoginLimiter()

	// Spread the attempts across addresses, as anyone with a proxy header can.
	for i := range maxFailuresPerUsername {
		limiter.RecordFailure("cfb-pbwf-admin", string(rune('a'+i)))
	}

	if limiter.Allow("cfb-pbwf-admin", "a-fresh-address") {
		t.Error("rotating the address bypassed the username limit")
	}
}

func TestClientAddr(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{"remote address host is taken", "192.0.2.4:51234", "", "192.0.2.4"},
		{"remote address without a port passes through", "192.0.2.4", "", "192.0.2.4"},
		{"forwarded header wins behind a proxy", "10.0.0.1:443", "203.0.113.5", "203.0.113.5"},
		{"first hop is taken from a chain", "10.0.0.1:443", "203.0.113.5, 10.0.0.1", "203.0.113.5"},
		{"surrounding space is trimmed", "10.0.0.1:443", "  203.0.113.5 , 10.0.0.1", "203.0.113.5"},
		{"ipv6 remote address", "[2001:db8::1]:443", "", "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}

			if got := clientAddr(req); got != tt.want {
				t.Errorf("clientAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}
