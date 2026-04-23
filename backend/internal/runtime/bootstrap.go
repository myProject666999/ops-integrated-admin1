package runtime

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ops-admin-backend/internal/project"

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
	loadEnvFiles(".env", "../.env")
	cfg := loadAppConfig()
	runtimeCfg = cfg
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

	if err = initDB(db, cfg); err != nil {
		log.Fatalf("init db failed: %v", err)
	}

	srv := &server{
		db:                 db,
		tokenTTL:           24 * time.Hour,
		cfg:                cfg,
		jobs:               make(map[string]*asyncOperateJob),
		projectSessions:    newProjectSessionManager(),
		browserCloseStates: make(map[string]*browserCloseState),
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
