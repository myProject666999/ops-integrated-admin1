package models

type APIError struct {
	Error string `json:"error"`
}

type AuthedUser struct {
	ID       int64
	Username string
	Token    string
}

type BrowserCloseEventReq struct {
	Reason         string `json:"reason"`
	ClosedAtMS     int64  `json:"closed_at_ms"`
	TimeoutAtMS    int64  `json:"timeout_at_ms"`
	ReopenedAtMS   int64  `json:"reopened_at_ms"`
	IdleTTLSeconds int    `json:"idle_ttl_seconds"`
}

type ProjectCredentialReq struct {
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type OperateReq struct {
	Action string                 `json:"action" binding:"required"`
	Params map[string]interface{} `json:"params"`
}

type LoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8"`
}

type ChangePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

type BrowserCloseState struct {
	User         AuthedUser
	Req          BrowserCloseEventReq
	StartedLog   bool
	StartTimer   interface{}
	TimeoutTimer interface{}
}
