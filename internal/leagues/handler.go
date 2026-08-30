package leagues

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Handler handles league HTTP requests.
type Handler struct {
	service   *Service
	templates *templates.Renderer
}

// NewHandler creates a new leagues handler.
func NewHandler(service *Service, renderer *templates.Renderer) *Handler {
	return &Handler{
		service:   service,
		templates: renderer,
	}
}

// RegisterRoutes registers league routes on the provided mux.
// All routes require authentication via the provided middleware.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Wrap handlers with auth middleware.
	wrap := func(hf http.HandlerFunc) http.Handler {
		return authMiddleware(hf)
	}

	mux.Handle("GET /leagues", wrap(h.ListLeagues))
	mux.Handle("GET /leagues/create", wrap(h.ShowCreateForm))
	mux.Handle("POST /leagues", wrap(h.CreateLeague))
	mux.Handle("GET /leagues/{id}", wrap(h.ShowLeague))
	mux.Handle("POST /leagues/{id}/join", wrap(h.JoinLeague))
	mux.Handle("POST /leagues/join", wrap(h.JoinByCode))
	mux.Handle("POST /leagues/{id}/leave", wrap(h.LeaveLeague))
	mux.Handle("POST /leagues/{id}/delete", wrap(h.DeleteLeague))
}

// ListLeagues renders the leagues page showing user's leagues and available public leagues.
func (h *Handler) ListLeagues(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	myLeagues, err := h.service.GetUserLeagues(user.ID)
	if err != nil {
		slog.Error("failed to fetching user leagues", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	availableLeagues, err := h.service.GetAvailableLeagues(user.ID)
	if err != nil {
		slog.Error("failed to fetching available leagues", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.templates.Render(w, "leagues", map[string]any{
		"Title":            "My Leagues",
		"User":             user,
		"MyLeagues":        myLeagues,
		"AvailableLeagues": availableLeagues,
		"Success":          r.URL.Query().Get("success"),
		"Error":            r.URL.Query().Get("error"),
	})
}

// ShowCreateForm renders the create league form.
func (h *Handler) ShowCreateForm(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.templates.Render(w, "league_create", map[string]any{
		"Title": "Create League",
		"User":  user,
	})
}

// CreateLeague handles the create league form submission.
func (h *Handler) CreateLeague(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderCreateError(w, user, "League name is required")
		return
	}

	isPublic := r.FormValue("is_public") == "on" || r.FormValue("is_public") == "true"

	// Parse starting balance with default of 1000.
	startingBalance := decimal.NewFromInt(1000)
	if balanceStr := r.FormValue("starting_balance"); balanceStr != "" {
		if parsed, err := decimal.NewFromString(balanceStr); err == nil && parsed.IsPositive() {
			startingBalance = parsed
		}
	}

	league, err := h.service.CreateLeague(name, user.ID, isPublic, startingBalance)
	if err != nil {
		slog.Error("failed to creating league", "error", err)
		h.renderCreateError(w, user, "Failed to create league")
		return
	}

	http.Redirect(w, r, "/leagues/"+league.ID.String()+"?success=created", http.StatusSeeOther)
}

// ShowLeague renders the league detail page.
func (h *Handler) ShowLeague(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	leagueID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	details, err := h.service.GetLeagueDetails(leagueID, user.ID)
	if err != nil {
		if errors.Is(err, ErrLeagueNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("failed to fetching league", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Get purse balance if user is a member.
	var purseBalance decimal.Decimal
	if details.IsMember {
		purseBalance, _ = h.service.GetPurseBalance(leagueID, user.ID)
	}

	// Get leaderboard data.
	leaderboard, err := h.service.GetLeaderboard(leagueID)
	if err != nil {
		slog.Error("failed to fetching leaderboard", "error", err)
		// Continue without leaderboard data.
		leaderboard = nil
	}

	weeklyStats, err := h.service.GetWeeklyStats(leagueID, user.ID)
	if err != nil {
		slog.Error("failed to fetch weekly stats", "error", err)
		// Continue without the weekly breakdown.
		weeklyStats = nil
	}

	h.templates.Render(w, "league_detail", map[string]any{
		"Title":        details.League.Name,
		"User":         user,
		"League":       details.League,
		"IsMember":     details.IsMember,
		"IsAdmin":      details.IsAdmin,
		"IsCreator":    details.IsCreator,
		"PurseBalance": purseBalance,
		"Leaderboard":  leaderboard,
		"WeeklyStats":  weeklyStats,
		"Success":      r.URL.Query().Get("success"),
		"Error":        r.URL.Query().Get("error"),
	})
}

// JoinLeague handles joining a public league.
func (h *Handler) JoinLeague(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	leagueID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	if err := h.service.JoinLeague(leagueID, user.ID); err != nil {
		redirectURL := "/leagues?error=" + errorMessage(err)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/leagues/"+leagueID.String()+"?success=joined", http.StatusSeeOther)
}

// JoinByCode handles joining a league via invite code.
func (h *Handler) JoinByCode(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	code := strings.TrimSpace(r.FormValue("code"))
	if code == "" {
		http.Redirect(w, r, "/leagues?error=code_required", http.StatusSeeOther)
		return
	}

	league, err := h.service.JoinByCode(code, user.ID)
	if err != nil {
		redirectURL := "/leagues?error=" + errorMessage(err)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/leagues/"+league.ID.String()+"?success=joined", http.StatusSeeOther)
}

// LeaveLeague handles leaving a league.
func (h *Handler) LeaveLeague(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	leagueID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	if err := h.service.LeaveLeague(leagueID, user.ID); err != nil {
		redirectURL := "/leagues/" + leagueID.String() + "?error=" + errorMessage(err)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/leagues?success=left", http.StatusSeeOther)
}

// DeleteLeague handles deleting a league (admin only).
func (h *Handler) DeleteLeague(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	leagueID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	if err := h.service.DeleteLeague(leagueID, user.ID); err != nil {
		redirectURL := "/leagues/" + leagueID.String() + "?error=" + errorMessage(err)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/leagues?success=deleted", http.StatusSeeOther)
}

// renderCreateError re-renders create form with error.
func (h *Handler) renderCreateError(w http.ResponseWriter, user any, message string) {
	w.WriteHeader(http.StatusBadRequest)
	h.templates.Render(w, "league_create", map[string]any{
		"Title": "Create League",
		"User":  user,
		"Error": message,
	})
}

// errorMessage converts errors to URL-safe error codes.
func errorMessage(err error) string {
	switch {
	case errors.Is(err, ErrLeagueNotFound):
		return "not_found"
	case errors.Is(err, ErrAlreadyMember):
		return "already_member"
	case errors.Is(err, ErrNotMember):
		return "not_member"
	case errors.Is(err, ErrCannotLeave):
		return "cannot_leave"
	case errors.Is(err, ErrLeagueNotPublic):
		return "not_public"
	case errors.Is(err, ErrInvalidCode):
		return "invalid_code"
	case errors.Is(err, ErrNotAuthorized):
		return "not_authorized"
	default:
		return "error"
	}
}
