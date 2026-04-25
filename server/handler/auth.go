package handler

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/royalrick/anbanwriter/server/auth"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AuthHandler handles authentication-related HTTP endpoints.
type AuthHandler struct {
	jwtSvc           *auth.JWTService
	wechatSvc        *auth.WeChatService
	repo             repository.Repository
	emailSvc         *service.EmailService
	logger           *zerolog.Logger
	hub              *WebSocketHub
	inviteEnabled    bool
	maxInvitePerUser int
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(
	jwtSvc *auth.JWTService,
	wechatSvc *auth.WeChatService,
	repo repository.Repository,
	emailSvc *service.EmailService,
	logger *zerolog.Logger,
	hub *WebSocketHub,
	inviteEnabled bool,
	maxInvitePerUser int,
) *AuthHandler {
	return &AuthHandler{
		jwtSvc:           jwtSvc,
		wechatSvc:        wechatSvc,
		repo:             repo,
		emailSvc:         emailSvc,
		logger:           logger,
		hub:              hub,
		inviteEnabled:    inviteEnabled,
		maxInvitePerUser: maxInvitePerUser,
	}
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type registerRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	Code       string `json:"code"`
	Nickname   string `json:"nickname,omitempty"`
	InviteCode string `json:"invite_code"`
}

type sendCodeRequest struct {
	Email string `json:"email"`
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

const inviteCodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// requireDB returns 503 if the repository (database) is unavailable.
func (h *AuthHandler) requireDB(c fiber.Ctx) error {
	if h.repo == nil {
		return Error(c, fiber.StatusServiceUnavailable, "database is not available")
	}
	return nil
}

// generateInviteCode generates a random 8-character invite code using crypto/rand.
// Excludes ambiguous characters: O/0, I/1/L for readability.
func generateInviteCode() (string, error) {
	result := make([]byte, 8)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteCodeChars))))
		if err != nil {
			return "", err
		}
		result[i] = inviteCodeChars[n.Int64()]
	}
	return string(result), nil
}

// generateTokenPair creates an access token, a refresh token, and a LoginSession.
// Precondition: h.repo must be non-nil (callers must check via requireDB).
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

// SendCode handles POST /api/v1/auth/send-code.
func (h *AuthHandler) SendCode(c fiber.Ctx) error {
	if err := h.requireDB(c); err != nil {
		return err
	}

	var req sendCodeRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.Email = strings.TrimSpace(req.Email)

	if req.Email == "" || !strings.Contains(req.Email, "@") {
		return Error(c, fiber.StatusBadRequest, "请输入有效的邮箱地址")
	}

	if h.emailSvc == nil {
		return Error(c, fiber.StatusServiceUnavailable, "邮件服务未启用")
	}

	if err := h.emailSvc.SendVerificationCode(c.Context(), req.Email); err != nil {
		var userErr *service.UserError
		if errors.As(err, &userErr) {
			return Error(c, fiber.StatusBadRequest, userErr.Msg)
		}
		h.logger.Error().Err(err).Str("email", req.Email).Msg("failed to send verification code")
		return Error(c, fiber.StatusInternalServerError, "发送验证码失败，请稍后重试")
	}

	return Success(c, fiber.Map{"msg": "验证码已发送"})
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c fiber.Ctx) error {
	if err := h.requireDB(c); err != nil {
		return err
	}

	var req registerRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)
	req.Code = strings.TrimSpace(req.Code)
	req.InviteCode = strings.TrimSpace(strings.ToUpper(req.InviteCode))

	if req.Email == "" || req.Password == "" {
		return Error(c, fiber.StatusBadRequest, "email and password are required")
	}

	// Verification code is required when email service is configured.
	if h.emailSvc != nil && req.Code == "" {
		return Error(c, fiber.StatusBadRequest, "验证码不能为空")
	}

	if req.Email != "" && !strings.Contains(req.Email, "@") {
		return Error(c, fiber.StatusBadRequest, "invalid email format")
	}

	if len(req.Password) < 8 {
		return Error(c, fiber.StatusBadRequest, "password must be at least 8 characters")
	}

	// Verify the email verification code.
	if h.emailSvc != nil {
		ok, err := h.emailSvc.VerifyCode(c.Context(), req.Email, req.Code)
		if err != nil {
			h.logger.Warn().Err(err).Str("email", req.Email).Msg("verification failed")
			return Error(c, fiber.StatusTooManyRequests, err.Error())
		}
		if !ok {
			return Error(c, fiber.StatusBadRequest, "验证码错误或已过期")
		}
	}

	// Validate invite code when invitation is enabled.
	var inviter *model.User
	if h.inviteEnabled {
		if req.InviteCode == "" {
			return Error(c, fiber.StatusBadRequest, "请输入邀请码")
		}
		var err error
		inviter, err = h.repo.Users().FindByInviteCode(c.Context(), req.InviteCode)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return Error(c, fiber.StatusBadRequest, "邀请码无效")
			}
			h.logger.Error().Err(err).Str("invite_code", req.InviteCode).Msg("failed to look up invite code")
			return Error(c, fiber.StatusInternalServerError, "internal error")
		}
		if inviter.InviteCount >= h.maxInvitePerUser {
			return Error(c, fiber.StatusForbidden, "该邀请码已达使用上限")
		}
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

	inviteCode, err := generateInviteCode()
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to generate invite code")
		return Error(c, fiber.StatusInternalServerError, "internal error")
	}

	user := &model.User{
		ID:         uuid.New().String(),
		Email:      req.Email,
		Nickname:   nickname,
		Password:   string(hashed),
		InviteCode: inviteCode,
	}
	if inviter != nil {
		user.InvitedBy = inviter.ID
	}

	// Use transaction for user creation + invite count increment.
	if inviter != nil {
		if err := h.repo.WithTx(ctx, func(txRepo repository.Repository) error {
			if err := txRepo.Users().Create(ctx, user); err != nil {
				return err
			}
			ok, err := txRepo.Users().IncrementInviteCount(ctx, inviter.ID, h.maxInvitePerUser)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("invite limit reached")
			}
			return nil
		}); err != nil {
			if err.Error() == "invite limit reached" {
				return Error(c, fiber.StatusForbidden, "该邀请码已达使用上限")
			}
			h.logger.Error().Err(err).Msg("failed to create user in transaction")
			return Error(c, fiber.StatusInternalServerError, "failed to create user")
		}
	} else {
		if err := h.repo.Users().Create(ctx, user); err != nil {
			h.logger.Error().Err(err).Msg("failed to create user")
			return Error(c, fiber.StatusInternalServerError, "failed to create user")
		}
	}

	resp, err := h.generateTokenPair(c, user.ID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to generate tokens")
	}

	return Success(c, resp)
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c fiber.Ctx) error {
	if err := h.requireDB(c); err != nil {
		return err
	}

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
	if err := h.requireDB(c); err != nil {
		return err
	}

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
	if err := h.requireDB(c); err != nil {
		return err
	}

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
	if err := h.requireDB(c); err != nil {
		return err
	}

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

	// Auto-generate invite code for existing users who don't have one.
	if user.InviteCode == "" {
		inviteCode, err := generateInviteCode()
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to generate invite code for existing user")
		} else {
			user.InviteCode = inviteCode
			if err := h.repo.Users().Update(c.Context(), user); err != nil {
				h.logger.Error().Err(err).Str("user_id", user.ID).Msg("failed to save invite code for existing user")
			}
		}
	}

	return Success(c, fiber.Map{
		"id":                   user.ID,
		"email":                user.Email,
		"phone":                user.Phone,
		"nickname":             user.Nickname,
		"avatar":               user.Avatar,
		"credits_balance":      user.CreditsBalance,
		"tier":                 tier,
		"max_concurrent_limit": model.GetTierMaxConcurrentTasks(tier),
		"invite_code":          user.InviteCode,
		"invite_count":         user.InviteCount,
		"max_invites":          h.maxInvitePerUser,
		"created_at":           user.CreatedAt,
		"updated_at":           user.UpdatedAt,
	})
}

// WXLogin handles POST /api/v1/auth/wx-login.
func (h *AuthHandler) WXLogin(c fiber.Ctx) error {
	if err := h.requireDB(c); err != nil {
		return err
	}

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
		inviteCode, err := generateInviteCode()
		if err != nil {
			h.logger.Error().Err(err).Msg("failed to generate invite code for wx login")
			return Error(c, fiber.StatusInternalServerError, "internal error")
		}
		user = &model.User{
			ID:         uuid.New().String(),
			OpenID:     wxSession.OpenID,
			UnionID:    wxSession.UnionID,
			InviteCode: inviteCode,
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
