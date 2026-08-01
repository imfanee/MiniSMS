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
	HTTPCarrierInsecureTLS       bool // skip TLS verify for outbound HTTP carriers (self-signed Gateway, etc.)
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
	// Async send queue (reserve-on-accept, worker-pool dispatch). Off by default:
	// when disabled, Submit dispatches synchronously exactly as before.
	SendQueueEnabled     bool
	SendQueueWorkers     int
	SendQueueBatch       int
	SendMessageTTLSecs   int // validity: drop to undelivered after this many seconds
	SendRetryBackoffSecs int // base backoff between dispatch retries
	SendStuckSecs        int // reclaim rows left 'sending' by a dead worker after this
	// Age out messages the carrier accepted but never sent a final DLR for: after this many seconds
	// a still-'accepted' + DLR-requested message becomes 'undelivered' and the client is notified.
	// 0 disables the reaper. No refund: the carrier accepted and billed the message; a missing DLR is
	// not proof of non-delivery.
	AcceptedDLRTTLSecs int
	// Per-entity wire logging: every SMPP PDU and HTTP request/response to/from each carrier and client
	// to dedicated, size-rotated files under WireLogDir (secrets always masked).
	WireLogEnabled  bool
	WireLogDir      string
	WireLogMaxMB    int
	WireLogMaxFiles int
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

	c.SendQueueEnabled = parseBoolDefault("SEND_QUEUE_ENABLED", false)
	c.SendQueueWorkers, err = parseIntDefault("SEND_QUEUE_WORKERS", 16, 1, 512)
	if err != nil {
		return nil, err
	}
	c.SendQueueBatch, err = parseIntDefault("SEND_QUEUE_BATCH", 100, 1, 5000)
	if err != nil {
		return nil, err
	}
	c.SendMessageTTLSecs, err = parseIntDefault("SEND_MESSAGE_TTL_S", 3600, 30, 604800)
	if err != nil {
		return nil, err
	}
	c.SendRetryBackoffSecs, err = parseIntDefault("SEND_RETRY_BACKOFF_S", 5, 1, 3600)
	if err != nil {
		return nil, err
	}
	c.SendStuckSecs, err = parseIntDefault("SEND_STUCK_S", 120, 30, 3600)
	if err != nil {
		return nil, err
	}
	c.AcceptedDLRTTLSecs, err = parseIntDefault("SEND_ACCEPTED_DLR_TTL_S", 0, 0, 2592000)
	if err != nil {
		return nil, err
	}
	c.WireLogEnabled = parseBoolDefault("WIRE_LOG_ENABLED", false)
	c.WireLogDir = strings.TrimSpace(os.Getenv("WIRE_LOG_DIR"))
	if c.WireLogDir == "" {
		c.WireLogDir = "/var/log/minisms"
	}
	c.WireLogMaxMB, err = parseIntDefault("WIRE_LOG_MAX_MB", 100, 1, 10240)
	if err != nil {
		return nil, err
	}
	c.WireLogMaxFiles, err = parseIntDefault("WIRE_LOG_MAX_FILES", 5, 1, 100)
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
