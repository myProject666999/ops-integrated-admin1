package models

import "time"

type OperationLog struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Username    string    `json:"username"`
	Action      string    `json:"action"`
	ProjectType string    `json:"project_type"`
	Detail      string    `json:"detail"`
	CreatedAt   time.Time `json:"created_at"`
}

type LogRow struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Action      string `json:"action"`
	ProjectType string `json:"project_type"`
	Detail      string `json:"detail"`
	CreatedAt   string `json:"created_at"`
}

type LogsResponse struct {
	Items    []LogRow `json:"items"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}
