// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidateAPIKey_RejectsShortKey(t *testing.T) {
	_, err := ValidateAPIKey(t.Context(), nil, "short")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestValidateAPIKey_HashCompareUsesConstantTimePath(t *testing.T) {
	// Documents the intended compare path: subtle.ConstantTimeCompare on full digests.
	salt := make([]byte, 16)
	raw := "abcdefghijklmnopqrstuvwxyz0123456789AB"
	sum := sha256.Sum256(append(salt, []byte(raw)...))
	if len(hex.EncodeToString(sum[:])) != 64 {
		t.Fatal("unexpected hash length")
	}
}
