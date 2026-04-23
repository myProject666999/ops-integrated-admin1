package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/project"
	"ops-admin-backend/internal/repositories"
	"ops-admin-backend/internal/utils"

	"golang.org/x/crypto/bcrypt"
)

type AppService struct {
	db          *sql.DB
	cfg         config.AppConfig
	tokenTTL    time.Duration
	staticDir   string

	adminRepo      *repositories.AdminRepository
	credentialRepo *repositories.CredentialRepository
	logRepo        *repositories.LogRepository
	asyncJobRepo   *repositories.AsyncJobRepository

	projectSessions    *ProjectSessionManager
	browserCloseStates map[string]*models.BrowserCloseState
	browserCloseMu     sync.Mutex
}

func NewAppService(db *sql.DB, cfg config.AppConfig) *AppService {
	return &AppService{
		db:          db,
		cfg:         cfg,
		tokenTTL:    24 * time.Hour,
		staticDir:   cfg.StaticDir,

		adminRepo:      repositories.NewAdminRepository(db),
		credentialRepo: repositories.NewCredentialRepository(db),
		logRepo:        repositories.NewLogRepository(db),
		asyncJobRepo:   repositories.NewAsyncJobRepository(),

		projectSessions:    NewProjectSessionManager(),
		browserCloseStates: make(map[string]*models.BrowserCloseState),
	}
}

func (s *AppService) GetConfig() config.AppConfig {
	return s.cfg
}

func (s *AppService) GetTokenTTL() time.Duration {
	return s.tokenTTL
}

func (s *AppService) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

func (s *AppService) GetDB() *sql.DB {
	return s.db
}

func NewAppServiceFromDSN(dsn string, cfg config.AppConfig) (*AppService, error) {
	dir := filepath.Dir(dsn)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0o755)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.Exec(`PRAGMA busy_timeout = 5000`)

	statements := []string{
		`CREATE TABLE IF NOT EXISTS admins (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_tokens (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES admins(id)
		)`,
		`CREATE TABLE IF NOT EXISTS project_credentials (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			project_type TEXT NOT NULL,
			account TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			UNIQUE(user_id, project_type),
			FOREIGN KEY(user_id) REFERENCES admins(id)
		)`,
		`CREATE TABLE IF NOT EXISTS operation_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			username TEXT,
			action TEXT NOT NULL,
			project_type TEXT,
			detail TEXT,
			created_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range statements {
		if _, err = db.Exec(stmt); err != nil {
			db.Close()
			return nil, err
		}
	}

	return NewAppService(db, cfg), nil
}

func (s *AppService) LoadAuthedUser(token string) (*models.AuthedUser, error) {
	return s.adminRepo.LoadAuthedUser(token, time.Now().Format(time.RFC3339))
}

type LoginResult struct {
	Token                  string
	Username               string
	ExpireAt               string
	DefaultPwd             bool
	ProjectCacheTTLSeconds int
	SessionIdleTTLSeconds  int
}

func (s *AppService) Login(username, password string) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)

	if username == "" || password == "" {
		return nil, errors.New("账号和密码不能为空")
	}

	var adminCount int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM admins`).Scan(&adminCount); err != nil {
		return nil, errors.New("查询管理员失败")
	}
	if adminCount == 0 {
		return nil, errors.New("暂无管理员账号，请先注册")
	}

	var userID int64
	var hash string
	err := s.db.QueryRow(
		`SELECT id,password_hash FROM admins WHERE username=?`,
		username,
	).Scan(&userID, &hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("账号或密码错误")
		}
		return nil, errors.New("查询管理员失败")
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, errors.New("账号或密码错误")
	}

	token, err := utils.RandomToken(48)
	if err != nil {
		return nil, errors.New("生成令牌失败")
	}

	exp := time.Now().Add(s.tokenTTL)
	now := utils.NowStr()
	if _, err = s.db.Exec(
		`INSERT INTO auth_tokens(token,user_id,expires_at,created_at) VALUES(?,?,?,?)`,
		token, userID, exp.Format(time.RFC3339), now,
	); err != nil {
		return nil, errors.New("创建登录会话失败")
	}

	if err = s.credentialRepo.EnsureDefaultProjectCredentialsForUser(userID); err != nil {
		return nil, errors.New("初始化项目凭据失败")
	}

	s.logRepo.LogAction(userID, username, "login", "", "用户登录成功")

	return &LoginResult{
		Token:                  token,
		Username:               username,
		ExpireAt:               exp.Format(time.RFC3339),
		DefaultPwd:             false,
		ProjectCacheTTLSeconds: int(s.cfg.ProjectCacheTTL.Seconds()),
		SessionIdleTTLSeconds:  int(s.cfg.SessionIdleTTL.Seconds()),
	}, nil
}

func (s *AppService) Register(username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)

	if len(username) < 3 || len(username) > 32 {
		return errors.New("用户名长度必须为3-32位")
	}
	if len(password) < 8 {
		return errors.New("密码长度至少8位")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("密码加密失败")
	}

	res, err := s.db.Exec(
		`INSERT INTO admins(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`,
		username, string(hash), utils.NowStr(), utils.NowStr(),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return errors.New("用户名已存在")
		}
		return errors.New("创建用户失败")
	}

	userID, _ := res.LastInsertId()
	if err = s.credentialRepo.EnsureDefaultProjectCredentialsForUser(userID); err != nil {
		return errors.New("初始化项目凭据失败")
	}

	s.logRepo.LogAction(userID, username, "register", "", "管理员注册成功")
	return nil
}

func (s *AppService) GetUserInfo(token string) (int64, string, error) {
	now := time.Now().Format(time.RFC3339)
	var u models.AuthedUser
	err := s.db.QueryRow(
		`SELECT a.id,a.username FROM auth_tokens t JOIN admins a ON a.id=t.user_id WHERE t.token=? AND t.expires_at>?`,
		token, now,
	).Scan(&u.ID, &u.Username)
	if err != nil {
		return 0, "", errors.New("无效或已过期的令牌")
	}
	return u.ID, u.Username, nil
}

func (s *AppService) Logout(userID int64, token, reason string) {
	s.CancelBrowserCloseStatesByUser(userID)
	_, _ = s.db.Exec(`DELETE FROM auth_tokens WHERE user_id=?`, userID)

	if reason == "reopen_timeout" {
		detail := utils.FormatBrowserCloseEventDetail(
			"页面关闭超时，已清理该账号全部 Token 与项目会话缓存",
			0, 0, int(s.cfg.SessionIdleTTL.Seconds()),
		)
		s.logRepo.LogAction(userID, "", "logout", "", detail)
	} else {
		s.logRepo.LogAction(userID, "", "logout", "", "管理员退出登录")
	}
}

func (s *AppService) ChangePassword(userID int64, oldPassword, newPassword string) error {
	oldPassword = strings.TrimSpace(oldPassword)
	newPassword = strings.TrimSpace(newPassword)

	if oldPassword == "" || newPassword == "" {
		return errors.New("原密码和新密码不能为空")
	}
	if len(newPassword) < 8 {
		return errors.New("密码长度至少8位")
	}

	var hash string
	if err := s.db.QueryRow(
		`SELECT password_hash FROM admins WHERE id=?`,
		userID,
	).Scan(&hash); err != nil {
		return errors.New("查询当前密码失败")
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return errors.New("原密码错误")
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("密码加密失败")
	}

	if _, err = s.db.Exec(
		`UPDATE admins SET password_hash=?,updated_at=? WHERE id=?`,
		string(newHash), utils.NowStr(), userID,
	); err != nil {
		return errors.New("更新密码失败")
	}

	s.logRepo.LogAction(userID, "", "change_password", "", "管理员修改密码")
	return nil
}

func (s *AppService) GetProjectCredentials(userID int64) ([]map[string]string, error) {
	if err := s.credentialRepo.EnsureDefaultProjectCredentialsForUser(userID); err != nil {
		return nil, errors.New("初始化项目凭据失败")
	}

	rows, err := s.db.Query(
		`SELECT project_type,account,password,updated_at FROM project_credentials WHERE user_id=? ORDER BY project_type`,
		userID,
	)
	if err != nil {
		return nil, errors.New("查询项目凭据失败")
	}
	defer rows.Close()

	items := make([]map[string]string, 0)
	for rows.Next() {
		var t, account, password, updated string
		if err = rows.Scan(&t, &account, &password, &updated); err != nil {
			return nil, errors.New("读取项目凭据失败")
		}
		plainPwd, decErr := utils.DecryptCredentialPassword(password, s.cfg.CredentialKey)
		if decErr != nil {
			return nil, errors.New("项目凭据解密失败")
		}
		items = append(items, map[string]string{
			"project_type": t,
			"account":      account,
			"password":     plainPwd,
			"updated_at":   updated,
		})
	}
	return items, nil
}

func (s *AppService) UpdateProjectCredential(userID int64, projectType, account, password string) error {
	if !utils.ValidCredentialProjectType(projectType) {
		return errors.New("无效的项目类型")
	}

	account = strings.TrimSpace(account)
	password = strings.TrimSpace(password)

	if account == "" || password == "" {
		return errors.New("账号和密码不能为空")
	}

	encryptedPwd, err := utils.EncryptCredentialPassword(password, s.cfg.CredentialKey)
	if err != nil {
		return errors.New("凭据加密失败")
	}

	_, err = s.db.Exec(
		`INSERT INTO project_credentials(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(user_id,project_type) DO UPDATE SET account=excluded.account,password=excluded.password,updated_at=excluded.updated_at`,
		userID, projectType, account, encryptedPwd, utils.NowStr(),
	)
	if err != nil {
		return errors.New("更新项目凭据失败")
	}

	s.projectSessions.ClearUserProject(userID, projectType)
	s.logRepo.LogAction(userID, "", "update_project_credential", projectType, "更新项目凭据")
	return nil
}

type ProjectLoadResult struct {
	Loaded       bool
	FirstLoad    bool
	Message      string
	SessionState string
}

func (s *AppService) LoadProject(userID int64, token, projectType string) (*ProjectLoadResult, error) {
	if !utils.ValidProjectType(projectType) {
		return nil, errors.New("无效的项目类型")
	}

	account, password, err := s.getProjectCredential(userID, projectType)
	if err != nil {
		s.logRepo.LogAction(userID, "", "project_load_failed", projectType, utils.Truncate(err.Error(), 600))
		return nil, err
	}

	u := &models.AuthedUser{ID: userID, Token: token}
	entry, didLogin, message, err := s.projectSessions.Ensure(u, projectType, account, password, s.cfg.ProjectCacheTTL, false)
	if err != nil {
		s.logRepo.LogAction(userID, "", "project_load_failed", projectType, utils.Truncate(err.Error(), 600))
		return nil, err
	}
	_ = entry

	if !didLogin {
		return &ProjectLoadResult{
			Loaded:       true,
			FirstLoad:    false,
			SessionState: "reused",
		}, nil
	}

	s.logRepo.LogAction(userID, "", "project_load", projectType, "首次加载完成")
	return &ProjectLoadResult{
		Loaded:       true,
		FirstLoad:    true,
		Message:      message,
		SessionState: "first_login",
	}, nil
}

type ProjectOperateResult struct {
	OK           bool
	Error        string
	Message      string
	Data         map[string]interface{}
	SessionState string
}

func (s *AppService) OperateProject(userID int64, token, projectType, action string, params map[string]interface{}) (*ProjectOperateResult, error) {
	if !utils.ValidProjectType(projectType) {
		return nil, errors.New("无效的项目类型")
	}

	if strings.TrimSpace(action) == "" {
		return nil, errors.New("操作类型不能为空")
	}

	if params == nil {
		params = map[string]interface{}{}
	}

	if projectType == "vpn" && action == "delete_users" && utils.ToBoolDefault(params["remote_firewall"], false) {
		fwAccount, fwPassword, fwErr := s.getProjectCredential(userID, "vpn_firewall")
		if fwErr != nil {
			params["__vpn_fw_configured"] = false
			params["__vpn_fw_error"] = fwErr.Error()
		} else {
			params["__vpn_fw_configured"] = true
			params["__vpn_fw_account"] = fwAccount
			params["__vpn_fw_password"] = fwPassword
		}
	}

	u := &models.AuthedUser{ID: userID, Token: token}
	entry, didLogin, _, err := s.projectSessions.Ensure(u, projectType, "", "", s.cfg.ProjectCacheTTL, false)
	if err != nil {
		return nil, err
	}

	result, err := s.operateWithProjectSession(entry, action, params)
	if err != nil {
		s.logRepo.LogAction(userID, "", "project_operate_failed", projectType, fmt.Sprintf("action=%s, err=%v", action, err))
		return nil, err
	}

	if !result.OK {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = result.Message
		}
		s.logRepo.LogAction(userID, "", "project_operate_failed", projectType, fmt.Sprintf("action=%s, err=%s", action, errMsg))
		return &ProjectOperateResult{
			OK:           false,
			Error:        errMsg,
			Message:      result.Message,
			Data:         result.Data,
			SessionState: "",
		}, nil
	}

	s.logRepo.LogAction(userID, "", "project_operate", projectType, fmt.Sprintf("action=%s", action))
	return &ProjectOperateResult{
		OK:           true,
		Error:        "",
		Message:      result.Message,
		Data:         result.Data,
		SessionState: utils.ProjectSessionStateFromDidLogin(didLogin),
	}, nil
}

type ProjectReloginResult struct {
	Items         []map[string]interface{}
	NextCleanupAt string
}

func (s *AppService) ReloginProjects(userID int64, token string) *ProjectReloginResult {
	u := &models.AuthedUser{ID: userID, Token: token}
	s.projectSessions.ClearToken(token)

	reloginItems := make([]map[string]interface{}, 0, 3)
	for _, projectType := range []string{"ad", "print", "vpn"} {
		account, password, err := s.getProjectCredential(userID, projectType)
		if err != nil {
			reloginItems = append(reloginItems, map[string]interface{}{
				"project_type": projectType,
				"ok":           false,
				"message":      err.Error(),
			})
			continue
		}

		_, didLogin, message, err := s.projectSessions.Ensure(u, projectType, account, password, s.cfg.ProjectCacheTTL, true)
		if err != nil {
			reloginItems = append(reloginItems, map[string]interface{}{
				"project_type": projectType,
				"ok":           false,
				"message":      err.Error(),
			})
			continue
		}

		item := map[string]interface{}{
			"project_type":  projectType,
			"ok":            true,
		}
		if message != "" {
			item["message"] = message
		}
		if didLogin {
			item["session_state"] = "countdown_relogin"
		}
		reloginItems = append(reloginItems, item)
	}

	s.logRepo.LogAction(userID, "", "project_relogin", "", "手动触发项目重新登录")
	return &ProjectReloginResult{
		Items:         reloginItems,
		NextCleanupAt: time.Now().Add(s.cfg.ProjectCacheTTL).Format(time.RFC3339),
	}
}

func (s *AppService) GetLogs(page, pageSize int, projectType string) (*models.LogsResponse, error) {
	return s.logRepo.GetLogs(page, pageSize, projectType)
}

func (s *AppService) CreateAsyncJob(userID int64, username, projectType, action string) (*models.AsyncOperateJob, error) {
	user := &models.AuthedUser{ID: userID, Username: username}
	return s.asyncJobRepo.CreateJob(user, projectType, action)
}

func (s *AppService) UpdateAsyncJob(jobID string, fn func(*models.AsyncOperateJob)) {
	s.asyncJobRepo.UpdateJob(jobID, fn)
}

func (s *AppService) GetAsyncJobView(jobID string, userID int64) (*models.AsyncOperateJobView, bool) {
	return s.asyncJobRepo.GetJobView(jobID, userID)
}

func (s *AppService) CalcJobProgress(processed, total, logCount int, done bool) int {
	return s.asyncJobRepo.CalcJobProgress(processed, total, logCount, done)
}

func (s *AppService) ScheduleBrowserCloseLifecycle(userID int64, token, username string, req models.BrowserCloseEventReq) {
	token = strings.TrimSpace(token)
	req = s.normalizeBrowserCloseEventReq(req)

	if token == "" {
		detail := utils.FormatBrowserCloseEventDetail(
			"检测到浏览器最后一个系统页面已关闭，开始计时",
			req.ClosedAtMS, req.TimeoutAtMS, req.IdleTTLSeconds,
		)
		s.logRepo.LogAction(userID, username, "browser_close_timer_started", "", detail)
		fmt.Printf("[browser-close] user=%s detail=%s\n", username, detail)
		s.executeBrowserCloseTimeout(userID, username, req)
		return
	}

	delayUntilTimeout := time.Until(time.UnixMilli(req.TimeoutAtMS))
	if delayUntilTimeout < 0 {
		delayUntilTimeout = 0
	}

	state := &models.BrowserCloseState{
		User:       models.AuthedUser{ID: userID, Token: token, Username: username},
		Req:        req,
		StartedLog: false,
	}

	state.StartTimer = time.AfterFunc(config.BrowserCloseLogGracePeriod, func() {
		s.activateBrowserCloseStartLog(token, req.ClosedAtMS)
	})
	state.TimeoutTimer = time.AfterFunc(delayUntilTimeout, func() {
		s.handleBrowserCloseTimeout(token, req.ClosedAtMS)
	})

	s.browserCloseMu.Lock()
	if old := s.browserCloseStates[token]; old != nil {
		s.stopBrowserCloseTimer(old.StartTimer)
		s.stopBrowserCloseTimer(old.TimeoutTimer)
	}
	s.browserCloseStates[token] = state
	s.browserCloseMu.Unlock()
}

func (s *AppService) activateBrowserCloseStartLog(token string, expectedClosedAtMS int64) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}

	s.browserCloseMu.Lock()
	state := s.browserCloseStates[token]
	if state == nil || state.Req.ClosedAtMS != expectedClosedAtMS || state.StartedLog {
		s.browserCloseMu.Unlock()
		return
	}
	state.StartedLog = true
	userID := state.User.ID
	username := state.User.Username
	req := state.Req
	s.browserCloseMu.Unlock()

	detail := utils.FormatBrowserCloseEventDetail(
		"检测到浏览器最后一个系统页面已关闭，开始计时",
		req.ClosedAtMS, req.TimeoutAtMS, req.IdleTTLSeconds,
	)
	s.logRepo.LogAction(userID, username, "browser_close_timer_started", "", detail)
	fmt.Printf("[browser-close] user=%s detail=%s\n", username, detail)
}

func (s *AppService) handleBrowserCloseTimeout(token string, expectedClosedAtMS int64) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}

	s.browserCloseMu.Lock()
	state := s.browserCloseStates[token]
	if state == nil || state.Req.ClosedAtMS != expectedClosedAtMS {
		s.browserCloseMu.Unlock()
		return
	}
	delete(s.browserCloseStates, token)
	userID := state.User.ID
	username := state.User.Username
	req := state.Req
	started := state.StartedLog
	s.browserCloseMu.Unlock()

	s.stopBrowserCloseTimer(state.StartTimer)
	s.stopBrowserCloseTimer(state.TimeoutTimer)

	if !started {
		detail := utils.FormatBrowserCloseEventDetail(
			"检测到浏览器最后一个系统页面已关闭，开始计时",
			req.ClosedAtMS, req.TimeoutAtMS, req.IdleTTLSeconds,
		)
		s.logRepo.LogAction(userID, username, "browser_close_timer_started", "", detail)
		fmt.Printf("[browser-close] user=%s detail=%s\n", username, detail)
	}

	s.executeBrowserCloseTimeout(userID, username, req)
}

func (s *AppService) executeBrowserCloseTimeout(userID int64, username string, req models.BrowserCloseEventReq) {
	s.CancelBrowserCloseStatesByUser(userID)
	_, _ = s.db.Exec(`DELETE FROM auth_tokens WHERE user_id=?`, userID)

	detail := utils.FormatBrowserCloseEventDetail(
		"页面关闭超时，后端已自动清理该账号全部 Token 与项目会话缓存",
		req.ClosedAtMS, req.TimeoutAtMS, req.IdleTTLSeconds,
	)
	s.logRepo.LogAction(userID, username, "logout", "", detail)
	fmt.Printf("[browser-close-timeout-auto] user=%s detail=%s\n", username, detail)
}

func (s *AppService) CancelBrowserCloseState(token string) (bool, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return false, false
	}

	s.browserCloseMu.Lock()
	state := s.browserCloseStates[token]
	if state != nil {
		delete(s.browserCloseStates, token)
	}
	s.browserCloseMu.Unlock()

	if state == nil {
		return false, false
	}

	s.stopBrowserCloseTimer(state.StartTimer)
	s.stopBrowserCloseTimer(state.TimeoutTimer)
	return true, state.StartedLog
}

func (s *AppService) CancelBrowserCloseStatesByUser(userID int64) {
	if userID <= 0 {
		return
	}

	toStop := make([]*models.BrowserCloseState, 0)
	s.browserCloseMu.Lock()
	for token, state := range s.browserCloseStates {
		if state == nil || state.User.ID != userID {
			continue
		}
		toStop = append(toStop, state)
		delete(s.browserCloseStates, token)
	}
	s.browserCloseMu.Unlock()

	for _, state := range toStop {
		s.stopBrowserCloseTimer(state.StartTimer)
		s.stopBrowserCloseTimer(state.TimeoutTimer)
	}
}

func (s *AppService) LogAction(userID int64, username, action, projectType, detail string) {
	s.logRepo.LogAction(userID, username, action, projectType, detail)
}

func (s *AppService) GetProjectCredential(userID int64, projectType string) (string, string, error) {
	return s.getProjectCredential(userID, projectType)
}

func (s *AppService) GetBatchFiles(projectType string) ([]map[string]string, string, error) {
	if projectType != "ad" {
		return nil, "", errors.New("批量文件仅支持AD项目")
	}

	files, err := project.BatchExcelFiles()
	if err != nil {
		return nil, "", err
	}

	items := make([]map[string]string, 0, len(files))
	for _, name := range files {
		items = append(items, map[string]string{
			"name": name,
			"path": filepath.Join(project.BatchUploadDir(), name),
		})
	}
	return items, project.BatchUploadDir(), nil
}

func (s *AppService) GetBatchTemplatePath(projectType string) (string, error) {
	if projectType != "ad" {
		return "", errors.New("批量模板仅支持AD项目")
	}

	path := project.BatchTemplatePath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("模板文件不存在")
		}
		return "", err
	}
	return path, nil
}

type BatchUploadResult struct {
	Name         string
	OriginalName string
	Path         string
}

func (s *AppService) UploadBatchFile(projectType, oldFile string, fileHeader interface{}, fileReader io.Reader) (*BatchUploadResult, error) {
	if projectType != "ad" {
		return nil, errors.New("批量上传仅支持AD项目")
	}

	if err := os.MkdirAll(project.BatchUploadDir(), 0o755); err != nil {
		return nil, err
	}

	var filename string
	var ok bool
	if filename, ok = fileHeader.(string); !ok {
		return nil, errors.New("无效的文件名")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".xlsx" && ext != ".xls" {
		return nil, errors.New("仅支持上传 xlsx/.xls 文件")
	}

	storedName := fmt.Sprintf("ad_batch_%d%s", time.Now().UnixNano(), ext)
	outPath := filepath.Join(project.BatchUploadDir(), storedName)
	outFile, err := os.Create(outPath)
	if err != nil {
		return nil, err
	}
	defer outFile.Close()

	if _, err = io.Copy(outFile, fileReader); err != nil {
		return nil, errors.New("保存文件失败")
	}

	if oldFile != "" {
		_ = os.Remove(filepath.Join(project.BatchUploadDir(), oldFile))
	}

	return &BatchUploadResult{
		Name:         storedName,
		OriginalName: filename,
		Path:         outPath,
	}, nil
}

func (s *AppService) getProjectCredential(userID int64, projectType string) (string, string, error) {
	var account, password string
	err := s.db.QueryRow(
		`SELECT account,password FROM project_credentials WHERE user_id=? AND project_type=?`,
		userID, projectType,
	).Scan(&account, &password)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", errors.New("项目凭据未配置")
		}
		return "", "", err
	}

	password, err = utils.DecryptCredentialPassword(password, s.cfg.CredentialKey)
	if err != nil {
		return "", "", errors.New("凭据解密失败")
	}

	if strings.TrimSpace(account) == "" || strings.TrimSpace(password) == "" {
		return "", "", errors.New("项目凭据未配置")
	}
	return account, password, nil
}

func (s *AppService) operateWithProjectSession(entry *ManagedProjectSession, action string, params map[string]interface{}) (project.Result, error) {
	if entry == nil || entry.Session == nil {
		return project.Result{}, errors.New("project session not initialized")
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	if entry.ProjectType == "vpn" {
		params["__vpn_account"] = entry.Username
		params["__vpn_password"] = entry.Password
	}
	entry.LastUsedAt = time.Now()
	return entry.Session.Operate(action, params)
}

func (s *AppService) normalizeBrowserCloseEventReq(req models.BrowserCloseEventReq) models.BrowserCloseEventReq {
	idleTTLSeconds := req.IdleTTLSeconds
	if idleTTLSeconds <= 0 {
		idleTTLSeconds = int(s.cfg.SessionIdleTTL.Seconds())
	}
	req.IdleTTLSeconds = idleTTLSeconds
	if req.TimeoutAtMS <= 0 && req.ClosedAtMS > 0 && idleTTLSeconds > 0 {
		req.TimeoutAtMS = req.ClosedAtMS + int64(idleTTLSeconds)*1000
	}
	return req
}

func (s *AppService) stopBrowserCloseTimer(timer interface{}) {
	if timer == nil {
		return
	}
	if t, ok := timer.(*time.Timer); ok && t != nil {
		t.Stop()
	}
}

type ManagedProjectSession struct {
	Token       string
	UserID      int64
	ProjectType string
	Username    string
	Password    string
	Session     project.Session
	LoadedAt    time.Time
	LastUsedAt  time.Time
}

type ProjectSessionManager struct {
	mu       sync.Mutex
	sessions map[string]map[string]*ManagedProjectSession
}

func NewProjectSessionManager() *ProjectSessionManager {
	return &ProjectSessionManager{
		sessions: make(map[string]map[string]*ManagedProjectSession),
	}
}

func (m *ProjectSessionManager) Ensure(
	user *models.AuthedUser,
	projectType, username, password string,
	ttl time.Duration,
	forceRelogin bool,
) (*ManagedProjectSession, bool, string, error) {
	now := time.Now()

	m.mu.Lock()
	existing := m.getLocked(user.Token, projectType)
	if existing != nil && !forceRelogin && !m.isExpiredLocked(existing, username, password, ttl, now) {
		existing.LastUsedAt = now
		m.mu.Unlock()
		return existing, false, "", nil
	}
	if existing != nil {
		m.removeLocked(user.Token, projectType)
	}
	m.mu.Unlock()

	session, message, err := project.OpenSession(projectType, username, password)
	if err != nil {
		if message == "" {
			message = utils.LoginFailureMessage(projectType)
		}
		return nil, false, message, err
	}
	if session == nil {
		return nil, false, "", fmt.Errorf("unknown project type: %s", projectType)
	}

	entry := &ManagedProjectSession{
		Token:       user.Token,
		UserID:      user.ID,
		ProjectType: projectType,
		Username:    username,
		Password:    password,
		Session:     session,
		LoadedAt:    now,
		LastUsedAt:  now,
	}

	var stale project.Session
	m.mu.Lock()
	current := m.getLocked(user.Token, projectType)
	if current != nil {
		stale = current.Session
	}
	m.setLocked(entry)
	m.mu.Unlock()

	if stale != nil && stale != session {
		_ = stale.Close()
	}
	return entry, true, message, nil
}

func (m *ProjectSessionManager) getLocked(token, projectType string) *ManagedProjectSession {
	items := m.sessions[token]
	if items == nil {
		return nil
	}
	return items[projectType]
}

func (m *ProjectSessionManager) setLocked(entry *ManagedProjectSession) {
	items := m.sessions[entry.Token]
	if items == nil {
		items = make(map[string]*ManagedProjectSession)
		m.sessions[entry.Token] = items
	}
	items[entry.ProjectType] = entry
}

func (m *ProjectSessionManager) removeLocked(token, projectType string) {
	items := m.sessions[token]
	if items == nil {
		return
	}
	item := items[projectType]
	delete(items, projectType)
	if len(items) == 0 {
		delete(m.sessions, token)
	}
	if item != nil && item.Session != nil {
		go func(session project.Session) {
			_ = session.Close()
		}(item.Session)
	}
}

func (m *ProjectSessionManager) isExpiredLocked(item *ManagedProjectSession, username, password string, ttl time.Duration, now time.Time) bool {
	if item == nil || item.Session == nil {
		return true
	}
	if item.Username != username || item.Password != password {
		return true
	}
	if ttl > 0 && now.Sub(item.LoadedAt) >= ttl {
		return true
	}
	return false
}

func (m *ProjectSessionManager) ClearToken(token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	toClose := make([]project.Session, 0)
	m.mu.Lock()
	if items, ok := m.sessions[token]; ok {
		for _, item := range items {
			if item != nil && item.Session != nil {
				toClose = append(toClose, item.Session)
			}
		}
		delete(m.sessions, token)
	}
	m.mu.Unlock()
	for _, one := range toClose {
		_ = one.Close()
	}
}

func (m *ProjectSessionManager) ClearUserProject(userID int64, projectType string) {
	m.clearMatching(func(item *ManagedProjectSession) bool {
		return item.UserID == userID && item.ProjectType == projectType
	})
}

func (m *ProjectSessionManager) clearMatching(match func(*ManagedProjectSession) bool) {
	toClose := make([]project.Session, 0)
	m.mu.Lock()
	for token, items := range m.sessions {
		for projectType, item := range items {
			if item == nil || !match(item) {
				continue
			}
			if item.Session != nil {
				toClose = append(toClose, item.Session)
			}
			delete(items, projectType)
		}
		if len(items) == 0 {
			delete(m.sessions, token)
		}
	}
	m.mu.Unlock()
	for _, one := range toClose {
		_ = one.Close()
	}
}

func CloneInterfaceMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]interface{}, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]interface{}
	if err = json.Unmarshal(b, &out); err != nil {
		out = make(map[string]interface{}, len(in))
		for k, v := range in {
			out[k] = v
		}
	}
	return out
}
