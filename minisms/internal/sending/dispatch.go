// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/billing"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/routecache"
	"github.com/minisms/minisms/internal/smpp/egress"
	"github.com/minisms/minisms/internal/smslog"
)

func (s *Service) dispatchWithFailover(
	ctx context.Context,
	messageID, clientID string,
	msg AcceptedMessage,
	route *RouteEntry,
	clientRate string,
	sidResolution carrier.SenderIDResolution,
	timeout time.Duration,
) (*dispatchOutcome, error) {
	failoverOn := strings.EqualFold(db.Setting(ctx, s.Pool, "failover_enabled", "true"), "true")
	carriers := buildFailoverCarriers(route, failoverOn)
	out := &dispatchOutcome{}
	encoding, segments := billing.SegmentInfo(msg.Body)

	for _, c := range carriers {
		prof, ok := s.carrierProfile(ctx, c.id)
		if !ok || prof.Status != "active" {
			last := 503
			out.LastCode = &last
			out.LastBody = "carrier not found"
			out.Timeline = append(out.Timeline, smslog.NewEvent(
				smslog.EventCarrierSkipped,
				fmt.Sprintf("%s skipped — carrier inactive or missing", c.id),
				out.LastBody,
				map[string]any{"failover_sequence": c.n, "failover_label": smslog.FailoverLabel(c.n)},
			))
			continue
		}
		carrierName := prof.Name
		endpointURL := prof.EndpointURL
		method := prof.HTTPMethod
		senderIDPolicy := prof.SenderIDPolicy
		egressTransport := prof.EgressTransport
		defaultSenderIDValue := prof.DefaultSenderIDValue
		carrierRateGroupID := prof.RateGroupID
		dlrCallbackURLTemplate := prof.DLRCallbackURLTemplate
		effectiveSenderID, eligible, skipReason, err := carrier.CheckCarrierEligibility(
			ctx, s.Pool, c.id, carrierName, senderIDPolicy, defaultSenderIDValue, carrierRateGroupID, sidResolution, clientRate, clientID, billingSegmentNormalize(msg.To),
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
			out.Timeline = append(out.Timeline, smslog.NewEvent(
				smslog.EventCarrierSkipped,
				fmt.Sprintf("%s skipped — %s", carrierName, skipReason),
				"",
				map[string]any{
					"carrier_id": c.id, "carrier_name": carrierName,
					"failover_sequence": c.n, "failover_label": smslog.FailoverLabel(c.n),
					"reason": skipReason,
				},
			))
			continue
		}
		smppResolved := carrier.ResolveTONNPI(carrier.SMPPConfig{
			SourceAddrTON: prof.SMPPSourceAddrTON,
			SourceAddrNPI: prof.SMPPSourceAddrNPI,
			DestAddrTON:   prof.SMPPDestAddrTON,
			DestAddrNPI:   prof.SMPPDestAddrNPI,
		}, effectiveSenderID, msg.To)

		attempts := egressAttempts(egressTransport)
		for _, mode := range attempts {
			transport := string(mode)
			out.Timeline = append(out.Timeline, smslog.NewEvent(
				smslog.EventCarrierDispatch,
				fmt.Sprintf("Sending to %s (%s)", carrierName, smslog.FailoverLabel(c.n)),
				fmt.Sprintf("Transport: %s", strings.ToUpper(transport)),
				map[string]any{
					"carrier_id": c.id, "carrier_name": carrierName,
					"failover_sequence": c.n, "failover_label": smslog.FailoverLabel(c.n),
					"transport": transport,
				},
			))
			var ok bool
			switch mode {
			case EgressSMPP:
				ok, err = s.trySMPPDispatch(ctx, out, c, carrierName, messageID, effectiveSenderID, msg, encoding, segments, timeout, smppResolved)
			case EgressHTTP:
				ok, err = s.tryHTTPDispatch(ctx, out, c, carrierName, messageID, clientID, effectiveSenderID, msg, method, endpointURL, dlrCallbackURLTemplate, timeout, smppResolved)
			}
			recordCarrierResponseTimeline(out, carrierName, c.n, ok)
			if ok {
				out.CarrierID = c.id
				out.CarrierName = carrierName
				out.FailoverSequence = c.n
				out.SourceAddrTON = &smppResolved.SourceAddrTON
				out.SourceAddrNPI = &smppResolved.SourceAddrNPI
				out.DestAddrTON = &smppResolved.DestAddrTON
				out.DestAddrNPI = &smppResolved.DestAddrNPI
				out.Timeline = append(out.Timeline, smslog.NewEvent(
					smslog.EventDispatchAccepted,
					fmt.Sprintf("Accepted by %s (%s)", carrierName, smslog.FailoverLabel(c.n)),
					"",
					map[string]any{
						"carrier_id": c.id, "carrier_name": carrierName,
						"failover_sequence": c.n,
						"carrier_message_id": out.CarrierMessageID,
					},
				))
				return out, nil
			}
			if err != nil {
				last := 503
				out.LastCode = &last
				out.LastBody = err.Error()
			}
		}
	}
	return out, errors.New("all carriers failed")
}

func egressAttempts(egressTransport string) []EgressTransport {
	if strings.ToLower(strings.TrimSpace(egressTransport)) == "smpp" {
		return []EgressTransport{EgressSMPP}
	}
	return []EgressTransport{EgressHTTP}
}

func (s *Service) carrierProfile(ctx context.Context, carrierID string) (routecache.CarrierProfile, bool) {
	if s.Routes != nil {
		return s.Routes.Carrier(carrierID)
	}
	var p routecache.CarrierProfile
	err := s.Pool.QueryRow(ctx, `
		SELECT carrier_id::text, name, status, egress_transport, endpoint_url, http_method,
			sender_id_policy, default_sender_id_value, rate_group_id::text,
			dlr_callback_url_template,
			smpp_source_addr_ton, smpp_source_addr_npi, smpp_dest_addr_ton, smpp_dest_addr_npi
		FROM carriers WHERE carrier_id = $1::uuid`, carrierID).Scan(
		&p.CarrierID, &p.Name, &p.Status, &p.EgressTransport, &p.EndpointURL, &p.HTTPMethod,
		&p.SenderIDPolicy, &p.DefaultSenderIDValue, &p.RateGroupID,
		&p.DLRCallbackURLTemplate,
		&p.SMPPSourceAddrTON, &p.SMPPSourceAddrNPI, &p.SMPPDestAddrTON, &p.SMPPDestAddrNPI,
	)
	if err != nil {
		return routecache.CarrierProfile{}, false
	}
	p.EgressTransport = strings.ToLower(strings.TrimSpace(p.EgressTransport))
	if p.EgressTransport != "smpp" {
		p.EgressTransport = "http"
	}
	return p, true
}

func (s *Service) trySMPPDispatch(
	ctx context.Context,
	out *dispatchOutcome,
	c failoverCarrier,
	carrierName, messageID, from string,
	msg AcceptedMessage,
	encoding string,
	segments int,
	timeout time.Duration,
	smpp carrier.SMPPParams,
) (bool, error) {
	if s.Egress == nil {
		out.LastBody = "smpp egress not configured"
		return false, nil
	}
	res, err := s.Egress.Submit(ctx, c.id, egress.SubmitRequest{
		Src:       from,
		Dst:       msg.To,
		Body:      msg.Body,
		SourceTON: uint8(smpp.SourceAddrTON),
		SourceNPI: uint8(smpp.SourceAddrNPI),
		DestTON:   uint8(smpp.DestAddrTON),
		DestNPI:   uint8(smpp.DestAddrNPI),
		Encoding:  encoding,
		Segments:  segments,
		Timeout:   timeout,
	})
	if err != nil {
		if res != nil && res.CommandStatus != 0 {
			code := smppHTTPStatus(res.CommandStatus)
			out.LastCode = &code
			out.LastBody = res.ResponseBody
			out.LastBodyText = res.ResponseBody
			out.StatusCode = code
			out.CarrierMessageID = res.CarrierMessageID
			out.Egress = EgressSMPP
			return false, nil
		}
		out.LastBody = err.Error()
		return false, err
	}
	if res == nil || res.CommandStatus != 0 {
		if res != nil {
			code := smppHTTPStatus(res.CommandStatus)
			out.LastCode = &code
			out.LastBody = res.ResponseBody
			out.LastBodyText = res.ResponseBody
			out.StatusCode = code
			out.CarrierMessageID = res.CarrierMessageID
		}
		return false, nil
	}
	code := 200
	out.LastCode = &code
	out.LastBody = res.ResponseBody
	out.LastBodyText = res.ResponseBody
	out.StatusCode = code
	out.CarrierMessageID = res.CarrierMessageID
	out.Egress = EgressSMPP
	return true, nil
}

func (s *Service) tryHTTPDispatch(
	ctx context.Context,
	out *dispatchOutcome,
	c failoverCarrier,
	carrierName, messageID, clientID, from string,
	msg AcceptedMessage,
	method, endpointURL string,
	dlrCallbackURLTemplate *string,
	timeout time.Duration,
	smpp carrier.SMPPParams,
) (bool, error) {
	tpl, err := db.GetRequestTemplate(ctx, s.Pool, c.id)
	if strings.TrimSpace(endpointURL) == "" {
		out.LastBody = "carrier HTTP endpoint not configured"
		return false, nil
	}
	if err != nil || tpl == nil {
		out.LastBody = "carrier template missing"
		return false, nil
	}
	hdrRows, err := db.ListAuthHeaders(ctx, s.Pool, c.id, s.Config.SecretKey)
	if err != nil {
		out.LastBody = "carrier auth headers unavailable"
		return false, nil
	}
	hdrs := make(map[string]string, len(hdrRows))
	for _, h := range hdrRows {
		hdrs[h.HeaderName] = h.Value
	}
	dlrCallbackURL := ""
	if dlrCallbackURLTemplate != nil && strings.TrimSpace(*dlrCallbackURLTemplate) != "" {
		dlrCallbackURL = strings.ReplaceAll(strings.TrimSpace(*dlrCallbackURLTemplate), "{{message_id}}", messageID)
	}
	dlrCallbackURLEncoded := ""
	if dlrCallbackURL != "" {
		dlrCallbackURLEncoded = url.QueryEscape(dlrCallbackURL)
	}
	vars := map[string]string{
		"to":                         msg.To,
		"from":                       from,
		"message":                    msg.Body,
		"message_id":                 messageID,
		"timestamp":                  time.Now().UTC().Format(time.RFC3339),
		"client_id":                  clientID,
		"dlr_callback_url":           dlrCallbackURL,
		"dlr_callback_url_encoded":   dlrCallbackURLEncoded,
		"source_addr_ton":  strconv.Itoa(int(smpp.SourceAddrTON)),
		"source_addr_npi":  strconv.Itoa(int(smpp.SourceAddrNPI)),
		"dest_addr_ton":    strconv.Itoa(int(smpp.DestAddrTON)),
		"dest_addr_npi":    strconv.Itoa(int(smpp.DestAddrNPI)),
	}
	resp, err := HTTPTransport{}.Dispatch(ctx, CarrierDispatchInput{
		Method:      method,
		EndpointURL: endpointURL,
		ContentType: tpl.ContentType,
		Body:        carrier.InjectVariables(tpl.BodyTemplate, vars),
		Query:       carrier.InjectQueryVariables(tpl.QueryTemplate, vars),
		Headers:     hdrs,
		Timeout:     timeout,
	})
	if err != nil {
		return false, err
	}
	out.LastCode = &resp.StatusCode
	out.LastBody = resp.Body
	out.LastBodyText = resp.Body
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		out.StatusCode = resp.StatusCode
		out.CarrierMessageID = resp.MessageID
		out.Egress = EgressHTTP
		return true, nil
	}
	return false, nil
}

func recordCarrierResponseTimeline(out *dispatchOutcome, carrierName string, failoverSeq int, accepted bool) {
	meta := map[string]any{
		"carrier_name": carrierName,
		"failover_sequence": failoverSeq,
		"failover_label": smslog.FailoverLabel(failoverSeq),
		"accepted": accepted,
	}
	if out.LastCode != nil {
		meta["http_status"] = *out.LastCode
	}
	if out.CarrierMessageID != "" {
		meta["carrier_message_id"] = out.CarrierMessageID
	}
	if out.Egress != "" {
		meta["transport"] = string(out.Egress)
	}
	detail := out.LastBodyText
	if detail == "" {
		detail = out.LastBody
	}
	out.Timeline = append(out.Timeline, smslog.NewEvent(
		smslog.EventCarrierResponse,
		fmt.Sprintf("Response from %s (%s)", carrierName, smslog.FailoverLabel(failoverSeq)),
		detail,
		meta,
	))
}

func smppHTTPStatus(commandStatus int) int {
	if commandStatus == 0 {
		return 200
	}
	return 502
}

// Service holds dependencies for the shared send pipeline.
type Service struct {
	Pool      *pgxpool.Pool
	Config    *config.Config
	Egress    *egress.Manager
	Routes    *routecache.Cache
	Transport CarrierTransport // legacy default HTTP when Egress nil; unused when Egress set
}

func New(pool *pgxpool.Pool, cfg *config.Config) *Service {
	return &Service{
		Pool:      pool,
		Config:    cfg,
		Transport: HTTPTransport{},
	}
}

func NewWithEgress(pool *pgxpool.Pool, cfg *config.Config, eg *egress.Manager, routes *routecache.Cache) *Service {
	return &Service{
		Pool:      pool,
		Config:    cfg,
		Egress:    eg,
		Routes:    routes,
		Transport: HTTPTransport{},
	}
}
