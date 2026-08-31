package config

import (
	"strings"
	"testing"
	"time"
)

func TestIsDevelopment(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected bool
	}{
		{"development", "development", true},
		{"production", "production", false},
		{"empty", "", false},
		{"staging", "staging", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env}
			if got := cfg.IsDevelopment(); got != tt.expected {
				t.Errorf("IsDevelopment() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsProduction(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected bool
	}{
		{"production", "production", true},
		{"development", "development", false},
		{"empty", "", false},
		{"staging", "staging", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Env: tt.env}
			if got := cfg.IsProduction(); got != tt.expected {
				t.Errorf("IsProduction() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV", "custom")
		if got := getEnv("TEST_GET_ENV", "default"); got != "custom" {
			t.Errorf("getEnv() = %q, want %q", got, "custom")
		}
	})

	t.Run("returns default when unset", func(t *testing.T) {
		if got := getEnv("TEST_GET_ENV_MISSING", "default"); got != "default" {
			t.Errorf("getEnv() = %q, want %q", got, "default")
		}
	})

	t.Run("returns default when empty", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_EMPTY", "")
		if got := getEnv("TEST_GET_ENV_EMPTY", "default"); got != "default" {
			t.Errorf("getEnv() = %q, want %q", got, "default")
		}
	})
}

func TestGetEnvInt(t *testing.T) {
	t.Run("returns int value when set", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_INT", "30")
		if got := getEnvInt("TEST_GET_ENV_INT", 15); got != 30 {
			t.Errorf("getEnvInt() = %d, want %d", got, 30)
		}
	})

	t.Run("returns default when unset", func(t *testing.T) {
		if got := getEnvInt("TEST_GET_ENV_INT_MISSING", 15); got != 15 {
			t.Errorf("getEnvInt() = %d, want %d", got, 15)
		}
	})

	t.Run("returns default when invalid", func(t *testing.T) {
		t.Setenv("TEST_GET_ENV_INT_BAD", "abc")
		if got := getEnvInt("TEST_GET_ENV_INT_BAD", 15); got != 15 {
			t.Errorf("getEnvInt() = %d, want %d", got, 15)
		}
	})
}

func TestLoadLocation(t *testing.T) {
	tests := []struct {
		name     string
		timeZone string
		want     string
		wantErr  bool
	}{
		{"eastern", "America/New_York", "America/New_York", false},
		{"utc", "UTC", "UTC", false},
		{"pacific", "America/Los_Angeles", "America/Los_Angeles", false},
		// A typo must not take the process down, so it degrades to UTC and
		// reports the problem for the caller to log.
		{"unknown zone falls back to UTC", "Mars/Olympus_Mons", "UTC", true},
		{"empty falls back to UTC", "", "UTC", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{TimeZone: tt.timeZone}

			loc, err := cfg.LoadLocation()
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadLocation() error = %v, wantErr %v", err, tt.wantErr)
			}
			if loc == nil {
				t.Fatal("LoadLocation() returned a nil location")
			}
			if loc.String() != tt.want {
				t.Errorf("LoadLocation() = %s, want %s", loc, tt.want)
			}
		})
	}
}

func TestLoadDefaultsTimeZone(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "")

	cfg := Load()
	if want := "America/New_York"; cfg.TimeZone != want {
		t.Errorf("TimeZone = %q, want %q", cfg.TimeZone, want)
	}

	loc, err := cfg.LoadLocation()
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	if loc == time.UTC {
		t.Error("default timezone resolved to UTC, which reintroduces the day-boundary bug")
	}
}

// TestValidate pins the checks that keep a silently insecure deployment from
// starting. Each of these produces a server that boots, serves traffic and logs
// nothing unusual, so refusing to start is the only signal that gets through.
func TestValidate(t *testing.T) {
	strongKey := strings.Repeat("a", minSessionKeyBytes)

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "development accepts the committed default key",
			cfg:  Config{Env: EnvDevelopment, SessionKey: defaultSessionKey},
		},
		{
			name: "production accepts a strong key",
			cfg:  Config{Env: EnvProduction, SessionKey: strongKey},
		},
		{
			// The key is the HMAC secret behind every session cookie and it is
			// committed to this repository, so anyone could mint a cookie for
			// the administrator.
			name:    "production rejects the committed default key",
			cfg:     Config{Env: EnvProduction, SessionKey: defaultSessionKey},
			wantErr: true,
		},
		{
			name:    "production rejects a short key",
			cfg:     Config{Env: EnvProduction, SessionKey: strings.Repeat("a", minSessionKeyBytes-1)},
			wantErr: true,
		},
		{
			name:    "production rejects an empty key",
			cfg:     Config{Env: EnvProduction, SessionKey: ""},
			wantErr: true,
		},
		{
			// IsProduction gates the Secure flag on the session cookie, so a
			// value that is neither known would ship cookies over plain HTTP
			// while looking like a production deployment.
			name:    "an unrecognised env is rejected",
			cfg:     Config{Env: "prod", SessionKey: strongKey},
			wantErr: true,
		},
		{
			name:    "staging is not a recognised env",
			cfg:     Config{Env: "staging", SessionKey: strongKey},
			wantErr: true,
		},
		{
			name:    "an empty env is rejected",
			cfg:     Config{Env: "", SessionKey: strongKey},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			// Every case above is about the session key or ENV. Give the pool
			// and the database URL usable values so none of them can pass or
			// fail for the wrong reason; both have their own tests below.
			cfg.DBMaxOpenConns = 20
			if cfg.DatabaseURL == "" {
				cfg.DatabaseURL = "postgres://user:pw@db.example.com:5432/app"
			}

			err := cfg.Validate()

			if tt.wantErr && err == nil {
				t.Error("Validate() = nil, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

// An unlimited pool is database/sql's default and is the thing the setting
// exists to prevent, so zero must not quietly pass through as "no limit".
// An unresolved platform reference resolves to an empty string, which getEnv
// treats as absent -- so the server would otherwise dial localhost in
// production and report "connection refused" for an address nobody configured.
func TestValidateRejectsTheDevelopmentDatabaseInProduction(t *testing.T) {
	base := Config{Env: EnvProduction, SessionKey: strings.Repeat("a", minSessionKeyBytes), DBMaxOpenConns: 20}

	t.Run("production rejects the development default", func(t *testing.T) {
		cfg := base
		cfg.DatabaseURL = defaultDatabaseURL
		err := cfg.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want an error")
		}
		if !strings.Contains(err.Error(), "DATABASE_URL") {
			t.Errorf("error = %q, want it to name DATABASE_URL", err)
		}
	})

	t.Run("production accepts a real database url", func(t *testing.T) {
		cfg := base
		cfg.DatabaseURL = "postgres://user:pw@db.example.com:5432/app"
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	// Development is where the default belongs, so it must stay usable there.
	t.Run("development accepts the default", func(t *testing.T) {
		cfg := Config{Env: EnvDevelopment, SessionKey: defaultSessionKey, DatabaseURL: defaultDatabaseURL, DBMaxOpenConns: 20}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})
}

func TestValidateRejectsUnusablePoolSize(t *testing.T) {
	for _, size := range []int{0, -1} {
		cfg := Config{Env: EnvDevelopment, SessionKey: defaultSessionKey, DBMaxOpenConns: size}
		if err := cfg.Validate(); err == nil {
			t.Errorf("DB_MAX_OPEN_CONNS = %d: Validate() = nil, want an error", size)
		}
	}
}

// The defaults Load produces must themselves pass validation, or a fresh
// checkout cannot run.
func TestLoadDefaultsAreValidForDevelopment(t *testing.T) {
	t.Setenv("ENV", "")
	t.Setenv("SESSION_KEY", "")
	t.Setenv("DATABASE_URL", "")

	if err := Load().Validate(); err != nil {
		t.Errorf("default configuration does not validate: %v", err)
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset falls back", "", true},
		{"true", "true", true},
		{"false", "false", false},
		{"one", "1", true},
		{"zero", "0", false},
		{"capitalised", "False", false},
		// A typo must not silently disable migrations on boot; fall back to the
		// default rather than reading anything unparseable as false.
		{"unparseable falls back", "yes-please", true},
		{"whitespace falls back", " ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MIGRATE_ON_START", tt.value)

			if got := getEnvBool("MIGRATE_ON_START", true); got != tt.want {
				t.Errorf("getEnvBool(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
