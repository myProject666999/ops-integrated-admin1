package runtime

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ops-admin-backend/internal/project"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type registerReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8"`
}

type changePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

type browserCloseEventReq struct {
	Reason         string `json:"reason"`
	ClosedAtMS     int64  `json:"closed_at_ms"`
	TimeoutAtMS    int64  `json:"timeout_at_ms"`
	ReopenedAtMS   int64  `json:"reopened_at_ms"`
	IdleTTLSeconds int    `json:"idle_ttl_seconds"`
}

type projectCredentialReq struct {
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type operateReq struct {
	Action string                 `json:"action" binding:"required"`
	Params map[string]interface{} `json:"params"`
}

func (s *server) handleLoginGin(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "用户名和密码不能为空"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)

	var adminCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM admins`).Scan(&adminCount); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "查询管理员失败"})
		return
	}
	if adminCount == 0 {
		c.JSON(http.StatusBadRequest, apiError{Error: "暂无管理员账号，请先注册"})
		return
	}

	var userID int64
	var username, hash string
	err := s.db.QueryRow(`SELECT id,username,password_hash FROM admins WHERE username=?`, req.Username).Scan(&userID, &username, &hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, apiError{Error: "账号或密码错误"})
			return
		}
		c.JSON(http.StatusInternalServerError, apiError{Error: "查询管理员失败"})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, apiError{Error: "账号或密码错误"})
		return
	}

	token, err := randomToken(48)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "生成令牌失败"})
		return
	}
	exp := time.Now().Add(s.tokenTTL)
	now := nowStr()
	if _, err = s.db.Exec(`INSERT INTO auth_tokens(token,user_id,expires_at,created_at) VALUES(?,?,?,?)`, token, userID, exp.Format(time.RFC3339), now); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "创建登录会话失败"})
		return
	}
	if err = ensureDefaultProjectCredentialsForUser(s.db, userID); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "初始化项目凭据失败"})
		return
	}

	s.logAction(userID, username, "login", "", "用户登录成功")
	c.JSON(http.StatusOK, gin.H{
		"token":                     token,
		"username":                  username,
		"expire_at":                 exp.Format(time.RFC3339),
		"default_pwd":               false,
		"project_cache_ttl_seconds": int(s.cfg.ProjectCacheTTL.Seconds()),
		"session_idle_ttl_seconds":  int(s.cfg.SessionIdleTTL.Seconds()),
	})
}

func (s *server) handleRegisterGin(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "用户名长度必须为3-32位，密码长度至少8位"})
		return
	}

	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "密码加密失败"})
		return
	}
	res, err := s.db.Exec(`INSERT INTO admins(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`, username, string(hash), nowStr(), nowStr())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, apiError{Error: "用户名已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, apiError{Error: "创建用户失败"})
		return
	}
	userID, _ := res.LastInsertId()
	if err = ensureDefaultProjectCredentialsForUser(s.db, userID); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "初始化项目凭据失败"})
		return
	}
	s.logAction(userID, username, "register", "", "管理员注册成功")
	c.JSON(http.StatusOK, gin.H{"message": "注册成功"})
}

func (s *server) handleMeGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	c.JSON(http.StatusOK, gin.H{
		"id":                        u.ID,
		"username":                  u.Username,
		"project_cache_ttl_seconds": int(s.cfg.ProjectCacheTTL.Seconds()),
		"session_idle_ttl_seconds":  int(s.cfg.SessionIdleTTL.Seconds()),
	})
}

func (s *server) handleWindowCloseStartGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	var req browserCloseEventReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "请求体格式错误"})
		return
	}
	s.scheduleBrowserCloseLifecycle(u, req)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *server) handleWindowCloseCancelGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	var req browserCloseEventReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "请求体格式错误"})
		return
	}
	found, started := s.cancelBrowserCloseState(u.Token)
	if !found {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if !started {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	detail := formatBrowserCloseCancelDetail(req)
	s.logAction(u.ID, u.Username, "browser_close_timer_canceled", "", detail)
	fmt.Printf("[browser-close-cancel] user=%s detail=%s\n", u.Username, detail)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *server) handleLogoutGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	var req browserCloseEventReq
	c.ShouldBindJSON(&req)

	s.cancelBrowserCloseStatesByUser(u.ID)
	s.cleanupUserAuthTokens(u.ID)
	if strings.TrimSpace(req.Reason) == "reopen_timeout" {
		detail := formatBrowserCloseEventDetail("页面关闭超时，已清理该账号全部 Token 与项目会话缓存", req)
		s.logAction(u.ID, u.Username, "logout", "", detail)
		fmt.Printf("[browser-close-timeout] user=%s detail=%s\n", u.Username, detail)
	} else {
		s.logAction(u.ID, u.Username, "logout", "", "管理员退出登录")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *server) handleChangePasswordGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "请求体格式错误"})
		return
	}

	req.OldPassword = strings.TrimSpace(req.OldPassword)
	req.NewPassword = strings.TrimSpace(req.NewPassword)

	if req.OldPassword == "" || req.NewPassword == "" {
		c.JSON(http.StatusBadRequest, apiError{Error: "原密码和新密码不能为空"})
		return
	}

	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM admins WHERE id=?`, u.ID).Scan(&hash); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "查询当前密码失败"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)) != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "原密码错误"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "密码加密失败"})
		return
	}
	if _, err = s.db.Exec(`UPDATE admins SET password_hash=?,updated_at=? WHERE id=?`, string(newHash), nowStr(), u.ID); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "更新密码失败"})
		return
	}
	s.logAction(u.ID, u.Username, "change_password", "", "管理员修改密码")
	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}

func (s *server) handleProjectCredentialsGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	if err := ensureDefaultProjectCredentialsForUser(s.db, u.ID); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "初始化项目凭据失败"})
		return
	}
	rows, err := s.db.Query(`SELECT project_type,account,password,updated_at FROM project_credentials WHERE user_id=? ORDER BY project_type`, u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "查询项目凭据失败"})
		return
	}
	defer rows.Close()

	items := make([]map[string]string, 0)
	for rows.Next() {
		var t, account, password, updated string
		if err = rows.Scan(&t, &account, &password, &updated); err != nil {
			c.JSON(http.StatusInternalServerError, apiError{Error: "读取项目凭据失败"})
			return
		}
		plainPwd, decErr := decryptCredentialPassword(password, s.cfg.CredentialKey)
		if decErr != nil {
			c.JSON(http.StatusInternalServerError, apiError{Error: "项目凭据解密失败"})
			return
		}
		items = append(items, map[string]string{"project_type": t, "account": account, "password": plainPwd, "updated_at": updated})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *server) handleProjectCredentialByTypeGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	projectType := c.Param("project_type")
	if !validCredentialProjectType(projectType) {
		c.JSON(http.StatusBadRequest, apiError{Error: "无效的项目类型"})
		return
	}
	var req projectCredentialReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "请求体格式错误"})
		return
	}
	if strings.TrimSpace(req.Account) == "" || strings.TrimSpace(req.Password) == "" {
		c.JSON(http.StatusBadRequest, apiError{Error: "账号和密码不能为空"})
		return
	}
	encryptedPwd, err := encryptCredentialPassword(req.Password, s.cfg.CredentialKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "凭据加密失败"})
		return
	}
	if _, err = s.db.Exec(`INSERT INTO project_credentials(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)
	ON CONFLICT(user_id,project_type) DO UPDATE SET account=excluded.account,password=excluded.password,updated_at=excluded.updated_at`,
		u.ID, projectType, req.Account, encryptedPwd, nowStr(),
	); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "更新项目凭据失败"})
		return
	}
	s.projectSessions.clearUserProject(u.ID, projectType)
	s.logAction(u.ID, u.Username, "update_project_credential", projectType, "更新项目凭据")
	c.JSON(http.StatusOK, gin.H{"message": "更新成功"})
}

func (s *server) handleProjectLoadGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	projectType := c.Param("project_type")
	if !validProjectType(projectType) {
		c.JSON(http.StatusBadRequest, apiError{Error: "无效的项目类型"})
		return
	}

	_, didLogin, message, err := s.ensureProjectSession(u, projectType, false)
	if err != nil {
		s.logAction(u.ID, u.Username, "project_load_failed", projectType, truncate(err.Error(), 600))
		c.JSON(http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	if !didLogin {
		c.JSON(http.StatusOK, gin.H{"loaded": true, "first_load": false, "session_state": "reused"})
		return
	}
	s.logAction(u.ID, u.Username, "project_load", projectType, "首次加载完成")
	c.JSON(http.StatusOK, gin.H{"loaded": true, "first_load": true, "message": message, "session_state": "first_login"})
}

func (s *server) handleProjectBatchFilesGin(c *gin.Context) {
	projectType := c.Param("project_type")
	if projectType != "ad" {
		c.JSON(http.StatusBadRequest, apiError{Error: "批量文件仅支持AD项目"})
		return
	}
	files, err := project.BatchExcelFiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	items := make([]map[string]string, 0, len(files))
	for _, name := range files {
		items = append(items, map[string]string{
			"name": name,
			"path": filepath.Join(project.BatchUploadDir(), name),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"dir":   project.BatchUploadDir(),
	})
}

func (s *server) handleProjectBatchTemplateGin(c *gin.Context) {
	projectType := c.Param("project_type")
	if projectType != "ad" {
		c.JSON(http.StatusBadRequest, apiError{Error: "批量模板仅支持AD项目"})
		return
	}
	path := project.BatchTemplatePath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, apiError{Error: "模板文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	filename := filepath.Base(path)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename)))
	c.File(path)
}

func (s *server) handleProjectBatchUploadGin(c *gin.Context) {
	projectType := c.Param("project_type")
	if projectType != "ad" {
		c.JSON(http.StatusBadRequest, apiError{Error: "批量上传仅支持AD项目"})
		return
	}
	if err := os.MkdirAll(project.BatchUploadDir(), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "文件不能为空"})
		return
	}
	defer file.Close()

	oldFile := filepath.Base(strings.TrimSpace(c.PostForm("old_file")))

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".xlsx" && ext != ".xls" {
		c.JSON(http.StatusBadRequest, apiError{Error: "仅支持上传 xlsx/.xls 文件"})
		return
	}

	storedName := fmt.Sprintf("ad_batch_%d%s", time.Now().UnixNano(), ext)
	outPath := filepath.Join(project.BatchUploadDir(), storedName)
	outFile, err := os.Create(outPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	defer outFile.Close()
	if _, err = io.Copy(outFile, file); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "保存文件失败"})
		return
	}

	if oldFile != "" {
		_ = os.Remove(filepath.Join(project.BatchUploadDir(), oldFile))
	}

	c.JSON(http.StatusOK, gin.H{
		"name":          storedName,
		"original_name": header.Filename,
		"path":          outPath,
	})
}

func (s *server) handleProjectOperateGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	projectType := c.Param("project_type")
	if !validProjectType(projectType) {
		c.JSON(http.StatusBadRequest, apiError{Error: "无效的项目类型"})
		return
	}

	var req operateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "请求体格式错误"})
		return
	}
	if strings.TrimSpace(req.Action) == "" {
		c.JSON(http.StatusBadRequest, apiError{Error: "操作类型不能为空"})
		return
	}
	if req.Params == nil {
		req.Params = map[string]interface{}{}
	}
	if projectType == "vpn" && req.Action == "delete_users" && toBoolDefault(req.Params["remote_firewall"], false) {
		fwAccount, fwPassword, fwErr := s.getProjectCredential(u.ID, "vpn_firewall")
		if fwErr != nil {
			req.Params["__vpn_fw_configured"] = false
			req.Params["__vpn_fw_error"] = fwErr.Error()
		} else {
			req.Params["__vpn_fw_configured"] = true
			req.Params["__vpn_fw_account"] = fwAccount
			req.Params["__vpn_fw_password"] = fwPassword
		}
	}

	entry, didLogin, _, err := s.ensureProjectSession(u, projectType, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}

	result, err := s.operateWithProjectSession(entry, req.Action, req.Params)
	if err != nil {
		s.logAction(u.ID, u.Username, "project_operate_failed", projectType, fmt.Sprintf("action=%s, err=%v", req.Action, err))
		c.JSON(http.StatusBadGateway, apiError{Error: err.Error()})
		return
	}
	if !result.OK {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = result.Message
		}
		s.logAction(u.ID, u.Username, "project_operate_failed", projectType, fmt.Sprintf("action=%s, err=%s", req.Action, errMsg))
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": errMsg, "message": result.Message, "data": result.Data})
		return
	}
	s.logAction(u.ID, u.Username, "project_operate", projectType, fmt.Sprintf("action=%s", req.Action))
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": result.Message, "data": result.Data, "session_state": projectSessionStateFromDidLogin(didLogin)})
}

func (s *server) handleProjectsReloginGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	s.projectSessions.clearToken(u.Token)
	reloginItems := make([]map[string]interface{}, 0, 3)
	for _, projectType := range []string{"ad", "print", "vpn"} {
		_, _, message, err := s.ensureProjectSession(u, projectType, true)
		if err != nil {
			reloginItems = append(reloginItems, map[string]interface{}{
				"project_type": projectType,
				"ok":           false,
				"message":      err.Error(),
			})
			continue
		}
		reloginItems = append(reloginItems, map[string]interface{}{
			"project_type":  projectType,
			"ok":            true,
			"message":       message,
			"session_state": "countdown_relogin",
		})
	}
	s.logAction(u.ID, u.Username, "project_relogin", "", "手动触发项目重新登录")
	c.JSON(http.StatusOK, gin.H{
		"items":           reloginItems,
		"next_cleanup_at": time.Now().Add(s.cfg.ProjectCacheTTL).Format(time.RFC3339),
	})
}

func (s *server) handleProjectOperateAsyncStartGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	var req asyncOperateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: "请求体格式错误"})
		return
	}
	req.ProjectType = strings.TrimSpace(req.ProjectType)
	req.Action = strings.TrimSpace(req.Action)
	if !validProjectType(req.ProjectType) {
		c.JSON(http.StatusBadRequest, apiError{Error: "无效的项目类型"})
		return
	}
	if req.Action == "" {
		c.JSON(http.StatusBadRequest, apiError{Error: "操作类型不能为空"})
		return
	}

	params := cloneInterfaceMap(req.Params)
	if params == nil {
		params = map[string]interface{}{}
	}
	if req.ProjectType == "vpn" && req.Action == "delete_users" && toBoolDefault(params["remote_firewall"], false) {
		fwAccount, fwPassword, fwErr := s.getProjectCredential(u.ID, "vpn_firewall")
		if fwErr != nil {
			params["__vpn_fw_configured"] = false
			params["__vpn_fw_error"] = fwErr.Error()
		} else {
			params["__vpn_fw_configured"] = true
			params["__vpn_fw_account"] = fwAccount
			params["__vpn_fw_password"] = fwPassword
		}
	}

	_, didLogin, _, err := s.ensureProjectSession(u, req.ProjectType, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}

	job, createErr := s.createAsyncOperateJob(u, req.ProjectType, req.Action)
	if createErr != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "创建异步任务失败"})
		return
	}
	go s.runAsyncOperate(job.ID, u, req.ProjectType, req.Action, params)
	c.JSON(http.StatusOK, gin.H{
		"job_id":        job.ID,
		"status":        job.Status,
		"created_at":    job.CreatedAt.Format(time.RFC3339),
		"project_type":  req.ProjectType,
		"action":        req.Action,
		"session_state": projectSessionStateFromDidLogin(didLogin),
	})
}

func (s *server) handleProjectOperateAsyncStatusGin(c *gin.Context) {
	u := getAuthedUserGin(c)
	jobID := strings.TrimSpace(c.Param("job_id"))
	if jobID == "" || strings.Contains(jobID, "/") {
		c.JSON(http.StatusNotFound, apiError{Error: "任务不存在"})
		return
	}
	view, ok := s.getAsyncOperateJobView(jobID, u.ID)
	if !ok {
		c.JSON(http.StatusNotFound, apiError{Error: "任务不存在或已过期"})
		return
	}
	c.JSON(http.StatusOK, view)
}

func (s *server) handleLogsGin(c *gin.Context) {
	page := 1
	pageSize := 20

	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 && n <= 1000 {
			pageSize = n
		}
	}
	if v := strings.TrimSpace(c.Query("page")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			page = n
		}
	}
	if v := strings.TrimSpace(c.Query("page_size")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}

	projectType := strings.TrimSpace(c.Query("project_type"))
	where := ""
	countArgs := make([]interface{}, 0, 1)
	if projectType != "" {
		where = ` WHERE project_type=?`
		countArgs = append(countArgs, projectType)
	}

	var total int
	countQuery := `SELECT COUNT(1) FROM operation_logs` + where
	if err := s.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "查询日志失败"})
		return
	}

	offset := (page - 1) * pageSize
	query := `SELECT id,COALESCE(user_id,0),COALESCE(username,''),COALESCE(action,''),COALESCE(project_type,''),COALESCE(detail,''),created_at FROM operation_logs` +
		where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args := make([]interface{}, 0, len(countArgs)+2)
	args = append(args, countArgs...)
	args = append(args, pageSize, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apiError{Error: "查询日志失败"})
		return
	}
	defer rows.Close()
	items := make([]logRow, 0)
	for rows.Next() {
		var row logRow
		if err = rows.Scan(&row.ID, &row.UserID, &row.Username, &row.Action, &row.ProjectType, &row.Detail, &row.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, apiError{Error: "读取日志失败"})
			return
		}
		row.Detail = normalizeGarbledText(row.Detail)
		items = append(items, row)
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (s *server) getProjectCredential(userID int64, projectType string) (string, string, error) {
	var account, password string
	err := s.db.QueryRow(`SELECT account,password FROM project_credentials WHERE user_id=? AND project_type=?`, userID, projectType).Scan(&account, &password)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", errors.New("项目凭据未配置")
		}
		return "", "", err
	}
	password, err = decryptCredentialPassword(password, s.cfg.CredentialKey)
	if err != nil {
		return "", "", errors.New("凭据解密失败")
	}
	if strings.TrimSpace(account) == "" || strings.TrimSpace(password) == "" {
		return "", "", errors.New("项目凭据未配置")
	}
	return account, password, nil
}

func (s *server) logAction(userID int64, username, action, projectType, detail string) {
	detail = normalizeGarbledText(detail)
	_, _ = s.db.Exec(`INSERT INTO operation_logs(user_id,username,action,project_type,detail,created_at) VALUES(?,?,?,?,?,?)`, userID, username, action, projectType, detail, nowStr())
}

func (s *server) scheduleBrowserCloseLifecycle(u authedUser, req browserCloseEventReq) {
	token := strings.TrimSpace(u.Token)
	req = normalizeBrowserCloseEventReq(req)
	if token == "" {
		detail := formatBrowserCloseEventDetail("检测到浏览器最后一个系统页面已关闭，开始计时", req)
		s.logAction(u.ID, u.Username, "browser_close_timer_started", "", detail)
		fmt.Printf("[browser-close] user=%s detail=%s\n", u.Username, detail)
		s.executeBrowserCloseTimeout(u, req)
		return
	}

	delayUntilTimeout := time.Until(time.UnixMilli(req.TimeoutAtMS))
	if delayUntilTimeout < 0 {
		delayUntilTimeout = 0
	}

	state := &browserCloseState{
		user: u,
		req:  req,
	}
	state.startTimer = time.AfterFunc(browserCloseLogGracePeriod, func() {
		s.activateBrowserCloseStartLog(token, req.ClosedAtMS)
	})
	state.timeoutTimer = time.AfterFunc(delayUntilTimeout, func() {
		s.handleBrowserCloseTimeout(token, req.ClosedAtMS)
	})

	s.browserCloseLogMu.Lock()
	if old := s.browserCloseStates[token]; old != nil {
		stopBrowserCloseTimer(old.startTimer)
		stopBrowserCloseTimer(old.timeoutTimer)
	}
	s.browserCloseStates[token] = state
	s.browserCloseLogMu.Unlock()
}

func (s *server) activateBrowserCloseStartLog(token string, expectedClosedAtMS int64) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}

	s.browserCloseLogMu.Lock()
	state := s.browserCloseStates[token]
	if state == nil || state.req.ClosedAtMS != expectedClosedAtMS || state.startedLog {
		s.browserCloseLogMu.Unlock()
		return
	}
	state.startedLog = true
	user := state.user
	req := state.req
	s.browserCloseLogMu.Unlock()

	detail := formatBrowserCloseEventDetail("检测到浏览器最后一个系统页面已关闭，开始计时", req)
	s.logAction(user.ID, user.Username, "browser_close_timer_started", "", detail)
	fmt.Printf("[browser-close] user=%s detail=%s\n", user.Username, detail)
}

func (s *server) handleBrowserCloseTimeout(token string, expectedClosedAtMS int64) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}

	s.browserCloseLogMu.Lock()
	state := s.browserCloseStates[token]
	if state == nil || state.req.ClosedAtMS != expectedClosedAtMS {
		s.browserCloseLogMu.Unlock()
		return
	}
	delete(s.browserCloseStates, token)
	user := state.user
	req := state.req
	started := state.startedLog
	s.browserCloseLogMu.Unlock()

	stopBrowserCloseTimer(state.startTimer)
	stopBrowserCloseTimer(state.timeoutTimer)

	if !started {
		detail := formatBrowserCloseEventDetail("检测到浏览器最后一个系统页面已关闭，开始计时", req)
		s.logAction(user.ID, user.Username, "browser_close_timer_started", "", detail)
		fmt.Printf("[browser-close] user=%s detail=%s\n", user.Username, detail)
	}

	s.executeBrowserCloseTimeout(user, req)
}

func (s *server) executeBrowserCloseTimeout(u authedUser, req browserCloseEventReq) {
	s.cancelBrowserCloseStatesByUser(u.ID)
	s.cleanupUserAuthTokens(u.ID)
	detail := formatBrowserCloseEventDetail("页面关闭超时，后端已自动清理该账号全部 Token 与项目会话缓存", req)
	s.logAction(u.ID, u.Username, "logout", "", detail)
	fmt.Printf("[browser-close-timeout-auto] user=%s detail=%s\n", u.Username, detail)
}

func (s *server) cancelBrowserCloseState(token string) (bool, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return false, false
	}

	s.browserCloseLogMu.Lock()
	state := s.browserCloseStates[token]
	if state != nil {
		delete(s.browserCloseStates, token)
	}
	s.browserCloseLogMu.Unlock()
	if state == nil {
		return false, false
	}

	stopBrowserCloseTimer(state.startTimer)
	stopBrowserCloseTimer(state.timeoutTimer)
	return true, state.startedLog
}

func (s *server) cancelBrowserCloseStatesByUser(userID int64) {
	if userID <= 0 {
		return
	}

	toStop := make([]*browserCloseState, 0)
	s.browserCloseLogMu.Lock()
	for token, state := range s.browserCloseStates {
		if state == nil || state.user.ID != userID {
			continue
		}
		toStop = append(toStop, state)
		delete(s.browserCloseStates, token)
	}
	s.browserCloseLogMu.Unlock()

	for _, state := range toStop {
		stopBrowserCloseTimer(state.startTimer)
		stopBrowserCloseTimer(state.timeoutTimer)
	}
}

func normalizeBrowserCloseEventReq(req browserCloseEventReq) browserCloseEventReq {
	idleTTLSeconds := req.IdleTTLSeconds
	if idleTTLSeconds <= 0 {
		idleTTLSeconds = int(runtimeCfg.SessionIdleTTL.Seconds())
	}
	req.IdleTTLSeconds = idleTTLSeconds
	if req.TimeoutAtMS <= 0 && req.ClosedAtMS > 0 && idleTTLSeconds > 0 {
		req.TimeoutAtMS = req.ClosedAtMS + int64(idleTTLSeconds)*1000
	}
	return req
}

func stopBrowserCloseTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	timer.Stop()
}

func formatBrowserCloseEventDetail(prefix string, req browserCloseEventReq) string {
	req = normalizeBrowserCloseEventReq(req)
	idleTTLSeconds := req.IdleTTLSeconds
	closedAt := formatUnixMilliForLog(req.ClosedAtMS)
	timeoutAtMS := req.TimeoutAtMS
	timeoutAt := formatUnixMilliForLog(timeoutAtMS)
	return fmt.Sprintf("%s，开始触发时间：%s，超时时长：%d 秒，超时时间：%s", prefix, closedAt, idleTTLSeconds, timeoutAt)
}

func formatBrowserCloseCancelDetail(req browserCloseEventReq) string {
	req = normalizeBrowserCloseEventReq(req)
	idleTTLSeconds := req.IdleTTLSeconds
	closedAt := formatUnixMilliForLog(req.ClosedAtMS)
	timeoutAtMS := req.TimeoutAtMS
	timeoutAt := formatUnixMilliForLog(timeoutAtMS)
	reopenedAt := formatUnixMilliForLog(req.ReopenedAtMS)
	return fmt.Sprintf("浏览器系统页面已重新打开，取消页面关闭超时计时，开始触发时间：%s，超时时长：%d 秒，原超时时间：%s，重新打开时间：%s", closedAt, idleTTLSeconds, timeoutAt, reopenedAt)
}

func formatUnixMilliForLog(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func (s *server) serveStatic(w http.ResponseWriter, r *http.Request) {
	staticDir := s.cfg.StaticDir
	if staticDir == "" {
		staticDir = "./static"
	}
	staticDir = filepath.Clean(staticDir)

	requestPath := r.URL.Path
	if requestPath == "/" {
		requestPath = "/index.html"
	}

	filePath := filepath.Join(staticDir, requestPath)
	filePath = filepath.Clean(filePath)

	if !strings.HasPrefix(filePath, staticDir+string(filepath.Separator)) && filePath != staticDir {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			indexPath := filepath.Join(staticDir, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if _, indexErr := os.Stat(indexPath); indexErr == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	case ".woff", ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
	case ".ttf":
		w.Header().Set("Content-Type", "font/ttf")
	}

	http.ServeFile(w, r, filePath)
}
