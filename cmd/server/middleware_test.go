package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name         string
		isProduction bool
		wantHSTS     bool
	}{
		{"development", false, false},
		// Pinning https:// for a year is right in production and unworkable
		// against a localhost served over plain HTTP.
		{"production", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := securityHeaders(tt.isProduction)(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			want := map[string]string{
				"X-Content-Type-Options":  "nosniff",
				"X-Frame-Options":         "DENY",
				"Referrer-Policy":         "same-origin",
				"Content-Security-Policy": contentSecurityPolicy,
			}
			for header, value := range want {
				if got := rec.Header().Get(header); got != value {
					t.Errorf("%s = %q, want %q", header, got, value)
				}
			}

			hsts := rec.Header().Get("Strict-Transport-Security")
			if tt.wantHSTS && hsts == "" {
				t.Error("Strict-Transport-Security is absent, want it set in production")
			}
			if !tt.wantHSTS && hsts != "" {
				t.Errorf("Strict-Transport-Security = %q, want it unset outside production", hsts)
			}
		})
	}
}

// The policy is only as good as the directives that do not depend on script-src,
// since inline scripts force 'unsafe-inline'. Those are the ones worth pinning.
func TestContentSecurityPolicyKeepsFramingAndFormDirectives(t *testing.T) {
	for _, directive := range []string{
		"frame-ancestors 'none'",
		"form-action 'self'",
		"base-uri 'self'",
		"object-src 'none'",
	} {
		if !strings.Contains(contentSecurityPolicy, directive) {
			t.Errorf("policy is missing %q:\n%s", directive, contentSecurityPolicy)
		}
	}
}

func TestRecoverPanicsReturnsInternalServerError(t *testing.T) {
	handler := recoverPanics(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic("index out of range")
		}))

	rec := httptest.NewRecorder()

	// The point of the middleware: this call returns rather than unwinding into
	// net/http, which would drop the connection with no response at all.
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/games", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// A template that panics part way through has already sent 200 and a partial
// body. There is no status left to change, and trying produces a spurious
// "superfluous WriteHeader" rather than a useful error page.
func TestRecoverPanicsLeavesAnAlreadyStartedResponseAlone(t *testing.T) {
	handler := recoverPanics(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html>partial"))
			panic("render blew up")
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/games", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the already-sent %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "<html>partial" {
		t.Errorf("body = %q, want the partial response untouched", got)
	}
}

// ErrAbortHandler is how a handler says "stop, silently". Converting it into a
// 500 would manufacture an incident out of an intentional abort.
func TestRecoverPanicsRepanicsOnErrAbortHandler(t *testing.T) {
	handler := recoverPanics(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate to net/http", v)
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("ErrAbortHandler did not propagate")
}

func TestRecoverPanicsPassesThroughNormalResponses(t *testing.T) {
	handler := recoverPanics(discardLogger())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("fine"))
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot || rec.Body.String() != "fine" {
		t.Errorf("status = %d body = %q, want %d and %q",
			rec.Code, rec.Body.String(), http.StatusTeapot, "fine")
	}
}
