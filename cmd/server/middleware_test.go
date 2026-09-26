package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
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

// The ID exists to tie a panic's stack trace to the request line for the same
// request, so the test is that both lines carry the ID the response returned.
func TestRequestIDTiesThePanicToItsRequestLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := applyMiddleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			panic("render blew up")
		}),
		requestID,
		requestLogger(logger),
		recoverPanics(logger),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/games", nil))

	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("X-Request-ID is absent")
	}

	var messages []string
	for line := range strings.Lines(buf.String()) {
		var entry struct {
			Msg       string `json:"msg"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}
		if entry.RequestID != id {
			t.Errorf("%q logged request_id %q, want %q", entry.Msg, entry.RequestID, id)
		}
		messages = append(messages, entry.Msg)
	}
	if len(messages) != 2 {
		t.Errorf("logged %q, want the panic and the request line", messages)
	}
}

func TestRequestIDDiffersPerRequest(t *testing.T) {
	handler := requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	seen := map[string]bool{}
	for range 3 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		// A caller-supplied ID is not adopted.
		req.Header.Set("X-Request-ID", "chosen-by-the-caller")
		handler.ServeHTTP(rec, req)

		id := rec.Header().Get("X-Request-ID")
		if id == "chosen-by-the-caller" || seen[id] {
			t.Fatalf("request ID %q repeated or taken from the caller", id)
		}
		seen[id] = true
	}
}

func TestCacheVersionedAssets(t *testing.T) {
	files := fstest.MapFS{
		"app.css":   {Data: []byte("body{}")},
		"js/app.js": {Data: []byte("0")},
	}
	handler := cacheVersionedAssets(http.StripPrefix("/static/", http.FileServerFS(files)))

	tests := []struct {
		name      string
		target    string
		wantCode  int
		wantCache string
	}{
		{"a versioned URL is kept until it changes", "/static/app.css?v=0123abcd", http.StatusOK, immutableAssetCache},
		// Nothing says this URL will change when the file does.
		{"an unversioned URL is left to the browser", "/static/app.css", http.StatusOK, ""},
		// FileServerFS strips Cache-Control from an error, so a missing file is
		// not remembered as missing for a year.
		{"a missing file is not cached", "/static/gone.css?v=0123abcd", http.StatusNotFound, ""},
		// FileServerFS answers these with a redirect or a directory listing,
		// neither of which is the content v names.
		{"a directory's redirect is not cached", "/static/js?v=0123abcd", http.StatusMovedPermanently, ""},
		{"an index.html redirect is not cached", "/static/index.html?v=0123abcd", http.StatusMovedPermanently, ""},
		{"a directory listing is not cached", "/static/js/?v=0123abcd", http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}
}

// Behind a proxy that rewrites Host, the Origin fallback refuses every
// legitimate POST and htmx shows the user nothing, so the refusal has to say
// what it compared.
func TestCrossOriginProtectionLogsWhatItRefused(t *testing.T) {
	var buf bytes.Buffer
	handler := crossOriginProtection(slog.New(slog.NewJSONHandler(&buf, nil)))(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "http://internal:8080/bets/spread", nil)
	req.Header.Set("Origin", "https://bets.example.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("want one JSON log line, got %q: %v", buf.String(), err)
	}
	for key, want := range map[string]string{
		"msg":    "cross-origin request refused",
		"host":   "internal:8080",
		"origin": "https://bets.example.com",
		"path":   "/bets/spread",
	} {
		if entry[key] != want {
			t.Errorf("%s = %v, want %q", key, entry[key], want)
		}
	}
}
