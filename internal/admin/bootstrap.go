package admin

import (
	"errors"
	"log/slog"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// EnsureAdminUser makes the configured administrator exist and be an admin.
//
// It runs on every boot and is idempotent. The password is only written when
// the account is created or when the stored hash no longer matches what is
// configured, which turns ADMIN_PASSWORD into a way back in after a lockout
// without rehashing on every restart.
//
// This is the only path that sets is_admin. There is deliberately no route that
// grants it, so the portal cannot be handed to a second account by mistake.
func (s *Service) EnsureAdminUser(username, password string) error {
	user, err := s.userRepo.FindByUsername(username)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		if password == "" {
			return ErrNoAdminPassword
		}

		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}
		created := &models.User{
			Username:     username,
			PasswordHash: string(hash),
			IsAdmin:      true,
		}
		if createErr := s.userRepo.Create(created); createErr != nil {
			return createErr
		}
		slog.Info("created administrator account", "username", username)
		return nil
	}

	if err != nil {
		return err
	}

	if !user.IsAdmin {
		if err := s.userRepo.SetAdminByUsername(username, true); err != nil {
			return err
		}
		slog.Info("restored administrator privileges", "username", username)
	}

	if password == "" {
		slog.Warn("ADMIN_PASSWORD is not set; leaving the existing administrator password in place",
			"username", username)
		return nil
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	if err := s.userRepo.Update(user); err != nil {
		return err
	}
	// Recovering the account is only worth doing if it also evicts whoever might
	// be holding a session on it.
	if err := s.userRepo.BumpSessionVersion(user.ID); err != nil {
		return err
	}
	slog.Info("reset administrator password from ADMIN_PASSWORD", "username", username)
	return nil
}
