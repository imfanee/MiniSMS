// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/sending"
	"golang.org/x/time/rate"
)

const unboundReadDeadline = 30 * time.Second

// Server is the client-facing SMPP SMSC (ESME binds inbound).
type Server struct {
	Pool       *pgxpool.Pool
	Config     *config.Config
	Send       *sending.Service
	ListenAddr string
	SystemID   string
	TLSEnabled bool
	TLSCert    string
	TLSKey     string

	sessions *sessionRegistry

	mu     sync.Mutex
	ln     net.Listener
	cancel context.CancelFunc
}

func New(pool *pgxpool.Pool, cfg *config.Config, send *sending.Service) *Server {
	sid := strings.TrimSpace(cfg.SMPPSystemID)
	if sid == "" {
		sid = "MiniSMS"
	}
	return &Server{
		Pool:       pool,
		Config:     cfg,
		Send:       send,
		ListenAddr: cfg.SMPPListenAddr,
		SystemID:   sid,
		TLSEnabled: cfg.SMPPTLSEnabled,
		TLSCert:    cfg.SMPPTLSCertFile,
		TLSKey:     cfg.SMPPTLSKeyFile,
		sessions:   newSessionRegistry(),
	}
}

// Start listens for ESME connections until Stop is called.
func (s *Server) Start(parent context.Context) error {
	addr := strings.TrimSpace(s.ListenAddr)
	if addr == "" {
		addr = ":2775"
	}
	var ln net.Listener
	var err error
	if s.TLSEnabled {
		cert, certErr := tls.LoadX509KeyPair(s.TLSCert, s.TLSKey)
		if certErr != nil {
			return fmt.Errorf("smpp server tls: %w", certErr)
		}
		ln, err = tls.Listen("tcp", addr, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.ln = ln
	s.cancel = cancel
	s.mu.Unlock()
	slog.Info("smpp server listening", "addr", ln.Addr().String(), "tls", s.TLSEnabled)
	go s.acceptLoop(ctx, ln)
	return nil
}

func (s *Server) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		raw, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Warn("smpp accept", "error", err)
				continue
			}
		}
		go s.handleConn(ctx, newConn(raw))
	}
}

// Stop closes the listener, active sessions, and waits for connection handlers to exit.
func (s *Server) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	ln := s.ln
	s.cancel = nil
	s.ln = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.sessions.closeAll()
	if ln != nil {
		_ = ln.Close()
	}
	s.sessions.wait(25 * time.Second)
}

func (s *Server) handleConn(ctx context.Context, c *conn) {
	defer c.Close()
	s.sessions.trackConn()
	defer s.sessions.untrackConn()
	_ = c.rwc.SetReadDeadline(time.Now().Add(unboundReadDeadline))
	var sess *session
	for {
		p, err := c.Read()
		if err != nil {
			return
		}
		if p == nil {
			continue
		}
		if sess == nil && !isBindPDU(p.Header().ID) {
			return
		}
		switch p.Header().ID {
		case pdu.BindTransmitterID, pdu.BindReceiverID, pdu.BindTransceiverID:
			if sess != nil {
				s.writeBindResp(c, p, StatusRBindFail)
				continue
			}
			sess = s.handleBind(ctx, c, p)
			if sess != nil {
				_ = c.rwc.SetReadDeadline(time.Time{})
			}
		case pdu.SubmitSMID:
			if sess == nil || !sess.mode.canSubmit() {
				s.writeResp(c, pdu.NewSubmitSMResp(), p.Header().Seq, StatusRBindFail)
				continue
			}
			s.handleSubmit(ctx, c, sess, p)
		case pdu.EnquireLinkID:
			resp := pdu.NewEnquireLinkResp()
			resp.Header().Seq = p.Header().Seq
			_ = c.Write(resp)
		case pdu.UnbindID:
			resp := pdu.NewUnbindResp()
			resp.Header().Seq = p.Header().Seq
			_ = c.Write(resp)
			if sess != nil {
				s.unregisterSession(ctx, sess)
				sess = nil
			}
			return
		default:
			if sess == nil {
				return
			}
			nack := pdu.NewGenericNACK()
			nack.Header().Seq = p.Header().Seq
			_ = c.Write(nack)
		}
	}
}

func isBindPDU(id pdu.ID) bool {
	switch id {
	case pdu.BindTransmitterID, pdu.BindReceiverID, pdu.BindTransceiverID:
		return true
	default:
		return false
	}
}

func (s *Server) handleBind(ctx context.Context, c *conn, p pdu.Body) *session {
	mode := bindTRX
	switch p.Header().ID {
	case pdu.BindTransmitterID:
		mode = bindTX
	case pdu.BindReceiverID:
		mode = bindRX
	}
	f := p.Fields()
	systemID := fieldString(f, pdufield.SystemID)
	password := fieldString(f, pdufield.Password)
	if systemID == "" || password == "" {
		s.writeBindResp(c, p, StatusRInvSysID)
		return nil
	}
	host := ""
	if h := remoteHost(c.remote); h != nil {
		host = *h
	}
	throttleKey := bindThrottleKey(host, systemID)
	now := time.Now()
	if isBindBlocked(throttleKey, now) {
		s.writeBindResp(c, p, StatusRBindFail)
		return nil
	}
	profile, err := db.LookupClientSMPPIngress(ctx, s.Pool, systemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			markBindFailure(throttleKey, now)
		}
		s.writeBindResp(c, p, StatusRBindFail)
		return nil
	}
	if profile.Status != "active" {
		markBindFailure(throttleKey, now)
		s.writeBindResp(c, p, StatusRBindFail)
		_ = db.InsertSMPPBindEvent(ctx, s.Pool, profile.ClientID, "bind_fail", mode.String(), remoteHost(c.remote), intPtr(int(StatusRBindFail)), "client not active")
		return nil
	}
	if !cidrAllowed(c.remote, profile.SMPPAllowedCIDRs) {
		markBindFailure(throttleKey, now)
		s.writeBindResp(c, p, StatusRBindFail)
		_ = db.InsertSMPPBindEvent(ctx, s.Pool, profile.ClientID, "bind_fail", mode.String(), remoteHost(c.remote), intPtr(int(StatusRBindFail)), "cidr denied")
		return nil
	}
	if !db.VerifyClientSMPPPassword(s.Config.SecretKey, profile.SMPPPasswordEnc, password) {
		markBindFailure(throttleKey, now)
		s.writeBindResp(c, p, StatusRBindFail)
		_ = db.InsertSMPPBindEvent(ctx, s.Pool, profile.ClientID, "bind_fail", mode.String(), remoteHost(c.remote), intPtr(int(StatusRBindFail)), "bad password")
		return nil
	}
	if profile.SMPPMaxBinds > 0 && s.sessions.bindCount(profile.ClientID) >= profile.SMPPMaxBinds {
		s.writeBindResp(c, p, StatusRThrottled)
		_ = db.InsertSMPPBindEvent(ctx, s.Pool, profile.ClientID, "bind_fail", mode.String(), remoteHost(c.remote), intPtr(int(StatusRThrottled)), "max binds")
		return nil
	}
	clearBindFailures(throttleKey)
	perS := profile.SMPPThroughputPerS
	if perS < 1 {
		perS = 50
	}
	lim := rate.NewLimiter(rate.Limit(perS), perS)
	sess := &session{clientID: profile.ClientID, mode: mode, remote: c.remote, conn: c, limiter: lim}
	s.sessions.add(sess)
	_ = db.InsertSMPPBindEvent(ctx, s.Pool, profile.ClientID, "bind_ok", mode.String(), remoteHost(c.remote), intPtr(0), "")
	s.writeBindResp(c, p, StatusROK)
	return sess
}

func (s *Server) unregisterSession(ctx context.Context, sess *session) {
	s.sessions.remove(sess)
	_ = db.InsertSMPPBindEvent(ctx, s.Pool, sess.clientID, "unbind", sess.mode.String(), remoteHost(sess.remote), nil, "")
}

func (s *Server) handleSubmit(ctx context.Context, c *conn, sess *session, p pdu.Body) {
	if sess.limiter != nil {
		if err := sess.limiter.Wait(ctx); err != nil {
			s.writeResp(c, pdu.NewSubmitSMResp(), p.Header().Seq, StatusRThrottled)
			return
		}
	}
	dec, err := decodeSubmitSM(p.Fields())
	if err != nil {
		s.writeResp(c, pdu.NewSubmitSMResp(), p.Header().Seq, StatusRInvMsgLen)
		return
	}
	if err := validateMessageBody(dec.Body); err != nil {
		s.writeResp(c, pdu.NewSubmitSMResp(), p.Header().Seq, StatusRInvMsgLen)
		return
	}
	client, err := db.GetClient(ctx, s.Pool, sess.clientID)
	if err != nil {
		s.writeResp(c, pdu.NewSubmitSMResp(), p.Header().Seq, StatusRSubmitFail)
		return
	}
	systemDefault := db.Setting(ctx, s.Pool, "default_sender_id", "MiniSMS")
	sid, err := carrier.ResolveSenderID(ctx, s.Pool, client, dec.From, systemDefault)
	if err != nil {
		s.writeResp(c, pdu.NewSubmitSMResp(), p.Header().Seq, StatusRInvSrcAddr)
		return
	}
	from := sid.Value
	if strings.TrimSpace(dec.From) == "" {
		from = systemDefault
	}
	dlrRequested := registeredDLRRequested(p.Fields())
	out := s.Send.Submit(ctx, sending.SubmitParams{
		Client: client,
		Message: sending.AcceptedMessage{
			To:               dec.To,
			From:             from,
			Body:             dec.Body,
			ClientRef:        dec.ClientRef,
			DLRRequested:     dlrRequested,
			DLRWebhookURL:    sending.ResolveDLRWebhookURL(dlrRequested, "", client.DLRWebhookURL),
			IngressTransport: sending.IngressSMPP,
		},
		SenderID: sid,
	})
	st := CommandStatusForOutcome(out)
	resp := pdu.NewSubmitSMResp()
	resp.Header().Seq = p.Header().Seq
	resp.Header().Status = st
	if out.Kind == sending.OutcomeAccepted && out.Accepted != nil {
		_ = resp.Fields().Set(pdufield.MessageID, out.Accepted.MessageID)
	}
	_ = c.Write(resp)
}

func (s *Server) writeBindResp(c *conn, req pdu.Body, st pdu.Status) {
	var resp pdu.Body
	switch req.Header().ID {
	case pdu.BindTransmitterID:
		resp = pdu.NewBindTransmitterResp()
	case pdu.BindReceiverID:
		resp = pdu.NewBindReceiverResp()
	default:
		resp = pdu.NewBindTransceiverResp()
	}
	resp.Header().Seq = req.Header().Seq
	resp.Header().Status = st
	if st == StatusROK {
		_ = resp.Fields().Set(pdufield.SystemID, s.SystemID)
	}
	_ = c.Write(resp)
}

func (s *Server) writeResp(c *conn, resp pdu.Body, seq uint32, st pdu.Status) {
	resp.Header().Seq = seq
	resp.Header().Status = st
	_ = c.Write(resp)
}

func remoteHost(addr net.Addr) *string {
	if addr == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	if host == "" {
		return nil
	}
	return &host
}

func intPtr(v int) *int { return &v }

func registeredDLRRequested(f pdufield.Map) bool {
	reg, ok := fieldUint8(f, pdufield.RegisteredDelivery)
	if !ok {
		return false
	}
	return reg&0x01 != 0 || reg&0x02 != 0 || reg&0x04 != 0
}
