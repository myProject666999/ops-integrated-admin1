package runtime

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/controllers"
	"ops-admin-backend/internal/project"
	"ops-admin-backend/internal/services"

	_ "modernc.org/sqlite"
)

type authedUser struct {
	ID       int64
	Username string
	Token    string
}

type appConfig struct {
	ADAPIURL        string
	PrintAPIURL     string
	VPNSshAddr      string
	FirewallSSHAddr string
	CredentialKey   string
	ProjectCacheTTL time.Duration
	SessionIdleTTL  time.Duration
	StaticDir       string
}

type server struct {
	db                 *sql.DB
	tokenTTL           time.Duration
	cfg                appConfig
	jobMu              sync.Mutex
	jobs               map[string]*asyncOperateJob
	projectSessions    *projectSessionManager
	browserCloseLogMu  sync.Mutex
	browserCloseStates map[string]*browserCloseState

	appService    *services.AppService
	appController *controllers.AppController
}

type apiError struct {
	Error string `json:"error"`
}

type logRow struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Action      string `json:"action"`
	ProjectType string `json:"project_type"`
	Detail      string `json:"detail"`
	CreatedAt   string `json:"created_at"`
}

type browserCloseState struct {
	user         authedUser
	req          browserCloseEventReq
	startedLog   bool
	startTimer   *time.Timer
	timeoutTimer *time.Timer
}

var runtimeCfg appConfig

const credentialCipherPrefix = "enc:v1:"
const browserCloseLogGracePeriod = 3 * time.Second

func Run() {
	config.LoadEnvFiles(".env.local", ".env", "../.env")
	cfg := config.LoadAppConfig()
	config.RuntimeCfg = cfg

	runtimeCfg = appConfig{
		ADAPIURL:        cfg.ADAPIURL,
		PrintAPIURL:     cfg.PrintAPIURL,
		VPNSshAddr:      cfg.VPNSshAddr,
		FirewallSSHAddr: cfg.FirewallSSHAddr,
		CredentialKey:   cfg.CredentialKey,
		ProjectCacheTTL: cfg.ProjectCacheTTL,
		SessionIdleTTL:  cfg.SessionIdleTTL,
		StaticDir:       cfg.StaticDir,
	}

	project.SetConfig(project.Config{
		ADAPIURL:        cfg.ADAPIURL,
		PrintAPIURL:     cfg.PrintAPIURL,
		VPNSshAddr:      cfg.VPNSshAddr,
		FirewallSSHAddr: cfg.FirewallSSHAddr,
	})

	dbPath := filepath.Clean("./db/ops_admin.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("prepare sqlite dir failed: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		log.Printf("set busy_timeout failed: %v", err)
	}

	if err = initDB(db, runtimeCfg); err != nil {
		log.Fatalf("init db failed: %v", err)
	}

	appService := services.NewAppService(db, cfg)
	appController := controllers.NewAppController(appService, db)

	if err = ensureDefaultAdmin(db); err != nil {
		log.Printf("ensure default admin failed: %v", err)
	} else {
		log.Println("初始化管理员账号完成，账号：admin，密码：admin123")
	}

	srv := &server{
		db:                 db,
		tokenTTL:           24 * time.Hour,
		cfg:                runtimeCfg,
		jobs:               make(map[string]*asyncOperateJob),
		projectSessions:    newProjectSessionManager(),
		browserCloseStates: make(map[string]*browserCloseState),
		appService:         appService,
		appController:      appController,
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	router := srv.setupRouter()
	log.Printf("backend started on %s, sqlite=%s", addr, dbPath)
	if err = router.Run(addr); err != nil {
		log.Fatalf("listen failed: %v", err)
	}
}

func ensureDefaultAdmin(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM admins`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	svc, err := services.NewAppServiceFromDSN(filepath.Clean("./db/ops_admin.db"), config.RuntimeCfg)
	if err != nil {
		return err
	}
	defer svc.Close()
	return svc.Register("admin", "admin123")
}
