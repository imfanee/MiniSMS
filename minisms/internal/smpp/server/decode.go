// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdutext"
	"github.com/nyaruka/phonenumbers"
)

type decodedSubmit struct {
	From       string
	To         string
	Body       string
	Encoding   string
	ClientRef  string
}

func decodeSubmitSM(f pdufield.Map) (decodedSubmit, error) {
	src := fieldString(f, pdufield.SourceAddr)
	dst := fieldString(f, pdufield.DestinationAddr)
	srcTON, _ := fieldUint8(f, pdufield.SourceAddrTON)
	srcNPI, _ := fieldUint8(f, pdufield.SourceAddrNPI)
	dstTON, _ := fieldUint8(f, pdufield.DestAddrTON)
	dstNPI, _ := fieldUint8(f, pdufield.DestAddrNPI)
	dataCoding, _ := fieldUint8(f, pdufield.DataCoding)
	esmClass, _ := fieldUint8(f, pdufield.ESMClass)

	sm := f[pdufield.ShortMessage]
	if sm == nil {
		return decodedSubmit{}, fmt.Errorf("missing short_message")
	}
	raw := sm.Bytes()
	if esmClass&0x40 != 0 && len(raw) > 0 && raw[0] > 0 {
		udhLen := int(raw[0])
		if len(raw) > udhLen+1 {
			raw = raw[udhLen+1:]
		}
	}
	body, enc := decodeMessageBody(raw, dataCoding)
	from := formatAddress(src, srcTON, srcNPI)
	to := formatAddress(dst, dstTON, dstNPI)
	if to == "" {
		return decodedSubmit{}, fmt.Errorf("invalid destination")
	}
	return decodedSubmit{
		From:     from,
		To:       to,
		Body:     body,
		Encoding: enc,
	}, nil
}

func decodeMessageBody(raw []byte, dataCoding uint8) (string, string) {
	switch dataCoding {
	case uint8(pdutext.UCS2Type):
		u := pdutext.UCS2(raw)
		return string(u.Decode()), "UCS2"
	default:
		g := pdutext.GSM7(raw)
		return string(g.Decode()), "GSM7"
	}
}

func formatAddress(addr string, ton, npi uint8) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	digits := onlyDigits(addr)
	if digits == "" {
		return ""
	}
	if strings.HasPrefix(addr, "+") {
		return "+" + onlyDigits(addr)
	}
	if ton == 1 || (ton == 0 && len(digits) >= 10) {
		e164 := "+" + digits
		if n, err := phonenumbers.Parse(e164, ""); err == nil {
			return phonenumbers.Format(n, phonenumbers.E164)
		}
		return e164
	}
	return "+" + digits
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fieldString(f pdufield.Map, k pdufield.Name) string {
	if v := f[k]; v != nil {
		return strings.TrimSpace(v.String())
	}
	return ""
}

func fieldUint8(f pdufield.Map, k pdufield.Name) (uint8, bool) {
	if v := f[k]; v != nil {
		switch b := v.Bytes(); len(b) {
		case 1:
			return b[0], true
		case 0:
			return 0, false
		default:
			return b[0], true
		}
	}
	return 0, false
}

func validateMessageBody(body string) error {
	n := utf8.RuneCountInString(body)
	if n < 1 || n > 1600 {
		return fmt.Errorf("message length %d out of range", n)
	}
	return nil
}
