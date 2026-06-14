package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

func masterKey() ([]byte, error) {
	raw := os.Getenv("CTO_ENCRYPTION_KEY")
	if raw == "" {
		return nil, errors.New("CTO_ENCRYPTION_KEY is not set")
	}
	// Derive a stable 32-byte key via SHA-256 so any string length works.
	h := sha256.Sum256([]byte(raw))
	return h[:], nil
}

func sealAES(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func openAES(key []byte, encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}

// EncryptSecret encrypts value using a fresh random DEK wrapped with the master key.
// Returns (encryptedValue, encryptedDEK, error).
func EncryptSecret(value string) (encryptedValue, encryptedDEK string, err error) {
	mk, err := masterKey()
	if err != nil {
		return "", "", err
	}

	dek := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, dek); err != nil {
		return "", "", fmt.Errorf("failed to generate DEK: %w", err)
	}

	encryptedValue, err = sealAES(dek, []byte(value))
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt value: %w", err)
	}

	encryptedDEK, err = sealAES(mk, dek)
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt DEK: %w", err)
	}

	return encryptedValue, encryptedDEK, nil
}

// DecryptSecret decrypts an encrypted vault secret using its wrapped DEK and the master key.
func DecryptSecret(encryptedValue, encryptedDEK string) (string, error) {
	mk, err := masterKey()
	if err != nil {
		return "", err
	}

	dek, err := openAES(mk, encryptedDEK)
	if err != nil {
		return "", fmt.Errorf("failed to unwrap DEK: %w", err)
	}

	plaintext, err := openAES(dek, encryptedValue)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt value: %w", err)
	}

	return string(plaintext), nil
}
