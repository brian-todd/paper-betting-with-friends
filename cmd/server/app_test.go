package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/pagetest"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
)

// serve builds the application buildHandler ships, pins its clocks to env's
// instant, and serves it over httptest.
func serve(t *testing.T, env *pagetest.Env) (*application, *httptest.Server) {
	t.Helper()

	// Quiet: every request through the stack logs a line, and a week of page
	// renders is a wall of them with nothing in it.
	logger := slog.New(slog.DiscardHandler)

	// The scheduler is built but never started, and no job is registered on it.
	// main registers the sync jobs, and this process has no API key and wants
	// no metered request; the admin service holds the scheduler only to report
	// on it.
	//
	// pagetest's config says development, which is load-bearing for the session
	// cookie -- production marks it Secure and Go's jar then refuses to send it
	// over plain HTTP. The cost is that buildHandler's renderer runs in dev mode
	// and re-parses every template on every render. That is the real behaviour
	// for that config and is left alone rather than worked around; it is a
	// second or two across a whole test.
	app, err := buildHandler(env.Config, env.DB, env.Location, pagetest.Assets, scheduler.New(logger), logger)
	if err != nil {
		t.Fatalf("building the application: %v", err)
	}
	app.SetClock(env.Now)

	server := httptest.NewServer(app.Handler)
	t.Cleanup(server.Close)
	return app, server
}

// login posts the real login form.
func login(t *testing.T, client *http.Client, base, username, password string) *http.Response {
	t.Helper()

	resp, _ := postForm(t, client, base+"/login", url.Values{"username": {username}, "password": {password}})
	return resp
}

// requireLoggedIn fails unless client's cookie still gets it a guarded page.
func requireLoggedIn(t *testing.T, client *http.Client, base, as string) {
	t.Helper()

	resp, _ := get(t, client, base+"/bets")
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/bets" {
		t.Fatalf("%s: GET /bets ended at %s with %d, want /bets with 200", as, resp.Request.URL.Path, resp.StatusCode)
	}
}

// requireLoggedOut fails unless client's cookie is sent back to /login.
func requireLoggedOut(t *testing.T, client *http.Client, base, as string) {
	t.Helper()

	resp, _ := get(t, client, base+"/bets")
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("%s: GET /bets ended at %s, want /login", as, resp.Request.URL.Path)
	}
}

// A session is a signed cookie carrying the account's session version, and the
// server re-reads the account on every request. That is what makes a password
// reset evict a session that already exists, and it only holds if every piece
// -- the handler that mints the cookie, the middleware that reads it and the
// admin route that bumps the version -- is wired the way main wires it.
func TestSessionsThroughTheRouter(t *testing.T) {
	env := pagetest.Open(t, firstSnapshot)
	app, server := serve(t, env)

	// main provisions the administrator the same way on every boot.
	const adminPassword = "admin-password-long-enough"
	if err := app.Admin.EnsureAdminUser(env.Config.AdminUsername, adminPassword); err != nil {
		t.Fatalf("provisioning the administrator: %v", err)
	}

	alice := newClient(t, server)
	register(t, alice, server.URL, "alice", "first-password")
	requireLoggedIn(t, alice, server.URL, "alice after registering")

	admin := newClient(t, server)
	if resp := login(t, admin, server.URL, env.Config.AdminUsername, adminPassword); resp.Request.URL.Path != "/" {
		t.Fatalf("administrator login ended at %s, want /", resp.Request.URL.Path)
	}

	t.Run("the admin portal refuses an ordinary member", func(t *testing.T) {
		resp, _ := get(t, alice, server.URL+"/admin/users")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("alice GET /admin/users: status %d, want 403", resp.StatusCode)
		}
	})

	t.Run("every admin page renders for the administrator", func(t *testing.T) {
		// None of these had a render test. A template that fails to execute
		// after the first byte still answers 200 with half a page, so the
		// check is on the closing tag as well as the status.
		for _, path := range []string{
			"/admin", "/admin/users", "/admin/leagues", "/admin/bets",
			"/admin/sync", "/admin/games", "/admin/audit",
		} {
			resp, body := get(t, admin, server.URL+path)
			if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != path {
				t.Errorf("GET %s: ended at %s with %d, want 200", path, resp.Request.URL.Path, resp.StatusCode)
				continue
			}
			if !strings.Contains(body, "</html>") {
				t.Errorf("GET %s: page is truncated", path)
			}
		}
	})

	t.Run("a password reset evicts the member's existing session", func(t *testing.T) {
		var user models.User
		if err := env.DB.Where("username = ?", "alice").First(&user).Error; err != nil {
			t.Fatalf("finding alice: %v", err)
		}

		resp, _ := postForm(t, admin, server.URL+"/admin/users/"+user.ID.String()+"/password",
			url.Values{"password": {"second-password"}})
		if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/admin/users" {
			t.Fatalf("resetting the password ended at %s with %d", resp.Request.URL.Path, resp.StatusCode)
		}

		requireLoggedOut(t, alice, server.URL, "alice after a reset")

		if resp := login(t, alice, server.URL, "alice", "first-password"); resp.Request.URL.Path == "/" {
			t.Error("the old password still logs in")
		}
		login(t, alice, server.URL, "alice", "second-password")
		requireLoggedIn(t, alice, server.URL, "alice with the new password")

		_, body := get(t, admin, server.URL+"/admin/audit")
		if !strings.Contains(body, models.AuditActionUserPasswordReset) {
			t.Error("the audit page does not record the reset")
		}
	})

	t.Run("logging out ends the session in the browser", func(t *testing.T) {
		resp, _ := postForm(t, alice, server.URL+"/logout", nil)
		if resp.Request.URL.Path != "/login" {
			t.Fatalf("logout ended at %s, want /login", resp.Request.URL.Path)
		}
		requireLoggedOut(t, alice, server.URL, "alice after logging out")
		// The administrator's session is its own and is untouched.
		requireLoggedIn(t, admin, server.URL, "the administrator")
	})
}
