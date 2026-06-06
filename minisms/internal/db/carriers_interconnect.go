// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpdateCarrierInterconnect sets egress_transport to http or smpp only.
func UpdateCarrierInterconnect(ctx context.Context, pool *pgxpool.Pool, carrierID, transport string) error {
	_, err := pool.Exec(ctx, `
		UPDATE carriers SET egress_transport = $1, updated_at = now()
		WHERE carrier_id = $2::uuid`, transport, carrierID)
	return err
}

// UpdateCarrierHTTPInterconnect sets HTTP dispatch endpoint and method.
func UpdateCarrierHTTPInterconnect(ctx context.Context, pool *pgxpool.Pool, carrierID, endpointURL, httpMethod string) error {
	_, err := pool.Exec(ctx, `
		UPDATE carriers SET endpoint_url = $1, http_method = $2, updated_at = now()
		WHERE carrier_id = $3::uuid`, endpointURL, httpMethod, carrierID)
	return err
}
