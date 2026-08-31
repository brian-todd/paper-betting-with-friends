package admin

import (
	"errors"
	"strings"

	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/games"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	// ErrProtectedAccount guards the bootstrap administrator. It is the only
	// account that can reach this portal, so letting it be renamed, demoted or
	// deleted from inside the portal is a way to lock everyone out of the site
	// with a single misclick.
	ErrProtectedAccount = errors.New("the site administrator account cannot be renamed or deleted")

	ErrUserNotFound   = errors.New("user not found")
	ErrLeagueNotFound = errors.New("league not found")
	ErrUsernameTaken  = errors.New("username is already taken")
	ErrInvalidBalance = errors.New("balance must be a number")

	// ErrInvalidSeason backs the optional season on a manual seed.
	ErrInvalidSeason = errors.New("season is not a plausible year")

	// ErrConfirmationMismatch backs the type-the-name confirmation on the
	// destructive actions.
	ErrConfirmationMismatch = errors.New("confirmation did not match")

	// ErrNoAdminPassword is returned by EnsureAdminUser when the account has to
	// be created but no password was configured.
	ErrNoAdminPassword = errors.New("ADMIN_PASSWORD must be set to create the administrator account")
)

// Service handles admin operations.
type Service struct {
	userRepo   *repository.UserRepository
	leagueRepo *repository.LeagueRepository
	purseRepo  *repository.PurseRepository
	gameRepo   *repository.GameRepository
	teamRepo   *repository.TeamRepository
	auditRepo  *repository.AuditLogRepository
	statsRepo  *repository.StatsRepository

	spreadOddsRepo    *repository.SpreadOddsRepository
	moneyLineOddsRepo *repository.MoneyLineOddsRepository
	overUnderOddsRepo *repository.OverUnderOddsRepository

	cfg   *config.Config
	sched *scheduler.Scheduler
	bets  *bets.Service
	games *games.Service
}

// NewService creates a new admin service.
//
// It takes the scheduler and the bets and games services rather than rebuilding
// their logic: settling a bet has to move a purse the same way the automatic
// path does, and a manual sync has to be recorded the same way a scheduled one
// is.
func NewService(db *gorm.DB, cfg *config.Config, sched *scheduler.Scheduler, betsService *bets.Service, gamesService *games.Service) *Service {
	return &Service{
		userRepo:          repository.NewUserRepository(db),
		leagueRepo:        repository.NewLeagueRepository(db),
		purseRepo:         repository.NewPurseRepository(db),
		gameRepo:          repository.NewGameRepository(db),
		teamRepo:          repository.NewTeamRepository(db),
		auditRepo:         repository.NewAuditLogRepository(db),
		statsRepo:         repository.NewStatsRepository(db),
		spreadOddsRepo:    repository.NewSpreadOddsRepository(db),
		moneyLineOddsRepo: repository.NewMoneyLineOddsRepository(db),
		overUnderOddsRepo: repository.NewOverUnderOddsRepository(db),
		cfg:               cfg,
		sched:             sched,
		bets:              betsService,
		games:             gamesService,
	}
}

// AdminUsername is the protected account's name.
func (s *Service) AdminUsername() string {
	return s.cfg.AdminUsername
}

// protected reports whether a username is the bootstrap administrator's.
func (s *Service) protected(username string) bool {
	return username == s.cfg.AdminUsername
}

// UserView is a user together with what the portal needs to decide which
// actions to offer for them.
type UserView struct {
	User models.User
	// Protected is true for the bootstrap administrator, whose destructive
	// controls the page must not render. It mirrors the service's own guard, so
	// the page never offers an action the service will refuse.
	Protected bool
	Purses    []models.Purse
}

// ListUsers retrieves every user with their purses.
func (s *Service) ListUsers() ([]UserView, error) {
	users, err := s.userRepo.FindAll()
	if err != nil {
		return nil, err
	}

	views := make([]UserView, 0, len(users))
	for _, user := range users {
		purses, err := s.purseRepo.FindByUser(user.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, UserView{
			User:      user,
			Protected: s.protected(user.Username),
			Purses:    purses,
		})
	}
	return views, nil
}

// GetAllUsers retrieves all users.
func (s *Service) GetAllUsers() ([]models.User, error) {
	return s.userRepo.FindAll()
}

// GetUserByID retrieves a user by ID.
func (s *Service) GetUserByID(id uuid.UUID) (*models.User, error) {
	user, err := s.userRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

// UpdateUserPassword sets a user's password.
//
// The administrator's own password is resettable here: locking that out would
// only push the operator towards editing the database by hand.
func (s *Service) UpdateUserPassword(actor *models.User, userID uuid.UUID, newPassword string) error {
	user, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hash)
	if err := s.userRepo.Update(user); err != nil {
		return err
	}

	// Revoke every session already issued for this account. Without this a reset
	// changes what the attacker would have to type but not what their existing
	// cookie can already do -- which is the whole point of resetting it.
	//
	// This logs the account out everywhere, including the administrator's own
	// browser when they reset their own password.
	if err := s.userRepo.BumpSessionVersion(user.ID); err != nil {
		return err
	}

	// The detail names the account only. A password never reaches the log.
	s.audit(actor, models.AuditActionUserPasswordReset, models.AuditTargetUser, &user.ID, user.Username)
	return nil
}

// UpdateUserUsername renames a user.
func (s *Service) UpdateUserUsername(actor *models.User, userID uuid.UUID, newUsername string) error {
	user, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}
	if s.protected(user.Username) {
		return ErrProtectedAccount
	}

	newUsername = strings.TrimSpace(newUsername)
	if newUsername == user.Username {
		return nil
	}
	// Renaming someone onto the administrator's name would hand them the
	// portal on the next boot, when EnsureAdminUser forces that name admin.
	if s.protected(newUsername) {
		return ErrProtectedAccount
	}

	taken, err := s.userRepo.Exists(newUsername)
	if err != nil {
		return err
	}
	if taken {
		return ErrUsernameTaken
	}

	old := user.Username
	user.Username = newUsername
	if err := s.userRepo.Update(user); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionUserRenamed, models.AuditTargetUser, &user.ID, old+" -> "+newUsername)
	return nil
}

// DeleteUser removes a user and everything hanging off them.
//
// Bets, purses and league memberships cascade from users(id), so this takes
// their whole betting history with it. confirm must repeat the username.
func (s *Service) DeleteUser(actor *models.User, userID uuid.UUID, confirm string) error {
	user, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}
	if s.protected(user.Username) {
		return ErrProtectedAccount
	}
	if strings.TrimSpace(confirm) != user.Username {
		return ErrConfirmationMismatch
	}

	if err := s.userRepo.Delete(user.ID); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionUserDeleted, models.AuditTargetUser, &user.ID, user.Username)
	return nil
}

// MemberView is one league membership with the member's purse balance.
type MemberView struct {
	Member   models.LeagueMember
	Balance  decimal.Decimal
	HasPurse bool
}

// LeagueView is a league with its members' balances resolved.
type LeagueView struct {
	League  models.League
	Members []MemberView
}

// ListLeagues retrieves every league with its members and their balances.
func (s *Service) ListLeagues() ([]LeagueView, error) {
	leagues, err := s.leagueRepo.FindAll()
	if err != nil {
		return nil, err
	}

	views := make([]LeagueView, 0, len(leagues))
	for _, league := range leagues {
		purses, err := s.purseRepo.FindByLeague(league.ID)
		if err != nil {
			return nil, err
		}
		balances := make(map[uuid.UUID]decimal.Decimal, len(purses))
		for _, purse := range purses {
			balances[purse.UserID] = purse.Balance
		}

		members := make([]MemberView, 0, len(league.Members))
		for _, member := range league.Members {
			balance, ok := balances[member.UserID]
			members = append(members, MemberView{Member: member, Balance: balance, HasPurse: ok})
		}

		views = append(views, LeagueView{League: league, Members: members})
	}
	return views, nil
}

// GetAllLeagues retrieves all leagues.
func (s *Service) GetAllLeagues() ([]models.League, error) {
	return s.leagueRepo.FindAll()
}

// GetLeagueByID retrieves a league by ID.
func (s *Service) GetLeagueByID(id uuid.UUID) (*models.League, error) {
	league, err := s.leagueRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLeagueNotFound
		}
		return nil, err
	}
	return league, nil
}

// CreateLeague creates a league with the actor as its league admin.
func (s *Service) CreateLeague(actor *models.User, name string, startingBalance decimal.Decimal) (*models.League, error) {
	league := &models.League{
		Name:            name,
		CreatedBy:       actor.ID,
		StartingBalance: startingBalance,
	}

	if err := s.leagueRepo.Create(league); err != nil {
		return nil, err
	}
	if err := s.leagueRepo.AddMember(league.ID, actor.ID, "admin"); err != nil {
		return nil, err
	}
	// Without a purse the member cannot place a bet at all, which is how
	// admin-created leagues used to arrive dead. leagues.Service does the same
	// three steps on every join.
	if err := s.ensurePurse(league.ID, actor.ID, startingBalance); err != nil {
		return nil, err
	}

	s.audit(actor, models.AuditActionLeagueCreated, models.AuditTargetLeague, &league.ID,
		name+" starting balance "+startingBalance.StringFixed(2))
	return league, nil
}

// AddLeagueMember adds a user to a league and funds their purse.
func (s *Service) AddLeagueMember(actor *models.User, leagueID, userID uuid.UUID) error {
	league, err := s.GetLeagueByID(leagueID)
	if err != nil {
		return err
	}
	user, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}

	if err := s.leagueRepo.AddMember(leagueID, userID, "member"); err != nil {
		return err
	}
	if err := s.ensurePurse(leagueID, userID, league.StartingBalance); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionMemberAdded, models.AuditTargetLeague, &leagueID, user.Username+" -> "+league.Name)
	return nil
}

// RemoveLeagueMember removes a user from a league.
//
// Their purse is left alone: a member who is re-added should come back to the
// balance they had, not to a fresh stake.
func (s *Service) RemoveLeagueMember(actor *models.User, leagueID, userID uuid.UUID) error {
	if err := s.leagueRepo.RemoveMember(leagueID, userID); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionMemberRemoved, models.AuditTargetLeague, &leagueID, userID.String())
	return nil
}

// SetPurseBalance overwrites a member's balance in a league, creating the purse
// if it is missing.
func (s *Service) SetPurseBalance(actor *models.User, leagueID, userID uuid.UUID, balance decimal.Decimal) error {
	purse, err := s.purseRepo.FindByUserAndLeague(userID, leagueID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := s.purseRepo.Create(&models.Purse{UserID: userID, LeagueID: leagueID, Balance: balance}); err != nil {
			return err
		}
		s.audit(actor, models.AuditActionPurseSet, models.AuditTargetPurse, &userID,
			"created at "+balance.StringFixed(2))
		return nil
	}

	old := purse.Balance
	purse.Balance = balance
	if err := s.purseRepo.Update(purse); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionPurseSet, models.AuditTargetPurse, &userID,
		old.StringFixed(2)+" -> "+balance.StringFixed(2))
	return nil
}

// DeleteLeague removes a league along with its memberships, purses and every
// bet placed in it. confirm must repeat the league name.
func (s *Service) DeleteLeague(actor *models.User, leagueID uuid.UUID, confirm string) error {
	league, err := s.GetLeagueByID(leagueID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirm) != league.Name {
		return ErrConfirmationMismatch
	}

	if err := s.leagueRepo.Delete(leagueID); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionLeagueDeleted, models.AuditTargetLeague, &leagueID, league.Name)
	return nil
}

// ensurePurse creates a purse at balance unless the member already has one.
func (s *Service) ensurePurse(leagueID, userID uuid.UUID, balance decimal.Decimal) error {
	_, err := s.purseRepo.FindByUserAndLeague(userID, leagueID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.purseRepo.Create(&models.Purse{UserID: userID, LeagueID: leagueID, Balance: balance})
}
