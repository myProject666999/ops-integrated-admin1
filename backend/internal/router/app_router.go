package router

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/controllers"
	"ops-admin-backend/internal/repositories"
	"ops-admin-backend/internal/services"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type AppRouter struct {
	db         *sql.DB
	service    *services.AppService
	controller *controllers.AppController
	engine     *gin.Engine
}

func NewAppRouter(db *sql.DB, cfg config.AppConfig) *AppRouter {
	service := services.NewAppService(db, cfg)
	controller := controllers.NewAppController(service, db)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	ar := &AppRouter{
		db:         db,
		service:    service,
		controller: controller,
		engine:     r,
	}
	ar.setupRoutes()
	return ar
}

func NewAppRouterFromDSN(dsn string, cfg config.AppConfig) *AppRouter {
	service, err := services.NewAppServiceFromDSN(dsn, cfg)
	if err != nil {
		return nil
	}

	controller := controllers.NewAppController(service, service.GetDB())

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	ar := &AppRouter{
		db:         service.GetDB(),
		service:    service,
		controller: controller,
		engine:     r,
	}
	ar.setupRoutes()
	return ar
}

func (ar *AppRouter) setupRoutes() {
	r := ar.engine
	authRequired := ar.controller.AuthMiddleware()

	r.GET("/health", ar.controller.Health)

	api := r.Group("/api")
	{
		api.GET("/health", ar.controller.Health)

		auth := api.Group("/auth")
		{
			auth.POST("/login", ar.controller.Login)
			auth.POST("/register", ar.controller.Register)
			auth.GET("/me", authRequired, ar.controller.Me)
			auth.POST("/logout", authRequired, ar.controller.Logout)
			auth.POST("/change-password", authRequired, ar.controller.ChangePassword)
			auth.POST("/window-close-start", authRequired, ar.controller.WindowCloseStart)
			auth.POST("/window-close-cancel", authRequired, ar.controller.WindowCloseCancel)
		}

		api.GET("/projects/credentials", authRequired, ar.controller.GetProjectCredentials)
		api.PUT("/projects/credentials/:project_type", authRequired, ar.controller.UpdateProjectCredential)
		api.POST("/projects/relogin", authRequired, ar.controller.ReloginProjects)

		projects := api.Group("/projects")
		projects.Use(authRequired)
		{
			projects.POST("/:project_type/load", ar.controller.LoadProject)
			projects.GET("/:project_type/batch-template", ar.controller.DownloadBatchTemplate)
			projects.POST("/:project_type/batch-upload", ar.controller.UploadBatchFile)
			projects.GET("/:project_type/batch-files", ar.controller.GetBatchFiles)
			projects.POST("/:project_type/operate", ar.controller.OperateProject)
		}

		api.POST("/projects/operate-async", authRequired, ar.controller.StartAsyncOperate)
		api.GET("/projects/operate-async/:job_id", authRequired, ar.controller.GetAsyncJobStatus)

		api.GET("/logs", authRequired, ar.controller.GetLogs)
	}

	r.NoRoute(ar.serveStatic)
}

func (ar *AppRouter) serveStatic(ctx *gin.Context) {
	staticDir := ar.service.GetConfig().StaticDir
	if staticDir == "" {
		staticDir = "./static"
	}
	staticDir = filepath.Clean(staticDir)

	requestPath := ctx.Request.URL.Path
	if requestPath == "/" {
		requestPath = "/index.html"
	}

	filePath := filepath.Join(staticDir, requestPath)
	filePath = filepath.Clean(filePath)

	if !strings.HasPrefix(filePath, staticDir+string(filepath.Separator)) && filePath != staticDir {
		if strings.HasPrefix(requestPath, "/api/") {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		ctx.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			indexPath := filepath.Join(staticDir, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				ctx.File(indexPath)
				return
			}
			ctx.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}

	if info.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if _, indexErr := os.Stat(indexPath); indexErr == nil {
			ctx.File(indexPath)
			return
		}
		ctx.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	ctx.File(filePath)
}

func (ar *AppRouter) GetEngine() *gin.Engine {
	return ar.engine
}

func (ar *AppRouter) GetService() *services.AppService {
	return ar.service
}

func (ar *AppRouter) GetController() *controllers.AppController {
	return ar.controller
}

func (ar *AppRouter) GetDB() *sql.DB {
	return ar.db
}

func (ar *AppRouter) CreateDefaultAdmin() error {
	var count int
	if err := ar.db.QueryRow(`SELECT COUNT(1) FROM admins`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	err := ar.service.Register("admin", "admin123")
	if err != nil {
		return err
	}
	return nil
}

func (ar *AppRouter) InitDefaultCredentials() error {
	return repositories.NewCredentialRepository(ar.db).EnsureDefaultProjectCredentialsForAllUsers()
}

func corsMiddleware() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
