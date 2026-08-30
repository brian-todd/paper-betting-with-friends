package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	assets "github.com/brian/paper-betting-with-friends"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
)

// newTestHandler builds a Handler with a real renderer and no service. Every
// case below fails validation, or is refused by the rate limiter, before the
// service is consulted, so a nil service is enough and the tests need no
// database.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	return NewHandler(nil, renderer)
}

func postForm(t *testing.T, h *Handler, path, body string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}

	w := httptest.NewRecorder()
	switch path {
	case "/register":
		h.Register(w, req)
	case "/login":
		h.Login(w, req)
	default:
		t.Fatalf("unsupported path %q", path)
	}
	return w
}

// Validation failures must reach the user. The HTMX branch returns a bare
// fragment because the form swaps it into #auth-error -- a full document there
// would nest the site layout inside the auth card.
func TestAuthValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want string
	}{
		{
			name: "password mismatch",
			path: "/register",
			body: "username=alice&password=hunter2hunter2&confirm_password=hunter2hunter3",
			want: "Passwords do not match",
		},
		{
			name: "password too short",
			path: "/register",
			body: "username=alice&password=short&confirm_password=short",
			want: "Password must be at least 8 characters",
		},
		{
			name: "missing register fields",
			path: "/register",
			body: "username=&password=",
			want: "Username and password are required",
		},
		{
			name: "missing login fields",
			path: "/login",
			body: "username=&password=",
			want: "Username and password are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (htmx)", func(t *testing.T) {
			w := postForm(t, newTestHandler(t), tt.path, tt.body, true)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
			body := w.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Errorf("body missing %q, got %q", tt.want, body)
			}
			// A fragment, not a page: no doctype and none of the layout chrome.
			if strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "nav-brand") {
				t.Errorf("HTMX response should be a fragment, got a full page: %q", body)
			}
		})

		t.Run(tt.name+" (form post)", func(t *testing.T) {
			w := postForm(t, newTestHandler(t), tt.path, tt.body, false)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
			body := w.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Errorf("body missing %q, got %q", tt.want, body)
			}
			// Without JavaScript the browser replaces the document, so the
			// error has to arrive inside the full page.
			if !strings.Contains(body, "<!DOCTYPE") {
				t.Errorf("form post should get a full page, got %q", body)
			}
		})
	}
}

// The base layout must opt 400 into being swapped, or every message above is
// rendered correctly by the server and then discarded by htmx.
func TestBaseLayoutAllowsSwappingValidationErrors(t *testing.T) {
	w := httptest.NewRecorder()
	renderer, err := templates.NewRenderer(assets.FS, false, time.UTC)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	if err := renderer.Render(w, "login", map[string]any{"Title": "Login"}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, `name="htmx-config"`) {
		t.Fatal("base layout is missing the htmx-config meta tag")
	}
	if !strings.Contains(body, `{"code":"400","swap":true}`) {
		t.Errorf("htmx-config does not allow swapping 400 responses: %q", body)
	}
	// A throttled login answers 429, which htmx would otherwise discard,
	// leaving the form looking like the click did nothing.
	if !strings.Contains(body, `{"code":"429","swap":true}`) {
		t.Errorf("htmx-config does not allow swapping 429 responses: %q", body)
	}
}

// TestLoginIsThrottledAfterRepeatedFailures drives the handler with a limiter
// already at its limit. The throttle is checked before the service is
// consulted, which is both the point -- a blocked attempt must not cost a
// bcrypt comparison -- and what lets this run with a nil service.
func TestLoginIsThrottledAfterRepeatedFailures(t *testing.T) {
	h := newTestHandler(t)

	const username = "cfb-pbwf-admin"
	const addr = "203.0.113.5"

	for range maxFailuresPerUsername {
		h.limiter.RecordFailure(username, addr)
	}

	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader("username="+username+"&password=whatever"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", addr)
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if !strings.Contains(w.Body.String(), "Too many failed attempts") {
		t.Errorf("body did not explain the throttle: %q", w.Body.String())
	}
}

// A throttled htmx request has to say so. htmx discards the body of a 4xx
// unless the config opts the code in, so a 429 would otherwise leave the form
// looking like nothing happened.
func TestThrottledLoginIsVisibleToHTMX(t *testing.T) {
	h := newTestHandler(t)

	for range maxFailuresPerUsername {
		h.limiter.RecordFailure("alice", "203.0.113.5")
	}

	req := httptest.NewRequest(http.MethodPost, "/login",
		strings.NewReader("username=alice&password=whatever"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if strings.Contains(w.Body.String(), "<html") {
		t.Error("an htmx request got the full page instead of the error fragment")
	}
}

// TestSessionIdentity covers the cookie half of session validation, which is
// everything that can be decided without reading the user back.
func TestSessionIdentity(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name        string
		values      map[any]any
		wantErr     error
		wantID      uuid.UUID
		wantVersion int
	}{
		{
			name:        "a versioned cookie is accepted",
			values:      map[any]any{sessionUserKey: id.String(), sessionVersionKey: 3},
			wantID:      id,
			wantVersion: 3,
		},
		{
			name:        "version zero is a real version, not a missing one",
			values:      map[any]any{sessionUserKey: id.String(), sessionVersionKey: 0},
			wantID:      id,
			wantVersion: 0,
		},
		{
			// Predates versioning, so it cannot be shown to have been issued
			// before a password reset.
			name:    "an unversioned cookie is refused",
			values:  map[any]any{sessionUserKey: id.String()},
			wantErr: ErrSessionExpired,
		},
		{
			name:    "a non-integer version is refused",
			values:  map[any]any{sessionUserKey: id.String(), sessionVersionKey: "3"},
			wantErr: ErrSessionExpired,
		},
		{
			name:    "an empty session has no user",
			values:  map[any]any{},
			wantErr: ErrUserNotFound,
		},
		{
			name:    "a blank user id has no user",
			values:  map[any]any{sessionUserKey: "", sessionVersionKey: 0},
			wantErr: ErrUserNotFound,
		},
		{
			name:    "a malformed user id has no user",
			values:  map[any]any{sessionUserKey: "not-a-uuid", sessionVersionKey: 0},
			wantErr: ErrUserNotFound,
		},
		{
			name:    "a non-string user id has no user",
			values:  map[any]any{sessionUserKey: 42, sessionVersionKey: 0},
			wantErr: ErrUserNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotVersion, err := sessionIdentity(tt.values)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error = %v", err)
			}
			if gotID != tt.wantID {
				t.Errorf("id = %v, want %v", gotID, tt.wantID)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("version = %d, want %d", gotVersion, tt.wantVersion)
			}
		})
	}
}
