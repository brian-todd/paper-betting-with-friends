package main

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// contentSecurityPolicy is sent on every response.
//
// script-src and style-src carry 'unsafe-inline' because the pages genuinely
// rely on it: several templates embed a <script> block, most carry inline
// onchange/onclick handlers, and inline style attributes are used throughout.
// That does mean the policy is not an XSS defence -- an injected <script> would
// still run -- and pretending otherwise by shipping a strict policy that broke
// every page would only get the header deleted.
//
// The directives that do not depend on script-src still earn their place:
// frame-ancestors blocks clickjacking, form-action stops an injected form
// posting a session elsewhere, and base-uri stops a rewritten <base> pointing
// every relative URL at another origin.
//
// img-src has to allow arbitrary https origins: team logos are URLs handed to
// us by the data provider and are served from whatever CDN it names.
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' https: data:; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"form-action 'self'; " +
	"base-uri 'self'; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'"

// hstsMaxAge is one year, the minimum most preload lists accept.
const hstsMaxAge = 31536000

// securityHeaders sets the response headers that constrain what a browser will
// do with a page.
//
// HSTS is production-only and deliberately so: pinning https:// for a year
// against localhost would make development over plain HTTP impossible in any
// browser that had once loaded the app, and clearing it is a per-browser chore.
func securityHeaders(isProduction bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Content-Security-Policy", contentSecurityPolicy)

			if isProduction {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder captures the response status, and whether anything has been
// written at all, so it can be logged and so a late panic knows whether a
// response body is already on the wire.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

// recoverPanics turns a panic in a handler into a logged 500.
//
// net/http already recovers handler panics, but what it does with one is close
// to the worst available outcome: it closes the connection with no response, so
// the visitor sees a network error rather than a page, and it writes the trace
// through the standard logger -- which means the one incident worth alerting on
// is the one thing that never reaches the structured logs.
//
// It must sit inside the request logger so that log line still records the 500.
func recoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			defer func() {
				v := recover()
				if v == nil {
					return
				}

				// ErrAbortHandler is net/http's documented way for a handler to
				// give up silently. Swallowing it would turn an intentional
				// abort into a spurious 500 in the logs.
				if v == http.ErrAbortHandler {
					panic(v)
				}

				logger.Error("panic serving request",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", v,
					"stack", string(debug.Stack()),
				)

				// Once bytes are out there is no status left to set: a template
				// that panics half way through has already sent 200 and a
				// partial page. Truncating it is all that is left.
				if rec.wrote {
					return
				}
				http.Error(rec, "Internal Server Error", http.StatusInternalServerError)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

// requestLogger logs each request with its status and duration.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start),
			)
		})
	}
}
