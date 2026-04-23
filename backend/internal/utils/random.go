package utils

import (
	crand "crypto/rand"
	"encoding/base64"
)

func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
