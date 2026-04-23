package repositories

import (
	"database/sql"
	"errors"
	"ops-admin-backend/internal/models"
	"ops-admin-backend/internal/utils"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type AdminRepository struct {
	db *sql.DB
}

func NewAdminRepository(db *sql.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

func (r *AdminRepository) GetAdminCount() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(1) FROM admins`).Scan(&count)
	return count, err
}

func (r *AdminRepository) FindByUsername(username string) (*models.Admin, error) {
	var admin models.Admin
	var createdAt, updatedAt string
	err := r.db.QueryRow(
		`SELECT id,username,password_hash,created_at,updated_at FROM admins WHERE username=?`,
		username,
	).Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	admin.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	admin.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &admin, nil
}

func (r *AdminRepository) VerifyPassword(admin *models.Admin, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)) == nil
}

func (r *AdminRepository) Create(username, password string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	now := utils.NowStr()
	res, err := r.db.Exec(
		`INSERT INTO admins(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`,
		username, string(hash), now, now,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (r *AdminRepository) FindByID(id int64) (*models.Admin, error) {
	var admin models.Admin
	var createdAt, updatedAt string
	err := r.db.QueryRow(
		`SELECT id,username,password_hash,created_at,updated_at FROM admins WHERE id=?`,
		id,
	).Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	admin.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	admin.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &admin, nil
}

func (r *AdminRepository) UpdatePassword(id int64, oldPassword, newPassword string) error {
	admin, err := r.FindByID(id)
	if err != nil {
		return err
	}
	if !r.VerifyPassword(admin, oldPassword) {
		return errors.New("原密码错误")
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(
		`UPDATE admins SET password_hash=?,updated_at=? WHERE id=?`,
		string(newHash), utils.NowStr(), id,
	)
	return err
}

func (r *AdminRepository) CreateAuthToken(userID int64, token string, expireAt time.Time) error {
	now := utils.NowStr()
	_, err := r.db.Exec(
		`INSERT INTO auth_tokens(token,user_id,expires_at,created_at) VALUES(?,?,?,?)`,
		token, userID, expireAt.Format(time.RFC3339), now,
	)
	return err
}

func (r *AdminRepository) LoadAuthedUser(token, now string) (*models.AuthedUser, error) {
	var u models.AuthedUser
	err := r.db.QueryRow(
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

func (r *AdminRepository) CleanupAuthToken(token string) error {
	_, err := r.db.Exec(`DELETE FROM auth_tokens WHERE token=?`, token)
	return err
}

func (r *AdminRepository) CleanupUserAuthTokens(userID int64) error {
	_, err := r.db.Exec(`DELETE FROM auth_tokens WHERE user_id=?`, userID)
	return err
}

func (r *AdminRepository) GetTokenTTL() time.Duration {
	return 24 * time.Hour
}

func (r *AdminRepository) GenerateToken(length int) (string, error) {
	return utils.RandomToken(length)
}
