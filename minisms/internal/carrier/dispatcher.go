// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	dispatchTLSMu        sync.RWMutex
	dispatchInsecureTLS  bool
)

// SetDispatchInsecureTLS allows HTTPS carrier endpoints with untrusted/self-signed certificates when true.
func SetDispatchInsecureTLS(v bool) {
	dispatchTLSMu.Lock()
	dispatchInsecureTLS = v
	dispatchTLSMu.Unlock()
}

func dispatchHTTPClient(timeout time.Duration) *http.Client {
	dispatchTLSMu.RLock()
	insecure := dispatchInsecureTLS
	dispatchTLSMu.RUnlock()
	if !insecure {
		return &http.Client{Timeout: timeout}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for known carrier endpoints
	return &http.Client{Timeout: timeout, Transport: tr}
}

type DispatchRequest struct {
	Method      string
	EndpointURL string
	ContentType string
	Body        string
	Query       string
	Headers     map[string]string
	Timeout     time.Duration
}

type DispatchResult struct {
	StatusCode int
	Body       string
}

// dispatchEndpointValidator is overridden in tests that use httptest (127.0.0.1).
var dispatchEndpointValidator = ValidateEndpointURL

// SetDispatchEndpointValidatorForTest replaces SSRF validation (integration tests with httptest only).
func SetDispatchEndpointValidatorForTest(v func(string) error) {
	dispatchEndpointValidator = v
}

// ResetDispatchEndpointValidator restores production SSRF validation.
func ResetDispatchEndpointValidator() {
	dispatchEndpointValidator = ValidateEndpointURL
}

func DispatchToCarrier(req DispatchRequest) (*DispatchResult, error) {
	if err := dispatchEndpointValidator(req.EndpointURL); err != nil {
		return nil, err
	}
	u, err := url.Parse(req.EndpointURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Query) != "" {
		q, err := url.ParseQuery(req.Query)
		if err != nil {
			return nil, err
		}
		u.RawQuery = q.Encode()
	}
	httpReq, err := http.NewRequest(req.Method, u.String(), strings.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := dispatchHTTPClient(req.Timeout).Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return &DispatchResult{
		StatusCode: resp.StatusCode,
		Body:       string(b),
	}, nil
}
