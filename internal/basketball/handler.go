package basketball

import (
	"log/slog"
	"net/http"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/templates"
)

// Handler handles basketball HTTP requests.
type Handler struct {
	service   *Service
	templates *templates.Renderer
}

// NewHandler creates a new basketball handler.
func NewHandler(service *Service, renderer *templates.Renderer) *Handler {
	return &Handler{
		service:   service,
		templates: renderer,
	}
}

// RegisterRoutes registers basketball routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	wrap := func(hf http.HandlerFunc) http.Handler {
		return authMiddleware(hf)
	}

	mux.Handle("GET /basketball", wrap(h.Index))
	mux.Handle("GET /basketball/games", wrap(h.ListGames))
}

// Index redirects to the games listing with today's date.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	today := h.service.Today().Format("2006-01-02")
	http.Redirect(w, r, "/basketball/games?date="+today, http.StatusSeeOther)
}

// ListGames displays basketball games for a single day.
func (h *Handler) ListGames(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Which day a game falls on depends on a timezone, so the service owns it.
	date := h.service.ParseDate(r.URL.Query().Get("date"))

	search := r.URL.Query().Get("search")

	games, err := h.service.GetGamesForDate(date, search)
	if err != nil {
		slog.Error("failed to fetching basketball games", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	prevDate := date.AddDate(0, 0, -1).Format("2006-01-02")
	nextDate := date.AddDate(0, 0, 1).Format("2006-01-02")

	h.templates.Render(w, "basketball_games", map[string]any{
		"Title":      "Basketball",
		"User":       user,
		"Games":      games,
		"Date":       date,
		"DateStr":    date.Format("2006-01-02"),
		"PrevDate":   prevDate,
		"NextDate":   nextDate,
		"Search":     search,
		"TotalGames": len(games),
	})
}
