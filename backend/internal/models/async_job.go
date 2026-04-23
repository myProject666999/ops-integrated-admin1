package models

import "time"

const (
	AsyncJobStatusRunning = "running"
	AsyncJobStatusSuccess = "success"
	AsyncJobStatusFailed  = "failed"
)

type AsyncOperateReq struct {
	ProjectType string                 `json:"project_type"`
	Action      string                 `json:"action"`
	Params      map[string]interface{} `json:"params"`
}

type AsyncOperateJob struct {
	ID          string
	UserID      int64
	Username    string
	ProjectType string
	Action      string
	Status      string
	OK          bool
	Done        bool
	Message     string
	Error       string
	Progress    int
	Processed   int
	Total       int
	LogLines    []string
	ResultText  string
	ResultItems []interface{}
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AsyncOperateJobView struct {
	JobID       string        `json:"job_id"`
	ProjectType string        `json:"project_type"`
	Action      string        `json:"action"`
	Status      string        `json:"status"`
	OK          bool          `json:"ok"`
	Done        bool          `json:"done"`
	Message     string        `json:"message"`
	Error       string        `json:"error"`
	Progress    int           `json:"progress"`
	Processed   int           `json:"processed"`
	Total       int           `json:"total"`
	LogLines    []string      `json:"log_lines"`
	ResultText  string        `json:"result_text"`
	ResultItems []interface{} `json:"result_items"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
}

type AsyncOperateStartResponse struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	ProjectType  string `json:"project_type"`
	Action       string `json:"action"`
	SessionState string `json:"session_state"`
}
