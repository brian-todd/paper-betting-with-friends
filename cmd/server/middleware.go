package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
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
					"request_id", requestIDFrom(r.Context()),
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
				"request_id", requestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start),
			)
		})
	}
}

type requestIDKey struct{}

// requestID gives each request an identifier, puts it on the context and
// returns it as X-Request-ID.
//
// Without it a panic's stack trace and the 500 the request logger records for
// it are two unrelated lines. The header is so someone reporting a broken page
// can quote the one line worth reading.
//
// Only the middleware in this file reads it. Handlers and services log through
// slog.Default without the request context, so their lines do not carry it yet;
// that needs a context-aware slog.Handler and the *Context logging calls.
//
// An incoming X-Request-ID is ignored rather than adopted: the caller controls
// it, and nothing upstream of this server is known to set one.
//
// It must sit outside the request logger, or the logger reads an empty ID.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [8]byte
		_, _ = rand.Read(b[:]) // crypto/rand never returns an error; it crashes instead.
		id := hex.EncodeToString(b[:])

		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// requestIDFrom returns the ID requestID assigned, or "" outside that middleware.
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// immutableAssetCache is one year, the conventional ceiling for immutable.
const immutableAssetCache = "public, max-age=31536000, immutable"

// cacheVersionedAssets marks a static file requested with a ?v= version as
// cacheable forever.
//
// The templates' asset function puts a hash of the file's contents in v, so the
// URL changes whenever the file does and a cached copy can never be stale.
// Without this the embedded files are served with no Cache-Control and, since
// every file in an embed.FS reports the zero mtime, no Last-Modified either --
// nothing for a browser to cache against, so every page fetched htmx again.
//
// It trusts that v names the content being served, which holds while one
// version of the binary is serving. Under a rolling deploy an old process could
// answer a new page's URL with old content and pin it; checking v against the
// file's hash is the fix if that ever becomes the setup.
//
// Only a file is marked. FileServerFS answers some paths with a redirect (a
// directory without its slash, anything ending in index.html) or a directory
// listing, and neither is content v describes -- so the header is decided at
// WriteHeader, on the status, and never on a path ending in a slash. On an
// error FileServerFS strips Cache-Control itself, so a 404 is not cached.
func cacheVersionedAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") == "" || strings.HasSuffix(r.URL.Path, "/") {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&cacheOnSuccess{ResponseWriter: w}, r)
	})
}

// cacheOnSuccess sets the immutable Cache-Control only if the response turns
// out to be the file itself: 200, or 206 for a range of it.
type cacheOnSuccess struct {
	http.ResponseWriter
	decided bool
}

func (w *cacheOnSuccess) WriteHeader(code int) {
	if !w.decided {
		w.decided = true
		if code == http.StatusOK || code == http.StatusPartialContent {
			w.Header().Set("Cache-Control", immutableAssetCache)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *cacheOnSuccess) Write(b []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *cacheOnSuccess) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// crossOriginProtection refuses a state-changing request a browser says came
// from another origin, and logs why.
//
// The log is the point of wrapping it. Where the browser sends no
// Sec-Fetch-Site, the check falls back to comparing Origin with the request's
// Host, and behind a proxy that rewrites Host that refuses every legitimate
// POST. htmx discards a 403's body, so the user sees nothing happen and the
// request line says only 403; this line says which header disagreed with what.
func crossOriginProtection(logger *slog.Logger) func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Warn("cross-origin request refused",
			"request_id", requestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"host", r.Host,
			"origin", r.Header.Get("Origin"),
			"sec_fetch_site", r.Header.Get("Sec-Fetch-Site"),
		)
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	return protection.Handler
}
