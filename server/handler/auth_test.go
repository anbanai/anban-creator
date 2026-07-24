package handler

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
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
