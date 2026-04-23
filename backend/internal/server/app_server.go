package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/router"

	_ "modernc.org/sqlite"
)

func Run() {
	config.LoadEnvFiles(".env.local", ".env")
	cfg := config.LoadAppConfig()
	config.RuntimeCfg = cfg

	dbPath := filepath.Clean("./db/ops_admin.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("准备数据库目录失败: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		log.Printf("设置 busy_timeout 失败: %v", err)
	}

	if err = initDB(db, cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	appRouter := router.NewAppRouter(db, cfg)

	if err = appRouter.CreateDefaultAdmin(); err != nil {
		log.Printf("创建默认管理员失败: %v", err)
	} else {
		fmt.Println("初始化管理员账号完成，账号：admin，密码：admin123")
	}

	if err = appRouter.InitDefaultCredentials(); err != nil {
		log.Printf("初始化项目凭据失败: %v", err)
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr:    addr,
		Handler: appRouter.GetEngine(),
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("服务器运行在 %s, 数据库: %s", addr, dbPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("启动失败: %v", err)
		}
	}()

	<-stop
	log.Println("正在关闭服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("服务器关闭失败: %v", err)
	}

	log.Println("服务器已正常关闭")
}

func initDB(db *sql.DB, cfg config.AppConfig) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admins (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admins_username ON admins(username)`,
		`CREATE TABLE IF NOT EXISTS auth_tokens (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES admins(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_tokens_user ON auth_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_tokens_expires_at ON auth_tokens(expires_at)`,
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
		`CREATE INDEX IF NOT EXISTS idx_project_credentials_user ON project_credentials(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_credentials_type ON project_credentials(project_type)`,
		`CREATE TABLE IF NOT EXISTS operation_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			username TEXT,
			action TEXT NOT NULL,
			project_type TEXT,
			detail TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_op_logs_user ON operation_logs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_op_logs_project ON operation_logs(project_type)`,
		`CREATE INDEX IF NOT EXISTS idx_op_logs_action ON operation_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_op_logs_created_at ON operation_logs(created_at)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("执行迁移语句失败: %w", err)
		}
	}

	if err := migrateProjectCredentialsSchema(db); err != nil {
		return err
	}
	if err := migrateAuthTokensSchema(db); err != nil {
		return err
	}
	if err := dropLegacyProjectLoadStateTable(db); err != nil {
		return err
	}
	if err := encryptLegacyProjectCredentialPasswords(db, cfg.CredentialKey); err != nil {
		return err
	}
	return nil
}

func migrateAuthTokensSchema(db *sql.DB) error {
	hasLastSeen, err := tableHasColumn(db, "auth_tokens", "last_seen_at")
	if err != nil {
		return err
	}
	if !hasLastSeen {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS auth_tokens_new (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY(user_id) REFERENCES admins(id)
	)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO auth_tokens_new(token,user_id,expires_at,created_at)
		SELECT token,user_id,expires_at,created_at FROM auth_tokens`); err != nil {
		return err
	}
	if _, err = tx.Exec(`DROP TABLE auth_tokens`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE auth_tokens_new RENAME TO auth_tokens`); err != nil {
		return err
	}
	return tx.Commit()
}

func dropLegacyProjectLoadStateTable(db *sql.DB) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS project_load_state`)
	return err
}

func migrateProjectCredentialsSchema(db *sql.DB) error {
	hasUserID, err := tableHasColumn(db, "project_credentials", "user_id")
	if err != nil {
		return err
	}
	if hasUserID {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS project_credentials_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		project_type TEXT NOT NULL,
		account TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL,
		UNIQUE(user_id, project_type),
		FOREIGN KEY(user_id) REFERENCES admins(id)
	)`); err != nil {
		return err
	}

	var defaultUserID int64
	hasDefaultUser := true
	if err = tx.QueryRow(`SELECT id FROM admins ORDER BY id ASC LIMIT 1`).Scan(&defaultUserID); err != nil {
		if err == sql.ErrNoRows {
			hasDefaultUser = false
		} else {
			return err
		}
	}

	rows, err := tx.Query(`SELECT project_type,account,password,updated_at FROM project_credentials`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var projectType, account, password, updatedAt string
		if err = rows.Scan(&projectType, &account, &password, &updatedAt); err != nil {
			return err
		}
		if updatedAt == "" {
			updatedAt = time.Now().Format(time.RFC3339)
		}
		if hasDefaultUser {
			if _, err = tx.Exec(
				`INSERT OR IGNORE INTO project_credentials_new(user_id,project_type,account,password,updated_at) VALUES(?,?,?,?,?)`,
				defaultUserID, projectType, account, password, updatedAt,
			); err != nil {
				return err
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()

	if _, err = tx.Exec(`DROP TABLE project_credentials`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE project_credentials_new RENAME TO project_credentials`); err != nil {
		return err
	}
	return tx.Commit()
}

func tableHasColumn(db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue interface{}
		if err = rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func encryptLegacyProjectCredentialPasswords(db *sql.DB, key string) error {
	rows, err := db.Query(`SELECT rowid,password FROM project_credentials`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type credentialRow struct {
		ID       int64
		Password string
	}
	all := make([]credentialRow, 0)
	for rows.Next() {
		var item credentialRow
		if err = rows.Scan(&item.ID, &item.Password); err != nil {
			return err
		}
		all = append(all, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, item := range all {
		pwd := strings.TrimSpace(item.Password)
		if pwd == "" || strings.HasPrefix(pwd, config.CredentialCipherPrefix) {
			continue
		}
		encrypted, encErr := config.EncryptLegacyPassword(pwd, key)
		if encErr != nil {
			return encErr
		}
		if _, err = db.Exec(
			`UPDATE project_credentials SET password=?,updated_at=? WHERE rowid=?`,
			encrypted, time.Now().Format(time.RFC3339), item.ID,
		); err != nil {
			return err
		}
	}
	return nil
}
