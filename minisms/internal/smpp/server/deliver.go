// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdutext"
)

func receiptStat(dlrStatus string) string {
	switch strings.ToLower(strings.TrimSpace(dlrStatus)) {
	case "delivered":
		return "DELIVRD"
	case "undelivered", "failed":
		return "UNDELIV"
	case "rejected":
		return "REJECTD"
	default:
		return "UNKNOWN"
	}
}

func buildDeliveryReceipt(messageID, stat string) string {
	now := time.Now().UTC().Format("0601021504")
	return fmt.Sprintf("id:%s sub:001 dlvrd:001 submit date:%s done date:%s stat:%s err:000 Text:",
		messageID, now, now, stat)
}

// DeliverDLR sends deliver_sm with an Appendix B receipt to a bound RX/TRX session.
func (s *Server) DeliverDLR(clientID, messageID, dlrStatus string) bool {
	sess := s.sessions.pickDeliver(clientID)
	if sess == nil || sess.conn == nil {
		s.logEvent(clientID, "WARN", "deliver_sm not sent", "message_id", messageID, "reason", "no bound rx/trx session")
		return false
	}
	stat := receiptStat(dlrStatus)
	body := buildDeliveryReceipt(messageID, stat)
	p := pdu.NewDeliverSM()
	f := p.Fields()
	_ = f.Set(pdufield.SourceAddr, "MiniSMS")
	_ = f.Set(pdufield.DestinationAddr, "client")
	_ = f.Set(pdufield.ShortMessage, pdutext.Raw(body))
	_ = f.Set(pdufield.ESMClass, uint8(0x04)) // delivery receipt indicator
	_ = f.Set(pdufield.DataCoding, uint8(pdutext.DefaultType))
	if err := sess.conn.Write(p); err != nil {
		s.logEvent(clientID, "ERROR", "deliver_sm write failed", "message_id", messageID, "error", err.Error())
		return false
	}
	s.logEvent(clientID, "INFO", "deliver_sm DLR sent", "message_id", messageID, "stat", stat)
	return true
}
