package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AuthHandler handles authentication-related HTTP endpoints.
type AuthHandler struct {
	jwtSvc    *auth.JWTService
	wechatSvc *auth.WeChatService
	repo      repository.Repository
	logger    *zerolog.Logger
	hub       *WebSocketHub
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(
	jwtSvc *auth.JWTService,
	wechatSvc *auth.WeChatService,
	repo repository.Repository,
	logger *zerolog.Logger,
	hub *WebSocketHub,
) *AuthHandler {
	return &AuthHandler{
		jwtSvc:    jwtSvc,
		wechatSvc: wechatSvc,
		repo:      repo,
		logger:    logger,
		hub:       hub,
	}
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Nickname string `json:"nickname,omitempty"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type wxLoginRequest struct {
	Code     string `json:"code"`
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

type tokenResponse struct {
	Token        string      `json:"token"`
	RefreshToken string      `json:"refresh_token"`
	ExpiresAt    int64       `json:"expires_at"`
	User         *model.User `json:"user"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateTokenPair creates an access token, a refresh token, and a LoginSession.
func (h *AuthHandler) generateTokenPair(ctx any, userID string) (*tokenResponse, error) {
	accessToken, err := h.jwtSvc.GenerateAccessToken(userID)
	if err != nil {
		return nil, err
	}

	refreshToken, err := h.jwtSvc.GenerateRefreshToken(userID)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(h.jwtSvc.AccessExpiry())
	session := &model.LoginSession{
		ID:           uuid.New().String(),
		UserID:       userID,
		Token:        accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}

	fiberCtx, ok := ctx.(fiber.Ctx)
	var errCtx error
	if ok {
		errCtx = h.repo.Sessions().Create(fiberCtx.Context(), session)
	} else {
		errCtx = h.repo.Sessions().Create(nil, session)
	}
	if errCtx != nil {
		h.logger.Error().Err(errCtx).Msg("failed to create login session")
		// Non-fatal: still return tokens
	}

	// Fetch the user for the response.
	var user *model.User
	if ok {
		user, err = h.repo.Users().FindByID(fiberCtx.Context(), userID)
	} else {
		user, err = h.repo.Users().FindByID(nil, userID)
	}
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to find user for token response")
	}

	return &tokenResponse{
		Token:        accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt.Unix(),
		User:         user,
	}, nil
}

// ---------------------------------------------------------------------------
// Endpoint handlers
// ---------------------------------------------------------------------------

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req registerRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)

	if req.Email == "" || req.Password == "" {
		return Error(c, fiber.StatusBadRequest, "email and password are required")
	}

	if req.Email != "" && !strings.Contains(req.Email, "@") {
		return Error(c, fiber.StatusBadRequest, "invalid email format")
	}

	if len(req.Password) < 6 {
		return Error(c, fiber.StatusBadRequest, "password must be at least 6 characters")
	}

	// Check if user already exists.
	ctx := c.Context()
	existing, err := h.repo.Users().FindByEmail(ctx, req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.logger.Error().Err(err).Str("email", req.Email).Msg("failed to check existing user")
		return Error(c, fiber.StatusInternalServerError, "internal error")
	}
	if existing != nil {
		return Error(c, fiber.StatusConflict, "email already registered")
	}

	// Hash password.
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to hash password")
		return Error(c, fiber.StatusInternalServerError, "internal error")
	}

	nickname := req.Nickname
	if nickname == "" {
		nickname = strings.Split(req.Email, "@")[0]
	}

	user := &model.User{
		ID:       uuid.New().String(),
		Email:    req.Email,
		Nickname: nickname,
		Password: string(hashed),
	}

	if err := h.repo.Users().Create(ctx, user); err != nil {
		h.logger.Error().Err(err).Msg("failed to create user")
		return Error(c, fiber.StatusInternalServerError, "failed to create user")
	}

	resp, err := h.generateTokenPair(c, user.ID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to generate tokens")
	}

	return Success(c, resp)
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req loginRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)

	if req.Email == "" || req.Password == "" {
		return Error(c, fiber.StatusBadRequest, "email and password are required")
	}

	ctx := c.Context()

	// Try email first, then phone.
	user, err := h.repo.Users().FindByEmail(ctx, req.Email)
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		user, err = h.repo.Users().FindByPhone(ctx, req.Email)
	}
	if err != nil {
		return Error(c, fiber.StatusUnauthorized, "invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return Error(c, fiber.StatusUnauthorized, "invalid email or password")
	}

	resp, err := h.generateTokenPair(c, user.ID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to generate tokens")
	}

	return Success(c, resp)
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req refreshRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.RefreshToken == "" {
		return Error(c, fiber.StatusBadRequest, "refresh_token is required")
	}

	// Validate refresh JWT.
	claims, err := h.jwtSvc.ValidateToken(req.RefreshToken)
	if err != nil {
		return Error(c, fiber.StatusUnauthorized, "invalid refresh token")
	}
	if claims.UserID == "" {
		return Error(c, fiber.StatusUnauthorized, "invalid refresh token: missing user_id")
	}

	ctx := c.Context()

	// Check session exists and is not expired.
	session, err := h.repo.Sessions().FindByRefreshToken(ctx, req.RefreshToken)
	if err != nil {
		return Error(c, fiber.StatusUnauthorized, "refresh token not found or expired")
	}
	if session.ExpiresAt.Before(time.Now()) {
		return Error(c, fiber.StatusUnauthorized, "refresh token expired")
	}
	if session.UserID != claims.UserID {
		return Error(c, fiber.StatusUnauthorized, "refresh token mismatch")
	}

	// Generate new token pair.
	resp, err := h.generateTokenPair(c, claims.UserID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to generate tokens")
	}

	// Delete old session.
	if err := h.repo.Sessions().Delete(ctx, session.Token); err != nil {
		h.logger.Error().Err(err).Msg("failed to delete old session during refresh")
	}

	return Success(c, resp)
}

// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	var token string
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			token = parts[1]
		}
	}

	if token != "" {
		ctx := c.Context()
		if err := h.repo.Sessions().Delete(ctx, token); err != nil {
			h.logger.Error().Err(err).Msg("failed to delete session on logout")
		}
	}

	return Success(c, fiber.Map{"message": "logged out"})
}

// Me handles GET /api/v1/auth/me.
func (h *AuthHandler) Me(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Use the user object already stored by auth middleware to avoid a redundant DB lookup.
	user, ok := c.Locals("user").(*model.User)
	if !ok || user == nil {
		return Error(c, fiber.StatusNotFound, "user not found")
	}

	tier := model.ResolveTier(user.Tier)

	return Success(c, fiber.Map{
		"id":                   user.ID,
		"email":                user.Email,
		"phone":                user.Phone,
		"nickname":             user.Nickname,
		"avatar":               user.Avatar,
		"credits_balance":      user.CreditsBalance,
		"tier":                 tier,
		"max_concurrent_limit": model.GetTierMaxConcurrentTasks(tier),
		"created_at":           user.CreatedAt,
		"updated_at":           user.UpdatedAt,
	})
}

// WXLogin handles POST /api/v1/auth/wx-login.
func (h *AuthHandler) WXLogin(c fiber.Ctx) error {
	if h.wechatSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "WeChat login is not configured")
	}

	var req wxLoginRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Code == "" {
		return Error(c, fiber.StatusBadRequest, "code is required")
	}

	ctx := c.Context()

	// Exchange code for openID.
	wxSession, err := h.wechatSvc.Code2Session(req.Code)
	if err != nil {
		h.logger.Error().Err(err).Msg("wechat code2session failed")
		return Error(c, fiber.StatusUnauthorized, "WeChat login failed")
	}

	// Find or create user by openID.
	user, err := h.repo.Users().FindByOpenID(ctx, wxSession.OpenID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.logger.Error().Err(err).Msg("failed to find user by openID")
		return Error(c, fiber.StatusInternalServerError, "internal error")
	}

	if user == nil {
		user = &model.User{
			ID:      uuid.New().String(),
			OpenID:  wxSession.OpenID,
			UnionID: wxSession.UnionID,
		}
		if req.Nickname != "" {
			user.Nickname = req.Nickname
		}
		if req.Avatar != "" {
			user.Avatar = req.Avatar
		}
		if err := h.repo.Users().Create(ctx, user); err != nil {
			h.logger.Error().Err(err).Msg("failed to create user from WeChat login")
			return Error(c, fiber.StatusInternalServerError, "failed to create user")
		}
	} else {
		// Update profile if provided.
		updated := false
		if req.Nickname != "" && req.Nickname != user.Nickname {
			user.Nickname = req.Nickname
			updated = true
		}
		if req.Avatar != "" && req.Avatar != user.Avatar {
			user.Avatar = req.Avatar
			updated = true
		}
		if wxSession.UnionID != "" && wxSession.UnionID != user.UnionID {
			user.UnionID = wxSession.UnionID
			updated = true
		}
		if updated {
			if err := h.repo.Users().Update(ctx, user); err != nil {
				h.logger.Error().Err(err).Msg("failed to update user from WeChat login")
			}
		}
	}

	resp, err := h.generateTokenPair(c, user.ID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to generate tokens")
	}

	return Success(c, resp)
}

// GetUserID extracts the authenticated user ID from Fiber locals.
func GetUserID(c fiber.Ctx) string {
	if userID, ok := c.Locals("user_id").(string); ok {
		return userID
	}
	return ""
}
