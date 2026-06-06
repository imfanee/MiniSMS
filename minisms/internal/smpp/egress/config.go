// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"fmt"
	"time"

	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
)

// CarrierConfig is the runtime view of a carrier SMPP egress row.
type CarrierConfig struct {
	CarrierID           string
	EgressTransport     string
	Addr                string
	SystemID            string
	Password            string
	SystemType          string
	BindMode            string
	TLS                 bool
	EnquireLink         time.Duration
	WindowSize          uint
	ThroughputPerSecond int
}

func carrierConfigFromRow(row db.CarrierSMPPEgress, password string, cfg *config.Config) CarrierConfig {
	st := ""
	if row.SMPPSystemType != nil {
		st = *row.SMPPSystemType
	}
	enquireS := row.SMPPEnquireLinkS
	window := row.SMPPWindowSize
	throughput := row.SMPPThroughputPerS
	if cfg != nil {
		if enquireS < 5 {
			enquireS = cfg.SMPPEnquireLinkSecs
		}
		if window < 1 {
			window = cfg.SMPPWindowSize
		}
		if throughput < 1 {
			throughput = cfg.SMPPThroughputPerS
		}
	}
	return CarrierConfig{
		CarrierID:           row.CarrierID,
		EgressTransport:     row.EgressTransport,
		Addr:                fmt.Sprintf("%s:%d", row.SMPPHost, row.SMPPPort),
		SystemID:            row.SMPPSystemID,
		Password:            password,
		SystemType:          st,
		BindMode:            row.SMPPBindMode,
		TLS:                 row.SMPPTLS,
		EnquireLink:         time.Duration(enquireS) * time.Second,
		WindowSize:          uint(window),
		ThroughputPerSecond: throughput,
	}
}
