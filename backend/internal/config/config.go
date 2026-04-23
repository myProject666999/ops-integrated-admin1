package config

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type AppConfig struct {
	ADAPIURL        string
	PrintAPIURL     string
	VPNSshAddr      string
	FirewallSSHAddr string
	CredentialKey   string
	ProjectCacheTTL time.Duration
	SessionIdleTTL  time.Duration
	StaticDir       string
}

var RuntimeCfg AppConfig

const CredentialCipherPrefix = "enc:v1:"
const BrowserCloseLogGracePeriod = 3 * time.Second

func LoadEnvFiles(paths ...string) {
	for _, p := range paths {
		_ = loadEnvFile(p)
	}
}

func loadEnvFile(path string) error {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		idx := strings.Index(s, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(s[:idx])
		val := strings.TrimSpace(s[idx+1:])
		val = strings.Trim(val, `"'`)
		if key == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); ok {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return nil
}

func LoadAppConfig() AppConfig {
	ttlMinutes := envInt("PROJECT_CACHE_TTL_MINUTES", 10)
	if ttlMinutes <= 0 {
		ttlMinutes = 10
	}
	idleMinutes := envInt("SESSION_IDLE_TTL_MINUTES", 60)
	if idleMinutes <= 0 {
		idleMinutes = 60
	}
	staticDir := strings.TrimSpace(envString("STATIC_DIR", "./static"))
	if staticDir == "" {
		staticDir = "./static"
	}
	return AppConfig{
		ADAPIURL:        normalizeBaseURL(envString("AD_API_URL", "http://ad.example.internal/")),
		PrintAPIURL:     normalizeBaseURL(envString("PRINT_API_URL", "http://print.example.internal/printhub/")),
		VPNSshAddr:      strings.TrimSpace(envString("VPN_SSH_ADDR", "vpn.example.internal")),
		FirewallSSHAddr: strings.TrimSpace(envString("FIREWALL_SSH_ADDR", "firewall.example.internal")),
		CredentialKey:   envString("CREDENTIAL_SECRET", "change-me-ops-credential-secret"),
		ProjectCacheTTL: time.Duration(ttlMinutes) * time.Minute,
		SessionIdleTTL:  time.Duration(idleMinutes) * time.Minute,
		StaticDir:       staticDir,
	}
}

func envString(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func normalizeBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	return strings.TrimRight(s, "/")
}

func JoinBaseURL(base, path string) string {
	base = normalizeBaseURL(base)
	if base == "" {
		return "/" + strings.TrimLeft(path, "/")
	}
	return base + "/" + strings.TrimLeft(path, "/")
}

func ADEndpoint(path string) string {
	return JoinBaseURL(RuntimeCfg.ADAPIURL, path)
}

func PrintEndpoint(path string) string {
	return JoinBaseURL(RuntimeCfg.PrintAPIURL, path)
}

func EncryptLegacyPassword(plain, key string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	if strings.HasPrefix(plain, CredentialCipherPrefix) {
		return plain, nil
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
	cipherText := gcm.Seal(nil, nonce, []byte(plain), nil)
	raw := append(nonce, cipherText...)
	return CredentialCipherPrefix + base64.RawStdEncoding.EncodeToString(raw), nil
}
