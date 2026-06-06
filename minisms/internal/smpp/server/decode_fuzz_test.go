// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"testing"

	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdutext"
)

// FuzzDecodeMessageBody ensures segment decoding never panics on arbitrary bytes.
func FuzzDecodeMessageBody(f *testing.F) {
	f.Add([]byte{0x00, 0x01, '+', '4', '4'})
	f.Add([]byte{0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, raw []byte) {
		for _, dc := range []uint8{0, uint8(pdutext.UCS2Type), 255} {
			_, _ = decodeMessageBody(raw, dc)
		}
		_ = validateMessageBody(string(raw))
	})
}

// FuzzDecodeSubmitSM_UDH exercises UDH stripping via a minimal field map.
func FuzzDecodeSubmitSM_UDH(f *testing.F) {
	f.Add(byte(0), byte(10))
	f.Fuzz(func(t *testing.T, udhLen byte, pad byte) {
		raw := make([]byte, 1+int(udhLen%32)+4)
		raw[0] = udhLen % 32
		for i := range raw[1:] {
			raw[1+i] = pad
		}
		m := pdufield.Map{
			pdufield.ShortMessage:    pdufield.New(pdufield.ShortMessage, raw),
			pdufield.DestinationAddr: pdufield.New(pdufield.DestinationAddr, []byte("+447700900123")),
			pdufield.ESMClass:        pdufield.New(pdufield.ESMClass, []byte{0x40}),
		}
		_, _ = decodeSubmitSM(m)
	})
}
