package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"time"

	mail "github.com/wneessen/go-mail"
	"github.com/redis/go-redis/v9"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/rs/zerolog"
)

// UserError is an error intended to be shown to the end user (e.g. validation failures).
// Handlers should pass its message through instead of replacing with a generic error.
type UserError struct {
	Msg string
}

func (e *UserError) Error() string { return e.Msg }

// EmailService handles verification code generation, storage, delivery, and validation.
type EmailService struct {
	cfg    *config.EmailConfig
	rdb    *redis.Client
	logger *zerolog.Logger
}

// NewEmailService creates a new EmailService. If rdb is nil, code operations
// will silently no-op (degraded mode).
func NewEmailService(cfg *config.EmailConfig, rdb *redis.Client, logger *zerolog.Logger) *EmailService {
	return &EmailService{cfg: cfg, rdb: rdb, logger: logger}
}

const (
	maxVerifyAttempts = 5
)

func (s *EmailService) codeKey(email string) string {
	return "verify:" + email
}

func (s *EmailService) attemptKey(email string) string {
	return "verify_attempts:" + email
}

func (s *EmailService) generateCode() (string, error) {
	const digits = "0123456789"
	length := s.cfg.CodeLength
	if length <= 0 {
		length = 6
	}
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		code[i] = digits[n.Int64()]
	}
	return string(code), nil
}

// SendVerificationCode generates a random verification code, stores it in Redis
// with a TTL, and sends it via SMTP. If SMTP is not configured, the code is logged
// instead (dev mode). If Redis is nil, the operation is a no-op.
func (s *EmailService) SendVerificationCode(ctx context.Context, email string) error {
	if s.rdb == nil {
		s.logger.Warn().Str("email", email).Msg("email service unavailable: no Redis")
		return fmt.Errorf("email service unavailable")
	}

	// Check if a code already exists — enforce per-email cooldown.
	existingTTL, err := s.rdb.TTL(ctx, s.codeKey(email)).Result()
	if err == nil && existingTTL > 0 {
		return &UserError{Msg: fmt.Sprintf("验证码已发送，请 %.0f 秒后再试", existingTTL.Seconds())}
	}

	code, err := s.generateCode()
	if err != nil {
		return fmt.Errorf("generate code: %w", err)
	}

	// Check if recipient's mail server is in China.
	if !isChineseEmailDomain(email) {
		return &UserError{Msg: "暂不支持境外邮箱，请使用国内邮箱注册"}
	}

	ttl := s.cfg.CodeTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	if err := s.rdb.Set(ctx, s.codeKey(email), code, ttl).Err(); err != nil {
		return fmt.Errorf("store code: %w", err)
	}

	if s.cfg.SMTPHost == "" {
		// Dev mode: log the code at debug level.
		s.logger.Debug().
			Str("email", email).
			Str("code", code).
			Dur("ttl", ttl).
			Msg("verification code generated (SMTP not configured, dev mode)")
		return nil
	}

	fromName := s.cfg.FromName
	if fromName == "" {
		fromName = "案板创作助手"
	}
	fromAddr := s.cfg.FromAddress

	m := mail.NewMsg()
	if err := m.FromFormat(fromName, fromAddr); err != nil {
		s.rdb.Del(ctx, s.codeKey(email))
		return fmt.Errorf("set from: %w", err)
	}
	if err := m.To(email); err != nil {
		s.rdb.Del(ctx, s.codeKey(email))
		return fmt.Errorf("set to: %w", err)
	}
	m.Subject("案板创作助手 验证码")
	m.SetBodyString(mail.TypeTextPlain,
		fmt.Sprintf("您的验证码是：%s（%d 分钟内有效）\r\n\r\n如非本人操作，请忽略此邮件。", code, int(ttl.Minutes())),
	)

	opts := []mail.Option{
		mail.WithPort(s.cfg.SMTPPort),
		mail.WithUsername(s.cfg.SMTPUsername),
		mail.WithPassword(s.cfg.SMTPPassword),
		mail.WithTimeout(15 * time.Second),
		mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
	}
	if s.cfg.SMTPPort == 465 {
		// Port 465 uses implicit TLS (connection is encrypted from the start).
		opts = append(opts, mail.WithSSL())
	} else {
		// Port 587 and others use STARTTLS (upgrade after plaintext handshake).
		opts = append(opts, mail.WithTLSPolicy(mail.TLSOpportunistic))
	}

	client, err := mail.NewClient(s.cfg.SMTPHost, opts...)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create mail client")
		s.rdb.Del(ctx, s.codeKey(email))
		return fmt.Errorf("create mail client: %w", err)
	}

	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		s.logger.Error().Err(err).Str("email", email).Msg("failed to send verification email")
		s.rdb.Del(ctx, s.codeKey(email))
		return fmt.Errorf("send email: %w", err)
	}

	s.logger.Info().Str("email", email).Msg("verification code sent")
	return nil
}

// VerifyCode checks the provided code against the stored value.
// Returns true if the code matches (and is consumed). Returns false with an error
// if the account is locked due to too many attempts.
func (s *EmailService) VerifyCode(ctx context.Context, email, code string) (bool, error) {
	if s.rdb == nil {
		return false, fmt.Errorf("email service unavailable")
	}

	// Check attempt lockout.
	attemptsStr, err := s.rdb.Get(ctx, s.attemptKey(email)).Result()
	if err == nil {
		attempts, _ := strconv.Atoi(attemptsStr)
		if attempts >= maxVerifyAttempts {
			return false, fmt.Errorf("too many verification attempts, please try again later")
		}
	}

	stored, err := s.rdb.Get(ctx, s.codeKey(email)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get code: %w", err)
	}

	if stored != code {
		// Increment failed attempts.
		pipe := s.rdb.Pipeline()
		pipe.Incr(ctx, s.attemptKey(email))
		pipe.Expire(ctx, s.attemptKey(email), 5*time.Minute)
		pipe.Exec(ctx)
		return false, nil
	}

	// Code matched — consume it.
	s.rdb.Del(ctx, s.codeKey(email))
	s.rdb.Del(ctx, s.attemptKey(email))
	return true, nil
}
