package leagues

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrLeagueNotFound  = errors.New("league not found")
	ErrAlreadyMember   = errors.New("already a member of this league")
	ErrNotMember       = errors.New("not a member of this league")
	ErrCannotLeave     = errors.New("league creator cannot leave the league")
	ErrLeagueNotPublic = errors.New("league is not public")
	ErrInvalidCode     = errors.New("invalid invite code")
	ErrNotAuthorized   = errors.New("not authorized to perform this action")
	ErrInvalidName     = errors.New("league name is required")
)

// MaxLeagueNameLength bounds a league name to the width of the column that
// stores it, counted in characters because that is what varchar(255) counts.
const MaxLeagueNameLength = 255

// Service handles league business logic.
type Service struct {
	leagueRepo    *repository.LeagueRepository
	purseRepo     *repository.PurseRepository
	betRecordRepo *repository.BetRecordRepository
	holyLockRepo  *repository.HolyLockRepository
}

// NewService creates a new leagues service.
func NewService(db *gorm.DB) *Service {
	return &Service{
		leagueRepo:    repository.NewLeagueRepository(db),
		purseRepo:     repository.NewPurseRepository(db),
		betRecordRepo: repository.NewBetRecordRepository(db),
		holyLockRepo:  repository.NewHolyLockRepository(db),
	}
}

// UserLeague is a league as seen by one member, paired with whether that
// member created it.
//
// The flag is resolved here rather than in the template because html/template
// cannot compare two uuid.UUID values: eq rejects an array type outright.
type UserLeague struct {
	models.League
	IsOwner bool
}

// GetUserLeagues retrieves all leagues a user is a member of.
func (s *Service) GetUserLeagues(userID uuid.UUID) ([]UserLeague, error) {
	leagues, err := s.leagueRepo.FindUserLeagues(userID)
	if err != nil {
		return nil, err
	}

	result := make([]UserLeague, 0, len(leagues))
	for _, league := range leagues {
		result = append(result, UserLeague{League: league, IsOwner: league.CreatedBy == userID})
	}
	return result, nil
}

// GetOwnedLeague retrieves a league only if userID created it.
//
// Renaming is the creator's alone: an admin member can already be several
// people, and the name is what every other member sees the league by.
func (s *Service) GetOwnedLeague(leagueID, userID uuid.UUID) (*models.League, error) {
	league, err := s.leagueRepo.FindByID(leagueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLeagueNotFound
		}
		return nil, err
	}

	if league.CreatedBy != userID {
		return nil, ErrNotAuthorized
	}
	return league, nil
}

// RenameLeague changes a league's name. Only the league's creator may do so.
//
// The returned league carries the new name, so a caller re-rendering the row
// does not have to read it back.
func (s *Service) RenameLeague(leagueID, userID uuid.UUID, name string) (*models.League, error) {
	league, err := s.GetOwnedLeague(leagueID, userID)
	if err != nil {
		return nil, err
	}

	name, err = validateLeagueName(name)
	if err != nil {
		return nil, err
	}

	if name == league.Name {
		return league, nil
	}

	if err := s.leagueRepo.UpdateName(league.ID, name); err != nil {
		return nil, err
	}

	league.Name = name
	return league, nil
}

// validateLeagueName trims a submitted name and rejects one the column cannot
// hold. Postgres truncates nothing -- an over-long value fails the insert -- so
// the check has to happen before the write.
func validateLeagueName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxLeagueNameLength {
		return "", ErrInvalidName
	}
	return name, nil
}

// GetAvailableLeagues retrieves public leagues the user hasn't joined.
func (s *Service) GetAvailableLeagues(userID uuid.UUID) ([]models.League, error) {
	publicLeagues, err := s.leagueRepo.FindPublicLeagues()
	if err != nil {
		return nil, err
	}

	// Filter out leagues user is already a member of.
	var available []models.League
	for _, league := range publicLeagues {
		isMember, err := s.leagueRepo.IsMember(league.ID, userID)
		if err != nil {
			return nil, err
		}
		if !isMember {
			available = append(available, league)
		}
	}
	return available, nil
}

// CreateLeague creates a new league with the user as the admin.
func (s *Service) CreateLeague(name string, creatorID uuid.UUID, isPublic bool, startingBalance decimal.Decimal) (*models.League, error) {
	league := &models.League{
		Name:            name,
		CreatedBy:       creatorID,
		IsPublic:        &isPublic,
		StartingBalance: startingBalance,
	}

	if err := s.leagueRepo.Create(league); err != nil {
		return nil, err
	}

	// Add creator as admin member.
	if err := s.leagueRepo.AddMember(league.ID, creatorID, "admin"); err != nil {
		return nil, err
	}

	// Create purse for the creator.
	purse := &models.Purse{
		UserID:   creatorID,
		LeagueID: league.ID,
		Balance:  startingBalance,
	}
	if err := s.purseRepo.Create(purse); err != nil {
		return nil, err
	}

	return league, nil
}

// GetLeagueByID retrieves a league by ID.
func (s *Service) GetLeagueByID(leagueID uuid.UUID) (*models.League, error) {
	league, err := s.leagueRepo.FindByID(leagueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLeagueNotFound
		}
		return nil, err
	}
	return league, nil
}

// LeagueDetails contains league info with membership context.
type LeagueDetails struct {
	League     *models.League
	IsMember   bool
	IsAdmin    bool
	IsCreator  bool
	Membership *models.LeagueMember
}

// GetLeagueDetails retrieves league details with membership context for a user.
func (s *Service) GetLeagueDetails(leagueID, userID uuid.UUID) (*LeagueDetails, error) {
	league, err := s.leagueRepo.FindByID(leagueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLeagueNotFound
		}
		return nil, err
	}

	details := &LeagueDetails{
		League:    league,
		IsCreator: league.CreatedBy == userID,
	}

	membership, err := s.leagueRepo.GetMembership(leagueID, userID)
	if err == nil {
		details.IsMember = true
		details.Membership = membership
		details.IsAdmin = membership.Role == "admin"
	}

	return details, nil
}

// JoinLeague adds a user to a public league.
func (s *Service) JoinLeague(leagueID, userID uuid.UUID) error {
	league, err := s.leagueRepo.FindByID(leagueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLeagueNotFound
		}
		return err
	}

	if league.IsPublic == nil || !*league.IsPublic {
		return ErrLeagueNotPublic
	}

	isMember, err := s.leagueRepo.IsMember(leagueID, userID)
	if err != nil {
		return err
	}
	if isMember {
		return ErrAlreadyMember
	}

	if err := s.leagueRepo.AddMember(leagueID, userID, "member"); err != nil {
		return err
	}

	// Create purse for the new member.
	purse := &models.Purse{
		UserID:   userID,
		LeagueID: leagueID,
		Balance:  league.StartingBalance,
	}
	return s.purseRepo.Create(purse)
}

// JoinByCode adds a user to a league using an invite code.
func (s *Service) JoinByCode(inviteCode string, userID uuid.UUID) (*models.League, error) {
	league, err := s.leagueRepo.FindByInviteCode(inviteCode)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCode
		}
		return nil, err
	}

	isMember, err := s.leagueRepo.IsMember(league.ID, userID)
	if err != nil {
		return nil, err
	}
	if isMember {
		return nil, ErrAlreadyMember
	}

	if err := s.leagueRepo.AddMember(league.ID, userID, "member"); err != nil {
		return nil, err
	}

	// Create purse for the new member.
	purse := &models.Purse{
		UserID:   userID,
		LeagueID: league.ID,
		Balance:  league.StartingBalance,
	}
	if err := s.purseRepo.Create(purse); err != nil {
		return nil, err
	}

	return league, nil
}

// LeaveLeague removes a user from a league.
func (s *Service) LeaveLeague(leagueID, userID uuid.UUID) error {
	league, err := s.leagueRepo.FindByID(leagueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLeagueNotFound
		}
		return err
	}

	// Creator cannot leave their own league.
	if league.CreatedBy == userID {
		return ErrCannotLeave
	}

	isMember, err := s.leagueRepo.IsMember(leagueID, userID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrNotMember
	}

	return s.leagueRepo.RemoveMember(leagueID, userID)
}

// DeleteLeague removes a league. Only league admins can delete.
func (s *Service) DeleteLeague(leagueID, userID uuid.UUID) error {
	league, err := s.leagueRepo.FindByID(leagueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLeagueNotFound
		}
		return err
	}

	// Check if user is an admin of this league.
	membership, err := s.leagueRepo.GetMembership(leagueID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotAuthorized
		}
		return err
	}

	if membership.Role != "admin" {
		return ErrNotAuthorized
	}

	return s.leagueRepo.Delete(league.ID)
}

// GetPurseBalance retrieves a user's purse balance for a specific league.
func (s *Service) GetPurseBalance(leagueID, userID uuid.UUID) (decimal.Decimal, error) {
	purse, err := s.purseRepo.FindByUserAndLeague(userID, leagueID)
	if err != nil {
		return decimal.Zero, err
	}
	return purse.Balance, nil
}

// GetUserPurses retrieves all purses for a user.
func (s *Service) GetUserPurses(userID uuid.UUID) ([]models.Purse, error) {
	return s.purseRepo.FindByUser(userID)
}

// LeaderboardEntry represents a single entry in the league leaderboard.
type LeaderboardEntry struct {
	Rank     int
	Username string
	Balance  decimal.Decimal
	Wins     int
	Losses   int
	Pushes   int
	// The Lock* counts are the same record over the Holy Locks alone.
	LockWins   int
	LockLosses int
	LockPushes int
}

// WeeklyUserStats aggregates one member's bets for one week.
//
// Staked sums every bet placed that week, pending included. Winnings is what
// settlement actually credited back: full payouts for wins, refunded stakes
// for pushes. Net covers settled bets only -- won profit minus lost stakes --
// so a week that is still in flight reads $0.00 rather than as a loss.
type WeeklyUserStats struct {
	Username      string
	IsCurrentUser bool
	Wins          int
	Losses        int
	Pushes        int
	Pending       int
	Staked        decimal.Decimal
	Winnings      decimal.Decimal
	Net           decimal.Decimal
}

// WeekStats groups the per-user rows for a single season week.
type WeekStats struct {
	Label string
	Rows  []WeeklyUserStats
}

// GetWeeklyStats returns per-week, per-user bet totals for a league, newest
// week first. Within each week the current user's row sorts first.
func (s *Service) GetWeeklyStats(leagueID, currentUserID uuid.UUID) ([]WeekStats, error) {
	rows, err := s.betRecordRepo.FindLeagueBets(leagueID)
	if err != nil {
		return nil, err
	}
	return buildWeeklyStats(rows, currentUserID), nil
}

// buildWeeklyStats pivots flat bet rows into per-week, per-user aggregates.
func buildWeeklyStats(rows []repository.LeagueBetRow, currentUserID uuid.UUID) []WeekStats {
	type weekKey struct {
		season int // -1 when unknown
		week   int // -1 when unknown
	}
	type userKey struct {
		week weekKey
		user uuid.UUID
	}

	stats := make(map[userKey]*WeeklyUserStats)
	for _, row := range rows {
		key := userKey{week: weekKey{season: -1, week: -1}, user: row.UserID}
		if row.Season != nil {
			key.week.season = *row.Season
		}
		if row.Week != nil {
			key.week.week = *row.Week
		}

		entry, ok := stats[key]
		if !ok {
			entry = &WeeklyUserStats{
				Username:      row.Username,
				IsCurrentUser: row.UserID == currentUserID,
			}
			stats[key] = entry
		}

		switch models.BetStatus(row.Status) {
		case models.BetStatusWon:
			entry.Wins++
			payout := models.PayoutForOdds(row.Stake, row.OddsSnapshot).Round(2)
			entry.Winnings = entry.Winnings.Add(payout)
			entry.Net = entry.Net.Add(payout.Sub(row.Stake))
		case models.BetStatusLost:
			entry.Losses++
			entry.Net = entry.Net.Sub(row.Stake)
		case models.BetStatusPush:
			entry.Pushes++
			entry.Winnings = entry.Winnings.Add(row.Stake)
		case models.BetStatusPending:
			entry.Pending++
		default:
			// A void bet was cancelled and refunded; it never counted.
			continue
		}
		entry.Staked = entry.Staked.Add(row.Stake)
	}

	// Group user entries under their week.
	grouped := make(map[weekKey][]WeeklyUserStats)
	for key, entry := range stats {
		grouped[key.week] = append(grouped[key.week], *entry)
	}

	weeks := make([]weekKey, 0, len(grouped))
	for wk := range grouped {
		weeks = append(weeks, wk)
	}
	// Newest first; rows with no calendar data sort last.
	sort.Slice(weeks, func(i, j int) bool {
		if weeks[i].season != weeks[j].season {
			return weeks[i].season > weeks[j].season
		}
		return weeks[i].week > weeks[j].week
	})

	result := make([]WeekStats, 0, len(weeks))
	for _, wk := range weeks {
		users := grouped[wk]
		sort.Slice(users, func(i, j int) bool {
			if users[i].IsCurrentUser != users[j].IsCurrentUser {
				return users[i].IsCurrentUser
			}
			return strings.ToLower(users[i].Username) < strings.ToLower(users[j].Username)
		})
		result = append(result, WeekStats{Label: weekLabel(wk.season, wk.week), Rows: users})
	}
	return result
}

// weekLabel renders a season/week pair for display; -1 means unknown.
func weekLabel(season, week int) string {
	switch {
	case season >= 0 && week >= 0:
		return fmt.Sprintf("%d · Week %d", season, week)
	case season >= 0:
		return fmt.Sprintf("%d", season)
	default:
		return "Unscheduled"
	}
}

// GetLeaderboard retrieves the leaderboard for a league, sorted by balance.
func (s *Service) GetLeaderboard(leagueID uuid.UUID) ([]LeaderboardEntry, error) {
	// Get all purses for this league, ordered by balance.
	purses, err := s.purseRepo.FindByLeague(leagueID)
	if err != nil {
		return nil, err
	}

	// Get bet records for all users in this league.
	records, err := s.betRecordRepo.GetRecordsByLeague(leagueID)
	if err != nil {
		return nil, err
	}

	// Compose leaderboard entries.
	entries := make([]LeaderboardEntry, 0, len(purses))
	for i, purse := range purses {
		entry := LeaderboardEntry{
			Rank:     i + 1,
			Username: purse.User.Username,
			Balance:  purse.Balance,
		}
		if rec, ok := records[purse.UserID]; ok {
			entry.Wins = rec.Wins
			entry.Losses = rec.Losses
			entry.Pushes = rec.Pushes
			entry.LockWins = rec.LockWins
			entry.LockLosses = rec.LockLosses
			entry.LockPushes = rec.LockPushes
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// HolyLockEntry is one member's Holy Lock for a week, as displayed.
type HolyLockEntry struct {
	Username      string
	IsCurrentUser bool
	Matchup       string // "CLEM @ GT"
	Pick          string // "GT -7" / "GT +150" / "Over 54.5"
	Stake         decimal.Decimal
	Status        models.BetStatus
	ScheduledAt   time.Time
}

// HolyLockWeek groups a week's Holy Locks.
type HolyLockWeek struct {
	Label string
	Rows  []HolyLockEntry
}

// GetHolyLocks returns each member's Holy Lock per week, newest week first.
// Within each week the current user's row sorts first, matching GetWeeklyStats.
func (s *Service) GetHolyLocks(leagueID, currentUserID uuid.UUID) ([]HolyLockWeek, error) {
	rows, err := s.holyLockRepo.FindLeagueLocks(leagueID)
	if err != nil {
		return nil, err
	}
	return buildHolyLockWeeks(rows, currentUserID), nil
}

// buildHolyLockWeeks groups flat lock rows by week.
//
// A member with no lock that week simply does not appear, the same way the
// weekly breakdown drops a member with no bets, which keeps this a pure
// function over the rows it is handed.
func buildHolyLockWeeks(rows []repository.LeagueHolyLockRow, currentUserID uuid.UUID) []HolyLockWeek {
	type weekKey struct {
		season     int
		week       int
		seasonType string
	}

	grouped := make(map[weekKey][]HolyLockEntry)
	for _, row := range rows {
		key := weekKey{season: row.Season, week: row.Week, seasonType: row.SeasonType}
		grouped[key] = append(grouped[key], HolyLockEntry{
			Username:      row.Username,
			IsCurrentUser: row.UserID == currentUserID,
			Matchup:       row.AwayAbbr + " @ " + row.HomeAbbr,
			Pick:          bets.HolyLockPick(row),
			Stake:         row.Stake,
			Status:        models.BetStatus(row.Status),
			ScheduledAt:   row.ScheduledAt,
		})
	}

	weeks := make([]weekKey, 0, len(grouped))
	for wk := range grouped {
		weeks = append(weeks, wk)
	}
	// Newest first. The postseason sorts ahead of every regular week because it
	// is played after all of them -- its week numbers restart at 1, so ranking
	// the season type has to come before comparing them.
	sort.Slice(weeks, func(i, j int) bool {
		if weeks[i].season != weeks[j].season {
			return weeks[i].season > weeks[j].season
		}
		if ri, rj := seasonTypeRank(weeks[i].seasonType), seasonTypeRank(weeks[j].seasonType); ri != rj {
			return ri > rj
		}
		return weeks[i].week > weeks[j].week
	})

	result := make([]HolyLockWeek, 0, len(weeks))
	for _, wk := range weeks {
		entries := grouped[wk]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsCurrentUser != entries[j].IsCurrentUser {
				return entries[i].IsCurrentUser
			}
			return strings.ToLower(entries[i].Username) < strings.ToLower(entries[j].Username)
		})
		result = append(result, HolyLockWeek{
			Label: holyLockWeekLabel(wk.season, wk.week, wk.seasonType),
			Rows:  entries,
		})
	}
	return result
}

// seasonTypeRank orders the parts of a season chronologically.
func seasonTypeRank(seasonType string) int {
	if seasonType == string(models.SeasonTypePostseason) {
		return 1
	}
	return 0
}

// holyLockWeekLabel names a week, distinguishing the postseason.
//
// weekLabel alone collapses regular week 1 and postseason week 1 onto one
// heading. The one-lock-per-week rule is keyed on the week row, which treats
// them as different weeks, so without the suffix a member legitimately holding
// both would appear twice under a single heading and read as a bug.
func holyLockWeekLabel(season, week int, seasonType string) string {
	label := weekLabel(season, week)
	if seasonType == string(models.SeasonTypePostseason) {
		return label + " · Postseason"
	}
	return label
}
