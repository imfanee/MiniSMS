// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/db"
)

// Client allowed sender ID modes (clients.allowed_sender_ids_mode).
const (
	AllowedSenderList  = "list"
	AllowedSenderPhone = "phone"
	AllowedSenderAny   = "any"
)

// Default allows letters, digits, space, underscore, dot, hyphen (typical alphanumeric originators).
const defaultAnySenderPattern = `^[A-Za-z0-9 _.-]{1,15}$`
const maxSenderIDRunes = 15

// ParseAllowedSenderIDsMode normalizes form/API values.
func ParseAllowedSenderIDsMode(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case AllowedSenderList, "from_sender_id_list":
		return AllowedSenderList, true
	case AllowedSenderPhone, "any_valid_phone_number", "e164", "numeric":
		return AllowedSenderPhone, true
	case AllowedSenderAny:
		return AllowedSenderAny, true
	default:
		return "", false
	}
}

// IsValidPhoneSenderID accepts national numeric (1–15 digits) or E.164.
func IsValidPhoneSenderID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return reNumeric.MatchString(value) || reE164.MatchString(value)
}

// IsValidAnySenderID checks value against the global pattern system setting.
func IsValidAnySenderID(ctx context.Context, pool *pgxpool.Pool, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || utf8RuneCount(value) > maxSenderIDRunes {
		return false
	}
	pattern := defaultAnySenderPattern
	if pool != nil {
		pattern = strings.TrimSpace(db.Setting(ctx, pool, "sender_id_any_allowed_pattern", defaultAnySenderPattern))
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = regexp.MustCompile(defaultAnySenderPattern)
	}
	return re.MatchString(value)
}

func utf8RuneCount(s string) int {
	return len([]rune(s))
}

// ValidateClientSenderID checks whether value is allowed for the client's configured mode.
func ValidateClientSenderID(ctx context.Context, pool *pgxpool.Pool, clientID, mode, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("sender ID is required")
	}
	if utf8RuneCount(value) > maxSenderIDRunes {
		return fmt.Errorf("sender ID must be at most %d characters", maxSenderIDRunes)
	}
	mode, ok := ParseAllowedSenderIDsMode(mode)
	if !ok {
		return fmt.Errorf("invalid allowed sender IDs mode")
	}
	switch mode {
	case AllowedSenderList:
		if pool == nil {
			return ErrSenderNotAllowed
		}
		var n int
		err := pool.QueryRow(ctx, `
			SELECT 1
			FROM client_sender_ids csi
			JOIN sender_ids si ON si.sender_id = csi.sender_id
			WHERE csi.client_id = $1::uuid AND si.value = $2 AND si.is_active = TRUE
			LIMIT 1`, clientID, value).Scan(&n)
		if err == pgx.ErrNoRows {
			return ErrSenderNotAllowed
		}
		if err != nil {
			return err
		}
		return nil
	case AllowedSenderPhone:
		if !IsValidPhoneSenderID(value) {
			return ErrSenderNotAllowed
		}
		return nil
	case AllowedSenderAny:
		if !IsValidAnySenderID(ctx, pool, value) {
			return ErrSenderNotAllowed
		}
		return nil
	default:
		return ErrSenderNotAllowed
	}
}

// ClientAllowsSenderID is used by ResolveSenderID for inbound requests.
func ClientAllowsSenderID(ctx context.Context, pool *pgxpool.Pool, clientID, mode, value string) bool {
	return ValidateClientSenderID(ctx, pool, clientID, mode, value) == nil
}

// SenderNotAllowedDetail returns a mode-specific API error detail for a rejected sender ID.
func SenderNotAllowedDetail(ctx context.Context, pool *pgxpool.Pool, clientID, value string) string {
	var mode string
	if pool != nil {
		_ = pool.QueryRow(ctx, `SELECT allowed_sender_ids_mode FROM clients WHERE client_id=$1::uuid`, clientID).Scan(&mode)
	}
	mode, ok := ParseAllowedSenderIDsMode(mode)
	if !ok {
		return "Sender ID is not allowed for this client"
	}
	value = strings.TrimSpace(value)
	switch mode {
	case AllowedSenderList:
		return "Sender ID is not in this client's allowed sender ID list"
	case AllowedSenderPhone:
		return "Sender ID must be a valid phone number (1–15 digits or E.164)"
	case AllowedSenderAny:
		pat := defaultAnySenderPattern
		if pool != nil {
			pat = strings.TrimSpace(db.Setting(ctx, pool, "sender_id_any_allowed_pattern", defaultAnySenderPattern))
		}
		if value == "" {
			return "Sender ID is required"
		}
		if utf8RuneCount(value) > maxSenderIDRunes {
			return fmt.Sprintf("Sender ID must be at most %d characters", maxSenderIDRunes)
		}
		return fmt.Sprintf(
			"Sender ID %q does not match this account's allowed pattern (max %d characters). Pattern: %s",
			value, maxSenderIDRunes, pat,
		)
	default:
		return "Sender ID is not allowed for this client"
	}
}

// ValidateDefaultSenderID validates an optional default sender on client create/update.
func ValidateDefaultSenderID(ctx context.Context, pool *pgxpool.Pool, clientID, mode, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	mode, ok := ParseAllowedSenderIDsMode(mode)
	if !ok {
		return fmt.Errorf("invalid allowed sender IDs mode")
	}
	if mode == AllowedSenderList && strings.TrimSpace(clientID) == "" {
		var n int
		err := pool.QueryRow(ctx, `SELECT 1 FROM sender_ids WHERE value = $1 AND is_active = TRUE LIMIT 1`, value).Scan(&n)
		if err == pgx.ErrNoRows {
			return ErrSenderNotAllowed
		}
		return err
	}
	return ValidateClientSenderID(ctx, pool, clientID, mode, value)
}
