// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
// Package runtime wires process lifecycle: SMPP egress supervisors, client SMSC, HTTP API.
package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/dlr"
	"github.com/minisms/minisms/internal/routecache"
	"github.com/minisms/minisms/internal/sending"
	"github.com/minisms/minisms/internal/smpp/egress"
	"github.com/minisms/minisms/internal/smpp/server"
)

// App holds long-lived services started at boot and stopped on shutdown.
type App struct {
	Pool       *pgxpool.Pool
	Config     *config.Config
	DLR        *dlr.Processor
	Egress     *egress.Manager
	Send       *sending.Service
	Routes     *routecache.Cache
	SMPPServer *server.Server
	Queue      *sending.QueueRunner
	Aging      *ageRunner
}

// Start initializes SMPP egress (always) and optional client SMSC listener.
func Start(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (*App, error) {
	dlrProc := &dlr.Processor{Pool: pool, SecretKey: cfg.SecretKey}
	egressMgr := egress.NewManager(pool, cfg, dlrProc)
	egressMgr.Start(ctx)

	routes := routecache.New()
	if err := routes.Reload(ctx, pool); err != nil {
		egressMgr.Stop()
		return nil, fmt.Errorf("route cache reload: %w", err)
	}
	sendSvc := sending.NewWithEgress(pool, cfg, egressMgr, routes)
	app := &App{
		Pool:   pool,
		Config: cfg,
		DLR:    dlrProc,
		Egress: egressMgr,
		Send:   sendSvc,
		Routes: routes,
	}

	if cfg.SMPPServerEnabled {
		smppSrv := server.New(pool, cfg, sendSvc)
		dlrProc.SMPP = smppSrv
		if err := smppSrv.Start(ctx); err != nil {
			egressMgr.Stop()
			return nil, fmt.Errorf("smpp server start: %w", err)
		}
		app.SMPPServer = smppSrv
		slog.Info("smpp ingress enabled", "listen", cfg.SMPPListenAddr, "tls", cfg.SMPPTLSEnabled)
	} else {
		slog.Info("smpp ingress disabled", "hint", "set SMPP_SERVER_ENABLED=true to listen for ESME binds")
	}

	if cfg.SendQueueEnabled {
		q := sending.NewQueueRunner(sendSvc)
		// On expiry/undelivered, notify the client (webhook + SMPP deliver_sm).
		q.NotifyUndelivered = func(ctx context.Context, messageID string) {
			_ = dlrProc.HandleInbound(ctx, messageID, map[string]string{"status": "undelivered"}, nil)
		}
		q.Start(ctx)
		app.Queue = q
	} else {
		slog.Info("send queue disabled", "hint", "set SEND_QUEUE_ENABLED=true for async queued dispatch")
	}

	if cfg.AcceptedDLRTTLSecs > 0 {
		app.Aging = startAgeRunner(ctx, pool, dlrProc, cfg.AcceptedDLRTTLSecs)
		slog.Info("accepted-no-dlr aging enabled", "ttl_seconds", cfg.AcceptedDLRTTLSecs)
	} else {
		slog.Info("accepted-no-dlr aging disabled", "hint", "set SEND_ACCEPTED_DLR_TTL_S>0 to close out messages with no final DLR")
	}

	return app, nil
}

// Stop shuts down SMPP ingress then carrier egress supervisors (reverse of Start).
func (a *App) Stop() {
	if a == nil {
		return
	}
	if a.Aging != nil {
		a.Aging.stop()
	}
	if a.Queue != nil {
		a.Queue.Stop()
	}
	if a.SMPPServer != nil {
		a.SMPPServer.Stop()
	}
	if a.Egress != nil {
		a.Egress.Stop()
	}
}

// SMPPEnabled reports whether the client SMSC listener is active.
func (a *App) SMPPEnabled() bool {
	return a != nil && a.SMPPServer != nil
}
