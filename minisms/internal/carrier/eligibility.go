package carrier

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CarrierSkipReason struct {
	CarrierID   string `json:"carrier_id"`
	CarrierName string `json:"carrier_name"`
	Reason      string `json:"reason"` // "sender_id_policy" | "in_loss" | "inactive"
}

var (
	reNumeric = regexp.MustCompile(`^[0-9]{1,15}$`)
	reE164    = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
)

func CheckCarrierEligibility(
	ctx context.Context,
	pool *pgxpool.Pool,
	carrierID string,
	carrierName string,
	senderIDPolicy string,
	defaultSenderIDValue *string,
	carrierRateGroupID *string,
	resolvedSID SenderIDResolution,
	clientRate string,
	clientID string,
	prefix string,
) (effectiveSenderID string, eligible bool, reason string, err error) {
	effectiveSenderID = strings.TrimSpace(resolvedSID.Value)
	if resolvedSID.Source == "carrier_default" || senderIDPolicy == "none" {
		if defaultSenderIDValue != nil && strings.TrimSpace(*defaultSenderIDValue) != "" {
			effectiveSenderID = strings.TrimSpace(*defaultSenderIDValue)
		} else {
			var sid string
			e := pool.QueryRow(ctx, `
				SELECT si.value
				FROM carrier_sender_ids csid
				JOIN sender_ids si ON si.sender_id = csid.sender_id
				WHERE csid.carrier_id=$1::uuid AND csid.is_default=TRUE AND si.is_active=TRUE
				LIMIT 1`, carrierID).Scan(&sid)
			if e == nil {
				effectiveSenderID = sid
			}
		}
	}

	switch senderIDPolicy {
	case "any", "none":
	case "numeric":
		if !reNumeric.MatchString(effectiveSenderID) {
			return effectiveSenderID, false, "sender_id_policy", nil
		}
	case "e164":
		if !reE164.MatchString(effectiveSenderID) {
			return effectiveSenderID, false, "sender_id_policy", nil
		}
	case "list":
		if pool == nil {
			return effectiveSenderID, false, "sender_id_policy", nil
		}
		var n int
		e := pool.QueryRow(ctx, `
			SELECT 1
			FROM carrier_sender_ids csid
			JOIN sender_ids si ON si.sender_id = csid.sender_id
			WHERE csid.carrier_id=$1::uuid AND si.value=$2 AND si.is_active=TRUE
			LIMIT 1`, carrierID, effectiveSenderID).Scan(&n)
		if e == pgx.ErrNoRows {
			return effectiveSenderID, false, "sender_id_policy", nil
		}
		if e != nil {
			return "", false, "", e
		}
	default:
		return effectiveSenderID, false, "sender_id_policy", nil
	}

	var allowInLoss bool
	if pool != nil {
		_ = pool.QueryRow(ctx, `SELECT allow_in_loss_delivery FROM clients WHERE client_id=$1::uuid`, clientID).Scan(&allowInLoss)
	} else {
		allowInLoss = true
	}
	if !allowInLoss && carrierRateGroupID != nil && strings.TrimSpace(*carrierRateGroupID) != "" {
		if pool == nil {
			return effectiveSenderID, true, "", nil
		}
		var carrierRate string
		rows, e := pool.Query(ctx, `
			SELECT rate_per_sms::text, prefix
			FROM v_active_rate_entries
			WHERE rate_group_id = $1::uuid`, *carrierRateGroupID)
		if e == nil {
			defer rows.Close()
			bestLen := -1
			for rows.Next() {
				var r, pfx string
				if se := rows.Scan(&r, &pfx); se != nil {
					continue
				}
				if pfx == "*" {
					if bestLen < 0 {
						carrierRate = r
					}
					continue
				}
				if strings.HasPrefix(prefix, pfx) && len(pfx) > bestLen {
					bestLen = len(pfx)
					carrierRate = r
				}
			}
		}
		if carrierRate != "" {
			var inLoss bool
			if e := pool.QueryRow(ctx, `SELECT ($1::numeric(18,6) > $2::numeric(18,6))`, carrierRate, clientRate).Scan(&inLoss); e == nil && inLoss {
				return effectiveSenderID, false, "in_loss", nil
			}
		}
	}

	_ = carrierName
	return effectiveSenderID, true, "", nil
}

