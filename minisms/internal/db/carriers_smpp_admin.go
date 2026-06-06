// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDuplicateSMPPSystemID is returned when smpp_system_id collides across carriers or clients.
var ErrDuplicateSMPPSystemID = errors.New("duplicate smpp system id")

// CarrierSMPPSettings is admin-editable carrier egress SMPP configuration.
type CarrierSMPPSettings struct {
	SMPPHost           *string
	SMPPPort           *int
	SMPPSystemID       *string
	SMPPPasswordEnc    *string // nil = leave unchanged
	SMPPSystemType     *string
	SMPPBindMode       string
	SMPPTLS            bool
	SMPPEnquireLinkS   int
	SMPPWindowSize     int
	SMPPThroughputPerS int
}

func UpdateCarrierSMPPSettings(ctx context.Context, pool *pgxpool.Pool, carrierID string, s CarrierSMPPSettings, keepPassword bool) error {
	var password any
	if keepPassword {
		password = nil // COALESCE in SQL via subselect
	} else {
		password = s.SMPPPasswordEnc
	}
	if keepPassword {
		_, err := pool.Exec(ctx, `
			UPDATE carriers SET
				smpp_host=$1, smpp_port=$2, smpp_system_id=$3,
				smpp_system_type=$4, smpp_bind_mode=$5, smpp_tls=$6,
				smpp_enquire_link_s=$7, smpp_window_size=$8, smpp_throughput_per_s=$9,
				updated_at=now()
			WHERE carrier_id=$10::uuid`,
			s.SMPPHost, s.SMPPPort, s.SMPPSystemID,
			s.SMPPSystemType, s.SMPPBindMode, s.SMPPTLS,
			s.SMPPEnquireLinkS, s.SMPPWindowSize, s.SMPPThroughputPerS, carrierID,
		)
		return mapSMPPUnique(err)
	}
	_, err := pool.Exec(ctx, `
		UPDATE carriers SET
			smpp_host=$1, smpp_port=$2, smpp_system_id=$3, smpp_password_enc=$4,
			smpp_system_type=$5, smpp_bind_mode=$6, smpp_tls=$7,
			smpp_enquire_link_s=$8, smpp_window_size=$9, smpp_throughput_per_s=$10,
			updated_at=now()
		WHERE carrier_id=$11::uuid`,
		s.SMPPHost, s.SMPPPort, s.SMPPSystemID, password,
		s.SMPPSystemType, s.SMPPBindMode, s.SMPPTLS,
		s.SMPPEnquireLinkS, s.SMPPWindowSize, s.SMPPThroughputPerS, carrierID,
	)
	return mapSMPPUnique(err)
}

func mapSMPPUnique(err error) error {
	if err == nil {
		return nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return ErrDuplicateSMPPSystemID
	}
	return err
}
