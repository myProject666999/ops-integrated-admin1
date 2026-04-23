package services

import (
	"database/sql"
	"errors"
	"fmt"
	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/repositories"
	"ops-admin-backend/internal/utils"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type AppServices struct {
	DB *sql.DB

	AdminRepo      *repositories.AdminRepository
	CredentialRepo *repositories.CredentialRepository
	LogRepo        *repositories.LogRepository
	AsyncJobRepo   *repositories.AsyncJobRepository
}

func NewAppServices(db *sql.DB) *AppServices {
	return &AppServices{
		DB:             db,
		AdminRepo:      repositories.NewAdminRepository(db),
		CredentialRepo: repositories.NewCredentialRepository(db),
		LogRepo:        repositories.NewLogRepository(db),
		AsyncJobRepo:   repositories.NewAsyncJobRepository(),
	}
}

func (s *AppServices) GetAdminCount() (int, error) {
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(1) FROM admins`).Scan(&count)
	return count, err
}

func (s *AppServices) FindAdminByUsername(username string) (userID int64, hash string, err error) {
	err = s.DB.QueryRow(
		`SELECT id,password_hash FROM admins WHERE username=?`,
		username,
	).Scan(&userID, &hash)
	return userID, hash, err
}

func (s *AppServices) VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *AppServices) CreateAuthToken(userID int64, ttl time.Duration) (string, time.Time, error) {
	token, err := utils.RandomToken(48)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(ttl)
	now := time.Now().Format(time.RFC3339)
	_, err = s.DB.Exec(
		`INSERT INTO auth_tokens(token,user_id,expires_at,created_at) VALUES(?,?,?,?)`,
		token, userID, exp.Format(time.RFC3339), now,
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

func (s *AppServices) LoadAuthedUser(token, now string) (*models.AuthedUser, error) {
	var u models.AuthedUser
	err := s.DB.QueryRow(
		`SELECT a.id,a.username,t.token FROM auth_tokens t JOIN admins a ON a.id=t.user_id WHERE t.token=? AND t.expires_at>?`,
		token, now,
	).Scan(&u.ID, &u.Username, &u.Token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return &u, nil
}

func (s *AppServices) CreateAdmin(username, password string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	now := time.Now().Format(time.RFC3339)
	res, err := s.DB.Exec(
		`INSERT INTO admins(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`,
		username, string(hash), now, now,
	)
	if err != nil {
		return 0, err
	}
	userID, _ := res.LastInsertId()
	return userID, nil
}

func (s *AppServices) LogAction(userID int64, username, action, projectType, detail string) {
	detail = utils.NormalizeGarbledText(detail)
	_, _ = s.DB.Exec(
		`INSERT INTO operation_logs(user_id,username,action,project_type,detail,created_at) VALUES(?,?,?,?,?,?)`,
		userID, username, action, projectType, detail, time.Now().Format(time.RFC3339),
	)
}

func (s *AppServices) EnsureDefaultProjectCredentialsForUser(userID int64) error {
	for _, p := range []string{"ad", "print", "vpn", "vpn_firewall"} {
		if _, err := s.DB.Exec(
			`INSERT OR IGNORE INTO project_credentials(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)`,
			userID, p, "", "", time.Now().Format(time.RFC3339),
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *AppServices) CleanupUserAuthTokens(userID int64) error {
	_, err := s.DB.Exec(`DELETE FROM auth_tokens WHERE user_id=?`, userID)
	return err
}

func (s *AppServices) GetProjectCredentials(userID int64) ([]models.ProjectCredentialResponse, error) {
	rows, err := s.DB.Query(
		`SELECT project_type,account,password,updated_at FROM project_credentials WHERE user_id=? ORDER BY project_type`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.ProjectCredentialResponse, 0)
	for rows.Next() {
		var t, account, password, updated string
		if err = rows.Scan(&t, &account, &password, &updated); err != nil {
			return nil, err
		}
		plainPwd, decErr := utils.DecryptCredentialPassword(password, config.RuntimeCfg.CredentialKey)
		if decErr != nil {
			return nil, decErr
		}
		items = append(items, models.ProjectCredentialResponse{
			ProjectType: t,
			Account:     account,
			Password:    plainPwd,
			UpdatedAt:   updated,
		})
	}
	return items, nil
}

func (s *AppServices) UpdateProjectCredential(userID int64, projectType, account, password string) error {
	encryptedPwd, err := utils.EncryptCredentialPassword(password, config.RuntimeCfg.CredentialKey)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`INSERT INTO project_credentials(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(user_id,project_type) DO UPDATE SET account=excluded.account,password=excluded.password,updated_at=excluded.updated_at`,
		userID, projectType, account, encryptedPwd, time.Now().Format(time.RFC3339),
	)
	return err
}

func (s *AppServices) GetLogs(page, pageSize int, projectType string) (*models.LogsResponse, error) {
	offset := (page - 1) * pageSize

	where := ""
	args := make([]interface{}, 0)
	if projectType != "" {
		where = ` WHERE project_type=?`
		args = append(args, projectType)
	}

	var total int64
	countQuery := `SELECT COUNT(1) FROM operation_logs` + where
	if err := s.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	query := `SELECT id,COALESCE(user_id,0),COALESCE(username,''),COALESCE(action,''),COALESCE(project_type,''),COALESCE(detail,''),created_at FROM operation_logs` +
		where + ` ORDER BY id DESC LIMIT ? OFFSET ?`

	finalArgs := make([]interface{}, 0, len(args)+2)
	finalArgs = append(finalArgs, args...)
	finalArgs = append(finalArgs, pageSize, offset)

	rows, err := s.DB.Query(query, finalArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.LogRow, 0)
	for rows.Next() {
		var row models.LogRow
		if err = rows.Scan(&row.ID, &row.UserID, &row.Username, &row.Action, &row.ProjectType, &row.Detail, &row.CreatedAt); err != nil {
			return nil, err
		}
		row.Detail = utils.NormalizeGarbledText(row.Detail)
		items = append(items, row)
	}

	return &models.LogsResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *AppServices) UpdateAdminPassword(userID int64, oldPassword, newPassword string) error {
	var oldHash string
	err := s.DB.QueryRow(`SELECT password_hash FROM admins WHERE id=?`, userID).Scan(&oldHash)
	if err != nil {
		return err
	}
	if !s.VerifyPassword(oldHash, oldPassword) {
		return errors.New("原密码错误")
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`UPDATE admins SET password_hash=?,updated_at=? WHERE id=?`,
		string(newHash), time.Now().Format(time.RFC3339), userID,
	)
	return err
}

func (s *AppServices) EnsureDefaultProjectCredentialsForAllUsers() error {
	rows, err := s.DB.Query(`SELECT id FROM admins`)
	if err != nil {
		return err
	}
	defer rows.Close()

	userIDs := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err = rows.Scan(&userID); err != nil {
			return err
		}
		userIDs = append(userIDs, userID)
	}
	if err = rows.Err(); err != nil {
		return err
	}

	for _, userID := range userIDs {
		if err = s.EnsureDefaultProjectCredentialsForUser(userID); err != nil {
			return err
		}
	}
	return nil
}

func CredentialTitle(projectType string) string {
	switch projectType {
	case "ad":
		return "AD"
	case "print":
		return "打印"
	case "vpn":
		return "VPN"
	case "vpn_firewall":
		return "防火墙"
	default:
		return projectType
	}
}

func FormatUnixMilliForLog(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func FormatBrowserCloseCancelDetail(req models.BrowserCloseEventReq) string {
	closedAt := FormatUnixMilliForLog(req.ClosedAtMS)
	timeoutAt := FormatUnixMilliForLog(req.TimeoutAtMS)
	reopenedAt := FormatUnixMilliForLog(req.ReopenedAtMS)
	return fmt.Sprintf("浏览器系统页面已重新打开，取消页面关闭超时计时，开始触发时间：%s，超时时长：%d 秒，原超时时间：%s，重新打开时间：%s", closedAt, req.IdleTTLSeconds, timeoutAt, reopenedAt)
}
