// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"testing"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/fiorix/go-smpp/v2/smpp/smpptest"
	"github.com/minisms/minisms/internal/config"
)

func submitSMRespHandler(cli smpptest.Conn, m pdu.Body) {
	if m.Header().ID == pdu.SubmitSMID {
		resp := pdu.NewSubmitSMResp()
		resp.Header().Seq = m.Header().Seq
		resp.Fields().Set(pdufield.MessageID, "fake-smsc-id")
		_ = cli.Write(resp)
		return
	}
	smpptest.EchoHandler(cli, m)
}

func TestManager_SubmitViaSMPTest(t *testing.T) {
	srv := smpptest.NewUnstartedServer()
	srv.Handler = submitSMRespHandler
	srv.Start()
	defer srv.Close()

	cfg := &config.Config{
		SecretKey: []byte("0123456789abcdef0123456789abcdef"),
	}
	mgr := NewManager(nil, cfg, nil)

	sess := newLiveSession(CarrierConfig{
		CarrierID:           "test-carrier",
		Addr:                srv.Addr(),
		SystemID:            smpptest.DefaultUser,
		Password:            smpptest.DefaultPasswd,
		BindMode:            "tx",
		EnquireLink:         30 * time.Second,
		WindowSize:          10,
		ThroughputPerSecond: 50,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		sess.run(ctx, func(string) {})
		close(done)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !sess.isReady() {
		if time.Now().After(deadline) {
			t.Fatal("session not ready")
		}
		time.Sleep(50 * time.Millisecond)
	}

	res, err := sess.submit(ctx, SubmitRequest{
		Src:       "MiniSMS",
		Dst:       "+447700900123",
		Body:      "hello smpp",
		SourceTON: 5,
		SourceNPI: 0,
		DestTON:   1,
		DestNPI:   1,
		Encoding:  "GSM7",
		Segments:  1,
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.CommandStatus != 0 {
		t.Fatalf("command_status=%d body=%s", res.CommandStatus, res.ResponseBody)
	}
	if res.CarrierMessageID == "" {
		t.Fatalf("expected carrier message id")
	}
	cancel()
	<-done
	_ = mgr
}
