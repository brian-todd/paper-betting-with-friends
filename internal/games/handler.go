package games

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Handler handles game HTTP requests.
type Handler struct {
	service    *Service
	templates  *templates.Renderer
	leagueRepo *repository.LeagueRepository
	purseRepo  *repository.PurseRepository
}

// NewHandler creates a new games handler.
func NewHandler(service *Service, renderer *templates.Renderer, db *gorm.DB) *Handler {
	return &Handler{
		service:    service,
		templates:  renderer,
		leagueRepo: repository.NewLeagueRepository(db),
		purseRepo:  repository.NewPurseRepository(db),
	}
}

// RegisterRoutes registers game routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Wrap handlers with auth middleware.
	wrap := func(hf http.HandlerFunc) http.Handler {
		return authMiddleware(hf)
	}

	mux.Handle("GET /games", wrap(h.ListGames))
	mux.Handle("GET /games/{season}/{seasonType}/{week}", wrap(h.ShowWeekGames))
	mux.Handle("GET /games/{gameID}", wrap(h.ShowGameDetail))
}

// ListGames redirects to the current week's games or shows a season selector.
func (h *Handler) ListGames(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Try to find the current week.
	season, weekNumber, seasonType, found := h.service.GetCurrentWeek()
	if found {
		url := "/games/" + strconv.Itoa(season) + "/" + string(seasonType) + "/" + strconv.Itoa(weekNumber)
		http.Redirect(w, r, url, http.StatusSeeOther)
		return
	}

	// No weeks available, show empty state.
	seasons, _ := h.service.GetAvailableSeasons()

	h.templates.Render(w, "games", map[string]any{
		"Title":   "Games",
		"User":    user,
		"Seasons": seasons,
	})
}

// ShowWeekGames displays all games for a given season, season type, and week.
func (h *Handler) ShowWeekGames(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse season from URL.
	season, err := strconv.Atoi(r.PathValue("season"))
	if err != nil {
		http.Error(w, "Invalid season", http.StatusBadRequest)
		return
	}

	// Parse season type from URL.
	seasonTypeStr := r.PathValue("seasonType")
	seasonType := models.SeasonType(seasonTypeStr)
	if seasonType != models.SeasonTypeRegular && seasonType != models.SeasonTypePostseason {
		http.Error(w, "Invalid season type", http.StatusBadRequest)
		return
	}

	// Parse week from URL.
	weekNumber, err := strconv.Atoi(r.PathValue("week"))
	if err != nil {
		http.Error(w, "Invalid week", http.StatusBadRequest)
		return
	}

	filter := ParseFilter(r.URL.Query())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))

	// Get the requested page of the week's filtered games.
	weekData, err := h.service.GetWeekWithGames(season, weekNumber, seasonType, filter.Repository(), page)
	if err != nil {
		if errors.Is(err, ErrWeekNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("failed to fetching week games", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Get all weeks for this season and type for navigation.
	seasonWeeks, err := h.service.GetSeasonWeeks(season, seasonType)
	if err != nil {
		slog.Error("failed to fetching season weeks", "error", err)
	}

	// Get available seasons for the dropdown.
	seasons, err := h.service.GetAvailableSeasons()
	if err != nil {
		slog.Error("failed to fetching seasons", "error", err)
	}

	// Get available season types for this season.
	seasonTypes, err := h.service.GetAvailableSeasonTypes(season)
	if err != nil {
		slog.Error("failed to fetching season types", "error", err)
	}

	weekPath := "/games/" + strconv.Itoa(season) + "/" + string(seasonType) + "/" + strconv.Itoa(weekNumber)

	h.templates.Render(w, "games", map[string]any{
		"Title":       "Week " + strconv.Itoa(weekNumber) + " Games",
		"User":        user,
		"Season":      season,
		"SeasonType":  seasonType,
		"WeekNumber":  weekNumber,
		"Week":        weekData.Week,
		"Games":       weekData.Games,
		"SeasonWeeks": seasonWeeks,
		"Seasons":     seasons,
		"SeasonTypes": seasonTypes,

		// Filter state, plus everything the controls need to redraw themselves
		// the way the user left them.
		"WeekPath": weekPath,
		"Filter":   filter,
		// FilterQuery lets the season and week navigation carry the filter
		// across, so changing week does not silently reset it.
		"FilterQuery":         filter.Query().Encode(),
		"FilterActive":        filter.Active(),
		"SelectedTiers":       filter.SelectedTiers(),
		"SelectedConferences": filter.SelectedConferences(),
		"SelectedWeekdays":    filter.SelectedWeekdays(),
		"ConferenceGroups":    GroupConferences(weekData.Conferences),
		"TierOptions":         TierOptions(),
		"StatusOptions":       StatusOptions(),
		"WeekdayOptions":      WeekdayOptions(),
		"HourOptions":         HourOptions(),
		"Zone":                ZoneAbbreviation(h.service.Location()),

		"Page":        weekData.Page,
		"TotalInWeek": weekData.TotalInWeek,
		"PrevURL":     pageURL(weekPath, filter, weekData.Page.Prev()),
		"NextURL":     pageURL(weekPath, filter, weekData.Page.Next()),
		"ClearURL":    weekPath + "?" + appliedParam + "=1",
	})
}

// ShowGameDetail displays a single game with all betting options.
func (h *Handler) ShowGameDetail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse game ID from URL.
	gameIDStr := r.PathValue("gameID")
	gameID, err := uuid.Parse(gameIDStr)
	if err != nil {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	// Get game detail with all odds.
	gameDetail, err := h.service.GetGameDetail(gameID)
	if err != nil {
		if errors.Is(err, ErrGameNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("failed to fetching game detail", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Get user's leagues for the bet slip.
	userLeagues, err := h.leagueRepo.FindUserLeagues(user.ID)
	if err != nil {
		slog.Error("failed to fetching user leagues", "error", err)
	}

	// Get user's purses for balance display.
	purses, err := h.purseRepo.FindByUser(user.ID)
	if err != nil {
		slog.Error("failed to fetching user purses", "error", err)
	}

	// Build a map of league ID to balance for easy template access.
	purseBalances := make(map[string]string)
	for _, purse := range purses {
		purseBalances[purse.LeagueID.String()] = purse.Balance.StringFixed(2)
	}

	// Build title from matchup.
	title := gameDetail.Game.AwayTeam.Abbreviation + " @ " + gameDetail.Game.HomeTeam.Abbreviation

	h.templates.Render(w, "game_detail", map[string]any{
		"Title":         title,
		"User":          user,
		"Game":          gameDetail.Game,
		"Result":        gameDetail.Result,
		"UnifiedOdds":   gameDetail.UnifiedOdds,
		"UserLeagues":   userLeagues,
		"PurseBalances": purseBalances,
		"Success":       r.URL.Query().Get("success"),
		"Error":         r.URL.Query().Get("error"),
	})
}

// pageURL builds a link to another page of the same filtered result set. The
// filter has to ride along or paging would silently widen it.
func pageURL(weekPath string, filter Filter, page int) string {
	query := filter.Query()
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}
	if len(query) == 0 {
		return weekPath
	}
	return weekPath + "?" + query.Encode()
}
