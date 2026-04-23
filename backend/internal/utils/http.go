package utils

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func ExtractBearerToken(authHeader string) string {
	authHeader = strings.TrimSpace(authHeader)
	if authHeader == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(strings.ToLower(authHeader), strings.ToLower(prefix)) {
		return ""
	}
	return strings.TrimSpace(authHeader[len(prefix):])
}

func DecodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func DecodeOptionalJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ValidProjectType(t string) bool {
	switch t {
	case "ad", "print", "vpn":
		return true
	default:
		return false
	}
}

func ValidCredentialProjectType(t string) bool {
	switch t {
	case "ad", "print", "vpn", "vpn_firewall":
		return true
	default:
		return false
	}
}

func ProjectSessionStateFromDidLogin(didLogin bool) string {
	if didLogin {
		return "first_login"
	}
	return "reused"
}

func LoginFailureMessage(projectType string) string {
	switch projectType {
	case "ad":
		return "AD 登录失败"
	case "print":
		return "打印管理登录失败"
	case "vpn":
		return "VPN 登录失败"
	default:
		return "项目登录失败"
	}
}
