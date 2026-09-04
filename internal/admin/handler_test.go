package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// readSource returns a file from this package, used by tests that assert on
// the routing table rather than on behaviour.
func readSource(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

// TestMalformedIDsAreRejected covers the paths that answer before the service
// is consulted, which is what lets these run with a nil service and no
// database. A bad UUID must not reach a query.
func TestMalformedIDsAreRejected(t *testing.T) {
	admin := adminUser()
	mux := newTestMux(admin)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/admin/users/not-a-uuid/password"},
		{http.MethodPost, "/admin/users/not-a-uuid/username"},
		{http.MethodPost, "/admin/users/not-a-uuid/delete"},
		{http.MethodPost, "/admin/leagues/not-a-uuid/delete"},
		{http.MethodPost, "/admin/leagues/not-a-uuid/members"},
		{http.MethodPost, "/admin/leagues/not-a-uuid/members/also-bad/remove"},
		{http.MethodPost, "/admin/leagues/not-a-uuid/members/also-bad/balance"},
		{http.MethodPost, "/admin/bets/spread/not-a-uuid/status"},
		{http.MethodGet, "/admin/games/not-a-uuid"},
		{http.MethodPost, "/admin/games/not-a-uuid/evaluate"},
		{http.MethodPost, "/admin/games/not-a-uuid/finalize"},
	}

	for _, p := range paths {
		t.Run(p.path, func(t *testing.T) {
			req := httptest.NewRequest(p.method, p.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestParseBalance(t *testing.T) {
	fallback := decimal.RequireFromString("1000")

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"empty falls back", "", "1000", false},
		{"whitespace falls back", "   ", "1000", false},
		{"plain amount", "250.75", "250.75", false},
		{"surrounding space is trimmed", "  42 ", "42", false},
		// A negative balance is accepted: a forced settlement can legitimately
		// leave a purse short, and the operator has to be able to record that.
		{"negative is allowed", "-15.25", "-15.25", false},
		{"zero is allowed", "0", "0", false},
		{"words are rejected", "a lot", "", true},
		{"currency symbols are rejected", "$100", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBalance(tt.raw, fallback)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBalance) {
					t.Fatalf("parseBalance(%q) error = %v, want ErrInvalidBalance", tt.raw, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseBalance(%q) error = %v", tt.raw, err)
			}
			if got.String() != tt.want {
				t.Errorf("parseBalance(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

// TestErrorMessageIsSpecific checks that the errors an operator will actually
// hit say what happened, rather than falling through to the generic message.
func TestErrorMessageIsSpecific(t *testing.T) {
	specific := []error{
		ErrProtectedAccount,
		ErrUserNotFound,
		ErrLeagueNotFound,
		ErrUsernameTaken,
		ErrConfirmationMismatch,
		ErrInvalidBalance,
		scheduler.ErrUnknownJob,
		scheduler.ErrRunPending,
	}

	generic := errorMessage(errors.New("boom"))

	for _, err := range specific {
		t.Run(err.Error(), func(t *testing.T) {
			if got := errorMessage(err); got == generic {
				t.Errorf("errorMessage(%v) fell through to the generic message", err)
			}
		})
	}

	// Wrapping must not lose the mapping, since the service layer wraps.
	wrapped := errorMessage(errors.Join(errors.New("saving purse"), ErrConfirmationMismatch))
	if wrapped == generic {
		t.Error("errorMessage lost a wrapped sentinel")
	}
}

// TestSuccessMessagesCoverEveryRedirect fails if a handler redirects with a
// success code that has no banner, which would silently show nothing.
func TestSuccessMessagesCoverEveryRedirect(t *testing.T) {
	source := readSource(t, "handler.go")

	for code := range successMessages {
		if !contains(source, "?success="+code) {
			t.Errorf("successMessages has %q but no handler redirects with it", code)
		}
	}

	for _, code := range redirectCodes(source) {
		if _, ok := successMessages[code]; !ok {
			t.Errorf("a handler redirects with ?success=%s but there is no banner for it", code)
		}
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// redirectCodes pulls every ?success=<code> a handler redirects with out of the
// source, so the banner table and the redirects cannot drift apart.
func redirectCodes(source string) []string {
	var codes []string
	const marker = "?success="

	for rest := source; ; {
		i := strings.Index(rest, marker)
		if i < 0 {
			return codes
		}
		rest = rest[i+len(marker):]

		end := strings.IndexAny(rest, `"`)
		if end < 0 {
			return codes
		}
		codes = append(codes, rest[:end])
	}
}

// TestAdminBetsURL covers the pager's filter round-trip. A pager that drops the
// active filters silently widens the browse on the second page.
func TestAdminBetsURL(t *testing.T) {
	leagueID := uuid.New().String()

	tests := []struct {
		name  string
		query url.Values
		page  int
		want  string
	}{
		{"no filters, first page", url.Values{}, 1, "/admin/bets"},
		{
			"no filters, later page",
			url.Values{},
			3,
			"/admin/bets?page=3",
		},
		{
			"filters carried forward",
			url.Values{"status": {"pending"}, "league_id": {leagueID}},
			2,
			"/admin/bets?league_id=" + leagueID + "&page=2&status=pending",
		},
		{
			// page=1 is omitted so the filter form, which has no page field,
			// lands on the first page of its new result set.
			"first page omits the parameter",
			url.Values{"status": {"won"}},
			1,
			"/admin/bets?status=won",
		},
		{
			// A stale banner must not ride along on every page turn.
			"success marker is not carried",
			url.Values{"success": {"bet"}, "status": {"lost"}},
			2,
			"/admin/bets?page=2&status=lost",
		},
		{
			"empty filter values are dropped",
			url.Values{"status": {""}, "user_id": {""}},
			1,
			"/admin/bets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminBetsURL(tt.query, tt.page); got != tt.want {
				t.Errorf("adminBetsURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBackURL covers the return destination reflected into a Location header
// after a status change. It is user-supplied, so anything not recognisably the
// bet listing has to be discarded rather than redirected to.
func TestBackURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty falls back", "", "/admin/bets"},
		{"bare listing", "/admin/bets", "/admin/bets"},
		{"filtered page", "/admin/bets?page=3&status=won", "/admin/bets?page=3&status=won"},
		{"absolute url rejected", "https://evil.example/x", "/admin/bets"},
		{"protocol relative rejected", "//evil.example/admin/bets", "/admin/bets"},
		{"other admin page rejected", "/admin/users", "/admin/bets"},
		{"prefix lookalike rejected", "/admin/betsomething", "/admin/bets"},
		{"path traversal rejected", "/admin/bets/../../etc", "/admin/bets"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backURL(tt.raw); got != tt.want {
				t.Errorf("backURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
