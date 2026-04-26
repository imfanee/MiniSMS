package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/billing"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
)

var e164Re = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type Handlers struct {
	Pool   *pgxpool.Pool
	Config *config.Config
}

type sendSMSRequest struct {
	To        string `json:"to"`
	From      string `json:"from"`
	Message   string `json:"message"`
	ClientRef string `json:"client_ref"`
	DLR       string `json:"dlr"`
	DLRURL    string `json:"dlr_url"`
}

type sendSMSResponse struct {
	Status           string  `json:"status"`
	MessageID        string  `json:"message_id"`
	ClientRef        string  `json:"client_ref,omitempty"`
	SenderID         string  `json:"sender_id"`
	SenderIDSource   string  `json:"sender_id_source"`
	Segments         int     `json:"segments"`
	Charged          string  `json:"charged"`
	BalanceRemaining string  `json:"balance_remaining"`
	Carrier          string  `json:"carrier"`
	FailoverSequence int     `json:"failover_sequence"`
	SourceAddrTON    *int16  `json:"source_addr_ton,omitempty"`
	SourceAddrNPI    *int16  `json:"source_addr_npi,omitempty"`
	DestAddrTON      *int16  `json:"dest_addr_ton,omitempty"`
	DestAddrNPI      *int16  `json:"dest_addr_npi,omitempty"`
	DLRRequested     bool    `json:"dlr_requested"`
	DLRWebhookURL    *string `json:"dlr_webhook_url"`
}

func NewHandlers(pool *pgxpool.Pool, cfg *config.Config) *Handlers {
	return &Handlers{Pool: pool, Config: cfg}
}

func (h *Handlers) SendSMS() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := APIClientFromContext(r.Context())
		if client == nil {
			writeJSONError(w, http.StatusUnauthorized, "SMS_ERR_UNAUTHORIZED", "missing authenticated client")
			return
		}
		var req sendSMSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "invalid json body")
			return
		}
		req.To = strings.TrimSpace(req.To)
		req.From = strings.TrimSpace(req.From)
		req.Message = strings.TrimSpace(req.Message)
		req.ClientRef = strings.TrimSpace(req.ClientRef)
		req.DLR = strings.TrimSpace(req.DLR)
		req.DLRURL = strings.TrimSpace(req.DLRURL)
		if !e164Re.MatchString(req.To) {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "to must be valid E.164 format")
			return
		}
		if len([]rune(req.Message)) < 1 || len([]rune(req.Message)) > 1600 {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "message must be between 1 and 1600 characters")
			return
		}
		dlrRequested, err := parseDLRRequested(req.DLR)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "dlr must be YES or NO")
			return
		}
		if req.DLRURL != "" && !isHTTPSURL(req.DLRURL) {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "dlr_url must be valid https:// URL")
			return
		}
		systemDefaultSenderID := h.systemSetting(r, "default_sender_id", "MiniSMS")
		sidResolution, err := carrier.ResolveSenderID(r.Context(), h.Pool, client, req.From, systemDefaultSenderID)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_SENDER_NOT_ALLOWED", "Sender ID not in client's allowed list")
			return
		}
		if req.From == "" {
			req.From = systemDefaultSenderID
		}
		timeoutS, _ := strconv.Atoi(h.systemSetting(r, "carrier_dispatch_timeout_s", strconv.Itoa(h.Config.CarrierDispatchTimeoutSecs)))
		if timeoutS < 1 {
			timeoutS = h.Config.CarrierDispatchTimeoutSecs
		}

		if client.RateGroupID == nil || *client.RateGroupID == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_NO_RATE", "client has no rate group")
			return
		}
		dlrWebhookURL := resolveDLRWebhookURL(dlrRequested, req.DLRURL, client.DLRWebhookURL)
		rateEntry, err := billing.LookupRate(r.Context(), h.Pool, *client.RateGroupID, req.To)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_NO_RATE", "no matching rate")
			return
		}
		encoding, segments := billing.SegmentInfo(req.Message)
		totalCharge, err := h.mulNumeric(r, rateEntry.RatePerSMS, segments)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_INTERNAL", "failed to compute total charge")
			return
		}

		routeEntry, err := h.lookupRouteEntry(r, client, req.To)
		if err != nil || routeEntry == nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_NO_ROUTE", "no matching route")
			return
		}

		tx, err := h.Pool.Begin(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "database unavailable")
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		balance, enough, err := h.lockAndCheckBalance(r, tx, client.ClientID, totalCharge)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to read balance")
			return
		}
		if !enough {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error":    "SMS_ERR_INSUFFICIENT_BALANCE",
				"balance":  balance,
				"required": totalCharge,
			})
			return
		}

		logID, err := db.CreateSMSLog(r.Context(), tx, db.SMSLog{
			ClientID:       client.ClientID,
			ClientRef:      optional(req.ClientRef),
			ToNumber:       req.To,
			FromNumber:     optional(req.From),
			MessageBody:    req.Message,
			MessageLength:  len([]rune(req.Message)),
			Segments:       segments,
			Encoding:       encoding,
			RateGroupID:    client.RateGroupID,
			PrefixMatched:  optional(rateEntry.Prefix),
			RateApplied:    rateEntry.RatePerSMS,
			TotalCharged:   totalCharge,
			Currency:       client.Currency,
			RoutingGroupID: client.RoutingGroupID,
			RouteEntryID:   optional(routeEntry.RouteEntryID),
			Status:         "pending",
			DLRRequested:   dlrRequested,
			DLRWebhookURL:  dlrWebhookURL,
		})
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to create sms log")
			return
		}

		win, dispatchErr := h.dispatchWithFailover(r, logID, client.ClientID, req, routeEntry, rateEntry.RatePerSMS, sidResolution, time.Duration(timeoutS)*time.Second)
		if len(win.SkipReasons) > 0 {
			if skipJSON, err := json.Marshal(win.SkipReasons); err == nil {
				_ = updateSMSLogCarrierSkipReason(r.Context(), tx, logID, skipJSON)
			}
		}
		if dispatchErr != nil {
			if len(win.SkipReasons) > 0 && win.LastCode == nil {
				_ = db.MarkSMSFailed(r.Context(), tx, logID, nil, "all carriers skipped by policy")
				if err := tx.Commit(r.Context()); err != nil {
					writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to finalize skip state")
					return
				}
				writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_NO_ELIGIBLE_CARRIER", "All carriers skipped due to Sender ID policy or in-loss protection")
				return
			}
			_ = db.MarkSMSFailed(r.Context(), tx, logID, win.LastCode, win.LastBody)
			if strings.EqualFold(h.systemSetting(r, "refund_on_carrier_failure", "true"), "true") {
				_, _ = billing.CreditClientBalance(r.Context(), tx, client.ClientID, totalCharge, client.Currency, "carrier_failure_refund", "Automatic refund on carrier failure")
			}
			if err := tx.Commit(r.Context()); err != nil {
				writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to finalize failure state")
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error":      "SMS_ERR_CARRIER_FAILURE",
				"message_id": logID,
			})
			return
		}

		carrierCostRate, err := billing.LookupCarrierCost(r.Context(), h.Pool, win.CarrierID, req.To, rateEntry.RatePerSMS)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to determine carrier cost")
			return
		}
		carrierCostTotal, err := h.mulNumeric(r, carrierCostRate, segments)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to compute carrier cost")
			return
		}

		remaining, err := billing.DeductClientBalance(r.Context(), tx, client.ClientID, totalCharge, logID, client.Currency)
		if err != nil {
			writeJSONError(w, http.StatusPaymentRequired, "SMS_ERR_INSUFFICIENT_BALANCE", "insufficient balance")
			return
		}
		if _, err := billing.DeductCarrierBalance(r.Context(), tx, win.CarrierID, carrierCostTotal, client.Currency, logID); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to deduct carrier balance")
			return
		}
		if err := billing.IncrementUsage(r.Context(), tx, win.CarrierID, segments, carrierCostTotal); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to increment carrier usage")
			return
		}
		var smppArr *[4]int16
		if win.SourceAddrTON != nil && win.SourceAddrNPI != nil && win.DestAddrTON != nil && win.DestAddrNPI != nil {
			tmp := [4]int16{*win.SourceAddrTON, *win.SourceAddrNPI, *win.DestAddrTON, *win.DestAddrNPI}
			smppArr = &tmp
		}
		if err := db.MarkSMSAccepted(r.Context(), tx, logID, win.CarrierID, win.FailoverSequence, win.CarrierMessageID, win.LastBodyText, win.StatusCode, smppArr); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to update sms log")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "transaction commit failed")
			return
		}

		resp := sendSMSResponse{
			Status:           "accepted",
			MessageID:        logID,
			ClientRef:        req.ClientRef,
			SenderID:         sidResolution.Value,
			SenderIDSource:   sidResolution.Source,
			Segments:         segments,
			Charged:          totalCharge,
			BalanceRemaining: remaining,
			Carrier:          win.CarrierName,
			FailoverSequence: win.FailoverSequence,
			SourceAddrTON:    win.SourceAddrTON,
			SourceAddrNPI:    win.SourceAddrNPI,
			DestAddrTON:      win.DestAddrTON,
			DestAddrNPI:      win.DestAddrNPI,
			DLRRequested:     dlrRequested,
			DLRWebhookURL:    dlrWebhookURL,
		}
		writeJSON(w, http.StatusAccepted, resp)
	}
}

func parseDLRRequested(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "YES":
		return true, nil
	case "NO":
		return false, nil
	default:
		return false, errors.New("invalid dlr value")
	}
}

func isHTTPSURL(v string) bool {
	u, err := url.Parse(v)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https") && strings.TrimSpace(u.Host) != ""
}

func resolveDLRWebhookURL(dlrRequested bool, reqDLRURL string, clientDLRURL *string) *string {
	if !dlrRequested {
		return nil
	}
	if strings.TrimSpace(reqDLRURL) != "" {
		return optional(reqDLRURL)
	}
	if clientDLRURL != nil && strings.TrimSpace(*clientDLRURL) != "" {
		return optional(strings.TrimSpace(*clientDLRURL))
	}
	return nil
}

func derefOr(s *string, def string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return def
	}
	return strings.TrimSpace(*s)
}

type routeEntry struct {
	RouteEntryID       string
	Prefix             string
	PrimaryCarrierID   string
	Failover1CarrierID *string
	Failover2CarrierID *string
}

type dispatchOutcome struct {
	CarrierID        string
	CarrierName      string
	FailoverSequence int
	StatusCode       int
	CarrierMessageID string
	LastCode         *int
	LastBody         string
	LastBodyText     string
	SkipReasons      []carrier.CarrierSkipReason
	SourceAddrTON    *int16
	SourceAddrNPI    *int16
	DestAddrTON      *int16
	DestAddrNPI      *int16
}

func (h *Handlers) lookupRouteEntry(r *http.Request, client *db.Client, to string) (*routeEntry, error) {
	if client.RoutingGroupID == nil || *client.RoutingGroupID == "" {
		return nil, pgx.ErrNoRows
	}
	rows, err := h.Pool.Query(r.Context(), `
		SELECT route_entry_id::text, prefix, primary_carrier_id::text, failover1_carrier_id::text, failover2_carrier_id::text
		FROM route_entries
		WHERE routing_group_id = $1::uuid AND status='active'`, *client.RoutingGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []routeEntry
	for rows.Next() {
		var e routeEntry
		if err := rows.Scan(&e.RouteEntryID, &e.Prefix, &e.PrimaryCarrierID, &e.Failover1CarrierID, &e.Failover2CarrierID); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	best := longestPrefixRoute(entries, to)
	if best == nil {
		return nil, pgx.ErrNoRows
	}
	return best, nil
}

func longestPrefixRoute(entries []routeEntry, destination string) *routeEntry {
	dst := billingSegmentNormalize(destination)
	var catchAll *routeEntry
	var best *routeEntry
	bestLen := -1
	for i := range entries {
		e := &entries[i]
		if e.Prefix == "*" {
			if catchAll == nil {
				catchAll = e
			}
			continue
		}
		if strings.HasPrefix(dst, e.Prefix) && len(e.Prefix) > bestLen {
			best = e
			bestLen = len(e.Prefix)
		}
	}
	if best != nil {
		return best
	}
	return catchAll
}

func billingSegmentNormalize(destination string) string {
	d := strings.TrimPrefix(strings.TrimSpace(destination), "+")
	var b strings.Builder
	for _, r := range d {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func optional(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := s
	return &v
}

func (h *Handlers) systemSetting(r *http.Request, key, def string) string {
	var v string
	err := h.Pool.QueryRow(r.Context(), `SELECT value FROM system_settings WHERE key = $1`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func (h *Handlers) mulNumeric(r *http.Request, perSMS string, segments int) (string, error) {
	var total string
	err := h.Pool.QueryRow(r.Context(), `SELECT ($1::numeric(18,6) * $2::int)::numeric(18,6)::text`, perSMS, segments).Scan(&total)
	return total, err
}

func (h *Handlers) lockAndCheckBalance(r *http.Request, tx pgx.Tx, clientID, required string) (string, bool, error) {
	var bal string
	var enough bool
	err := tx.QueryRow(r.Context(), `
		SELECT balance::text, (balance >= $2::numeric(18,6)) AS enough
		FROM clients
		WHERE client_id = $1::uuid
		FOR UPDATE`, clientID, required).Scan(&bal, &enough)
	return bal, enough, err
}

func updateSMSLogCarrierSkipReason(ctx context.Context, tx pgx.Tx, messageID string, skipJSON []byte) error {
	_, err := tx.Exec(ctx, `UPDATE sms_logs SET carrier_skip_reason = $2::jsonb WHERE message_id = $1::uuid`, messageID, string(skipJSON))
	return err
}

func (h *Handlers) dispatchWithFailover(r *http.Request, messageID, clientID string, req sendSMSRequest, route *routeEntry, clientRate string, sidResolution carrier.SenderIDResolution, timeout time.Duration) (*dispatchOutcome, error) {
	carriers := []struct {
		id string
		n  int
	}{
		{id: route.PrimaryCarrierID, n: 0},
	}
	if route.Failover1CarrierID != nil && *route.Failover1CarrierID != "" {
		carriers = append(carriers, struct {
			id string
			n  int
		}{id: *route.Failover1CarrierID, n: 1})
	}
	if route.Failover2CarrierID != nil && *route.Failover2CarrierID != "" {
		carriers = append(carriers, struct {
			id string
			n  int
		}{id: *route.Failover2CarrierID, n: 2})
	}
	out := &dispatchOutcome{}
	for _, c := range carriers {
		var carrierName, endpointURL, method, senderIDPolicy string
		var defaultSenderIDValue, carrierRateGroupID *string
		var dlrCallbackURLTemplate, smppSourceTON, smppSourceNPI, smppDestTON, smppDestNPI *string
		err := h.Pool.QueryRow(r.Context(), `
			SELECT name, endpoint_url, http_method, sender_id_policy, default_sender_id_value, rate_group_id::text,
				dlr_callback_url_template, smpp_source_addr_ton, smpp_source_addr_npi, smpp_dest_addr_ton, smpp_dest_addr_npi
			FROM carriers
			WHERE carrier_id = $1::uuid AND status = 'active'`, c.id).Scan(
			&carrierName, &endpointURL, &method, &senderIDPolicy, &defaultSenderIDValue, &carrierRateGroupID,
			&dlrCallbackURLTemplate, &smppSourceTON, &smppSourceNPI, &smppDestTON, &smppDestNPI,
		)
		if err != nil {
			last := 503
			out.LastCode = &last
			out.LastBody = "carrier not found"
			continue
		}
		effectiveSenderID, eligible, skipReason, err := carrier.CheckCarrierEligibility(
			r.Context(), h.Pool, c.id, carrierName, senderIDPolicy, defaultSenderIDValue, carrierRateGroupID, sidResolution, clientRate, clientID, billingSegmentNormalize(req.To),
		)
		if err != nil {
			last := 503
			out.LastCode = &last
			out.LastBody = err.Error()
			continue
		}
		if !eligible {
			out.SkipReasons = append(out.SkipReasons, carrier.CarrierSkipReason{
				CarrierID: c.id, CarrierName: carrierName, Reason: skipReason,
			})
			continue
		}
		tpl, err := db.GetRequestTemplate(r.Context(), h.Pool, c.id)
		if err != nil || tpl == nil {
			last := 503
			out.LastCode = &last
			out.LastBody = "carrier template missing"
			continue
		}
		hdrRows, err := db.ListAuthHeaders(r.Context(), h.Pool, c.id, h.Config.SecretKey)
		if err != nil {
			last := 503
			out.LastCode = &last
			out.LastBody = "carrier auth headers unavailable"
			continue
		}
		hdrs := make(map[string]string, len(hdrRows))
		for _, h := range hdrRows {
			hdrs[h.HeaderName] = h.Value
		}
		smpp := carrier.ResolveTONNPI(carrier.SMPPConfig{
			SourceAddrTON: derefOr(smppSourceTON, "dynamic"),
			SourceAddrNPI: derefOr(smppSourceNPI, "dynamic"),
			DestAddrTON:   derefOr(smppDestTON, "dynamic"),
			DestAddrNPI:   derefOr(smppDestNPI, "dynamic"),
		}, effectiveSenderID, req.To)
		dlrCallbackURL := ""
		if dlrCallbackURLTemplate != nil && strings.TrimSpace(*dlrCallbackURLTemplate) != "" {
			dlrCallbackURL = strings.ReplaceAll(strings.TrimSpace(*dlrCallbackURLTemplate), "{{message_id}}", messageID)
		}
		vars := map[string]string{
			"to":               req.To,
			"from":             effectiveSenderID,
			"message":          req.Message,
			"message_id":       messageID,
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
			"client_id":        clientID,
			"dlr_callback_url": dlrCallbackURL,
			"source_addr_ton":  strconv.Itoa(int(smpp.SourceAddrTON)),
			"source_addr_npi":  strconv.Itoa(int(smpp.SourceAddrNPI)),
			"dest_addr_ton":    strconv.Itoa(int(smpp.DestAddrTON)),
			"dest_addr_npi":    strconv.Itoa(int(smpp.DestAddrNPI)),
		}
		resp, err := carrier.DispatchToCarrier(carrier.DispatchRequest{
			Method:      method,
			EndpointURL: endpointURL,
			ContentType: tpl.ContentType,
			Body:        carrier.InjectVariables(tpl.BodyTemplate, vars),
			Query:       carrier.InjectVariables(tpl.QueryTemplate, vars),
			Headers:     hdrs,
			Timeout:     timeout,
		})
		if err != nil {
			last := 503
			out.LastCode = &last
			out.LastBody = err.Error()
			continue
		}
		out.LastCode = &resp.StatusCode
		out.LastBody = resp.Body
		out.LastBodyText = resp.Body
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			out.CarrierID = c.id
			out.CarrierName = carrierName
			out.FailoverSequence = c.n
			out.StatusCode = resp.StatusCode
			out.CarrierMessageID = ""
			out.SourceAddrTON = &smpp.SourceAddrTON
			out.SourceAddrNPI = &smpp.SourceAddrNPI
			out.DestAddrTON = &smpp.DestAddrTON
			out.DestAddrNPI = &smpp.DestAddrNPI
			return out, nil
		}
	}
	return out, errors.New("all carriers failed")
}
