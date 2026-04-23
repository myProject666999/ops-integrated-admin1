package models

import "time"

type Admin struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AuthToken struct {
	Token      string    `json:"token"`
	UserID     int64     `json:"user_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type LoginResponse struct {
	Token                  string `json:"token"`
	Username               string `json:"username"`
	ExpireAt               string `json:"expire_at"`
	DefaultPwd             bool   `json:"default_pwd"`
	ProjectCacheTTLSeconds int    `json:"project_cache_ttl_seconds"`
	SessionIdleTTLSeconds  int    `json:"session_idle_ttl_seconds"`
}

type MeResponse struct {
	ID                      int64 `json:"id"`
	Username                string `json:"username"`
	ProjectCacheTTLSeconds  int `json:"project_cache_ttl_seconds"`
	SessionIdleTTLSeconds   int `json:"session_idle_ttl_seconds"`
}
