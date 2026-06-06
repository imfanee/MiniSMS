// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package billing

import "testing"

func TestGSM7Detection(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		wantEncoding string
	}{
		{name: "ascii gsm7", message: "Hello world", wantEncoding: "GSM7"},
		{name: "emoji ucs2", message: "Hello 🙂", wantEncoding: "UCS2"},
		{name: "extended gsm7", message: "Price is 10€ and £5", wantEncoding: "GSM7"},
		{name: "mixed ucs2", message: "abc ü 🙂", wantEncoding: "UCS2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, seg := SegmentInfo(tt.message)
			if enc != tt.wantEncoding {
				t.Fatalf("encoding mismatch: want %s got %s", tt.wantEncoding, enc)
			}
			if seg < 1 {
				t.Fatalf("segments should be >= 1")
			}
		})
	}
}
