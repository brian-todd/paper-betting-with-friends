package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Recognised values for ENV. Anything else is rejected at startup rather than
// quietly treated as neither: IsProduction gates the Secure flag on the session
// cookie, so a typo like ENV=prod would ship session cookies over plain HTTP
// while every other signal said the deployment was fine.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// defaultSessionKey is the placeholder Load falls back to. It is committed to
// the repository, so it authenticates nobody -- see Validate.
const defaultSessionKey = "development-secret-key-change-in-production"

// defaultDatabaseURL is the local development database Load falls back to.
// Reaching for it in production means DATABASE_URL never arrived -- see
// Validate.
const defaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/betting_tracker?sslmode=disable"

// minSessionKeyBytes is the shortest session key accepted in production. The
// key is the HMAC secret behind every session cookie, and it is the only thing
// standing between a forged cookie and the admin portal.
const minSessionKeyBytes = 32

// Config holds all configuration for the application.
type Config struct {
	DatabaseURL         string
	SessionKey          string
	Port                string
	Env                 string
	CFBDataAPIKey       string // API key for collegefootballdata.com.
	CBBDataAPIKey       string // API key for collegebasketballdata.com.
	CBBSyncIntervalMins int    // How often to sync basketball games/lines in minutes.

	// CFBScoreboardClassifications is the divisions the live scoreboard sync
	// polls. The endpoint takes one division per call, so each entry is another
	// request every five minutes -- roughly 8,600 a month against an allowance
	// of 30,000 -- which is why adding one is a deliberate act rather than a
	// default.
	//
	// Empty means the sync's own default, which is FBS alone. The default lives
	// there rather than here so this package stays free of any knowledge of
	// what the feed divides the sport into.
	CFBScoreboardClassifications []string

	// DBMaxOpenConns caps the connection pool. It belongs in configuration
	// rather than a constant because the safe value is a property of the
	// database being connected to -- it has to leave headroom under that
	// server's own connection limit -- and that changes with the host.
	DBMaxOpenConns int

	// AdminUsername is the single site administrator. The account is created on
	// boot if missing and is protected from being renamed, demoted or deleted
	// through the admin portal, so there is always a way back in.
	AdminUsername string
	// AdminPassword seeds that account. It is only applied when the account is
	// created or when the stored hash no longer matches, which makes it a
	// lockout recovery lever rather than something rehashed on every boot.
	AdminPassword string

	// MigrateOnStart applies pending schema migrations during startup.
	//
	// On by default so a deploy is a single step on any host. The escape hatch
	// exists for the one case where it gets in the way: a migration that failed
	// leaves the schema marked dirty and the server then refuses to boot, and
	// getting it up without migrating is how you go and look.
	MigrateOnStart bool

	// TimeZone is the IANA zone the app reasons about calendar days in, e.g.
	// deciding which games count as "today". Per-user display times are
	// converted in the browser, so this only sets the day boundaries and the
	// no-JavaScript fallback -- see LoadLocation.
	TimeZone string
}

// Load reads configuration from environment variables.
// It attempts to load a .env file first, but does not fail if it doesn't exist.
func Load() *Config {
	// Load .env file if it exists (ignore error if not present).
	_ = godotenv.Load()

	return &Config{
		DatabaseURL:         getEnv("DATABASE_URL", defaultDatabaseURL),
		SessionKey:          getEnv("SESSION_KEY", defaultSessionKey),
		Port:                getEnv("PORT", "8080"),
		Env:                 getEnv("ENV", EnvDevelopment),
		CFBDataAPIKey:       getEnv("CFB_DATA_API_KEY", ""),
		CBBDataAPIKey:       getEnv("CBB_DATA_API_KEY", ""),
		CBBSyncIntervalMins: getEnvInt("CBB_SYNC_INTERVAL_MINS", 15),

		CFBScoreboardClassifications: getEnvList("CFB_SCOREBOARD_CLASSIFICATIONS"),

		DBMaxOpenConns: getEnvInt("DB_MAX_OPEN_CONNS", 20),
		AdminUsername:  getEnv("ADMIN_USERNAME", "cfb-pbwf-admin"),
		AdminPassword:  getEnv("ADMIN_PASSWORD", ""),
		MigrateOnStart: getEnvBool("MIGRATE_ON_START", true),
		TimeZone:       getEnv("APP_TIMEZONE", "America/New_York"),
	}
}

// Validate reports configuration the application must not start with.
//
// These are failures that are otherwise silent: the server boots, serves
// traffic and logs nothing unusual, while sessions are forgeable or cookies are
// travelling in the clear. Refusing to start is the only signal that reliably
// reaches whoever deployed it.
func (c *Config) Validate() error {
	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		return fmt.Errorf("ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.Env)
	}

	// An unlimited pool is what database/sql does by default and is exactly
	// what the setting exists to prevent, so zero is rejected rather than
	// passed through to mean "no limit".
	if c.DBMaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS must be at least 1, got %d", c.DBMaxOpenConns)
	}

	if !c.IsProduction() {
		return nil
	}

	// getEnv treats an empty value as absent, so an unset variable and one set
	// to "" -- which is what an unresolved platform reference like
	// ${{Postgres.DATABASE_URL}} produces -- both land here. Without this the
	// server dials localhost and reports "connection refused" against an
	// address nobody configured, which says nothing about the actual mistake.
	if c.DatabaseURL == defaultDatabaseURL {
		return errors.New("DATABASE_URL is not set; the server fell back to the localhost " +
			"development default. If it is set through a platform reference such as " +
			"${{Postgres.DATABASE_URL}}, check that the reference resolves and that the " +
			"database service name matches")
	}

	if c.SessionKey == defaultSessionKey {
		return errors.New("SESSION_KEY is still the committed development default; " +
			"anyone who has read the source can forge a session cookie for any account. " +
			"Generate one with: openssl rand -hex 32")
	}

	if len(c.SessionKey) < minSessionKeyBytes {
		return fmt.Errorf("SESSION_KEY is %d bytes, need at least %d; generate one with: openssl rand -hex 32",
			len(c.SessionKey), minSessionKeyBytes)
	}

	return nil
}

// LoadLocation resolves TimeZone to a *time.Location.
//
// An unknown zone returns UTC alongside the error rather than nothing, so the
// caller can log the misconfiguration once the logger exists and still boot
// with a usable location instead of a nil one.
func (c *Config) LoadLocation() (*time.Location, error) {
	loc, err := time.LoadLocation(c.TimeZone)
	if err != nil {
		return time.UTC, fmt.Errorf("loading timezone %q: %w", c.TimeZone, err)
	}
	return loc, nil
}

// IsDevelopment returns true if the application is running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// IsProduction returns true if the application is running in production mode.
func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

// getEnv retrieves an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool retrieves an environment variable as a boolean or returns a
// default value. An unparseable value falls back to the default rather than
// being read as false, so a typo cannot silently turn a feature off.
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getEnvList retrieves an environment variable as a comma-separated list.
// Blank entries are dropped, so a trailing comma or a stray space cannot
// produce an element that matches nothing.
func getEnvList(key string) []string {
	var values []string
	for _, part := range strings.Split(os.Getenv(key), ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

// getEnvInt retrieves an environment variable as an integer or returns a default value.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
