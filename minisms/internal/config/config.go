// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
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
	HTTPListenAddr             string // e.g. 127.0.0.1:18080; empty → ":"+PORT
	CSRFTrustedOrigins         []string // CSRF_TRUSTED_ORIGINS comma-separated (behind nginx TLS)
	TLSEnabled                 bool
	TLSCertFile                string
	TLSKeyFile                 string
	LogLevel                   string
	AppEnv                     string
	CSRFSigningKey             []byte
	SessionIdle                time.Duration
	CarrierDispatchTimeoutSecs   int
	HTTPCarrierInsecureTLS       bool // skip TLS verify for outbound HTTP carriers (self-signed Kamex, etc.)
	SMPPServerEnabled          bool
	SMPPListenAddr             string
	SMPPSystemID               string
	SMPPTLSEnabled             bool
	SMPPTLSCertFile            string
	SMPPTLSKeyFile             string
	// Defaults for carrier/client SMPP rows when DB values are unset (ADR §7).
	SMPPEnquireLinkSecs int
	SMPPWindowSize      int
	SMPPThroughputPerS  int
}

// Load reads configuration from the environment, optionally from a .env file.
func Load() (*Config, error) {
	_ = godotenv.Load()

	c := &Config{
		Port:              getDefault("PORT", "8080"),
		HTTPListenAddr:    strings.TrimSpace(os.Getenv("HTTP_LISTEN_ADDR")),
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
	c.HTTPCarrierInsecureTLS = parseBoolDefault("HTTP_CARRIER_INSECURE_TLS", false)

	c.SMPPServerEnabled = parseBoolDefault("SMPP_SERVER_ENABLED", false)
	c.SMPPListenAddr = getDefault("SMPP_LISTEN_ADDR", ":2775")
	c.SMPPSystemID = getDefault("SMPP_SYSTEM_ID", "MiniSMS")
	c.SMPPTLSEnabled = parseBoolDefault("SMPP_TLS_ENABLED", false)
	c.SMPPTLSCertFile = strings.TrimSpace(os.Getenv("SMPP_TLS_CERT_FILE"))
	c.SMPPTLSKeyFile = strings.TrimSpace(os.Getenv("SMPP_TLS_KEY_FILE"))
	if c.SMPPTLSEnabled {
		if c.SMPPTLSCertFile == "" {
			return nil, fmt.Errorf("SMPP_TLS_CERT_FILE is required when SMPP_TLS_ENABLED=true")
		}
		if c.SMPPTLSKeyFile == "" {
			return nil, fmt.Errorf("SMPP_TLS_KEY_FILE is required when SMPP_TLS_ENABLED=true")
		}
	}

	c.SMPPEnquireLinkSecs, err = parseIntDefault("SMPP_ENQUIRE_LINK_S", 30, 5, 3600)
	if err != nil {
		return nil, err
	}
	c.SMPPWindowSize, err = parseIntDefault("SMPP_WINDOW_SIZE", 10, 1, 1000)
	if err != nil {
		return nil, err
	}
	c.SMPPThroughputPerS, err = parseIntDefault("SMPP_THROUGHPUT_PER_S", 50, 1, 10000)
	if err != nil {
		return nil, err
	}

	if raw := strings.TrimSpace(os.Getenv("CSRF_TRUSTED_ORIGINS")); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				c.CSRFTrustedOrigins = append(c.CSRFTrustedOrigins, o)
			}
		}
	}

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

// HTTPAddr is the address passed to http.Server (Listen).
func (c *Config) HTTPAddr() string {
	if c.HTTPListenAddr != "" {
		return c.HTTPListenAddr
	}
	return ":" + c.Port
}
