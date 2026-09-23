package admin

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

const siteAdmin = "site-admin"

// portal is an admin service over a real database, with the bootstrap
// administrator already signed in as the actor.
type portal struct {
	t     *testing.T
	db    *gorm.DB
	svc   *Service
	actor *models.User
}

func newPortal(t *testing.T) *portal {
	t.Helper()

	db := testdb.Open(t)
	// The operations under test touch users, leagues, purses and the audit
	// trail only; the scheduler and games service are for the sync pages.
	svc := NewService(db, &config.Config{AdminUsername: siteAdmin}, nil, bets.NewService(db), nil)
	p := &portal{t: t, db: db, svc: svc}

	// Written directly rather than through EnsureAdminUser: a bcrypt hash costs
	// about a second under -race, and only bootstrap_test is about the hash.
	p.actor = &models.User{Username: siteAdmin, PasswordHash: "unused", IsAdmin: true}
	if err := db.Create(p.actor).Error; err != nil {
		t.Fatalf("creating administrator: %v", err)
	}
	return p
}

func (p *portal) user(name string) *models.User {
	p.t.Helper()

	user := &models.User{Username: name + "-" + uuid.NewString()[:8], PasswordHash: "unused"}
	if err := p.db.Create(user).Error; err != nil {
		p.t.Fatalf("creating user: %v", err)
	}
	return user
}

func (p *portal) userNamed(username string) *models.User {
	p.t.Helper()

	var user models.User
	if err := p.db.Where("username = ?", username).First(&user).Error; err != nil {
		p.t.Fatalf("reading %s: %v", username, err)
	}
	return &user
}

func (p *portal) reload(user *models.User) *models.User {
	p.t.Helper()

	var fresh models.User
	if err := p.db.First(&fresh, "id = ?", user.ID).Error; err != nil {
		p.t.Fatalf("reloading user: %v", err)
	}
	return &fresh
}

// lastAudit is the newest audit entry.
func (p *portal) lastAudit() models.AuditLog {
	p.t.Helper()

	entries, err := p.svc.ListAuditLog()
	if err != nil || len(entries) == 0 {
		p.t.Fatalf("ListAuditLog = %d entries, %v", len(entries), err)
	}
	return entries[0]
}

func (p *portal) balance(leagueID, userID uuid.UUID) (decimal.Decimal, bool) {
	p.t.Helper()

	purse, err := p.svc.purseRepo.FindByUserAndLeague(userID, leagueID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return decimal.Zero, false
	}
	if err != nil {
		p.t.Fatalf("reading purse: %v", err)
	}
	return purse.Balance, true
}

func TestAdminPasswordResetRevokesSessions(t *testing.T) {
	p := newPortal(t)
	alice := p.user("alice")

	if err := p.svc.UpdateUserPassword(p.actor, alice.ID, "new secret"); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}

	after := p.reload(alice)
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("new secret")) != nil {
		t.Error("the new password does not verify against the stored hash")
	}
	// A reset that leaves an attacker's cookie working has changed nothing.
	if after.SessionVersion != alice.SessionVersion+1 {
		t.Errorf("session_version = %d, want %d", after.SessionVersion, alice.SessionVersion+1)
	}

	entry := p.lastAudit()
	if entry.Action != models.AuditActionUserPasswordReset || entry.Detail != alice.Username {
		t.Errorf("audit = %s %q, want %s %q", entry.Action, entry.Detail, models.AuditActionUserPasswordReset, alice.Username)
	}
	if strings.Contains(entry.Detail, "new secret") {
		t.Error("the audit trail recorded the password")
	}
}

// The bootstrap administrator is the only way into the portal, so nothing in
// the portal may rename it, delete it, or hand its name to someone else.
func TestAdminProtectsTheSiteAdministrator(t *testing.T) {
	p := newPortal(t)
	alice := p.user("alice")

	if err := p.svc.UpdateUserUsername(p.actor, p.actor.ID, "someone-else"); !errors.Is(err, ErrProtectedAccount) {
		t.Errorf("renaming the administrator: error = %v, want ErrProtectedAccount", err)
	}
	if err := p.svc.UpdateUserUsername(p.actor, alice.ID, siteAdmin); !errors.Is(err, ErrProtectedAccount) {
		t.Errorf("renaming onto the administrator's name: error = %v, want ErrProtectedAccount", err)
	}
	if err := p.svc.DeleteUser(p.actor, p.actor.ID, siteAdmin); !errors.Is(err, ErrProtectedAccount) {
		t.Errorf("deleting the administrator: error = %v, want ErrProtectedAccount", err)
	}
	if got := p.reload(p.actor); got.Username != siteAdmin {
		t.Errorf("administrator is now %q", got.Username)
	}
}

func TestAdminRenameUser(t *testing.T) {
	p := newPortal(t)
	alice, bob := p.user("alice"), p.user("bob")

	if err := p.svc.UpdateUserUsername(p.actor, alice.ID, bob.Username); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("renaming onto a taken name: error = %v, want ErrUsernameTaken", err)
	}

	if err := p.svc.UpdateUserUsername(p.actor, alice.ID, "  alice-renamed  "); err != nil {
		t.Fatalf("renaming: %v", err)
	}
	if got := p.reload(alice).Username; got != "alice-renamed" {
		t.Errorf("username = %q, want the trimmed %q", got, "alice-renamed")
	}
	if entry := p.lastAudit(); entry.Detail != alice.Username+" -> alice-renamed" {
		t.Errorf("audit detail = %q", entry.Detail)
	}
}

// Deleting a user cascades through every bet and purse they hold, so it asks
// for the name typed back and refuses anything else.
func TestAdminDeleteUser(t *testing.T) {
	p := newPortal(t)
	alice := p.user("alice")
	league, err := p.svc.CreateLeague(p.actor, "Doomed", decimal.RequireFromString("100"))
	if err != nil {
		t.Fatalf("creating league: %v", err)
	}
	if err := p.svc.AddLeagueMember(p.actor, league.ID, alice.ID); err != nil {
		t.Fatalf("adding member: %v", err)
	}

	if err := p.svc.DeleteUser(p.actor, alice.ID, "not her name"); !errors.Is(err, ErrConfirmationMismatch) {
		t.Fatalf("mismatched confirmation: error = %v, want ErrConfirmationMismatch", err)
	}
	if _, ok := p.balance(league.ID, alice.ID); !ok {
		t.Fatal("a refused delete removed the purse")
	}

	if err := p.svc.DeleteUser(p.actor, alice.ID, alice.Username); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := p.svc.GetUserByID(alice.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("deleted user lookup: error = %v, want ErrUserNotFound", err)
	}
	if _, ok := p.balance(league.ID, alice.ID); ok {
		t.Error("the deleted user's purse survived")
	}
	if entry := p.lastAudit(); entry.Action != models.AuditActionUserDeleted {
		t.Errorf("audit action = %s, want %s", entry.Action, models.AuditActionUserDeleted)
	}
}

func TestAdminLeagueMembership(t *testing.T) {
	p := newPortal(t)
	alice := p.user("alice")

	league, err := p.svc.CreateLeague(p.actor, "Office Pool", decimal.RequireFromString("250"))
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	// Admin-created leagues used to arrive without the creator's purse, which
	// left them unable to place a bet at all.
	if got, ok := p.balance(league.ID, p.actor.ID); !ok || !got.Equal(decimal.RequireFromString("250")) {
		t.Errorf("creator purse = %s (exists %v), want 250", got, ok)
	}

	if err := p.svc.AddLeagueMember(p.actor, league.ID, alice.ID); err != nil {
		t.Fatalf("AddLeagueMember: %v", err)
	}
	if err := p.svc.SetPurseBalance(p.actor, league.ID, alice.ID, decimal.RequireFromString("40")); err != nil {
		t.Fatalf("SetPurseBalance: %v", err)
	}
	if entry := p.lastAudit(); entry.Detail != "250.00 -> 40.00" {
		t.Errorf("audit detail = %q, want the old and new balance", entry.Detail)
	}

	// A removed member keeps their purse, so re-adding them restores the
	// balance they had rather than a fresh stake.
	if err := p.svc.RemoveLeagueMember(p.actor, league.ID, alice.ID); err != nil {
		t.Fatalf("RemoveLeagueMember: %v", err)
	}
	if err := p.svc.AddLeagueMember(p.actor, league.ID, alice.ID); err != nil {
		t.Fatalf("re-adding: %v", err)
	}
	if got, _ := p.balance(league.ID, alice.ID); !got.Equal(decimal.RequireFromString("40")) {
		t.Errorf("re-added balance = %s, want 40", got)
	}

	if err := p.svc.DeleteLeague(p.actor, league.ID, "office pool"); !errors.Is(err, ErrConfirmationMismatch) {
		t.Errorf("deleting with the wrong case: error = %v, want ErrConfirmationMismatch", err)
	}
	if err := p.svc.DeleteLeague(p.actor, league.ID, "Office Pool"); err != nil {
		t.Fatalf("DeleteLeague: %v", err)
	}
	if _, ok := p.balance(league.ID, alice.ID); ok {
		t.Error("deleting the league left a purse behind")
	}
}

// SetPurseBalance also repairs a member with no purse at all.
func TestAdminSetPurseBalanceCreatesAMissingPurse(t *testing.T) {
	p := newPortal(t)
	alice := p.user("alice")
	league, err := p.svc.CreateLeague(p.actor, "Repairs", decimal.RequireFromString("100"))
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}

	if err := p.svc.SetPurseBalance(p.actor, league.ID, alice.ID, decimal.RequireFromString("75")); err != nil {
		t.Fatalf("SetPurseBalance: %v", err)
	}
	if got, ok := p.balance(league.ID, alice.ID); !ok || !got.Equal(decimal.RequireFromString("75")) {
		t.Errorf("purse = %s (exists %v), want 75", got, ok)
	}
	if entry := p.lastAudit(); entry.Detail != "created at 75.00" {
		t.Errorf("audit detail = %q", entry.Detail)
	}
}
