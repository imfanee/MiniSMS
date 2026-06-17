// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/smslog"
)

// forwardEndpointValidator guards client DLR webhook URLs against SSRF (loopback, RFC1918,
// link-local, metadata). It is a package var so tests can substitute httptest targets.
var forwardEndpointValidator = carrier.ValidateEndpointURL

// ClientDeliverer sends DLR to a bound client ESME session (RX/TRX).
type ClientDeliverer interface {
	DeliverDLR(clientID, messageID, dlrStatus string) bool
}

// Processor applies inbound DLR updates and forwards to client webhooks (single attempt).
type Processor struct {
	Pool      *pgxpool.Pool
	SecretKey []byte
	SMPP      ClientDeliverer
}

type forwardPayload struct {
	Event            string  `json:"event"`
	MessageID        string  `json:"message_id"`
	ClientRef        *string `json:"client_ref"`
	To               string  `json:"to"`
	From             *string `json:"from"`
	DLRStatus        string  `json:"dlr_status"`
	Carrier          *string `json:"carrier"`
	FailoverSequence int     `json:"failover_sequence"`
	ReceivedAt       string  `json:"received_at"`
	DLRReceivedAt    string  `json:"dlr_received_at"`
	Segments         int     `json:"segments"`
	Charged          string  `json:"charged"`
	SourceAddrTON    *int16  `json:"source_addr_ton"`
	SourceAddrNPI    *int16  `json:"source_addr_npi"`
	DestAddrTON      *int16  `json:"dest_addr_ton"`
	DestAddrNPI      *int16  `json:"dest_addr_npi"`
}

// HandleInbound updates sms_logs and forwards to the client when configured.
// messageID must be a MiniSMS UUID string.
func (p *Processor) HandleInbound(ctx context.Context, messageID string, payloadFields map[string]string, inbound *InboundCallback) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	if _, err := uuid.Parse(messageID); err != nil {
		return nil
	}
	row, err := db.GetSMSLogForDLR(ctx, p.Pool, messageID)
	if err != nil {
		return nil
	}
	hadPriorReceipt := row.DLRReceivedAt != nil
	dlrStatus := NormalizeFromFields(payloadFields, row.CarrierDLRStatusField, row.CarrierDLRStatusMap)
	// A multi-bit dlr-mask (e.g. Kamex 31) can deliver an intermediate receipt (SMSC ACK, queued)
	// before the final DELIVRD/UNDELIV. UpdateDLRReceived applies only while no final status is
	// stored, so an intermediate never blocks a later final and a final is never overwritten; when
	// it reports no change, this receipt is a duplicate/superseded one and is ignored.
	applied, err := db.UpdateDLRReceived(ctx, p.Pool, messageID, dlrStatus)
	if err != nil || !applied {
		return nil
	}
	rawMeta := inbound.TimelineMeta(dlrStatus)
	_ = db.AppendSMSEventTimeline(ctx, p.Pool, messageID, smslog.NewEvent(
		smslog.EventDLRReceived,
		"DLR received from carrier",
		fmt.Sprintf("Mapped status: %s", dlrStatus),
		rawMeta,
	))
	if !row.DLRRequested {
		return nil
	}
	// Forward to the client on the first receipt, and again only once a final state is reached, so
	// a trailing intermediate receipt does not trigger a duplicate forward.
	if !shouldForwardDLR(hadPriorReceipt, dlrStatus) {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(row.DLRDeliveryMode))
	if mode == "" {
		mode = "http"
	}
	if (mode == "smpp" || mode == "both") && p.SMPP != nil {
		if p.SMPP.DeliverDLR(row.ClientID, messageID, dlrStatus) {
			_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "smpp_ok", true, true)
			p.recordDLRForward(ctx, messageID, "smpp_ok", "DLR delivered to client SMPP session", map[string]any{
				"channel": "smpp", "dlr_status": dlrStatus,
			})
			return nil
		}
		if mode == "smpp" {
			_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "smpp_no_bind", false, false)
			p.recordDLRForward(ctx, messageID, "smpp_no_bind", "No active client SMPP bind for DLR delivery", map[string]any{
				"channel": "smpp", "dlr_status": dlrStatus,
			})
			return nil
		}
	}
	if row.DLRWebhookURL == nil || strings.TrimSpace(*row.DLRWebhookURL) == "" {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "no_url", false, false)
		p.recordDLRForward(ctx, messageID, "no_url", "Client has no DLR webhook URL configured", map[string]any{
			"channel": "http", "dlr_status": dlrStatus,
		})
		return nil
	}
	now := time.Now().UTC()
	fwd, err := BuildClientForward(row, dlrStatus, now)
	if err != nil {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "failed", false, true)
		p.recordDLRForward(ctx, messageID, "failed", "Failed to build DLR webhook payload", map[string]any{
			"channel": "http", "error": err.Error(),
		})
		return nil
	}
	if err := forwardEndpointValidator(fwd.URL); err != nil {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "blocked", false, false)
		p.recordDLRForward(ctx, messageID, "blocked", "DLR webhook URL blocked by SSRF guard", map[string]any{
			"channel": "http", "webhook_url": fwd.URL, "error": err.Error(),
		})
		slog.Warn("dlr forward blocked by ssrf guard", "message_id", messageID, "webhook_url", fwd.URL, "error", err.Error())
		return nil
	}
	var bodyReader io.Reader
	if len(fwd.Body) > 0 {
		bodyReader = bytes.NewReader(fwd.Body)
	}
	req, err := http.NewRequestWithContext(ctx, fwd.Method, fwd.URL, bodyReader)
	if err != nil {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "failed", false, true)
		p.recordDLRForward(ctx, messageID, "failed", "Failed to create DLR webhook request", map[string]any{
			"channel": "http", "webhook_url": fwd.URL, "error": err.Error(),
		})
		return nil
	}
	if fwd.ContentType != "" {
		req.Header.Set("Content-Type", fwd.ContentType)
	}
	req.Header.Set("User-Agent", "MiniSMS-DLR/1.0")
	if sig := signForward(fwd.SignPayload, p.SecretKey, row.ClientWebhookSecret); sig != "" {
		req.Header.Set("X-MiniSMS-Signature", sig)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "failed", false, true)
		p.recordDLRForward(ctx, messageID, "failed", "HTTP request to client webhook failed", map[string]any{
			"channel": "http", "webhook_url": fwd.URL, "http_method": fwd.Method, "error": err.Error(),
		})
		slog.Warn("dlr forward failed", "message_id", messageID, "webhook_url", fwd.URL, "method", fwd.Method, "error", err.Error())
		return nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "success", true, true)
		p.recordDLRForward(ctx, messageID, "success", "DLR forwarded to client webhook", map[string]any{
			"channel": "http", "webhook_url": fwd.URL, "http_method": fwd.Method, "http_status": resp.StatusCode,
		})
	} else {
		_ = db.UpdateDLRForwardStatus(ctx, p.Pool, messageID, "failed", false, true)
		p.recordDLRForward(ctx, messageID, "failed", fmt.Sprintf("Client webhook returned HTTP %d", resp.StatusCode), map[string]any{
			"channel": "http", "webhook_url": fwd.URL, "http_method": fwd.Method, "http_status": resp.StatusCode,
		})
		slog.Warn("dlr forward failed", "message_id", messageID, "webhook_url", fwd.URL, "method", fwd.Method, "http_status", resp.StatusCode)
	}
	return nil
}

// shouldForwardDLR decides whether an applied receipt should be pushed to the client. The first
// receipt always forwards; later receipts forward only once they reach a final state, so a trailing
// intermediate receipt (from a multi-bit dlr-mask) does not cause a duplicate client callback.
func shouldForwardDLR(hadPriorReceipt bool, status string) bool {
	return !hadPriorReceipt || IsFinalStatus(status)
}

func (p *Processor) recordDLRForward(ctx context.Context, messageID, status, detail string, meta map[string]any) {
	title := "DLR forward to client"
	switch status {
	case "success", "smpp_ok":
		title = "DLR forwarded to client"
	case "no_url":
		title = "DLR not forwarded — no client webhook"
	case "failed", "smpp_no_bind", "blocked":
		title = "DLR forward to client failed"
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["forward_status"] = status
	_ = db.AppendSMSEventTimeline(ctx, p.Pool, messageID, smslog.NewEvent(
		smslog.EventDLRForward, title, detail, meta,
	))
}

// HandleCarrierSMPP processes a delivery receipt received on a carrier TRX/RX session.
func (p *Processor) HandleCarrierSMPP(ctx context.Context, carrierID, receiptRef, receiptStat string) {
	receiptRef = strings.TrimSpace(receiptRef)
	if receiptRef == "" {
		return
	}
	messageID, err := db.ResolveSMSLogMessageID(ctx, p.Pool, carrierID, receiptRef)
	if err != nil || messageID == "" {
		return
	}
	fields := map[string]string{"status": StatusFromSMPPReceipt(receiptStat)}
	_ = p.HandleInbound(ctx, messageID, fields, InboundFromSMPP(receiptRef, receiptStat))
}

func signForward(body []byte, secretKey []byte, secretEnc *string) string {
	if secretEnc == nil || strings.TrimSpace(*secretEnc) == "" {
		return ""
	}
	secret, err := db.DecryptValue(secretKey, *secretEnc)
	if err != nil || strings.TrimSpace(secret) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyInboundSecret checks HTTP DLR callback authentication.
func VerifyInboundSecret(secretKey []byte, r *http.Request, row *db.DLRMessage) bool {
	if row.CarrierInboundSecret == nil || strings.TrimSpace(*row.CarrierInboundSecret) == "" {
		return true
	}
	expected, err := db.DecryptValue(secretKey, *row.CarrierInboundSecret)
	if err != nil {
		return false
	}
	candidate := strings.TrimSpace(r.URL.Query().Get("secret"))
	if candidate == "" {
		candidate = strings.TrimSpace(r.Header.Get("X-DLR-Secret"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.Header.Get("X-Callback-Secret"))
	}
	if candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}
