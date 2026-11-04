package basketball

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
)

// UserBetReader describes the bets a user already holds on a set of games, keyed
// by game ID. Narrow because this handler needs one method off the bets service.
type UserBetReader interface {
	UserBetSummaries(userID uuid.UUID, gameIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// Handler handles basketball HTTP requests.
type Handler struct {
	service   *Service
	templates *templates.Renderer
	userBets  UserBetReader
}

// NewHandler creates a new basketball handler.
func NewHandler(service *Service, renderer *templates.Renderer) *Handler {
	return &Handler{
		service:   service,
		templates: renderer,
	}
}

// SetUserBetReader wires in the source of "you already bet this" state. The grid
// simply shows no such marker without it.
func (h *Handler) SetUserBetReader(reader UserBetReader) {
	h.userBets = reader
}

// RegisterRoutes registers basketball routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	wrap := func(hf http.HandlerFunc) http.Handler {
		return authMiddleware(hf)
	}

	mux.Handle("GET /basketball", wrap(h.Index))
	mux.Handle("GET /basketball/games", wrap(h.ListGames))
}

// Index redirects to the games listing on the day to open on.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	day, err := h.service.DefaultDay()
	if err != nil {
		slog.Error("failed to resolve the default basketball day", "error", err)
		day = h.service.Today()
	}
	http.Redirect(w, r, dayURL(day, DayQuery{}), http.StatusSeeOther)
}

// ListGames displays basketball games for a single day. A season parameter with no
// date is the dropdown asking for that season, which redirects to a day in it.
func (h *Handler) ListGames(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	params := r.URL.Query()
	if params.Get("date") == "" {
		if season, err := strconv.Atoi(params.Get("season")); err == nil {
			day, ok, err := h.service.DayInSeason(season)
			if err != nil {
				slog.Error("failed to resolve a day in the basketball season", "season", season, "error", err)
			}
			if ok {
				http.Redirect(w, r, dayURL(day, DayQuery{}), http.StatusSeeOther)
				return
			}
		}
	}

	// Which day a game falls on depends on a timezone, so the service owns it.
	date := h.service.ParseDate(params.Get("date"))
	query := DayQuery{
		Search:     strings.TrimSpace(params.Get("search")),
		Conference: params.Get("conference"),
		AllGames:   params.Get("lines") == "all",
	}

	day, err := h.service.GetDay(date, query)
	if err != nil {
		slog.Error("failed to fetch basketball games", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.annotateUserBets(user.ID, day.Games)

	seasons, err := h.service.Seasons()
	if err != nil {
		// Losing the dropdown costs the reader a shortcut, not the page.
		slog.Error("failed to fetch basketball seasons", "error", err)
	}

	data := map[string]any{
		"Title":       "Basketball",
		"User":        user,
		"Games":       day.Games,
		"Date":        date,
		"DateStr":     date.Format(time.DateOnly),
		"Search":      query.Search,
		"Conference":  query.Conference,
		"AllGames":    query.AllGames,
		"Conferences": day.Conferences,
		"TotalGames":  day.Total,
		"LinesOnly":   day.LinesOnly,
		"HiddenGames": day.HiddenGames,
		"Seasons":     seasons,
		"Season":      h.service.SeasonOf(date),
	}
	if day.PrevDay != nil {
		data["PrevURL"] = dayURL(*day.PrevDay, query)
	}
	if day.NextDay != nil {
		data["NextURL"] = dayURL(*day.NextDay, query)
	}
	allGames, linesOnly := query, query
	allGames.AllGames, linesOnly.AllGames = true, false
	data["ShowAllURL"] = dayURL(date, allGames)
	data["LinesOnlyURL"] = dayURL(date, linesOnly)
	data["ClearURL"] = dayURL(date, DayQuery{})

	if err := h.templates.Render(w, "basketball_games", data); err != nil {
		slog.Error("failed to render basketball games", "error", err)
	}
}

// annotateUserBets describes, on each game, the bet the user already holds on it.
// A reader error, or none wired in, only costs the marker.
func (h *Handler) annotateUserBets(userID uuid.UUID, games []GameWithOdds) {
	if h.userBets == nil || len(games) == 0 {
		return
	}

	gameIDs := make([]uuid.UUID, len(games))
	for i, g := range games {
		gameIDs[i] = g.Game.ID
	}

	summaries, err := h.userBets.UserBetSummaries(userID, gameIDs)
	if err != nil {
		slog.Error("failed to fetch user bet markers", "error", err)
		return
	}
	for i, g := range games {
		games[i].BetSummary = summaries[g.Game.ID]
	}
}

// dayURL links to a day with the same query, so changing day does not silently
// reset a filter.
func dayURL(day time.Time, query DayQuery) string {
	values := url.Values{"date": {day.Format(time.DateOnly)}}
	if query.Search != "" {
		values.Set("search", query.Search)
	}
	if query.Conference != "" {
		values.Set("conference", query.Conference)
	}
	if query.AllGames {
		values.Set("lines", "all")
	}
	return "/basketball/games?" + values.Encode()
}
