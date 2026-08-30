package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

func TestUserFromContext(t *testing.T) {
	t.Run("empty context returns nil", func(t *testing.T) {
		got := UserFromContext(context.Background())
		if got != nil {
			t.Errorf("UserFromContext(empty) = %v, want nil", got)
		}
	})

	t.Run("context with user returns user", func(t *testing.T) {
		user := &models.User{ID: uuid.New(), Username: "testuser"}
		ctx := ContextWithUser(context.Background(), user)

		got := UserFromContext(ctx)
		if got == nil {
			t.Fatal("UserFromContext() = nil, want user")
		}
		if got.ID != user.ID {
			t.Errorf("UserFromContext().ID = %s, want %s", got.ID, user.ID)
		}
	})
}

func TestRequireAdmin(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	middleware := RequireAdmin()(next)

	t.Run("no user returns 403", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest("GET", "/admin", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
		if nextCalled {
			t.Error("next handler should not be called")
		}
	})

	t.Run("non-admin user returns 403", func(t *testing.T) {
		nextCalled = false
		user := &models.User{ID: uuid.New(), IsAdmin: false}
		req := httptest.NewRequest("GET", "/admin", nil)
		req = req.WithContext(ContextWithUser(req.Context(), user))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
		if nextCalled {
			t.Error("next handler should not be called")
		}
	})

	t.Run("admin user passes through", func(t *testing.T) {
		nextCalled = false
		user := &models.User{ID: uuid.New(), IsAdmin: true}
		req := httptest.NewRequest("GET", "/admin", nil)
		req = req.WithContext(ContextWithUser(req.Context(), user))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !nextCalled {
			t.Error("next handler should be called")
		}
	})
}
