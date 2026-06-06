// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"testing"

	"github.com/minisms/minisms/internal/sending"
)

func TestCommandStatusForOutcome(t *testing.T) {
	if CommandStatusForOutcome(sending.SubmitOutcome{Kind: sending.OutcomeAccepted}) != StatusROK {
		t.Fatal("accepted")
	}
	if CommandStatusForOutcome(sending.SubmitOutcome{Kind: sending.OutcomeInsufficientBalance}) != StatusRQueryFail {
		t.Fatal("balance")
	}
	if CommandStatusForOutcome(sending.SubmitOutcome{Kind: sending.OutcomeNoRoute}) != StatusRInvDstAddr {
		t.Fatal("route")
	}
}
