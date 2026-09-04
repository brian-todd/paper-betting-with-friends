package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/brian/paper-betting-with-friends/internal/auth"
	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/brian/paper-betting-with-friends/internal/templates"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Handler handles admin HTTP requests.
type Handler struct {
	service   *Service
	templates *templates.Renderer
}

// NewHandler creates a new admin handler.
func NewHandler(service *Service, renderer *templates.Renderer) *Handler {
	return &Handler{service: service, templates: renderer}
}

// RegisterRoutes registers admin routes on the provided mux.
//
// Every route goes through guard, which is the whole access control story for
// this package: authMiddleware resolves the session and puts the user in the
// context, and RequireAdmin refuses anyone without the flag. A route registered
// outside guard would be public, so there is a test asserting none is.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	guard := func(fn http.HandlerFunc) http.Handler {
		return authMiddleware(auth.RequireAdmin()(fn))
	}

	mux.Handle("GET /admin", guard(h.Dashboard))

	mux.Handle("GET /admin/users", guard(h.ListUsers))
	mux.Handle("POST /admin/users/{id}/password", guard(h.UpdatePassword))
	mux.Handle("POST /admin/users/{id}/username", guard(h.UpdateUsername))
	mux.Handle("POST /admin/users/{id}/delete", guard(h.DeleteUser))

	mux.Handle("GET /admin/leagues", guard(h.ListLeagues))
	mux.Handle("POST /admin/leagues", guard(h.CreateLeague))
	mux.Handle("POST /admin/leagues/{id}/delete", guard(h.DeleteLeague))
	mux.Handle("POST /admin/leagues/{id}/members", guard(h.AddMember))
	mux.Handle("POST /admin/leagues/{id}/members/{userId}/remove", guard(h.RemoveMember))
	mux.Handle("POST /admin/leagues/{id}/members/{userId}/balance", guard(h.SetBalance))

	mux.Handle("GET /admin/bets", guard(h.ListBets))
	mux.Handle("POST /admin/bets/{type}/{id}/status", guard(h.SetBetStatus))

	mux.Handle("GET /admin/sync", guard(h.ShowSync))
	mux.Handle("POST /admin/sync/{job}/run", guard(h.RunSync))

	mux.Handle("GET /admin/games", guard(h.SearchGames))
	mux.Handle("GET /admin/games/{id}", guard(h.ShowGame))
	mux.Handle("POST /admin/games/{id}/evaluate", guard(h.EvaluateGame))
	mux.Handle("POST /admin/games/{id}/finalize", guard(h.FinalizeGame))

	mux.Handle("GET /admin/audit", guard(h.ListAudit))
}

// successMessages maps the code carried by a post-redirect-get back to the
// banner it should show.
//
// The message is resolved here and passed in the render data. An earlier
// version had the templates reach for the request object, which was never put
// in the data at all -- the pages simply stopped rendering at the first banner.
var successMessages = map[string]string{
	"password":       "Password updated.",
	"username":       "Username updated.",
	"user_deleted":   "User deleted.",
	"created":        "League created.",
	"league_deleted": "League deleted.",
	"member_added":   "Member added.",
	"member_removed": "Member removed.",
	"balance":        "Balance updated.",
	"bet":            "Bet updated.",
	"sync":           "Sync started.",
	"evaluated":      "Bets re-evaluated.",
	"finalized":      "Result finalized and bets settled.",
}

// render writes a full admin page, folding in the fields every one of them
// needs and reporting a template failure rather than truncating in silence.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Title"] = title
	data["User"] = auth.UserFromContext(r.Context())
	if _, ok := data["Success"]; !ok {
		data["Success"] = successMessages[r.URL.Query().Get("success")]
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := h.templates.Render(w, name, data); err != nil {
		slog.Error("template render failed", "template", name, "error", err)
	}
}

// errorMessage turns a domain error into something worth showing an operator.
func errorMessage(err error) string {
	switch {
	case errors.Is(err, ErrProtectedAccount):
		return "That is the site administrator account and cannot be renamed or deleted."
	case errors.Is(err, ErrUserNotFound):
		return "User not found."
	case errors.Is(err, ErrLeagueNotFound):
		return "League not found."
	case errors.Is(err, ErrUsernameTaken):
		return "That username is already taken."
	case errors.Is(err, ErrConfirmationMismatch):
		return "Confirmation did not match. Nothing was deleted."
	case errors.Is(err, ErrInvalidBalance):
		return "Enter the balance as a number, for example 1000.00."
	case errors.Is(err, bets.ErrBetNotFound):
		return "Bet not found."
	case errors.Is(err, bets.ErrInvalidBetType), errors.Is(err, bets.ErrInvalidBetStatus):
		return "That is not a bet status this app uses."
	case errors.Is(err, scheduler.ErrUnknownJob):
		return "No sync job by that name is registered."
	case errors.Is(err, scheduler.ErrRunPending):
		return "That sync is already queued to run."
	case errors.Is(err, ErrInvalidSeason):
		return "Enter a season year from 2002 onwards, or leave it blank for the current season."
	default:
		return "Something went wrong. Check the logs for details."
	}
}

// Dashboard renders the admin dashboard.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "admin", "Admin Dashboard", nil)
}

// ---- Users ----

// ListUsers renders the user management page.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	h.renderUsers(w, r, http.StatusOK, "")
}

// renderUsers draws the users page, optionally with an error banner.
func (h *Handler) renderUsers(w http.ResponseWriter, r *http.Request, status int, message string) {
	users, err := h.service.ListUsers()
	if err != nil {
		slog.Error("failed to load users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{"Users": users, "AdminUsername": h.service.AdminUsername()}
	if message != "" {
		data["Error"] = message
		data["Success"] = ""
	}
	h.render(w, r, status, "admin_users", "Manage Users", data)
}

// UpdatePassword handles updating a user's password.
func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.pathUUID(w, r, "id", "user")
	if !ok {
		return
	}

	newPassword := r.FormValue("password")
	if len(newPassword) < 8 {
		h.renderUsers(w, r, http.StatusBadRequest, "Password must be at least 8 characters.")
		return
	}

	if err := h.service.UpdateUserPassword(auth.UserFromContext(r.Context()), userID, newPassword); err != nil {
		slog.Error("failed to update password", "user", userID, "error", err)
		h.renderUsers(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/users?success=password", http.StatusSeeOther)
}

// UpdateUsername handles renaming a user.
func (h *Handler) UpdateUsername(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.pathUUID(w, r, "id", "user")
	if !ok {
		return
	}

	newUsername := strings.TrimSpace(r.FormValue("username"))
	if newUsername == "" {
		h.renderUsers(w, r, http.StatusBadRequest, "Username cannot be empty.")
		return
	}

	if err := h.service.UpdateUserUsername(auth.UserFromContext(r.Context()), userID, newUsername); err != nil {
		slog.Error("failed to update username", "user", userID, "error", err)
		h.renderUsers(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/users?success=username", http.StatusSeeOther)
}

// DeleteUser removes a user and everything they have bet.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.pathUUID(w, r, "id", "user")
	if !ok {
		return
	}

	err := h.service.DeleteUser(auth.UserFromContext(r.Context()), userID, r.FormValue("confirm"))
	if err != nil {
		slog.Error("failed to delete user", "user", userID, "error", err)
		h.renderUsers(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/users?success=user_deleted", http.StatusSeeOther)
}

// ---- Leagues ----

// ListLeagues renders the league management page.
func (h *Handler) ListLeagues(w http.ResponseWriter, r *http.Request) {
	h.renderLeagues(w, r, http.StatusOK, "")
}

// renderLeagues draws the leagues page, optionally with an error banner.
func (h *Handler) renderLeagues(w http.ResponseWriter, r *http.Request, status int, message string) {
	leagues, err := h.service.ListLeagues()
	if err != nil {
		slog.Error("failed to load leagues", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	users, err := h.service.GetAllUsers()
	if err != nil {
		slog.Error("failed to load users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{"Leagues": leagues, "Users": users}
	if message != "" {
		data["Error"] = message
		data["Success"] = ""
	}
	h.render(w, r, status, "admin_leagues", "Manage Leagues", data)
}

// defaultStartingBalance matches the league model's column default, so a league
// created here funds its members like one created from the public form.
var defaultStartingBalance = decimal.NewFromInt(1000)

// CreateLeague handles creating a new league.
func (h *Handler) CreateLeague(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderLeagues(w, r, http.StatusBadRequest, "League name is required.")
		return
	}

	balance, err := parseBalance(r.FormValue("starting_balance"), defaultStartingBalance)
	if err != nil {
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	if _, err := h.service.CreateLeague(auth.UserFromContext(r.Context()), name, balance); err != nil {
		slog.Error("failed to create league", "error", err)
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/leagues?success=created", http.StatusSeeOther)
}

// DeleteLeague removes a league and every bet placed in it.
func (h *Handler) DeleteLeague(w http.ResponseWriter, r *http.Request) {
	leagueID, ok := h.pathUUID(w, r, "id", "league")
	if !ok {
		return
	}

	err := h.service.DeleteLeague(auth.UserFromContext(r.Context()), leagueID, r.FormValue("confirm"))
	if err != nil {
		slog.Error("failed to delete league", "league", leagueID, "error", err)
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/leagues?success=league_deleted", http.StatusSeeOther)
}

// AddMember handles adding a user to a league.
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	leagueID, ok := h.pathUUID(w, r, "id", "league")
	if !ok {
		return
	}

	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		h.renderLeagues(w, r, http.StatusBadRequest, "Select a user to add.")
		return
	}

	if err := h.service.AddLeagueMember(auth.UserFromContext(r.Context()), leagueID, userID); err != nil {
		slog.Error("failed to add member", "league", leagueID, "user", userID, "error", err)
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/leagues?success=member_added", http.StatusSeeOther)
}

// RemoveMember handles removing a user from a league.
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	leagueID, ok := h.pathUUID(w, r, "id", "league")
	if !ok {
		return
	}
	userID, ok := h.pathUUID(w, r, "userId", "user")
	if !ok {
		return
	}

	if err := h.service.RemoveLeagueMember(auth.UserFromContext(r.Context()), leagueID, userID); err != nil {
		slog.Error("failed to remove member", "league", leagueID, "user", userID, "error", err)
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/leagues?success=member_removed", http.StatusSeeOther)
}

// SetBalance overwrites a member's purse balance in a league.
func (h *Handler) SetBalance(w http.ResponseWriter, r *http.Request) {
	leagueID, ok := h.pathUUID(w, r, "id", "league")
	if !ok {
		return
	}
	userID, ok := h.pathUUID(w, r, "userId", "user")
	if !ok {
		return
	}

	balance, err := parseBalance(r.FormValue("balance"), decimal.Zero)
	if err != nil {
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	if err := h.service.SetPurseBalance(auth.UserFromContext(r.Context()), leagueID, userID, balance); err != nil {
		slog.Error("failed to set balance", "league", leagueID, "user", userID, "error", err)
		h.renderLeagues(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/leagues?success=balance", http.StatusSeeOther)
}

// ---- Bets ----

// ListBets renders the bet browser across every user and league.
func (h *Handler) ListBets(w http.ResponseWriter, r *http.Request) {
	h.renderBets(w, r, http.StatusOK, "")
}

func (h *Handler) renderBets(w http.ResponseWriter, r *http.Request, status int, message string) {
	filter := repository.BetFilter{}
	query := r.URL.Query()

	// A failed status change re-renders through here, and that POST carries
	// only ?back= -- none of the filter fields. Recover the browse it was made
	// from, so the error appears in place instead of at the unfiltered top of
	// the list, which is where the successful path already returns.
	if raw := query.Get("back"); raw != "" {
		if parsed, err := url.Parse(backURL(raw)); err == nil {
			query = parsed.Query()
		}
	}

	if raw := query.Get("league_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.LeagueID = &id
		}
	}
	if raw := query.Get("user_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.UserID = &id
		}
	}
	if raw := query.Get("status"); raw != "" {
		betStatus := models.BetStatus(raw)
		filter.Status = &betStatus
	}

	page, _ := strconv.Atoi(query.Get("page"))
	list, pageInfo, err := h.service.ListBets(filter, page)
	if err != nil {
		slog.Error("failed to load bets", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	users, err := h.service.GetAllUsers()
	if err != nil {
		slog.Error("failed to load users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	leagues, err := h.service.GetAllLeagues()
	if err != nil {
		slog.Error("failed to load leagues", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Bets":             list,
		"Page":             pageInfo,
		"PrevURL":          adminBetsURL(query, pageInfo.Prev()),
		"NextURL":          adminBetsURL(query, pageInfo.Next()),
		"BackURL":          adminBetsURL(query, pageInfo.Number),
		"Users":            users,
		"Leagues":          leagues,
		"SelectedLeague":   query.Get("league_id"),
		"SelectedUser":     query.Get("user_id"),
		"SelectedStatus":   query.Get("status"),
		"Statuses":         []models.BetStatus{models.BetStatusPending, models.BetStatusWon, models.BetStatusLost, models.BetStatusPush, models.BetStatusVoid},
		"SettleableStatus": []models.BetStatus{models.BetStatusWon, models.BetStatusLost, models.BetStatusPush, models.BetStatusVoid, models.BetStatusPending},
	}
	if message != "" {
		data["Error"] = message
		data["Success"] = ""
	}
	h.render(w, r, status, "admin_bets", "Manage Bets", data)
}

// SetBetStatus forces a bet into a status, moving the purse to match.
func (h *Handler) SetBetStatus(w http.ResponseWriter, r *http.Request) {
	betID, ok := h.pathUUID(w, r, "id", "bet")
	if !ok {
		return
	}

	betType := r.PathValue("type")
	status := models.BetStatus(r.FormValue("status"))

	if err := h.service.SetBetStatus(auth.UserFromContext(r.Context()), betType, betID, status); err != nil {
		slog.Error("failed to set bet status", "bet", betID, "type", betType, "status", status, "error", err)
		h.renderBets(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	// Return to the filters and page the correction was made from, rather than
	// dumping the admin at the unfiltered top of the list.
	//
	// The listing passes that destination in as ?back= on the form action. It
	// cannot travel as ordinary filter fields in the form body, because the
	// status filter and the new bet status would both be named "status".
	back := backURL(r.URL.Query().Get("back"))
	if strings.Contains(back, "?") {
		back += "&success=bet"
	} else {
		back += "?success=bet"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// backURL validates a caller-supplied return destination.
//
// It is reflected into a Location header, so anything that is not recognisably
// this one listing is discarded rather than trusted: a value the user controls
// reaching a redirect unchecked is an open redirect. Requiring the exact base
// path is what rejects an absolute URL, a protocol-relative "//host" that would
// leave the site entirely, and a "/admin/betsomething" lookalike alike.
func backURL(raw string) string {
	const base = "/admin/bets"
	if raw != base && !strings.HasPrefix(raw, base+"?") {
		return base
	}
	return raw
}

// adminBetsURL rebuilds the bet browser URL for a different page, carrying the
// active filters.
//
// It copies only the filter keys rather than the whole query, so that a
// success or error marker from a previous action is not carried forward into
// the pager links and re-shown on every page turn. page=1 is omitted, which
// keeps the filter form -- which has no page field and so drops the parameter
// on submit -- landing on the first page of its new result set.
func adminBetsURL(query url.Values, page int) string {
	next := url.Values{}
	for _, key := range []string{"league_id", "user_id", "status"} {
		if value := query.Get(key); value != "" {
			next.Set(key, value)
		}
	}
	if page > 1 {
		next.Set("page", strconv.Itoa(page))
	}
	if len(next) == 0 {
		return "/admin/bets"
	}
	return "/admin/bets?" + next.Encode()
}

// ---- Sync and health ----

// ShowSync renders the job table and system health.
func (h *Handler) ShowSync(w http.ResponseWriter, r *http.Request) {
	h.renderSync(w, r, http.StatusOK, "")
}

func (h *Handler) renderSync(w http.ResponseWriter, r *http.Request, status int, message string) {
	health, err := h.service.Health()
	if err != nil {
		slog.Error("failed to load system health", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{"Jobs": h.service.JobStatuses(), "Health": health}
	if message != "" {
		data["Error"] = message
		data["Success"] = ""
	}
	h.render(w, r, status, "admin_sync", "Sync & Health", data)
}

// RunSync asks a background job to run now.
func (h *Handler) RunSync(w http.ResponseWriter, r *http.Request) {
	job := r.PathValue("job")

	// Only the seed jobs read a season, and it is optional even for them: blank
	// means whatever season the app currently believes it is in.
	season := strings.TrimSpace(r.FormValue("season"))

	if err := h.service.TriggerSync(auth.UserFromContext(r.Context()), job, season); err != nil {
		slog.Error("failed to trigger sync", "job", job, "error", err)
		h.renderSync(w, r, http.StatusBadRequest, errorMessage(err))
		return
	}

	http.Redirect(w, r, "/admin/sync?success=sync", http.StatusSeeOther)
}

// ---- Games ----

// SearchGames renders the game lookup.
func (h *Handler) SearchGames(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	data := map[string]any{"Query": query}
	if query != "" {
		results, err := h.service.SearchGames(query)
		if err != nil {
			slog.Error("failed to search games", "query", query, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		data["Games"] = results
	}

	h.render(w, r, http.StatusOK, "admin_games", "Games", data)
}

// ShowGame renders one game with its odds and result.
func (h *Handler) ShowGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := h.pathUUID(w, r, "id", "game")
	if !ok {
		return
	}

	detail, err := h.service.GetGameDetail(gameID)
	if err != nil {
		slog.Error("failed to load game", "game", gameID, "error", err)
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}

	h.render(w, r, http.StatusOK, "admin_game_detail", "Game", map[string]any{"Detail": detail})
}

// EvaluateGame settles the game's pending bets against its recorded score.
func (h *Handler) EvaluateGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := h.pathUUID(w, r, "id", "game")
	if !ok {
		return
	}

	if err := h.service.EvaluateGame(auth.UserFromContext(r.Context()), gameID); err != nil {
		slog.Error("failed to evaluate game", "game", gameID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/games/"+gameID.String()+"?success=evaluated", http.StatusSeeOther)
}

// FinalizeGame marks a provisional score final and settles against it.
func (h *Handler) FinalizeGame(w http.ResponseWriter, r *http.Request) {
	gameID, ok := h.pathUUID(w, r, "id", "game")
	if !ok {
		return
	}

	if err := h.service.FinalizeGame(auth.UserFromContext(r.Context()), gameID); err != nil {
		slog.Error("failed to finalize game", "game", gameID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/games/"+gameID.String()+"?success=finalized", http.StatusSeeOther)
}

// ---- Audit ----

// ListAudit renders the admin audit trail.
func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := h.service.ListAuditLog()
	if err != nil {
		slog.Error("failed to load audit log", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, http.StatusOK, "admin_audit", "Audit Log", map[string]any{"Entries": entries})
}

// ---- Helpers ----

// pathUUID parses a UUID path value, answering the request itself and
// reporting false when it could not.
func (h *Handler) pathUUID(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		http.Error(w, "Invalid "+label+" ID", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

// parseBalance reads a money amount from a form field, falling back to
// fallback when the field is empty.
func parseBalance(raw string, fallback decimal.Decimal) (decimal.Decimal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}

	value, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, ErrInvalidBalance
	}
	return value, nil
}
