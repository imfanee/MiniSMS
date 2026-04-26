package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const gcmNonceSize = 12

// EncryptValue encrypts plaintext with AES-256-GCM; result is base64( nonce(12) || ciphertext ).
func EncryptValue(key32 []byte, plaintext string) (string, error) {
	if len(key32) != 32 {
		return "", errors.New("encryption key must be 32 bytes")
	}
	if plaintext == "" {
		return "", errors.New("empty plaintext")
	}
	block, err := aes.NewCipher(key32)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	// ct is 12-byte nonce + sealed (includes auth tag), per AES-GCM idiom
	return base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptValue decodes base64, extracts 12-byte nonce, decrypts with AES-256-GCM.
func DecryptValue(key32 []byte, b64 string) (string, error) {
	if len(key32) != 32 {
		return "", errors.New("encryption key must be 32 bytes")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	if len(raw) < gcmNonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce := raw[:gcmNonceSize]
	ct := raw[gcmNonceSize:]

	block, err := aes.NewCipher(key32)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}
