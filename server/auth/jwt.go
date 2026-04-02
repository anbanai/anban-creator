package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims holds the JWT token claims.
type JWTClaims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JWTService handles JWT token generation and validation.
type JWTService struct {
	secretKey     []byte
	accessExpiry  time.Duration
	refreshExpiry time.Duration
}

// NewJWTService creates a new JWTService with the given secret key and durations.
// The secretKey must not be empty. accessExpiry and refreshExpiry must be valid
// Go duration strings (e.g. "24h", "168h").
func NewJWTService(secretKey, accessExpiry, refreshExpiry string) (*JWTService, error) {
	if secretKey == "" {
		return nil, errors.New("secret key must not be empty")
	}

	accessDur, err := time.ParseDuration(accessExpiry)
	if err != nil {
		return nil, err
	}

	refreshDur, err := time.ParseDuration(refreshExpiry)
	if err != nil {
		return nil, err
	}

	return &JWTService{
		secretKey:     []byte(secretKey),
		accessExpiry:  accessDur,
		refreshExpiry: refreshDur,
	}, nil
}

// AccessExpiry returns the configured access token expiry duration.
func (j *JWTService) AccessExpiry() time.Duration {
	return j.accessExpiry
}

// GenerateAccessToken generates a signed access JWT for the given user.
func (j *JWTService) GenerateAccessToken(userID string) (string, error) {
	now := time.Now()
	claims := &JWTClaims{
		UserID: userID,
		Role:   "user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(j.accessExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secretKey)
}

// GenerateRefreshToken generates a signed refresh JWT for the given user.
// The refresh token only contains the UserID (no role) and has a longer expiry.
func (j *JWTService) GenerateRefreshToken(userID string) (string, error) {
	now := time.Now()
	claims := &JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(j.refreshExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secretKey)
}

// ValidateToken parses and validates a JWT string, returning the claims.
func (j *JWTService) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
