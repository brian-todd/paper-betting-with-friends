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
// RegisterRoutes: the point of the tests below is that nothing reaches a
// handler without the admin flag, and a route added outside the guard wrapper
// would be missed by a test that only probes the ones it already knows about.
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
	{http.MethodPost, "/admin/sync/cfb-games-and-lines/run"},
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

// TestAdminRoutesRejectWrongMethod guards against a route being registered
// without its method, which would expose a mutation to a GET.
func TestAdminRoutesRejectWrongMethod(t *testing.T) {
	mux := newTestMux(nil)

	for _, route := range adminRoutes {
		// Some paths legitimately answer both verbs -- /admin/leagues lists on
		// GET and creates on POST -- so only the POST-only ones are probed.
		if route.method != http.MethodPost || alsoServesGET(route.path) {
			continue
		}

		t.Run(route.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// A GET at a POST-only path matches no pattern, so it never reaches
			// the guard and falls through as 404/405 rather than 403.
			if w.Code == http.StatusForbidden {
				t.Errorf("GET %s reached the admin guard, so it is registered for GET too", route.path)
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
		if !strings.HasPrefix(line, "mux.Handle(") {
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
