// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"time"
)

// CarrierTransport dispatches one message attempt to a carrier (HTTP today; SMPP in Phase C).
type CarrierTransport interface {
	Dispatch(ctx context.Context, in CarrierDispatchInput) (*CarrierDispatchResult, error)
}

// CarrierDispatchInput is the per-attempt payload passed to CarrierTransport.
type CarrierDispatchInput struct {
	Method      string
	EndpointURL string
	ContentType string
	Body        string
	Query       string
	Headers     map[string]string
	Timeout     time.Duration
}

// CarrierDispatchResult is the normalized carrier response.
type CarrierDispatchResult struct {
	StatusCode int
	Body       string
	MessageID  string
}
