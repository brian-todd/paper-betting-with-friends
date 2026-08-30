package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/brian/paper-betting-with-friends/internal/templates"
)

// Handler handles authentication HTTP requests.
type Handler struct {
	service   *Service
	templates *templates.Renderer
	limiter   *LoginLimiter
}

// NewHandler creates a new authentication handler.
func NewHandler(service *Service, renderer *templates.Renderer) *Handler {
	return &Handler{
		service:   service,
		templates: renderer,
		limiter:   NewLoginLimiter(),
	}
}

// RegisterRoutes registers authentication routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", h.ShowLogin)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("GET /register", h.ShowRegister)
	mux.HandleFunc("POST /register", h.Register)
	mux.HandleFunc("POST /logout", h.Logout)
}

// ShowLogin renders the login page.
func (h *Handler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	h.templates.Render(w, "login", map[string]any{
		"Title": "Login",
	})
}

// Login handles the login form submission.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.renderLoginError(w, r, "Username and password are required")
		return
	}

	// The administrator's username is fixed by configuration and effectively
	// public, so without a limit the only secret left is a password an attacker
	// can guess at as fast as bcrypt will answer.
	addr := clientAddr(r)
	if !h.limiter.Allow(username, addr) {
		slog.Warn("login throttled", "username", username, "addr", addr)
		h.renderAuthError(w, r, http.StatusTooManyRequests, "login", "Login",
			"Too many failed attempts. Wait a few minutes and try again.")
		return
	}

	user, err := h.service.Login(username, password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			h.limiter.RecordFailure(username, addr)
			h.renderLoginError(w, r, "Invalid username or password")
			return
		}
		slog.Error("login failed", "error", err)
		h.renderLoginError(w, r, "An error occurred. Please try again.")
		return
	}

	h.limiter.RecordSuccess(username, addr)

	if err := h.service.CreateSession(w, r, user); err != nil {
		slog.Error("session creation failed", "error", err)
		h.renderLoginError(w, r, "An error occurred. Please try again.")
		return
	}

	// For HTMX requests, use HX-Redirect header.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ShowRegister renders the registration page.
func (h *Handler) ShowRegister(w http.ResponseWriter, r *http.Request) {
	h.templates.Render(w, "register", map[string]any{
		"Title": "Register",
	})
}

// Register handles the registration form submission.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if username == "" || password == "" {
		h.renderRegisterError(w, r, "Username and password are required")
		return
	}

	if password != confirmPassword {
		h.renderRegisterError(w, r, "Passwords do not match")
		return
	}

	if len(password) < 8 {
		h.renderRegisterError(w, r, "Password must be at least 8 characters")
		return
	}

	user, err := h.service.Register(username, password)
	if err != nil {
		if errors.Is(err, ErrUserExists) {
			h.renderRegisterError(w, r, "An account with this username already exists")
			return
		}
		slog.Error("registration failed", "error", err)
		h.renderRegisterError(w, r, "An error occurred. Please try again.")
		return
	}

	if err := h.service.CreateSession(w, r, user); err != nil {
		slog.Error("session creation failed", "error", err)
		// User was created, redirect to login.
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// For HTMX requests, use HX-Redirect header.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles user logout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DestroySession(w, r); err != nil {
		slog.Error("logout failed", "error", err)
	}

	// For HTMX requests, use HX-Redirect header.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// renderLoginError renders the login page with an error message.
func (h *Handler) renderLoginError(w http.ResponseWriter, r *http.Request, message string) {
	h.renderAuthError(w, r, http.StatusBadRequest, "login", "Login", message)
}

// renderRegisterError renders the registration page with an error message.
func (h *Handler) renderRegisterError(w http.ResponseWriter, r *http.Request, message string) {
	h.renderAuthError(w, r, http.StatusBadRequest, "register", "Register", message)
}

// renderAuthError reports a failed login or registration attempt.
//
// HTMX requests get only the alert fragment, which the form swaps into its
// #auth-error slot; rendering the full page here would nest the entire layout
// inside the auth card. Plain form posts still get the whole page so the
// message survives without JavaScript.
//
// The status is the real one -- 400 for a validation failure, 429 when the
// attempt was throttled. htmx drops the body of a 4xx by default, so the
// htmx-config meta tag in the base layout opts both codes into being swapped
// rather than the handler downgrading them to 200 to get the message through.
func (h *Handler) renderAuthError(w http.ResponseWriter, r *http.Request, status int, page, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	var err error
	if r.Header.Get("HX-Request") == "true" {
		err = h.templates.RenderPartial(w, "auth_error", map[string]any{"Error": message})
	} else {
		err = h.templates.Render(w, page, map[string]any{"Title": title, "Error": message})
	}
	if err != nil {
		slog.Error("failed to render auth error", "error", err, "page", page)
	}
}
