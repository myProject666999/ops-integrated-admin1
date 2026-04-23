package models

import "time"

type ProjectCredential struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	ProjectType string    `json:"project_type"`
	Account     string    `json:"account"`
	Password    string    `json:"password"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectCredentialResponse struct {
	ProjectType string `json:"project_type"`
	Account     string `json:"account"`
	Password    string `json:"password"`
	UpdatedAt   string `json:"updated_at"`
}

type ProjectCredentialsListResponse struct {
	Items []ProjectCredentialResponse `json:"items"`
}

type ProjectLoadResponse struct {
	Loaded       bool   `json:"loaded"`
	FirstLoad    bool   `json:"first_load"`
	Message      string `json:"message"`
	SessionState string `json:"session_state"`
}

type ProjectOperateResponse struct {
	OK           bool                   `json:"ok"`
	Error        string                 `json:"error"`
	Message      string                 `json:"message"`
	Data         map[string]interface{} `json:"data"`
	SessionState string                 `json:"session_state"`
}

type ProjectReloginItem struct {
	ProjectType  string `json:"project_type"`
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	SessionState string `json:"session_state"`
}

type ProjectReloginResponse struct {
	Items          []ProjectReloginItem `json:"items"`
	NextCleanupAt  string               `json:"next_cleanup_at"`
}

type ProjectBatchFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type ProjectBatchFilesResponse struct {
	Items []ProjectBatchFile `json:"items"`
	Dir   string             `json:"dir"`
}

type ProjectBatchUploadResponse struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Path         string `json:"path"`
}
