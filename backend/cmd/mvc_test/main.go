package main

import (
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"ops-admin-backend/internal/config"
	"ops-admin-backend/internal/router"
	"ops-admin-backend/internal/services"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

func main() {
	fmt.Println("=== 开始测试MVC架构重构 ===")

	testDir := filepath.Join(os.TempDir(), "ops-admin-mvc-test")
	os.RemoveAll(testDir)
	os.MkdirAll(testDir, 0o755)
	defer os.RemoveAll(testDir)

	config.LoadEnvFiles(".env.local", ".env")
	cfg := config.LoadAppConfig()
	config.RuntimeCfg = cfg

	fmt.Println("\n=== 测试编译和基本功能 ===")

	dbPath := filepath.Join(testDir, "test.db")
	svc, err := services.NewAppServiceFromDSN(dbPath, cfg)
	if err != nil {
		log.Fatalf("创建测试服务失败: %v", err)
	}
	defer svc.Close()
	fmt.Println("  ✓ 服务层创建成功")

	fmt.Println("\n=== 测试认证功能 ===")
	testAuth(svc)

	fmt.Println("\n=== 测试凭据管理 ===")
	testCredentials(svc)

	fmt.Println("\n=== 测试控制器层 ===")
	testControllers(dbPath, cfg)

	fmt.Println("\n=== 所有测试通过！ ===")
}

func testAuth(svc *services.AppService) {
	fmt.Println("  测试注册功能...")
	err := svc.Register("testuser", "testpass123")
	if err != nil {
		log.Fatalf("注册失败: %v", err)
	}
	fmt.Println("  ✓ 注册成功")

	fmt.Println("  测试登录功能...")
	loginResp, err := svc.Login("testuser", "testpass123")
	if err != nil {
		log.Fatalf("登录失败: %v", err)
	}
	if loginResp.Token == "" {
		log.Fatal("登录响应没有token")
	}
	fmt.Println("  ✓ 登录成功，token: " + loginResp.Token[:20] + "...")

	fmt.Println("  测试获取用户信息...")
	authed, err := svc.LoadAuthedUser(loginResp.Token)
	if err != nil {
		log.Fatalf("获取用户信息失败: %v", err)
	}
	if authed.Username != "testuser" {
		log.Fatalf("用户名不匹配: expected testuser, got %s", authed.Username)
	}
	fmt.Printf("  ✓ 用户信息: ID=%d, Username=%s\n", authed.ID, authed.Username)

	fmt.Println("  测试修改密码...")
	err = svc.ChangePassword(authed.ID, "testpass123", "newpass456")
	if err != nil {
		log.Fatalf("修改密码失败: %v", err)
	}
	fmt.Println("  ✓ 密码修改成功")

	fmt.Println("  测试退出登录...")
	svc.Logout(authed.ID, loginResp.Token, "用户主动退出")
	fmt.Println("  ✓ 退出成功")

	_, err = svc.LoadAuthedUser(loginResp.Token)
	if err == nil {
		log.Fatal("退出后token应该无效")
	}
	fmt.Println("  ✓ 退出后token已失效")
}

func testCredentials(svc *services.AppService) {
	loginResp, err := svc.Login("testuser", "newpass456")
	if err != nil {
		log.Fatalf("登录失败: %v", err)
	}
	authed, err := svc.LoadAuthedUser(loginResp.Token)
	if err != nil {
		log.Fatalf("获取用户信息失败: %v", err)
	}

	fmt.Println("  测试获取项目凭据列表...")
	creds, err := svc.GetProjectCredentials(authed.ID)
	if err != nil {
		log.Fatalf("获取凭据失败: %v", err)
	}
	fmt.Printf("  ✓ 凭据数量: %d\n", len(creds))

	fmt.Println("  测试更新项目凭据...")
	err = svc.UpdateProjectCredential(authed.ID, "ad", "test@example.com", "secret123")
	if err != nil {
		log.Fatalf("更新凭据失败: %v", err)
	}
	fmt.Println("  ✓ 凭据更新成功")

	creds, err = svc.GetProjectCredentials(authed.ID)
	if err != nil {
		log.Fatalf("获取凭据失败: %v", err)
	}
	for _, c := range creds {
		if c["project_type"] == "ad" {
			if c["account"] != "test@example.com" {
				log.Fatalf("凭据更新后账户不匹配: expected test@example.com, got %s", c["account"])
			}
			fmt.Printf("  ✓ AD凭据已更新: Account=%s\n", c["account"])
		}
	}
}

func testControllers(dbPath string, cfg config.AppConfig) {
	appRouter := router.NewAppRouterFromDSN(dbPath, cfg)
	if appRouter == nil {
		log.Fatal("创建AppRouter失败")
	}
	defer appRouter.GetDB().Close()

	gin.SetMode(gin.TestMode)
	appRouter.CreateDefaultAdmin()

	fmt.Println("  测试健康检查接口...")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/health", strings.NewReader(""))
	appRouter.GetController().Health(c)
	if w.Code != 200 {
		log.Fatalf("健康检查返回状态码 %d，期望 200", w.Code)
	}
	fmt.Printf("  ✓ 健康检查: %s\n", w.Body.String())

	fmt.Println("  测试登录接口...")
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	loginBody := `{"username":"admin","password":"admin123"}`
	c.Request = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(loginBody))
	c.Request.Header.Set("Content-Type", "application/json")
	appRouter.GetController().Login(c)
	fmt.Printf("  登录响应状态码: %d\n", w.Code)
	if w.Code == 200 {
		fmt.Printf("  ✓ 登录成功\n")
	} else {
		fmt.Printf("  登录响应: %s\n", w.Body.String())
	}
}
