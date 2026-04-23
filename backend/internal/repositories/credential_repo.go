package repositories

import (
	"database/sql"
	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/utils"
)

type CredentialRepository struct {
	db *sql.DB
}

func NewCredentialRepository(db *sql.DB) *CredentialRepository {
	return &CredentialRepository{db: db}
}

func (r *CredentialRepository) EnsureDefaultProjectCredentialsForUser(userID int64) error {
	for _, p := range []string{"ad", "print", "vpn", "vpn_firewall"} {
		if _, err := r.db.Exec(
			`INSERT OR IGNORE INTO project_credentials(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)`,
			userID, p, "", "", utils.NowStr(),
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *CredentialRepository) EnsureDefaultProjectCredentialsForAllUsers() error {
	rows, err := r.db.Query(`SELECT id FROM admins`)
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
		if err = r.EnsureDefaultProjectCredentialsForUser(userID); err != nil {
			return err
		}
	}
	return nil
}

func (r *CredentialRepository) ListCredentials(userID int64) ([]models.ProjectCredentialResponse, error) {
	rows, err := r.db.Query(
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

func (r *CredentialRepository) GetProjectCredential(userID int64, projectType string) (string, string, error) {
	var account, password string
	err := r.db.QueryRow(
		`SELECT account,password FROM project_credentials WHERE user_id=? AND project_type=?`,
		userID, projectType,
	).Scan(&account, &password)
	if err != nil {
		return "", "", err
	}
	password, err = utils.DecryptCredentialPassword(password, config.RuntimeCfg.CredentialKey)
	if err != nil {
		return "", "", err
	}
	if account == "" || password == "" {
		return "", "", nil
	}
	return account, password, nil
}

func (r *CredentialRepository) UpdateCredential(userID int64, projectType string, account, password string) error {
	encryptedPwd, err := utils.EncryptCredentialPassword(password, config.RuntimeCfg.CredentialKey)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(
		`INSERT INTO project_credentials(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(user_id,project_type) DO UPDATE SET account=excluded.account,password=excluded.password,updated_at=excluded.updated_at`,
		userID, projectType, account, encryptedPwd, utils.NowStr(),
	)
	return err
}
