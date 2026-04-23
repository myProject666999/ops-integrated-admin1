package runtime

import (
	"net/http"
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

	api := r.Group("/api")
	{
		api.GET("/health", s.handleHealth)

		auth := api.Group("/auth")
		{
			auth.POST("/login", s.handleLoginGin)
			auth.POST("/register", s.handleRegisterGin)
			auth.GET("/me", s.requireAuthGin(), s.handleMeGin)
			auth.POST("/window-close-start", s.requireAuthGin(), s.handleWindowCloseStartGin)
			auth.POST("/window-close-cancel", s.requireAuthGin(), s.handleWindowCloseCancelGin)
			auth.POST("/logout", s.requireAuthGin(), s.handleLogoutGin)
			auth.POST("/change-password", s.requireAuthGin(), s.handleChangePasswordGin)
		}

		api.GET("/projects/credentials", s.requireAuthGin(), s.handleProjectCredentialsGin)
		api.PUT("/projects/credentials/:project_type", s.requireAuthGin(), s.handleProjectCredentialByTypeGin)
		api.POST("/projects/relogin", s.requireAuthGin(), s.handleProjectsReloginGin)
		api.POST("/projects/operate-async", s.requireAuthGin(), s.handleProjectOperateAsyncStartGin)
		api.GET("/projects/operate-async/:job_id", s.requireAuthGin(), s.handleProjectOperateAsyncStatusGin)

		projects := api.Group("/projects")
		projects.Use(s.requireAuthGin())
		{
			projects.POST("/:project_type/load", s.handleProjectLoadGin)
			projects.GET("/:project_type/batch-template", s.handleProjectBatchTemplateGin)
			projects.POST("/:project_type/batch-upload", s.handleProjectBatchUploadGin)
			projects.GET("/:project_type/batch-files", s.handleProjectBatchFilesGin)
			projects.POST("/:project_type/operate", s.handleProjectOperateGin)
		}

		api.GET("/logs", s.requireAuthGin(), s.handleLogsGin)
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
	return func(c *gin.Context) {
		token := extractBearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.JSON(http.StatusUnauthorized, apiError{Error: "缺少认证令牌"})
			c.Abort()
			return
		}
		now := time.Now().Format(time.RFC3339)
		u, err := s.loadAuthedUser(token, now)
		if err != nil {
			c.JSON(http.StatusUnauthorized, apiError{Error: "令牌无效或已过期"})
			c.Abort()
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

func getAuthedUserGin(c *gin.Context) authedUser {
	u, _ := c.Get("user")
	if user, ok := u.(authedUser); ok {
		return user
	}
	return authedUser{}
}

func (s *server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleStatic(c *gin.Context) {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/") {
		c.JSON(http.StatusNotFound, apiError{Error: "接口不存在"})
		return
	}
	s.serveStatic(c.Writer, c.Request)
}
