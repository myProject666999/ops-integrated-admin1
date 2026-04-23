package runtime

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite"
)

func setupTestServer(t *testing.T) (*server, *gin.Engine, func()) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := appConfig{
		ADAPIURL:        "http://ad.example.internal/",
		PrintAPIURL:     "http://print.example.internal/printhub/",
		VPNSshAddr:      "vpn.example.internal",
		FirewallSSHAddr: "firewall.example.internal",
		CredentialKey:   "test-credential-key-32bytes-long!!",
		ProjectCacheTTL: 10 * time.Minute,
		SessionIdleTTL:  60 * time.Minute,
		StaticDir:       "./static",
	}

	db, err := initTestDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}

	srv := &server{
		db:                 db,
		tokenTTL:           24 * time.Hour,
		cfg:                cfg,
		jobs:               make(map[string]*asyncOperateJob),
		projectSessions:    newProjectSessionManager(),
		browserCloseStates: make(map[string]*browserCloseState),
	}

	router := srv.setupRouter()

	cleanup := func() {
		db.Close()
		os.RemoveAll(tempDir)
	}

	return srv, router, cleanup
}

func initTestDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := initDB(db, appConfig{CredentialKey: "test-key"}); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func TestHealthEndpoint(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/health", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestRegisterAndLogin(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Test register
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Test login
	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var loginResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &loginResp)
	assert.NoError(t, err)
	assert.NotEmpty(t, loginResp["token"])
	assert.Equal(t, "testuser", loginResp["username"])
}

func TestLoginWithInvalidCredentials(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// First register a user
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// Try login with wrong password
	loginBody := map[string]string{
		"username": "testuser",
		"password": "wrongpassword",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetMeUnauthorized(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/auth/me", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetMeWithValidToken(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Register and login
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)

	// Test /me endpoint
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var meResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &meResp)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", meResp["username"])
}

func TestChangePassword(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Register and login
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)

	// Change password
	changePwdBody := map[string]string{
		"old_password": "testpassword123",
		"new_password": "newpassword456",
	}
	body, _ = json.Marshal(changePwdBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/change-password", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Login with new password
	loginBody = map[string]string{
		"username": "testuser",
		"password": "newpassword456",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProjectCredentials(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Register and login
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)

	// Update credentials
	credBody := map[string]string{
		"account":  "testaccount",
		"password": "testcredpassword",
	}
	body, _ = json.Marshal(credBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/projects/credentials/ad", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Get credentials
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/projects/credentials", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var credsResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &credsResp)
	assert.NoError(t, err)
	items := credsResp["items"].([]interface{})
	assert.GreaterOrEqual(t, len(items), 1)
}

func TestLogsEndpoint(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Register and login
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)

	// Get logs
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/logs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var logsResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &logsResp)
	assert.NoError(t, err)
	assert.NotNil(t, logsResp["items"])
	assert.NotNil(t, logsResp["total"])
}

func TestInvalidProjectType(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Register and login
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)

	// Try to load invalid project type
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/projects/invalid/load", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLogout(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Register and login
	registerBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ := json.Marshal(registerBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/auth/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	loginBody := map[string]string{
		"username": "testuser",
		"password": "testpassword123",
	}
	body, _ = json.Marshal(loginBody)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"].(string)

	// Logout
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Try to use token after logout
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
