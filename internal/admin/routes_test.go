package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

// adminRoutes is every route the handler registers. Keep it in step with
// RegisterRoutes: TestRegisteredRoutesAreAllListed fails if a route is missing,
// so the wrong-method test below cannot quietly skip a new mutation.
var adminRoutes = []struct {
	method string
	path   string
}{
	{http.MethodGet, "/admin"},
	{http.MethodGet, "/admin/users"},
	{http.MethodPost, "/admin/users/" + uuid.Nil.String() + "/password"},
	{http.MethodPost, "/admin/users/" + uuid.Nil.String() + "/username"},
	{http.MethodPost, "/admin/users/" + uuid.Nil.String() + "/delete"},
	{http.MethodGet, "/admin/leagues"},
	{http.MethodPost, "/admin/leagues"},
	{http.MethodPost, "/admin/leagues/" + uuid.Nil.String() + "/delete"},
	{http.MethodPost, "/admin/leagues/" + uuid.Nil.String() + "/members"},
	{http.MethodPost, "/admin/leagues/" + uuid.Nil.String() + "/members/" + uuid.Nil.String() + "/remove"},
	{http.MethodPost, "/admin/leagues/" + uuid.Nil.String() + "/members/" + uuid.Nil.String() + "/balance"},
	{http.MethodGet, "/admin/bets"},
	{http.MethodPost, "/admin/bets/spread/" + uuid.Nil.String() + "/status"},
	{http.MethodGet, "/admin/sync"},
	{http.MethodPost, "/admin/sync/cfb-lines/run"},
	{http.MethodGet, "/admin/games"},
	{http.MethodGet, "/admin/games/" + uuid.Nil.String()},
	{http.MethodPost, "/admin/games/" + uuid.Nil.String() + "/evaluate"},
	{http.MethodPost, "/admin/games/" + uuid.Nil.String() + "/finalize"},
	{http.MethodGet, "/admin/audit"},
}

// newTestMux registers the admin routes behind a stand-in for RequireAuth that
// puts user into the context, or nobody when user is nil. The service is nil
// because no request in these tests is meant to reach a handler body.
func newTestMux(user *models.User) *http.ServeMux {
	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if user != nil {
				r = r.WithContext(auth.ContextWithUser(r.Context(), user))
			}
			next.ServeHTTP(w, r)
		})
	}

	mux := http.NewServeMux()
	NewHandler(nil, nil).RegisterRoutes(mux, authMiddleware)
	return mux
}

// TestAdminRoutesRejectAnonymous is the direct check on the one rule this
// package exists to enforce.
func TestAdminRoutesRejectAnonymous(t *testing.T) {
	mux := newTestMux(nil)

	for _, route := range adminRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
			}
		})
	}
}

func TestAdminRoutesRejectNonAdmin(t *testing.T) {
	mux := newTestMux(&models.User{ID: uuid.New(), Username: "testalice", IsAdmin: false})

	for _, route := range adminRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
			}
		})
	}
}

// TestUnregisteredAdminPathsAreGuarded is the property the sub-mux exists for:
// the guard covers the /admin prefix, not a list of routes, so a path nobody
// registered is refused rather than answered.
func TestUnregisteredAdminPathsAreGuarded(t *testing.T) {
	mux := newTestMux(&models.User{ID: uuid.New(), Username: "testalice", IsAdmin: false})

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/not-a-route"},
		{http.MethodPost, "/admin/not-a-route"},
		{http.MethodGet, "/admin/"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
			}
		})
	}
}

// TestAdminRoutesRejectWrongMethod guards against a route being registered
// without its method, which would expose a mutation to a GET.
//
// It asks as an admin, because the guard runs before routing and would refuse
// anyone else with a 403 whatever the method. Past the guard, a GET at a
// POST-only path must be a 405: reaching a handler instead would mean the route
// answers GET too.
func TestAdminRoutesRejectWrongMethod(t *testing.T) {
	mux := newTestMux(&models.User{ID: uuid.New(), Username: "testadmin", IsAdmin: true})

	for _, route := range adminRoutes {
		// Some paths legitimately answer both verbs -- /admin/leagues lists on
		// GET and creates on POST -- so only the POST-only ones are probed.
		if route.method != http.MethodPost || alsoServesGET(route.path) {
			continue
		}

		t.Run(route.path, func(t *testing.T) {
			// The handlers hold a nil service, so a GET that reaches one panics.
			// That is the failure this test is for, so report it as such rather
			// than let it take down the rest of the package's tests.
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("GET %s reached its handler, so it is registered for GET too (panicked: %v)", route.path, v)
				}
			}()

			req := httptest.NewRequest(http.MethodGet, route.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("GET %s: status = %d, want %d", route.path, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

// TestRegisteredRoutesAreAllListed fails if RegisterRoutes gains a route that
// the tables above do not probe.
func TestRegisteredRoutesAreAllListed(t *testing.T) {
	source := readSource(t, "handler.go")

	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "mux.Handle(") && !strings.HasPrefix(line, "mux.HandleFunc(") {
			continue
		}

		pattern := line[strings.Index(line, `"`)+1:]
		pattern = pattern[:strings.Index(pattern, `"`)]

		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			t.Fatalf("route pattern %q has no method", pattern)
		}

		if !covers(method, path) {
			t.Errorf("route %s is registered but not covered by adminRoutes", pattern)
		}
	}
}

// covers reports whether adminRoutes probes the given registered pattern,
// matching wildcard segments such as {id} against the concrete UUIDs used above.
func covers(method, pattern string) bool {
	want := strings.Split(strings.Trim(pattern, "/"), "/")

	for _, route := range adminRoutes {
		if route.method != method {
			continue
		}

		got := strings.Split(strings.Trim(route.path, "/"), "/")
		if len(got) != len(want) {
			continue
		}

		matched := true
		for i := range want {
			if strings.HasPrefix(want[i], "{") {
				continue
			}
			if want[i] != got[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// alsoServesGET reports whether adminRoutes registers the same path for GET.
func alsoServesGET(path string) bool {
	for _, route := range adminRoutes {
		if route.method == http.MethodGet && route.path == path {
			return true
		}
	}
	return false
}
