package admin

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/brian/paper-betting-with-friends/internal/bets"
	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
)

// EnsureAdminUser runs on every boot. It is the only thing that grants
// is_admin, and ADMIN_PASSWORD is the way back in after a lockout -- so each
// boot has to converge on the configured account without rehashing, or
// revoking sessions, when nothing has changed.
func TestEnsureAdminUser(t *testing.T) {
	db := testdb.Open(t)
	svc := NewService(db, &config.Config{AdminUsername: siteAdmin}, nil, bets.NewService(db), nil)
	p := &portal{t: t, db: db, svc: svc}

	t.Run("with no account and no password, it refuses", func(t *testing.T) {
		if err := svc.EnsureAdminUser(siteAdmin, ""); !errors.Is(err, ErrNoAdminPassword) {
			t.Fatalf("error = %v, want ErrNoAdminPassword", err)
		}
	})

	t.Run("with no account, it creates an administrator", func(t *testing.T) {
		if err := svc.EnsureAdminUser(siteAdmin, "first"); err != nil {
			t.Fatalf("EnsureAdminUser: %v", err)
		}
		admin := p.userNamed(siteAdmin)
		if !admin.IsAdmin {
			t.Error("created account is not an administrator")
		}
		if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("first")) != nil {
			t.Error("configured password does not verify")
		}
	})

	t.Run("an unchanged password neither rehashes nor revokes sessions", func(t *testing.T) {
		before := p.userNamed(siteAdmin)
		if err := svc.EnsureAdminUser(siteAdmin, "first"); err != nil {
			t.Fatalf("EnsureAdminUser: %v", err)
		}
		after := p.userNamed(siteAdmin)
		if after.PasswordHash != before.PasswordHash || after.SessionVersion != before.SessionVersion {
			t.Error("a boot with the same password rewrote the account")
		}
	})

	t.Run("a demoted administrator is restored", func(t *testing.T) {
		if err := db.Model(&models.User{}).Where("username = ?", siteAdmin).Update("is_admin", false).Error; err != nil {
			t.Fatalf("demoting: %v", err)
		}
		if err := svc.EnsureAdminUser(siteAdmin, ""); err != nil {
			t.Fatalf("EnsureAdminUser: %v", err)
		}
		if !p.userNamed(siteAdmin).IsAdmin {
			t.Error("administrator was not restored")
		}
	})

	t.Run("a changed password is written and revokes every session", func(t *testing.T) {
		before := p.userNamed(siteAdmin)
		if err := svc.EnsureAdminUser(siteAdmin, "second"); err != nil {
			t.Fatalf("EnsureAdminUser: %v", err)
		}
		after := p.userNamed(siteAdmin)
		if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("second")) != nil {
			t.Error("new password does not verify")
		}
		if after.SessionVersion != before.SessionVersion+1 {
			t.Errorf("session_version = %d, want %d", after.SessionVersion, before.SessionVersion+1)
		}
	})
}
