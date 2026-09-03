package bets

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Handler handles bet HTTP requests.
type Handler struct {
	service   *Service
	templates *templates.Renderer
	purseRepo *repository.PurseRepository
}

// NewHandler creates a new bets handler.
func NewHandler(service *Service, renderer *templates.Renderer, db *gorm.DB) *Handler {
	return &Handler{
		service:   service,
		templates: renderer,
		purseRepo: repository.NewPurseRepository(db),
	}
}

// RegisterRoutes registers bet routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	wrap := func(hf http.HandlerFunc) http.Handler {
		return authMiddleware(hf)
	}

	mux.Handle("GET /bets", wrap(h.ListBets))
	mux.Handle("POST /bets/spread", wrap(h.CreateSpreadBet))
	mux.Handle("POST /bets/moneyline", wrap(h.CreateMoneyLineBet))
	mux.Handle("POST /bets/overunder", wrap(h.CreateOverUnderBet))
	mux.Handle("POST /bets/spread/{id}/cancel", wrap(h.CancelSpreadBet))
	mux.Handle("POST /bets/moneyline/{id}/cancel", wrap(h.CancelMoneyLineBet))
	mux.Handle("POST /bets/overunder/{id}/cancel", wrap(h.CancelOverUnderBet))
	mux.Handle("POST /bets/spread/{id}/edit", wrap(h.UpdateSpreadBet))
	mux.Handle("POST /bets/moneyline/{id}/edit", wrap(h.UpdateMoneyLineBet))
	mux.Handle("POST /bets/overunder/{id}/edit", wrap(h.UpdateOverUnderBet))
	mux.Handle("POST /bets/{type}/{id}/holy-lock", wrap(h.SetHolyLock))
	mux.Handle("POST /bets/{type}/{id}/holy-lock/clear", wrap(h.ClearHolyLock))
}

// ListBets displays all bets for the current user.
func (h *Handler) ListBets(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse filter parameters.
	filter := BetListFilter{}

	if seasonStr := r.URL.Query().Get("season"); seasonStr != "" {
		if season, err := strconv.Atoi(seasonStr); err == nil {
			filter.Season = &season
		}
	}

	if weekStr := r.URL.Query().Get("week"); weekStr != "" {
		if week, err := strconv.Atoi(weekStr); err == nil {
			filter.Week = &week
		}
	}

	if leagueStr := r.URL.Query().Get("league"); leagueStr != "" {
		if leagueID, err := uuid.Parse(leagueStr); err == nil {
			filter.LeagueID = &leagueID
		}
	}

	result, err := h.service.GetUserBets(user.ID, filter)
	if err != nil {
		slog.Error("failed to fetching user bets", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// A template that panics mid-render has already sent a 200 and part of the
	// page, so this cannot become a 500 -- but an unchecked error here is how a
	// truncated page reaches the reader with nothing in the log to explain it.
	err = h.templates.Render(w, "bets", map[string]any{
		"Title":          "My Bets",
		"User":           user,
		"Bets":           result.Bets,
		"Seasons":        result.Seasons,
		"Weeks":          result.Weeks,
		"Leagues":        result.Leagues,
		"SelectedSeason": filter.Season,
		"SelectedWeek":   filter.Week,
		"SelectedLeague": filter.LeagueID,
	})
	if err != nil {
		slog.Error("failed to render bets page", "user", user.ID, "error", err)
	}
}

// CreateSpreadBet handles creating a new spread bet.
func (h *Handler) CreateSpreadBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	gameID, err := uuid.Parse(r.FormValue("game_id"))
	if err != nil {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	leagueID, err := uuid.Parse(r.FormValue("league_id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	pick := models.SpreadPick(strings.ToLower(r.FormValue("pick")))
	if pick != models.SpreadPickHome && pick != models.SpreadPickAway {
		http.Error(w, "Invalid pick", http.StatusBadRequest)
		return
	}

	stake, err := decimal.NewFromString(r.FormValue("stake"))
	if err != nil || stake.LessThanOrEqual(decimal.Zero) {
		http.Error(w, "Invalid stake", http.StatusBadRequest)
		return
	}

	input := CreateSpreadBetInput{
		UserID:   user.ID,
		LeagueID: leagueID,
		GameID:   gameID,
		Pick:     pick,
		Stake:    stake,
		HolyLock: r.FormValue("holy_lock") != "",
	}

	// Check if using existing odds or custom.
	oddsIDStr := r.FormValue("odds_id")
	if oddsIDStr != "" {
		oddsID, err := uuid.Parse(oddsIDStr)
		if err != nil {
			http.Error(w, "Invalid odds ID", http.StatusBadRequest)
			return
		}
		input.OddsID = &oddsID
	} else {
		// Custom odds.
		spreadStr := r.FormValue("custom_spread")
		oddsStr := r.FormValue("custom_odds")
		if spreadStr == "" || oddsStr == "" {
			http.Error(w, "Missing custom odds values", http.StatusBadRequest)
			return
		}
		spread, err := decimal.NewFromString(spreadStr)
		if err != nil {
			http.Error(w, "Invalid custom spread", http.StatusBadRequest)
			return
		}
		odds, err := decimal.NewFromString(oddsStr)
		if err != nil {
			http.Error(w, "Invalid custom odds", http.StatusBadRequest)
			return
		}
		input.CustomSpread = &spread
		input.CustomOdds = &odds
	}

	_, err = h.service.CreateSpreadBet(input)
	if err != nil {
		slog.Error("failed to creating spread bet", "error", err)
		h.respondWithError(w, r, gameID, user.ID, leagueID, err)
		return
	}

	// Get updated balance for HTMX response.
	newBalance := "0.00"
	if purse, err := h.purseRepo.FindByUserAndLeague(user.ID, leagueID); err == nil {
		newBalance = purse.Balance.StringFixed(2)
	}

	respondWithSuccess(w, r, gameID, leagueID, newBalance)
}

// CreateMoneyLineBet handles creating a new money line bet.
func (h *Handler) CreateMoneyLineBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	gameID, err := uuid.Parse(r.FormValue("game_id"))
	if err != nil {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	leagueID, err := uuid.Parse(r.FormValue("league_id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	pick := models.MoneyLinePick(strings.ToLower(r.FormValue("pick")))
	if pick != models.MoneyLinePickHome && pick != models.MoneyLinePickAway {
		http.Error(w, "Invalid pick", http.StatusBadRequest)
		return
	}

	stake, err := decimal.NewFromString(r.FormValue("stake"))
	if err != nil || stake.LessThanOrEqual(decimal.Zero) {
		http.Error(w, "Invalid stake", http.StatusBadRequest)
		return
	}

	input := CreateMoneyLineBetInput{
		UserID:   user.ID,
		LeagueID: leagueID,
		GameID:   gameID,
		Pick:     pick,
		Stake:    stake,
		HolyLock: r.FormValue("holy_lock") != "",
	}

	// Check if using existing odds or custom.
	oddsIDStr := r.FormValue("odds_id")
	if oddsIDStr != "" {
		oddsID, err := uuid.Parse(oddsIDStr)
		if err != nil {
			http.Error(w, "Invalid odds ID", http.StatusBadRequest)
			return
		}
		input.OddsID = &oddsID
	} else {
		// Custom odds.
		homeOddsStr := r.FormValue("custom_home_odds")
		awayOddsStr := r.FormValue("custom_away_odds")
		if homeOddsStr == "" || awayOddsStr == "" {
			http.Error(w, "Missing custom odds values", http.StatusBadRequest)
			return
		}
		homeOdds, err := decimal.NewFromString(homeOddsStr)
		if err != nil {
			http.Error(w, "Invalid custom home odds", http.StatusBadRequest)
			return
		}
		awayOdds, err := decimal.NewFromString(awayOddsStr)
		if err != nil {
			http.Error(w, "Invalid custom away odds", http.StatusBadRequest)
			return
		}
		input.CustomHomeOdds = &homeOdds
		input.CustomAwayOdds = &awayOdds
	}

	_, err = h.service.CreateMoneyLineBet(input)
	if err != nil {
		slog.Error("failed to creating money line bet", "error", err)
		h.respondWithError(w, r, gameID, user.ID, leagueID, err)
		return
	}

	// Get updated balance for HTMX response.
	newBalance := "0.00"
	if purse, err := h.purseRepo.FindByUserAndLeague(user.ID, leagueID); err == nil {
		newBalance = purse.Balance.StringFixed(2)
	}

	respondWithSuccess(w, r, gameID, leagueID, newBalance)
}

// CreateOverUnderBet handles creating a new over/under bet.
func (h *Handler) CreateOverUnderBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	gameID, err := uuid.Parse(r.FormValue("game_id"))
	if err != nil {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	leagueID, err := uuid.Parse(r.FormValue("league_id"))
	if err != nil {
		http.Error(w, "Invalid league ID", http.StatusBadRequest)
		return
	}

	pick := models.OverUnderPick(strings.ToLower(r.FormValue("pick")))
	if pick != models.OverUnderPickOver && pick != models.OverUnderPickUnder {
		http.Error(w, "Invalid pick", http.StatusBadRequest)
		return
	}

	stake, err := decimal.NewFromString(r.FormValue("stake"))
	if err != nil || stake.LessThanOrEqual(decimal.Zero) {
		http.Error(w, "Invalid stake", http.StatusBadRequest)
		return
	}

	input := CreateOverUnderBetInput{
		UserID:   user.ID,
		LeagueID: leagueID,
		GameID:   gameID,
		Pick:     pick,
		Stake:    stake,
		HolyLock: r.FormValue("holy_lock") != "",
	}

	// Check if using existing odds or custom.
	oddsIDStr := r.FormValue("odds_id")
	if oddsIDStr != "" {
		oddsID, err := uuid.Parse(oddsIDStr)
		if err != nil {
			http.Error(w, "Invalid odds ID", http.StatusBadRequest)
			return
		}
		input.OddsID = &oddsID
	} else {
		// Custom odds.
		totalStr := r.FormValue("custom_total")
		overOddsStr := r.FormValue("custom_over_odds")
		underOddsStr := r.FormValue("custom_under_odds")
		if totalStr == "" || overOddsStr == "" || underOddsStr == "" {
			http.Error(w, "Missing custom odds values", http.StatusBadRequest)
			return
		}
		total, err := decimal.NewFromString(totalStr)
		if err != nil {
			http.Error(w, "Invalid custom total", http.StatusBadRequest)
			return
		}
		overOdds, err := decimal.NewFromString(overOddsStr)
		if err != nil {
			http.Error(w, "Invalid custom over odds", http.StatusBadRequest)
			return
		}
		underOdds, err := decimal.NewFromString(underOddsStr)
		if err != nil {
			http.Error(w, "Invalid custom under odds", http.StatusBadRequest)
			return
		}
		input.CustomTotal = &total
		input.CustomOverOdds = &overOdds
		input.CustomUnderOdds = &underOdds
	}

	_, err = h.service.CreateOverUnderBet(input)
	if err != nil {
		slog.Error("failed to creating over/under bet", "error", err)
		h.respondWithError(w, r, gameID, user.ID, leagueID, err)
		return
	}

	// Get updated balance for HTMX response.
	newBalance := "0.00"
	if purse, err := h.purseRepo.FindByUserAndLeague(user.ID, leagueID); err == nil {
		newBalance = purse.Balance.StringFixed(2)
	}

	respondWithSuccess(w, r, gameID, leagueID, newBalance)
}

// UpdateSpreadBet handles editing a pending spread bet.
func (h *Handler) UpdateSpreadBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	pick := models.SpreadPick(strings.ToLower(r.FormValue("pick")))
	if pick != models.SpreadPickHome && pick != models.SpreadPickAway {
		http.Error(w, "Invalid pick", http.StatusBadRequest)
		return
	}

	stake, err := decimal.NewFromString(r.FormValue("stake"))
	if err != nil || stake.LessThanOrEqual(decimal.Zero) {
		http.Error(w, "Invalid stake", http.StatusBadRequest)
		return
	}

	input := UpdateSpreadBetInput{
		BetID:  betID,
		UserID: user.ID,
		Pick:   pick,
		Stake:  stake,
	}

	if oddsIDStr := r.FormValue("odds_id"); oddsIDStr != "" {
		oddsID, err := uuid.Parse(oddsIDStr)
		if err != nil {
			http.Error(w, "Invalid odds ID", http.StatusBadRequest)
			return
		}
		input.OddsID = &oddsID
	} else {
		spread, err := decimal.NewFromString(r.FormValue("custom_spread"))
		if err != nil {
			http.Error(w, "Invalid custom spread", http.StatusBadRequest)
			return
		}
		odds, err := decimal.NewFromString(r.FormValue("custom_odds"))
		if err != nil {
			http.Error(w, "Invalid custom odds", http.StatusBadRequest)
			return
		}
		input.CustomSpread = &spread
		input.CustomOdds = &odds
	}

	if _, err := h.service.UpdateSpreadBet(input); err != nil {
		slog.Error("failed to update spread bet", "bet", betID, "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}

// UpdateMoneyLineBet handles editing a pending money line bet.
func (h *Handler) UpdateMoneyLineBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	pick := models.MoneyLinePick(strings.ToLower(r.FormValue("pick")))
	if pick != models.MoneyLinePickHome && pick != models.MoneyLinePickAway {
		http.Error(w, "Invalid pick", http.StatusBadRequest)
		return
	}

	stake, err := decimal.NewFromString(r.FormValue("stake"))
	if err != nil || stake.LessThanOrEqual(decimal.Zero) {
		http.Error(w, "Invalid stake", http.StatusBadRequest)
		return
	}

	input := UpdateMoneyLineBetInput{
		BetID:  betID,
		UserID: user.ID,
		Pick:   pick,
		Stake:  stake,
	}

	if oddsIDStr := r.FormValue("odds_id"); oddsIDStr != "" {
		oddsID, err := uuid.Parse(oddsIDStr)
		if err != nil {
			http.Error(w, "Invalid odds ID", http.StatusBadRequest)
			return
		}
		input.OddsID = &oddsID
	} else {
		homeOdds, err := decimal.NewFromString(r.FormValue("custom_home_odds"))
		if err != nil {
			http.Error(w, "Invalid custom home odds", http.StatusBadRequest)
			return
		}
		awayOdds, err := decimal.NewFromString(r.FormValue("custom_away_odds"))
		if err != nil {
			http.Error(w, "Invalid custom away odds", http.StatusBadRequest)
			return
		}
		input.CustomHomeOdds = &homeOdds
		input.CustomAwayOdds = &awayOdds
	}

	if _, err := h.service.UpdateMoneyLineBet(input); err != nil {
		slog.Error("failed to update money line bet", "bet", betID, "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}

// UpdateOverUnderBet handles editing a pending over/under bet.
func (h *Handler) UpdateOverUnderBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	pick := models.OverUnderPick(strings.ToLower(r.FormValue("pick")))
	if pick != models.OverUnderPickOver && pick != models.OverUnderPickUnder {
		http.Error(w, "Invalid pick", http.StatusBadRequest)
		return
	}

	stake, err := decimal.NewFromString(r.FormValue("stake"))
	if err != nil || stake.LessThanOrEqual(decimal.Zero) {
		http.Error(w, "Invalid stake", http.StatusBadRequest)
		return
	}

	input := UpdateOverUnderBetInput{
		BetID:  betID,
		UserID: user.ID,
		Pick:   pick,
		Stake:  stake,
	}

	if oddsIDStr := r.FormValue("odds_id"); oddsIDStr != "" {
		oddsID, err := uuid.Parse(oddsIDStr)
		if err != nil {
			http.Error(w, "Invalid odds ID", http.StatusBadRequest)
			return
		}
		input.OddsID = &oddsID
	} else {
		total, err := decimal.NewFromString(r.FormValue("custom_total"))
		if err != nil {
			http.Error(w, "Invalid custom total", http.StatusBadRequest)
			return
		}
		overOdds, err := decimal.NewFromString(r.FormValue("custom_over_odds"))
		if err != nil {
			http.Error(w, "Invalid custom over odds", http.StatusBadRequest)
			return
		}
		underOdds, err := decimal.NewFromString(r.FormValue("custom_under_odds"))
		if err != nil {
			http.Error(w, "Invalid custom under odds", http.StatusBadRequest)
			return
		}
		input.CustomTotal = &total
		input.CustomOverOdds = &overOdds
		input.CustomUnderOdds = &underOdds
	}

	if _, err := h.service.UpdateOverUnderBet(input); err != nil {
		slog.Error("failed to update over/under bet", "bet", betID, "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}

// CancelSpreadBet handles cancelling a spread bet.
func (h *Handler) CancelSpreadBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	if err := h.service.CancelSpreadBet(betID, user.ID); err != nil {
		slog.Error("failed to cancelling spread bet", "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}

// CancelMoneyLineBet handles cancelling a money line bet.
func (h *Handler) CancelMoneyLineBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	if err := h.service.CancelMoneyLineBet(betID, user.ID); err != nil {
		slog.Error("failed to cancelling money line bet", "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}

// CancelOverUnderBet handles cancelling an over/under bet.
func (h *Handler) CancelOverUnderBet(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	if err := h.service.CancelOverUnderBet(betID, user.ID); err != nil {
		slog.Error("failed to cancelling over/under bet", "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}

// redirectBack returns the reader to the page they acted from, which for a bet
// is the list or the game detail depending on where the form lived.
func redirectBack(w http.ResponseWriter, r *http.Request) {
	referer := r.Header.Get("Referer")
	if referer == "" {
		referer = "/"
	}
	http.Redirect(w, r, referer, http.StatusSeeOther)
}

// isHTMXRequest checks if the request is an HTMX request.
func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// respondWithSuccess sends a success HTML fragment for HTMX or redirects.
func respondWithSuccess(w http.ResponseWriter, r *http.Request, gameID, leagueID uuid.UUID, newBalance string) {
	if isHTMXRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<div class="alert alert-success alert-dismissible" data-league-id="` + leagueID.String() + `" data-new-balance="` + newBalance + `">
			<span>Bet placed successfully!</span>
			<button type="button" class="alert-close" onclick="dismissAlert()">&times;</button>
		</div>`
		w.Write([]byte(html))
		return
	}
	http.Redirect(w, r, "/games/"+gameID.String()+"?success=bet_placed", http.StatusSeeOther)
}

// respondWithError sends an error HTML fragment for HTMX or redirects.
//
// It is a method rather than a plain function because a Holy Lock conflict has
// to name the bet already holding the week, which a sentinel error cannot carry
// and which is worth a query only once the placement has already failed.
func (h *Handler) respondWithError(w http.ResponseWriter, r *http.Request, gameID, userID, leagueID uuid.UUID, err error) {
	message := errorMessageText(err)
	if errors.Is(err, ErrHolyLockExists) {
		if detail := h.service.DescribeHolyLock(userID, leagueID, gameID); detail != "" {
			message = "You already have a Holy Lock this week: " + detail + ". Remove it from My Bets to move it."
		}
	}

	if isHTMXRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The detail is composed from team abbreviations, so it is escaped
		// rather than trusted: this builds HTML by concatenation.
		html := `<div class="alert alert-error alert-dismissible">
			<span>` + template.HTMLEscapeString(message) + `</span>
			<button type="button" class="alert-close" onclick="dismissAlert()">&times;</button>
		</div>`
		w.Write([]byte(html))
		return
	}
	http.Redirect(w, r, "/games/"+gameID.String()+"?error="+errorMessageCode(err), http.StatusSeeOther)
}

// errorMessageText returns a human-readable error message.
func errorMessageText(err error) string {
	switch {
	case errors.Is(err, ErrGameNotFound):
		return "Game not found."
	case errors.Is(err, ErrGameStarted):
		return "Game has already started."
	case errors.Is(err, ErrBetNotFound):
		return "Bet not found."
	case errors.Is(err, ErrBetNotPending):
		return "Bet is not pending."
	case errors.Is(err, ErrNotBetOwner):
		return "You do not own this bet."
	case errors.Is(err, ErrOddsNotFound):
		return "Selected odds not found."
	case errors.Is(err, ErrNotLeagueMember):
		return "You must be a member of the selected league."
	case errors.Is(err, ErrInsufficientFunds):
		return "Insufficient funds in your purse for this bet."
	case errors.Is(err, ErrBetNotFootballWeek):
		return "Only bets on football games can be a Holy Lock."
	case errors.Is(err, ErrHolyLockSettled):
		return "This week's Holy Lock is locked in — its game has started."
	case errors.Is(err, ErrHolyLockExists):
		return "You already have a Holy Lock this week."
	default:
		return "An error occurred. Please try again."
	}
}

// errorMessageCode converts errors to URL-safe error codes for redirects.
func errorMessageCode(err error) string {
	switch {
	case errors.Is(err, ErrGameNotFound):
		return "game_not_found"
	case errors.Is(err, ErrGameStarted):
		return "game_started"
	case errors.Is(err, ErrBetNotFound):
		return "bet_not_found"
	case errors.Is(err, ErrBetNotPending):
		return "bet_not_pending"
	case errors.Is(err, ErrNotBetOwner):
		return "not_bet_owner"
	case errors.Is(err, ErrOddsNotFound):
		return "odds_not_found"
	case errors.Is(err, ErrNotLeagueMember):
		return "not_league_member"
	case errors.Is(err, ErrInsufficientFunds):
		return "insufficient_funds"
	case errors.Is(err, ErrBetNotFootballWeek):
		return "not_football_week"
	case errors.Is(err, ErrHolyLockSettled):
		return "holy_lock_settled"
	case errors.Is(err, ErrHolyLockExists):
		return "holy_lock_exists"
	default:
		return "error"
	}
}

// SetHolyLock designates a bet as the caller's Holy Lock for its week.
func (h *Handler) SetHolyLock(w http.ResponseWriter, r *http.Request) {
	h.holyLock(w, r, h.service.SetHolyLock, "failed to set holy lock")
}

// ClearHolyLock removes the Holy Lock designation from a bet.
func (h *Handler) ClearHolyLock(w http.ResponseWriter, r *http.Request) {
	h.holyLock(w, r, h.service.ClearHolyLock, "failed to clear holy lock")
}

// holyLock is the shape both designation handlers share. The bet type comes
// from the path rather than from three separate routes, as it does for the
// admin status route, because the service already switches on it.
func (h *Handler) holyLock(w http.ResponseWriter, r *http.Request, action func(string, uuid.UUID, uuid.UUID) error, logMsg string) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	betID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid bet ID", http.StatusBadRequest)
		return
	}

	if err := action(r.PathValue("type"), betID, user.ID); err != nil {
		slog.Error(logMsg, "bet", betID, "error", err)
		http.Error(w, errorMessageText(err), http.StatusBadRequest)
		return
	}

	redirectBack(w, r)
}
