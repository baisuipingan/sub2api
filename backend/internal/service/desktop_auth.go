package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDesktopAuthInvalid      = errors.New("invalid desktop authorization request")
	ErrDesktopAuthInvalidGrant = errors.New("invalid or expired desktop authorization code")
	ErrDesktopAuthUnavailable  = errors.New("desktop authorization service unavailable")
	ErrDesktopAuthCodeNotFound = errors.New("desktop authorization code not found")
)

const desktopAuthCodeTTL = 3 * time.Minute

type DesktopAuthAuthorizeInput struct {
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
}

type DesktopAuthTokenInput struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

// DesktopAuthCode contains no bearer secret. Hash is the persistence key only.
type DesktopAuthCode struct {
	UserID        int64     `json:"user_id"`
	TokenVersion  int64     `json:"token_version"`
	RedirectURI   string    `json:"redirect_uri"`
	CodeChallenge string    `json:"code_challenge"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type DesktopAuthCodeStore interface {
	StoreDesktopAuthCode(context.Context, string, *DesktopAuthCode, time.Duration) error
	ConsumeDesktopAuthCode(context.Context, string) (*DesktopAuthCode, error)
}

type desktopAuthUsers interface {
	GetByID(context.Context, int64) (*User, error)
}
type desktopAuthTokens interface {
	GenerateTokenPair(context.Context, *User, string) (*TokenPair, error)
}
type desktopAuthSettings interface{ IsBackendModeEnabled(context.Context) bool }

type DesktopAuthService struct {
	codes    DesktopAuthCodeStore
	users    desktopAuthUsers
	tokens   desktopAuthTokens
	settings desktopAuthSettings
}

func NewDesktopAuthService(codes DesktopAuthCodeStore, users UserRepository, tokens *AuthService, settings *SettingService) *DesktopAuthService {
	svc := &DesktopAuthService{codes: codes, users: users}
	if tokens != nil {
		svc.tokens = tokens
	}
	if settings != nil {
		svc.settings = settings
	}
	return svc
}

func ParseDesktopAuthAuthorize(data []byte) (DesktopAuthAuthorizeInput, error) {
	var input DesktopAuthAuthorizeInput
	if decodeDesktopJSON(data, &input) != nil || validateDesktopAuthAuthorize(input) != nil {
		return input, ErrDesktopAuthInvalid
	}
	return input, nil
}

func ParseDesktopAuthToken(data []byte) (DesktopAuthTokenInput, error) {
	var input DesktopAuthTokenInput
	if decodeDesktopJSON(data, &input) != nil || !validDesktopAuthOpaque(input.Code, 43, 43) || !validDesktopAuthVerifier(input.CodeVerifier) || !validDesktopAuthRedirect(input.RedirectURI) {
		return input, ErrDesktopAuthInvalid
	}
	return input, nil
}

func validateDesktopAuthAuthorize(input DesktopAuthAuthorizeInput) error {
	if !validDesktopAuthRedirect(input.RedirectURI) || input.CodeChallengeMethod != "S256" || !validDesktopAuthOpaque(input.CodeChallenge, 43, 43) || !validDesktopAuthOpaque(input.State, 22, 128) {
		return ErrDesktopAuthInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(input.CodeChallenge)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != input.CodeChallenge {
		return ErrDesktopAuthInvalid
	}
	return nil
}

func validDesktopAuthOpaque(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validDesktopAuthVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
			return false
		}
	}
	return true
}

func validDesktopAuthRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "/callback" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port >= 1024 && port <= 65535 && raw == fmt.Sprintf("http://127.0.0.1:%d/callback", port)
}

func (s *DesktopAuthService) activeUser(ctx context.Context, userID int64) (*User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return nil, ErrDesktopAuthInvalidGrant
	}
	if err != nil {
		return nil, ErrDesktopAuthUnavailable
	}
	if user == nil || user.ID != userID || !user.IsActive() || user.DeletedAt != nil || (s.settings != nil && s.settings.IsBackendModeEnabled(ctx) && !user.IsAdmin()) {
		return nil, ErrDesktopAuthInvalidGrant
	}
	return user, nil
}

func (s *DesktopAuthService) Authorize(ctx context.Context, userID int64, input DesktopAuthAuthorizeInput) (string, error) {
	if validateDesktopAuthAuthorize(input) != nil || userID <= 0 {
		return "", ErrDesktopAuthInvalid
	}
	if s.codes == nil || s.users == nil || s.tokens == nil {
		return "", ErrDesktopAuthUnavailable
	}
	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return "", err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return "", ErrDesktopAuthUnavailable
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	entry := &DesktopAuthCode{UserID: user.ID, TokenVersion: resolvedTokenVersion(user), RedirectURI: input.RedirectURI, CodeChallenge: input.CodeChallenge, ExpiresAt: time.Now().Add(desktopAuthCodeTTL)}
	if err = s.codes.StoreDesktopAuthCode(ctx, hashToken(code), entry, desktopAuthCodeTTL); err != nil {
		return "", ErrDesktopAuthUnavailable
	}
	u, _ := url.Parse(input.RedirectURI)
	q := url.Values{"code": {code}, "state": {input.State}}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *DesktopAuthService) Exchange(ctx context.Context, input DesktopAuthTokenInput) (*TokenPair, *User, error) {
	if !validDesktopAuthOpaque(input.Code, 43, 43) || !validDesktopAuthVerifier(input.CodeVerifier) || !validDesktopAuthRedirect(input.RedirectURI) {
		return nil, nil, ErrDesktopAuthInvalid
	}
	if s.codes == nil || s.users == nil || s.tokens == nil {
		return nil, nil, ErrDesktopAuthUnavailable
	}
	// 原因：code 只能被消费一次；错误PKCE或redirect也不恢复，避免并发重放及验证预言机。
	entry, err := s.codes.ConsumeDesktopAuthCode(ctx, hashToken(input.Code))
	if errors.Is(err, ErrDesktopAuthCodeNotFound) {
		return nil, nil, ErrDesktopAuthInvalidGrant
	}
	if err != nil {
		return nil, nil, ErrDesktopAuthUnavailable
	}
	if entry == nil || !entry.ExpiresAt.After(time.Now()) || entry.RedirectURI != input.RedirectURI {
		return nil, nil, ErrDesktopAuthInvalidGrant
	}
	digest := sha256.Sum256([]byte(input.CodeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(challenge), []byte(entry.CodeChallenge)) != 1 {
		return nil, nil, ErrDesktopAuthInvalidGrant
	}
	user, err := s.activeUser(ctx, entry.UserID)
	if err != nil {
		return nil, nil, err
	}
	if entry.TokenVersion != resolvedTokenVersion(user) {
		return nil, nil, ErrDesktopAuthInvalidGrant
	}
	// 使用桌面换证请求的session binding，不继承浏览器UA/IP指纹；复用原刷新轮换与退出。
	pair, err := s.tokens.GenerateTokenPair(ctx, user, "")
	if err != nil || pair == nil || pair.AccessToken == "" || pair.RefreshToken == "" || pair.ExpiresIn <= 0 {
		return nil, nil, ErrDesktopAuthUnavailable
	}
	return pair, user, nil
}
