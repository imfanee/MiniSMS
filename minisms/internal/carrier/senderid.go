// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/db"
)

type SenderIDResolution struct {
	Value  string
	Source string // "client_provided" | "client_default" | "carrier_default" | "system_default"
}

var ErrSenderNotAllowed = errors.New("SMS_ERR_SENDER_NOT_ALLOWED")

func ResolveSenderID(
	ctx context.Context,
	pool *pgxpool.Pool,
	client *db.Client,
	requestedFrom string,
	systemDefault string,
) (SenderIDResolution, error) {
	req := strings.TrimSpace(requestedFrom)
	if req == "any" {
		return SenderIDResolution{Value: systemDefault, Source: "carrier_default"}, nil
	}
	if pool == nil {
		if req != "" {
			return SenderIDResolution{}, ErrSenderNotAllowed
		}
		return SenderIDResolution{Value: systemDefault, Source: "client_default"}, nil
	}
	if req != "" {
		var mode string
		if err := pool.QueryRow(ctx, `SELECT allowed_sender_ids_mode FROM clients WHERE client_id=$1::uuid`, client.ClientID).Scan(&mode); err != nil {
			return SenderIDResolution{}, err
		}
		if !ClientAllowsSenderID(ctx, pool, client.ClientID, mode, req) {
			return SenderIDResolution{}, ErrSenderNotAllowed
		}
		return SenderIDResolution{Value: req, Source: "client_provided"}, nil
	}

	mode := strings.TrimSpace(client.AllowedSenderIDsMode)
	if mode == "" {
		_ = pool.QueryRow(ctx, `SELECT allowed_sender_ids_mode FROM clients WHERE client_id=$1::uuid`, client.ClientID).Scan(&mode)
	}
	var clientDefault *string
	if client.DefaultSenderIDValue != nil && strings.TrimSpace(*client.DefaultSenderIDValue) != "" {
		clientDefault = client.DefaultSenderIDValue
	} else {
		_ = pool.QueryRow(ctx, `SELECT default_sender_id_value FROM clients WHERE client_id=$1::uuid`, client.ClientID).Scan(&clientDefault)
	}
	if clientDefault != nil && strings.TrimSpace(*clientDefault) != "" {
		val := strings.TrimSpace(*clientDefault)
		if ClientAllowsSenderID(ctx, pool, client.ClientID, mode, val) {
			return SenderIDResolution{Value: val, Source: "client_default"}, nil
		}
	}
	var sid string
	err := pool.QueryRow(ctx, `
		SELECT si.value
		FROM client_sender_ids csi
		JOIN sender_ids si ON si.sender_id = csi.sender_id
		WHERE csi.client_id = $1::uuid AND csi.is_default = TRUE AND si.is_active = TRUE
		LIMIT 1`, client.ClientID).Scan(&sid)
	if err == nil {
		return SenderIDResolution{Value: sid, Source: "client_default"}, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return SenderIDResolution{}, err
	}
	return SenderIDResolution{Value: systemDefault, Source: "client_default"}, nil
}

