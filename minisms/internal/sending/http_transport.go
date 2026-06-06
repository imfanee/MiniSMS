// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"

	"github.com/minisms/minisms/internal/carrier"
)

// HTTPTransport implements CarrierTransport via the existing HTTP carrier dispatcher.
type HTTPTransport struct{}

func (HTTPTransport) Dispatch(ctx context.Context, in CarrierDispatchInput) (*CarrierDispatchResult, error) {
	resp, err := carrier.DispatchToCarrier(carrier.DispatchRequest{
		Method:      in.Method,
		EndpointURL: in.EndpointURL,
		ContentType: in.ContentType,
		Body:        in.Body,
		Query:       in.Query,
		Headers:     in.Headers,
		Timeout:     in.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &CarrierDispatchResult{
		StatusCode: resp.StatusCode,
		Body:       resp.Body,
		MessageID:  "",
	}, nil
}
