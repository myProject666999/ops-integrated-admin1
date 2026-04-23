package controllers

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/services"
	"ops-admin-backend/internal/utils"

	"github.com/gin-gonic/gin"
)

type AppController struct {
	service *services.AppService
	db      *sql.DB
}

func NewAppController(service *services.AppService, db *sql.DB) *AppController {
	return &AppController{
		service: service,
		db:      db,
	}
}

func (c *AppController) Health(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (c *AppController) Login(ctx *gin.Context) {
	var req loginReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "用户名和密码不能为空"})
		return
	}

	result, err := c.service.Login(req.Username, req.Password)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, models.APIError{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"token":                     result.Token,
		"username":                  result.Username,
		"expire_at":                 result.ExpireAt,
		"default_pwd":               result.DefaultPwd,
		"project_cache_ttl_seconds": result.ProjectCacheTTLSeconds,
		"session_idle_ttl_seconds":  result.SessionIdleTTLSeconds,
	})
}

type registerReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8"`
}

func (c *AppController) Register(ctx *gin.Context) {
	var req registerReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "用户名长度必须为3-32位，密码长度至少8位"})
		return
	}

	if err := c.service.Register(req.Username, req.Password); err != nil {
		errMsg := err.Error()
		if strings.Contains(strings.ToLower(errMsg), "unique") || strings.Contains(strings.ToLower(errMsg), "已存在") {
			ctx.JSON(http.StatusConflict, models.APIError{Error: "用户名已存在"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: errMsg})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "注册成功"})
}

func (c *AppController) Me(ctx *gin.Context) {
	user := getAuthedUser(ctx)
	cfg := c.service.GetConfig()

	ctx.JSON(http.StatusOK, gin.H{
		"id":                        user.ID,
		"username":                  user.Username,
		"project_cache_ttl_seconds": int(cfg.ProjectCacheTTL.Seconds()),
		"session_idle_ttl_seconds":  int(cfg.SessionIdleTTL.Seconds()),
	})
}

type changePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

func (c *AppController) ChangePassword(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	var req changePasswordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "请求体格式错误"})
		return
	}

	if err := c.service.ChangePassword(user.ID, req.OldPassword, req.NewPassword); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}

type browserCloseEventReq struct {
	Reason         string `json:"reason"`
	ClosedAtMS     int64  `json:"closed_at_ms"`
	TimeoutAtMS    int64  `json:"timeout_at_ms"`
	ReopenedAtMS   int64  `json:"reopened_at_ms"`
	IdleTTLSeconds int    `json:"idle_ttl_seconds"`
}

func (c *AppController) WindowCloseStart(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	var req browserCloseEventReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "请求体格式错误"})
		return
	}

	c.service.ScheduleBrowserCloseLifecycle(
		user.ID, user.Token, user.Username,
		models.BrowserCloseEventReq(req),
	)
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

func (c *AppController) WindowCloseCancel(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	var req browserCloseEventReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "请求体格式错误"})
		return
	}

	found, started := c.service.CancelBrowserCloseState(user.Token)
	if found && started {
		detail := utils.FormatBrowserCloseCancelDetail(
			req.ClosedAtMS, req.TimeoutAtMS, req.ReopenedAtMS, req.IdleTTLSeconds,
		)
		c.service.LogAction(user.ID, user.Username, "browser_close_timer_canceled", "", detail)
		fmt.Printf("[browser-close-cancel] user=%s detail=%s\n", user.Username, detail)
	}

	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

func (c *AppController) Logout(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	var req browserCloseEventReq
	ctx.ShouldBindJSON(&req)

	c.service.Logout(user.ID, user.Token, req.Reason)
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

func (c *AppController) GetProjectCredentials(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	items, err := c.service.GetProjectCredentials(user.ID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

type projectCredentialReq struct {
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (c *AppController) UpdateProjectCredential(ctx *gin.Context) {
	user := getAuthedUser(ctx)
	projectType := ctx.Param("project_type")

	if !utils.ValidCredentialProjectType(projectType) {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "无效的项目类型"})
		return
	}

	var req projectCredentialReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "请求体格式错误"})
		return
	}

	if err := c.service.UpdateProjectCredential(user.ID, projectType, req.Account, req.Password); err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (c *AppController) LoadProject(ctx *gin.Context) {
	user := getAuthedUser(ctx)
	projectType := ctx.Param("project_type")

	if !utils.ValidProjectType(projectType) {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "无效的项目类型"})
		return
	}

	result, err := c.service.LoadProject(user.ID, user.Token, projectType)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, models.APIError{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"loaded":        result.Loaded,
		"first_load":  result.FirstLoad,
		"message":     result.Message,
		"session_state": result.SessionState,
	})
}

type operateReq struct {
	Action string                 `json:"action" binding:"required"`
	Params map[string]interface{} `json:"params"`
}

func (c *AppController) OperateProject(ctx *gin.Context) {
	user := getAuthedUser(ctx)
	projectType := ctx.Param("project_type")

	if !utils.ValidProjectType(projectType) {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "无效的项目类型"})
		return
	}

	var req operateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "请求体格式错误"})
		return
	}

	if strings.TrimSpace(req.Action) == "" {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "操作类型不能为空"})
		return
	}

	result, err := c.service.OperateProject(user.ID, user.Token, projectType, req.Action, req.Params)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, models.APIError{Error: err.Error()})
		return
	}

	if !result.OK {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"ok":           false,
			"error":        result.Error,
			"message":      result.Message,
			"data":         result.Data,
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"message":      result.Message,
		"data":         result.Data,
		"session_state": result.SessionState,
	})
}

func (c *AppController) ReloginProjects(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	result := c.service.ReloginProjects(user.ID, user.Token)
	ctx.JSON(http.StatusOK, gin.H{
		"items":           result.Items,
		"next_cleanup_at": result.NextCleanupAt,
	})
}

func (c *AppController) GetBatchFiles(ctx *gin.Context) {
	projectType := ctx.Param("project_type")

	items, dir, err := c.service.GetBatchFiles(projectType)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"items": items,
		"dir":   dir,
	})
}

func (c *AppController) DownloadBatchTemplate(ctx *gin.Context) {
	projectType := ctx.Param("project_type")

	path, err := c.service.GetBatchTemplatePath(projectType)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			ctx.JSON(http.StatusNotFound, models.APIError{Error: err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: err.Error()})
		return
	}

	filename := filepath.Base(path)
	ctx.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename)))
	ctx.File(path)
}

func (c *AppController) UploadBatchFile(ctx *gin.Context) {
	projectType := ctx.Param("project_type")

	if projectType != "ad" {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "批量上传仅支持AD项目"})
		return
	}

	oldFile := strings.TrimSpace(ctx.PostForm("old_file"))

	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "文件不能为空"})
		return
	}
	defer file.Close()

	filename := header.Filename
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".xlsx" && ext != ".xls" {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "仅支持上传 xlsx/.xls 文件"})
		return
	}

	dir := filepath.Join(config.RuntimeCfg.StaticDir, "uploads", "ad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: "无法准备上传目录"})
		return
	}

	storedName := fmt.Sprintf("ad_batch_%d%s", time.Now().UnixNano(), ext)
	outPath := filepath.Join(dir, storedName)
	outFile, err := os.Create(outPath)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: "无法创建文件"})
		return
	}
	defer outFile.Close()

	if _, err = io.Copy(outFile, file); err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: "保存文件失败"})
		return
	}

	if oldFile != "" {
		_ = os.Remove(filepath.Join(dir, oldFile))
	}

	ctx.JSON(http.StatusOK, gin.H{
		"name":          storedName,
		"original_name": filename,
		"path":          outPath,
	})
}

type asyncOperateReq struct {
	ProjectType string                 `json:"project_type"`
	Action      string                 `json:"action"`
	Params      map[string]interface{} `json:"params"`
}

func (c *AppController) StartAsyncOperate(ctx *gin.Context) {
	user := getAuthedUser(ctx)

	var req asyncOperateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "请求体格式错误"})
		return
	}

	req.ProjectType = strings.TrimSpace(req.ProjectType)
	req.Action = strings.TrimSpace(req.Action)

	if !utils.ValidProjectType(req.ProjectType) {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "无效的项目类型"})
		return
	}
	if req.Action == "" {
		ctx.JSON(http.StatusBadRequest, models.APIError{Error: "操作类型不能为空"})
		return
	}

	job, err := c.service.CreateAsyncJob(user.ID, user.Username, req.ProjectType, req.Action)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: "创建异步任务失败"})
		return
	}

	params := services.CloneInterfaceMap(req.Params)
	go c.runAsyncOperate(job.ID, user, req.ProjectType, req.Action, params)

	ctx.JSON(http.StatusOK, gin.H{
		"job_id":        job.ID,
		"status":        job.Status,
		"created_at":    job.CreatedAt.Format(time.RFC3339),
		"project_type":  req.ProjectType,
		"action":        req.Action,
		"session_state": "",
	})
}

func (c *AppController) GetAsyncJobStatus(ctx *gin.Context) {
	user := getAuthedUser(ctx)
	jobID := strings.TrimSpace(ctx.Param("job_id"))

	if jobID == "" || strings.Contains(jobID, "/") {
		ctx.JSON(http.StatusNotFound, models.APIError{Error: "任务不存在"})
		return
	}

	view, ok := c.service.GetAsyncJobView(jobID, user.ID)
	if !ok {
		ctx.JSON(http.StatusNotFound, models.APIError{Error: "任务不存在或已过期"})
		return
	}

	ctx.JSON(http.StatusOK, view)
}

func (c *AppController) GetLogs(ctx *gin.Context) {
	page := 1
	pageSize := 20

	if v := strings.TrimSpace(ctx.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			pageSize = n
		}
	}
	if v := strings.TrimSpace(ctx.Query("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := strings.TrimSpace(ctx.Query("page_size")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}

	projectType := strings.TrimSpace(ctx.Query("project_type"))

	result, err := c.service.GetLogs(page, pageSize, projectType)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, models.APIError{Error: "查询日志失败"})
		return
	}

	ctx.JSON(http.StatusOK, result)
}

func (c *AppController) runAsyncOperate(jobID string, user models.AuthedUser, projectType, action string, params map[string]interface{}) {
	result, err := c.service.OperateProject(user.ID, user.Token, projectType, action, params)

	if err != nil {
		errMsg := strings.TrimSpace(err.Error())
		if errMsg == "" {
			errMsg = "执行失败"
		}
		c.service.UpdateAsyncJob(jobID, func(job *models.AsyncOperateJob) {
			job.Status = models.AsyncJobStatusFailed
			job.OK = false
			job.Done = true
			job.Message = "执行失败"
			job.Error = errMsg
			job.Progress = 100
		})
		c.service.LogAction(user.ID, user.Username, "project_operate_failed", projectType, fmt.Sprintf("action=%s, err=%s", action, errMsg))
		return
	}

	if !result.OK {
		errMsg := strings.TrimSpace(result.Error)
		if errMsg == "" {
			errMsg = strings.TrimSpace(result.Message)
		}
		if errMsg == "" {
			errMsg = "执行失败"
		}
		c.service.UpdateAsyncJob(jobID, func(job *models.AsyncOperateJob) {
			job.Status = models.AsyncJobStatusFailed
			job.OK = false
			job.Done = true
			job.Message = result.Message
			job.Error = errMsg
			job.Progress = 100
		})
		c.service.LogAction(user.ID, user.Username, "project_operate_failed", projectType, fmt.Sprintf("action=%s, err=%s", action, errMsg))
		return
	}

	c.service.UpdateAsyncJob(jobID, func(job *models.AsyncOperateJob) {
		job.Status = models.AsyncJobStatusSuccess
		job.OK = true
		job.Done = true
		job.Message = "执行成功"
		if result.Message != "" {
			job.Message = result.Message
		}
		job.Error = ""
		job.Progress = 100
		if result.Data != nil {
			if items, ok := result.Data["items"].([]interface{}); ok {
				job.ResultItems = items
			}
		}
	})
	c.service.LogAction(user.ID, user.Username, "project_operate", projectType, fmt.Sprintf("action=%s", action))
}

func (c *AppController) AuthMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := utils.ExtractBearerToken(ctx.GetHeader("Authorization"))
		if token == "" {
			ctx.JSON(http.StatusUnauthorized, models.APIError{Error: "缺少 Bearer 令牌"})
			ctx.Abort()
			return
		}

		userID, username, err := c.service.GetUserInfo(token)
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, models.APIError{Error: "无效或已过期的令牌"})
			ctx.Abort()
			return
		}

		user := models.AuthedUser{
			ID:       userID,
			Username: username,
			Token:    token,
		}
		ctx.Set("user", user)
		ctx.Next()
	}
}

func getAuthedUser(ctx *gin.Context) models.AuthedUser {
	u, _ := ctx.Get("user")
	if user, ok := u.(models.AuthedUser); ok {
		return user
	}
	return models.AuthedUser{}
}
