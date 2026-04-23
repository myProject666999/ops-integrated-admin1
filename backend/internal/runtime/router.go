package runtime

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (s *server) setupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	authRequired := s.appController.AuthMiddleware()

	r.GET("/health", s.appController.Health)

	api := r.Group("/api")
	{
		api.GET("/health", s.appController.Health)

		auth := api.Group("/auth")
		{
			auth.POST("/login", s.appController.Login)
			auth.POST("/register", s.appController.Register)
			auth.GET("/me", authRequired, s.appController.Me)
			auth.POST("/window-close-start", authRequired, s.appController.WindowCloseStart)
			auth.POST("/window-close-cancel", authRequired, s.appController.WindowCloseCancel)
			auth.POST("/logout", authRequired, s.appController.Logout)
			auth.POST("/change-password", authRequired, s.appController.ChangePassword)
		}

		api.GET("/projects/credentials", authRequired, s.appController.GetProjectCredentials)
		api.PUT("/projects/credentials/:project_type", authRequired, s.appController.UpdateProjectCredential)
		api.POST("/projects/relogin", authRequired, s.appController.ReloginProjects)

		projects := api.Group("/projects")
		projects.Use(authRequired)
		{
			projects.POST("/:project_type/load", s.appController.LoadProject)
			projects.GET("/:project_type/batch-template", s.appController.DownloadBatchTemplate)
			projects.POST("/:project_type/batch-upload", s.appController.UploadBatchFile)
			projects.GET("/:project_type/batch-files", s.appController.GetBatchFiles)
			projects.POST("/:project_type/operate", s.appController.OperateProject)
		}

		api.POST("/projects/operate-async", authRequired, s.appController.StartAsyncOperate)
		api.GET("/projects/operate-async/:job_id", authRequired, s.appController.GetAsyncJobStatus)

		api.GET("/logs", authRequired, s.appController.GetLogs)
	}

	r.NoRoute(s.handleStatic)

	return r
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

func (s *server) requireAuthGin() gin.HandlerFunc {
	return s.appController.AuthMiddleware()
}

func getAuthedUserGin(c *gin.Context) authedUser {
	u, _ := c.Get("user")
	if user, ok := u.(authedUser); ok {
		return user
	}
	return authedUser{}
}

func (s *server) handleHealth(c *gin.Context) {
	s.appController.Health(c)
}

func (s *server) handleStatic(c *gin.Context) {
	staticDir := s.cfg.StaticDir
	if staticDir == "" {
		staticDir = "./static"
	}
	staticDir = filepath.Clean(staticDir)

	requestPath := c.Request.URL.Path
	if requestPath == "/" {
		requestPath = "/index.html"
	}

	filePath := filepath.Join(staticDir, requestPath)
	filePath = filepath.Clean(filePath)

	if !strings.HasPrefix(filePath, staticDir+string(filepath.Separator)) && filePath != staticDir {
		if strings.HasPrefix(requestPath, "/api/") {
			c.JSON(http.StatusNotFound, apiError{Error: "接口不存在"})
			return
		}
		c.JSON(http.StatusNotFound, apiError{Error: "文件不存在"})
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			indexPath := filepath.Join(staticDir, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				c.File(indexPath)
				return
			}
			c.JSON(http.StatusNotFound, apiError{Error: "文件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, apiError{Error: "服务器错误"})
		return
	}

	if info.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if _, indexErr := os.Stat(indexPath); indexErr == nil {
			c.File(indexPath)
			return
		}
		c.JSON(http.StatusNotFound, apiError{Error: "文件不存在"})
		return
	}

	c.File(filePath)
}
