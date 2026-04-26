package carrier

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

func DispatchToCarrier(req DispatchRequest) (*DispatchResult, error) {
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

	client := &http.Client{Timeout: req.Timeout}
	resp, err := client.Do(httpReq)
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
