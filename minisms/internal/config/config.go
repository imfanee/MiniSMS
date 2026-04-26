package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

// Config holds application settings loaded from the environment.
type Config struct {
	DatabaseURL                string
	SecretKey                  []byte
	AdminUsername              string
	AdminPasswordHash          string
	Port                       string
	TLSEnabled                 bool
	TLSCertFile                string
	TLSKeyFile                 string
	LogLevel                   string
	AppEnv                     string
	CSRFSigningKey             []byte
	SessionIdle                time.Duration
	CarrierDispatchTimeoutSecs int
}

// Load reads configuration from the environment, optionally from a .env file.
func Load() (*Config, error) {
	_ = godotenv.Load()

	c := &Config{
		Port:              getDefault("PORT", "8080"),
		LogLevel:          getDefault("LOG_LEVEL", "info"),
		AppEnv:            getDefault("APP_ENV", "development"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		TLSEnabled:        parseBoolDefault("TLS_ENABLED", false),
		TLSCertFile:       strings.TrimSpace(os.Getenv("TLS_CERT_FILE")),
		TLSKeyFile:        strings.TrimSpace(os.Getenv("TLS_KEY_FILE")),
		AdminUsername:     os.Getenv("ADMIN_USERNAME"),
		AdminPasswordHash: os.Getenv("ADMIN_PASSWORD_HASH"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	sk, err := parseHexKey("SECRET_KEY", 32)
	if err != nil {
		return nil, err
	}
	c.SecretKey = sk

	ck, err := parseHexKey("CSRF_AUTH_KEY", 32)
	if err != nil {
		return nil, err
	}
	c.CSRFSigningKey = ck

	if c.AdminUsername == "" {
		return nil, fmt.Errorf("ADMIN_USERNAME is required")
	}
	if c.AdminPasswordHash == "" {
		return nil, fmt.Errorf("ADMIN_PASSWORD_HASH is required")
	}
	if _, err := bcrypt.Cost([]byte(c.AdminPasswordHash)); err != nil {
		return nil, fmt.Errorf("ADMIN_PASSWORD_HASH is not a valid bcrypt hash (if set in .env, wrap value in single quotes): %w", err)
	}

	mins, err := parseIntDefault("SESSION_IDLE_MINUTES", 240, 1, 24*30*6)
	if err != nil {
		return nil, err
	}
	c.SessionIdle = time.Duration(mins) * time.Minute

	secs, err := parseIntDefault("CARRIER_DISPATCH_TIMEOUT_S", 10, 1, 3600)
	if err != nil {
		return nil, err
	}
	c.CarrierDispatchTimeoutSecs = secs
	if c.TLSEnabled {
		if c.TLSCertFile == "" {
			return nil, fmt.Errorf("TLS_CERT_FILE is required when TLS_ENABLED=true")
		}
		if c.TLSKeyFile == "" {
			return nil, fmt.Errorf("TLS_KEY_FILE is required when TLS_ENABLED=true")
		}
	}

	return c, nil
}

func getDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func parseHexKey(name string, n int) ([]byte, error) {
	s := os.Getenv(name)
	if s == "" {
		return nil, fmt.Errorf("%s is required (expect %d-byte hex = %d characters)", name, n, n*2)
	}
	if len(s) != n*2 {
		return nil, fmt.Errorf("%s must be exactly %d hex characters (%d bytes)", name, n*2, n)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid hex: %w", name, err)
	}
	if len(b) != n {
		return nil, fmt.Errorf("%s: decoded to wrong length", name)
	}
	return b, nil
}

func parseIntDefault(name string, def, min, max int) (int, error) {
	s := os.Getenv(name)
	if s == "" {
		return def, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer: %w", name, err)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%s: must be between %d and %d", name, min, max)
	}
	return v, nil
}

func parseBoolDefault(name string, def bool) bool {
	s := strings.TrimSpace(os.Getenv(name))
	if s == "" {
		return def
	}
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// IsProduction returns true when the app should use stricter security (e.g. Secure cookies).
func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}
