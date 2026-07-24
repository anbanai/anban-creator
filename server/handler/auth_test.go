package handler

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAuthUserCreationUsesProvisioningService(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve auth test path")
	}
	authPath := filepath.Join(filepath.Dir(currentFile), "auth.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), authPath, nil, 0)
	if err != nil {
		t.Fatalf("parse auth.go: %v", err)
	}

	creationEntrypoints := map[string]bool{
		"Register":        false,
		"CodeLogin":       false,
		"WXLogin":         false,
		"QRLoginCallback": false,
	}
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil {
			continue
		}
		if _, tracked := creationEntrypoints[fn.Name.Name]; !tracked {
			continue
		}

		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "createUser" {
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "h" {
					creationEntrypoints[fn.Name.Name] = true
				}
			}
			if selector.Sel.Name == "Create" && isAuthUsersCall(selector.X) {
				t.Errorf("%s directly creates a user; call h.createUser instead", fn.Name.Name)
			}
			return true
		})
	}

	for entrypoint, usesProvisioning := range creationEntrypoints {
		if !usesProvisioning {
			t.Errorf("%s does not call h.createUser", entrypoint)
		}
	}
}

func isAuthUsersCall(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Users"
}

func TestAuthNewUserCreationProvisionsWallet(t *testing.T) {
	t.Run("Register", func(t *testing.T) {
		_, repo := newAuthProvisioningTestRepository(t)
		handler := newAuthProvisioningTestHandler(t, repo, nil, nil)
		response := postAuthRequest(t, handler.Register, `{"email":"register@example.com","password":"password123"}`)
		assertAuthSuccess(t, response)

		user, err := repo.Users().FindByEmail(context.Background(), "register@example.com")
		if err != nil {
			t.Fatalf("find registered user: %v", err)
		}
		assertAuthEmptyWallet(t, repo, user)
	})

	t.Run("CodeLogin", func(t *testing.T) {
		_, repo := newAuthProvisioningTestRepository(t)
		email := "code-login@example.com"
		code := "123456"
		emailService := newAuthProvisioningEmailService(t, email, code)
		handler := newAuthProvisioningTestHandler(t, repo, emailService, nil)
		response := postAuthRequest(t, handler.CodeLogin, `{"email":"`+email+`","code":"`+code+`"}`)
		assertAuthSuccess(t, response)

		user, err := repo.Users().FindByEmail(context.Background(), email)
		if err != nil {
			t.Fatalf("find code-login user: %v", err)
		}
		assertAuthEmptyWallet(t, repo, user)
	})

	t.Run("WXLogin", func(t *testing.T) {
		_, repo := newAuthProvisioningTestRepository(t)
		openID := "wx-open-id"
		wechatService := newAuthProvisioningWeChatService(t, openID)
		handler := newAuthProvisioningTestHandler(t, repo, nil, wechatService)
		response := postAuthRequest(t, handler.WXLogin, `{"code":"wx-code","nickname":"WX User"}`)
		assertAuthSuccess(t, response)

		user, err := repo.Users().FindByOpenID(context.Background(), openID)
		if err != nil {
			t.Fatalf("find WeChat user: %v", err)
		}
		assertAuthEmptyWallet(t, repo, user)
	})

	t.Run("QRLoginCallback", func(t *testing.T) {
		_, repo := newAuthProvisioningTestRepository(t)
		openID := "qr-open-id"
		wechatService := newAuthProvisioningWeChatService(t, openID)
		handler := newAuthProvisioningTestHandler(t, repo, nil, wechatService)
		ctx := context.Background()
		if err := handler.qrStore.Create(ctx, "qr-scene"); err != nil {
			t.Fatalf("create QR scene: %v", err)
		}
		if _, transitioned, err := handler.qrStore.CompareAndSet(ctx, "qr-scene", qrStatusPending, qrStatusScanned); err != nil || !transitioned {
			t.Fatalf("scan QR scene: transitioned=%v err=%v", transitioned, err)
		}

		response := postAuthRequest(t, handler.QRLoginCallback, `{"scene":"qr-scene","code":"qr-code","nickname":"QR User"}`)
		assertAuthSuccess(t, response)

		user, err := repo.Users().FindByOpenID(ctx, openID)
		if err != nil {
			t.Fatalf("find QR user: %v", err)
		}
		assertAuthEmptyWallet(t, repo, user)
	})
}

func TestAuthRegisterRollsBackUserWhenWalletProvisioningFails(t *testing.T) {
	db, repo := newAuthProvisioningTestRepository(t)
	if err := db.Exec(`CREATE TRIGGER reject_auth_wallet BEFORE INSERT ON billing_wallet_accounts
		BEGIN SELECT RAISE(ABORT, 'forced auth wallet failure'); END`).Error; err != nil {
		t.Fatalf("create wallet trigger: %v", err)
	}
	handler := newAuthProvisioningTestHandler(t, repo, nil, nil)
	response := postAuthRequest(t, handler.Register, `{"email":"wallet-failure@example.com","password":"password123"}`)
	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusInternalServerError)
	}
	if _, err := repo.Users().FindByEmail(context.Background(), "wallet-failure@example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("user lookup error = %v, want record not found", err)
	}
	var walletCount int64
	if err := db.Model(&model.BillingWalletAccount{}).Count(&walletCount).Error; err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	if walletCount != 0 {
		t.Fatalf("wallet count = %d, want 0", walletCount)
	}
}

func TestCodeLoginRecoversFromMySQLDuplicateKeyRace(t *testing.T) {
	_, baseRepo := newAuthProvisioningTestRepository(t)
	ctx := context.Background()
	email := "duplicate-race@example.com"
	existing := &model.User{
		ID:         uuid.NewString(),
		Email:      email,
		Nickname:   "Concurrent Winner",
		Password:   "",
		InviteCode: "RACEWIN1",
	}
	if err := baseRepo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.Users().Create(ctx, existing); err != nil {
			return err
		}
		return tx.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: existing.ID})
	}); err != nil {
		t.Fatalf("seed concurrent winner: %v", err)
	}

	racingUsers := &duplicateRaceUserRepository{UserRepository: baseRepo.Users(), email: email}
	racingRepo := &duplicateRaceRepository{
		Repository: baseRepo,
		users:      racingUsers,
		err:        &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry for key users.email"},
	}
	code := "654321"
	emailService := newAuthProvisioningEmailService(t, email, code)
	handler := newAuthProvisioningTestHandler(t, racingRepo, emailService, nil)
	response := postAuthRequest(t, handler.CodeLogin, `{"email":"`+email+`","code":"`+code+`"}`)
	assertAuthSuccess(t, response)
	var payload struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode code-login response: %v", err)
	}
	if payload.Data.User.ID != existing.ID {
		t.Fatalf("response user ID = %q, want concurrent winner %q", payload.Data.User.ID, existing.ID)
	}
	if racingUsers.findByEmailCalls != 2 {
		t.Fatalf("FindByEmail calls = %d, want 2", racingUsers.findByEmailCalls)
	}
	assertAuthEmptyWallet(t, baseRepo, existing)
}

type duplicateRaceRepository struct {
	repository.Repository
	users repository.UserRepository
	err   error
}

func (r *duplicateRaceRepository) Users() repository.UserRepository {
	return r.users
}

func (r *duplicateRaceRepository) WithTx(context.Context, func(repository.Repository) error) error {
	return r.err
}

type duplicateRaceUserRepository struct {
	repository.UserRepository
	email            string
	findByEmailCalls int
}

func (r *duplicateRaceUserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	r.findByEmailCalls++
	if email == r.email && r.findByEmailCalls == 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return r.UserRepository.FindByEmail(ctx, email)
}

func newAuthProvisioningTestRepository(t *testing.T) (*gorm.DB, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open auth database: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate auth database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open auth sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db, repository.New(db)
}

func newAuthProvisioningTestHandler(t *testing.T, repo repository.Repository, emailService *service.EmailService, wechatService *auth.WeChatService) *AuthHandler {
	t.Helper()
	jwtService, err := auth.NewJWTService("auth-provisioning-secret", "1h", "24h")
	if err != nil {
		t.Fatalf("create JWT service: %v", err)
	}
	logger := zerolog.New(io.Discard)
	handler := NewAuthHandler(jwtService, wechatService, nil, repo, emailService, &logger, nil, false, 3, nil)
	if store, ok := handler.qrStore.(*memoryQRStateStore); ok {
		t.Cleanup(store.close)
	}
	return handler
}

func newAuthProvisioningEmailService(t *testing.T, email, code string) *service.EmailService {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	if err := redisClient.Set(context.Background(), "verify:"+email, code, 5*time.Minute).Err(); err != nil {
		t.Fatalf("store verification code: %v", err)
	}
	logger := zerolog.New(io.Discard)
	return service.NewEmailService(&config.EmailConfig{CodeTTL: 5 * time.Minute, CodeLength: len(code)}, redisClient, &logger)
}

func newAuthProvisioningWeChatService(t *testing.T, openID string) *auth.WeChatService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(auth.WxSession{OpenID: openID, UnionID: "union-" + openID, SessionKey: "session-key"})
	}))
	t.Cleanup(server.Close)
	logger := zerolog.New(io.Discard)
	wechatService := auth.NewWeChatService("test-app", "test-secret", &logger)
	wechatService.SetBaseURL(server.URL)
	return wechatService
}

func postAuthRequest(t *testing.T, handler fiber.Handler, body string) *http.Response {
	t.Helper()
	app := fiber.New()
	app.Post("/", handler)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("auth request: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func assertAuthSuccess(t *testing.T, response *http.Response) {
	t.Helper()
	if response.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d: %s", response.StatusCode, fiber.StatusOK, body)
	}
}

func assertAuthEmptyWallet(t *testing.T, repo repository.Repository, user *model.User) {
	t.Helper()
	account, err := repo.Billing().FindAccount(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find wallet for user %q: %v", user.ID, err)
	}
	if account.UserID != user.ID || account.PaidCredits != 0 || account.PromotionalCredits != 0 || account.DebtCredits != 0 || account.Version != 0 {
		t.Fatalf("wallet = %+v, want empty account for user %q", account, user.ID)
	}
}

func TestGenerateTokenPairDoesNotExposeWalletLedger(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	userID := "auth-wallet-ledger"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "auth-billing@example.com",
		Password:   "hashed",
		InviteCode: "authmult",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	jwtSvc, err := auth.NewJWTService("test-secret", "1h", "24h")
	if err != nil {
		t.Fatalf("jwt service: %v", err)
	}
	logger := zerolog.New(io.Discard)
	h := NewAuthHandler(jwtSvc, nil, nil, repo, nil, &logger, nil, false, 0, nil)

	resp, err := h.generateTokenPair(ctx, userID)
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(data), "credits_balance") || strings.Contains(string(data), "billing_multiplier") {
		t.Fatalf("token response leaked wallet projection: %s", data)
	}
}
