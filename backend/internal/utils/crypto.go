package utils

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

func EncryptCredentialPassword(plain, key string) (string, error) {
	value := strings.TrimSpace(plain)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "enc:v1:") {
		return value, nil
	}

	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = crand.Read(nonce); err != nil {
		return "", err
	}
	cipherText := gcm.Seal(nil, nonce, []byte(value), nil)
	raw := append(nonce, cipherText...)
	return "enc:v1:" + base64.RawStdEncoding.EncodeToString(raw), nil
}

func CredentialCandidateKeys(primary string) []string {
	items := make([]string, 0, 6)
	seen := map[string]bool{}
	appendKey := func(v string) {
		k := strings.TrimSpace(v)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		items = append(items, k)
	}

	appendKey(primary)
	// Built-in historical defaults for smoother key migration.
	appendKey("change-me-ops-credential-secret")
	appendKey("change-this-to-your-own-secret-key")
	return items
}

func decryptCredentialPasswordRaw(raw []byte, key string) (string, error) {
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", nil
	}
	nonce, body := raw[:ns], raw[ns:]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func DecryptCredentialPassword(cipherText, key string) (string, error) {
	value := strings.TrimSpace(cipherText)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "enc:v1:") {
		return value, nil
	}
	encoded := strings.TrimPrefix(value, "enc:v1:")
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	var lastErr error
	for _, candidate := range CredentialCandidateKeys(key) {
		plain, decErr := decryptCredentialPasswordRaw(raw, candidate)
		if decErr == nil {
			return plain, nil
		}
		lastErr = decErr
	}
	if lastErr == nil {
		lastErr = nil
	}
	return "", lastErr
}
