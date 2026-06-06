// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/minisms/minisms/internal/sending"
)

// SMPP command_status values (SMPP v3.4).
const (
	StatusROK          pdu.Status = 0x00000000
	StatusRInvDstAddr  pdu.Status = 0x0000000B
	StatusRInvSrcAddr  pdu.Status = 0x0000000A
	StatusRInvMsgLen   pdu.Status = 0x00000001
	StatusRBindFail    pdu.Status = 0x0000000D
	StatusRThrottled   pdu.Status = 0x00000058
	StatusRSubmitFail  pdu.Status = 0x00000045
	StatusRQueryFail    pdu.Status = 0x00000067
	StatusRInvSysID    pdu.Status = 0x00000015
)

// CommandStatusForOutcome maps sending.Submit outcomes to ESME command_status.
func CommandStatusForOutcome(out sending.SubmitOutcome) pdu.Status {
	switch out.Kind {
	case sending.OutcomeAccepted:
		return StatusROK
	case sending.OutcomeInsufficientBalance:
		return StatusRQueryFail
	case sending.OutcomeNoRate, sending.OutcomeNoRoute:
		return StatusRInvDstAddr
	case sending.OutcomeNoEligibleCarrier:
		return StatusRSubmitFail
	case sending.OutcomeCarrierFailure:
		return StatusRSubmitFail
	case sending.OutcomeTemporaryUnavailable:
		return StatusRSubmitFail
	default:
		return StatusRSubmitFail
	}
}
