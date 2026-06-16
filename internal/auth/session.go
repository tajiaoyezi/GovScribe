package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid token")
var ErrExpiredToken = errors.New("expired token")
var ErrTokenPurposeMismatch = errors.New("token purpose mismatch")

const sessionTokenPurpose = "session"

type SessionConfig struct {
	Issuer             string
	Audience           string
	Secret             []byte
	TTL                time.Duration
	OnlyOfficeAudience string
	OnlyOfficeSecret   []byte
}

type SessionClaims struct {
	Subject            string   `json:"sub"`
	Username           string   `json:"username"`
	Roles              []string `json:"roles"`
	MustChangePassword bool     `json:"must_change_password"`
	Issuer             string   `json:"iss"`
	Audience           string   `json:"aud"`
	ExpiresAt          int64    `json:"exp"`
	IssuedAt           int64    `json:"iat"`
	Purpose            string   `json:"token_use"`
}

type TokenManager struct {
	cfg SessionConfig
	now func() time.Time
}

func NewTokenManager(cfg SessionConfig) (*TokenManager, error) {
	if err := validateSessionConfig(cfg); err != nil {
		return nil, err
	}
	return &TokenManager{cfg: cfg, now: time.Now}, nil
}

func validateSessionConfig(cfg SessionConfig) error {
	if len(cfg.Secret) == 0 || strings.TrimSpace(cfg.Audience) == "" {
		return ErrInvalidToken
	}
	if cfg.TTL <= 0 {
		return ErrInvalidToken
	}
	if len(cfg.OnlyOfficeSecret) > 0 && hmac.Equal(cfg.Secret, cfg.OnlyOfficeSecret) {
		return ErrTokenPurposeMismatch
	}
	if strings.TrimSpace(cfg.OnlyOfficeAudience) != "" && cfg.Audience == cfg.OnlyOfficeAudience {
		return ErrTokenPurposeMismatch
	}
	return nil
}

func (m *TokenManager) Sign(principal Principal) (string, error) {
	now := m.nowTime()
	claims := SessionClaims{
		Subject:            principal.UserID,
		Username:           principal.Username,
		Roles:              principal.RoleStrings(),
		MustChangePassword: principal.MustChangePassword,
		Issuer:             m.cfg.Issuer,
		Audience:           m.cfg.Audience,
		IssuedAt:           now.Unix(),
		ExpiresAt:          now.Add(m.cfg.TTL).Unix(),
		Purpose:            sessionTokenPurpose,
	}
	return signClaims(claims, m.cfg.Secret)
}

func (m *TokenManager) Parse(token string) (SessionClaims, error) {
	claims, err := parseClaims(token, m.cfg.Secret)
	if err != nil {
		return SessionClaims{}, err
	}
	now := m.nowTime().Unix()
	if claims.ExpiresAt <= now {
		return SessionClaims{}, ErrExpiredToken
	}
	if claims.Issuer != m.cfg.Issuer || claims.Audience != m.cfg.Audience || claims.Purpose != sessionTokenPurpose {
		return SessionClaims{}, ErrTokenPurposeMismatch
	}
	return claims, nil
}

func (m *TokenManager) PrincipalFromToken(ctx context.Context, token string, resolver PrincipalResolver) (Principal, error) {
	claims, err := m.Parse(token)
	if err != nil {
		return Principal{}, err
	}
	if resolver == nil {
		return Principal{}, ErrUnauthenticated
	}
	return resolver.ResolvePrincipal(ctx, claims.Subject)
}

func (m *TokenManager) nowTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func signClaims(claims SessionClaims, secret []byte) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := hmacSHA256([]byte(unsigned), secret)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func parseClaims(token string, secret []byte) (SessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return SessionClaims{}, ErrInvalidToken
	}
	unsigned := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return SessionClaims{}, ErrInvalidToken
	}
	expected := hmacSHA256([]byte(unsigned), secret)
	if !hmac.Equal(signature, expected) {
		return SessionClaims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return SessionClaims{}, ErrInvalidToken
	}
	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return SessionClaims{}, ErrInvalidToken
	}
	return claims, nil
}

func hmacSHA256(payload, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
