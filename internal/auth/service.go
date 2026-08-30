package auth

import (
	"errors"
	"net/http"

	"github.com/brian/paper-betting-with-friends/internal/config"
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	sessionName    = "betting_session"
	sessionUserKey = "user_id"

	// sessionVersionKey pins a session to a point in the account's history. A
	// cookie without it predates this check and is refused, which costs one
	// round of logins on the deploy that introduces it.
	sessionVersionKey = "session_version"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserExists         = errors.New("user with this username already exists")
	ErrUserNotFound       = errors.New("user not found")

	// ErrSessionExpired means the cookie is well-formed and correctly signed but
	// was issued before the account's password last changed.
	ErrSessionExpired = errors.New("session is no longer valid")
)

// Service handles authentication operations.
type Service struct {
	userRepo *repository.UserRepository
	store    *sessions.CookieStore
	cfg      *config.Config
}

// NewService creates a new authentication service.
func NewService(db *gorm.DB, cfg *config.Config) *Service {
	store := sessions.NewCookieStore([]byte(cfg.SessionKey))

	// Configure session options.
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days.
		HttpOnly: true,
		Secure:   cfg.IsProduction(),
		SameSite: http.SameSiteLaxMode,
	}

	return &Service{
		userRepo: repository.NewUserRepository(db),
		store:    store,
		cfg:      cfg,
	}
}

// Register creates a new user with the given username and password.
func (s *Service) Register(username, password string) (*models.User, error) {
	// Check if user already exists.
	exists, err := s.userRepo.Exists(username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrUserExists
	}

	// Hash the password.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		Username:     username,
		PasswordHash: string(hash),
	}

	if err := s.userRepo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

// Login authenticates a user with the given username and password.
func (s *Service) Login(username, password string) (*models.User, error) {
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// CreateSession creates a new session for the user.
func (s *Service) CreateSession(w http.ResponseWriter, r *http.Request, user *models.User) error {
	session, err := s.store.Get(r, sessionName)
	if err != nil {
		return err
	}

	session.Values[sessionUserKey] = user.ID.String()
	session.Values[sessionVersionKey] = user.SessionVersion
	return session.Save(r, w)
}

// DestroySession destroys the current session.
func (s *Service) DestroySession(w http.ResponseWriter, r *http.Request) error {
	session, err := s.store.Get(r, sessionName)
	if err != nil {
		return err
	}

	session.Options.MaxAge = -1
	return session.Save(r, w)
}

// GetCurrentUser retrieves the current user from the session.
func (s *Service) GetCurrentUser(r *http.Request) (*models.User, error) {
	session, err := s.store.Get(r, sessionName)
	if err != nil {
		return nil, err
	}

	userID, version, err := sessionIdentity(session.Values)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}

	// The user is re-read on every request, so a reset takes effect immediately
	// rather than at the end of the cookie's seven days.
	if user.SessionVersion != version {
		return nil, ErrSessionExpired
	}

	return user, nil
}

// sessionIdentity reads the account and session version out of a decoded
// session, rejecting anything it cannot make sense of.
//
// It is split out from GetCurrentUser so the cookie handling can be tested
// without a database: everything here is a decision about the cookie, and
// everything left in the caller needs the stored user.
func sessionIdentity(values map[any]any) (uuid.UUID, int, error) {
	userIDStr, ok := values[sessionUserKey].(string)
	if !ok || userIDStr == "" {
		return uuid.Nil, 0, ErrUserNotFound
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return uuid.Nil, 0, ErrUserNotFound
	}

	version, ok := values[sessionVersionKey].(int)
	if !ok {
		// Issued before sessions were versioned, so it cannot be shown to
		// predate a password reset. Refuse rather than grandfather it in; the
		// cost is one round of logins on the deploy that adds the check.
		return uuid.Nil, 0, ErrSessionExpired
	}

	return userID, version, nil
}

// IsAuthenticated checks if the current request has a valid session.
func (s *Service) IsAuthenticated(r *http.Request) bool {
	_, err := s.GetCurrentUser(r)
	return err == nil
}

// Store returns the session store for use in middleware.
func (s *Service) Store() *sessions.CookieStore {
	return s.store
}
