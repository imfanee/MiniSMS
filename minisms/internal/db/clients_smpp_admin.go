// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ClientSMPPSettings is admin-editable client ingress SMPP configuration.
type ClientSMPPSettings struct {
	SMPPIngressEnabled bool
	SMPPSystemID       *string
	SMPPPasswordEnc    *string // nil when unchanged
	SMPPAllowedCIDRs   *string
	SMPPMaxBinds       int
	SMPPDefaultSrcTON  *int16
	SMPPDefaultSrcNPI  *int16
	SMPPThroughputPerS int
	DLRDeliveryMode    string
}

func UpdateClientSMPPSettings(ctx context.Context, pool *pgxpool.Pool, clientID string, s ClientSMPPSettings, keepPassword bool) error {
	if keepPassword {
		_, err := pool.Exec(ctx, `
			UPDATE clients SET
				smpp_ingress_enabled=$1, smpp_system_id=$2, smpp_allowed_cidrs=$3, smpp_max_binds=$4,
				smpp_default_src_ton=$5, smpp_default_src_npi=$6, smpp_throughput_per_s=$7,
				dlr_delivery_mode=$8, updated_at=now()
			WHERE client_id=$9::uuid`,
			s.SMPPIngressEnabled, s.SMPPSystemID, s.SMPPAllowedCIDRs, s.SMPPMaxBinds,
			s.SMPPDefaultSrcTON, s.SMPPDefaultSrcNPI, s.SMPPThroughputPerS, s.DLRDeliveryMode, clientID,
		)
		return mapSMPPUnique(err)
	}
	_, err := pool.Exec(ctx, `
		UPDATE clients SET
			smpp_ingress_enabled=$1, smpp_system_id=$2, smpp_password_enc=$3, smpp_allowed_cidrs=$4, smpp_max_binds=$5,
			smpp_default_src_ton=$6, smpp_default_src_npi=$7, smpp_throughput_per_s=$8,
			dlr_delivery_mode=$9, updated_at=now()
		WHERE client_id=$10::uuid`,
		s.SMPPIngressEnabled, s.SMPPSystemID, s.SMPPPasswordEnc, s.SMPPAllowedCIDRs, s.SMPPMaxBinds,
		s.SMPPDefaultSrcTON, s.SMPPDefaultSrcNPI, s.SMPPThroughputPerS, s.DLRDeliveryMode, clientID,
	)
	return mapSMPPUnique(err)
}
