//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type desktopAuthMemoryCodes struct {
	mu    sync.Mutex
	codes map[string]*DesktopAuthCode
	fail  bool
}

func (m *desktopAuthMemoryCodes) StoreDesktopAuthCode(_ context.Context, hash string, entry *DesktopAuthCode, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("unavailable")
	}
	m.codes[hash] = entry
	return nil
}
func (m *desktopAuthMemoryCodes) ConsumeDesktopAuthCode(_ context.Context, hash string) (*DesktopAuthCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return nil, errors.New("unavailable")
	}
	entry := m.codes[hash]
	delete(m.codes, hash)
	if entry == nil {
		return nil, ErrDesktopAuthCodeNotFound
	}
	return entry, nil
}

type desktopAuthTestUsers struct {
	user *User
	err  error
}

func (u *desktopAuthTestUsers) GetByID(context.Context, int64) (*User, error) {
	if u.err != nil {
		return nil, u.err
	}
	if u.user == nil {
		return nil, ErrUserNotFound
	}
	copy := *u.user
	return &copy, nil
}

type desktopAuthTestTokens struct {
	calls   atomic.Int32
	err     error
	partial bool
	binding string
}

func (i *desktopAuthTestTokens) GenerateTokenPair(ctx context.Context, _ *User, _ string) (*TokenPair, error) {
	i.calls.Add(1)
	i.binding = sessionBindingHashFromContext(ctx)
	if i.err != nil {
		return nil, i.err
	}
	if i.partial {
		return &TokenPair{AccessToken: "partial"}, nil
	}
	return &TokenPair{AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresIn: 3600}, nil
}

func TestDesktopAuthBindsNewSessionToDesktopExchangeContext(t *testing.T) {
	svc, _, users, tokens, input, verifier := newDesktopAuthTest(t)
	users.user.Role = RoleAdmin
	svc.settings = desktopAuthTestSettings(true)
	browser := &SessionBinding{IP: "203.0.113.10", UserAgent: "browser"}
	desktop := &SessionBinding{IP: "203.0.113.10", UserAgent: "zcode-host"}
	redirect, err := svc.Authorize(WithSessionBinding(context.Background(), browser), 7, input)
	require.NoError(t, err)
	u, err := url.Parse(redirect)
	require.NoError(t, err)
	pair, user, err := svc.Exchange(WithSessionBinding(context.Background(), desktop), DesktopAuthTokenInput{Code: u.Query().Get("code"), CodeVerifier: verifier, RedirectURI: input.RedirectURI})
	require.NoError(t, err)
	require.NotNil(t, pair)
	require.Equal(t, RoleAdmin, user.Role)
	require.Equal(t, desktop.Hash(), tokens.binding)
	require.NotEqual(t, browser.Hash(), tokens.binding)
}

type desktopAuthTestSettings bool

func (s desktopAuthTestSettings) IsBackendModeEnabled(context.Context) bool { return bool(s) }

func newDesktopAuthTest(t *testing.T) (*DesktopAuthService, *desktopAuthMemoryCodes, *desktopAuthTestUsers, *desktopAuthTestTokens, DesktopAuthAuthorizeInput, string) {
	t.Helper()
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	input := DesktopAuthAuthorizeInput{RedirectURI: "http://127.0.0.1:49152/callback", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256", State: strings.Repeat("s", 43)}
	codes := &desktopAuthMemoryCodes{codes: map[string]*DesktopAuthCode{}}
	users := &desktopAuthTestUsers{user: &User{ID: 7, Status: StatusActive, Role: RoleUser, Email: "test@example.test", PasswordHash: "hash"}}
	tokens := &desktopAuthTestTokens{}
	svc := &DesktopAuthService{codes: codes, users: users, tokens: tokens, settings: desktopAuthTestSettings(false)}
	return svc, codes, users, tokens, input, verifier
}
func desktopAuthIssueTest(t *testing.T, svc *DesktopAuthService, input DesktopAuthAuthorizeInput) string {
	t.Helper()
	redirect, err := svc.Authorize(context.Background(), 7, input)
	require.NoError(t, err)
	u, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Equal(t, input.State, u.Query().Get("state"))
	require.Len(t, u.Query(), 2)
	code := u.Query().Get("code")
	require.Len(t, code, 43)
	return code
}

func TestDesktopAuthExchangeOnceAndConcurrent(t *testing.T) {
	svc, codes, _, tokens, input, verifier := newDesktopAuthTest(t)
	code := desktopAuthIssueTest(t, svc, input)
	_, rawSecretStored := codes.codes[code]
	require.False(t, rawSecretStored)
	require.Contains(t, codes.codes, hashToken(code))
	var success atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pair, user, err := svc.Exchange(context.Background(), DesktopAuthTokenInput{Code: code, CodeVerifier: verifier, RedirectURI: input.RedirectURI})
			if err == nil {
				success.Add(1)
				if pair.AccessToken != "test-access" || user.ID != 7 {
					t.Error("unexpected account result")
				}
			} else if !errors.Is(err, ErrDesktopAuthInvalidGrant) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	require.EqualValues(t, 1, tokens.calls.Load())
}

func TestDesktopAuthRejectsInvalidGrantWithoutIssuingTokens(t *testing.T) {
	for _, scenario := range []string{"pkce", "redirect", "expired", "inactive", "deleted", "password", "missing", "database", "backend"} {
		t.Run(scenario, func(t *testing.T) {
			svc, codes, users, tokens, input, verifier := newDesktopAuthTest(t)
			code := desktopAuthIssueTest(t, svc, input)
			switch scenario {
			case "pkce":
				verifier = strings.Repeat("x", 43)
			case "redirect":
				input.RedirectURI = "http://127.0.0.1:49153/callback"
			case "expired":
				codes.codes[hashToken(code)].ExpiresAt = time.Now().Add(-time.Second)
			case "inactive":
				users.user.Status = "disabled"
			case "deleted":
				now := time.Now()
				users.user.DeletedAt = &now
			case "password":
				users.user.PasswordHash = "new-password-hash"
			case "missing":
				users.user = nil
			case "database":
				users.err = errors.New("database down")
			case "backend":
				svc.settings = desktopAuthTestSettings(true)
			}
			pair, user, err := svc.Exchange(context.Background(), DesktopAuthTokenInput{Code: code, CodeVerifier: verifier, RedirectURI: input.RedirectURI})
			require.Error(t, err)
			require.Nil(t, pair)
			require.Nil(t, user)
			require.Zero(t, tokens.calls.Load())
			_, _, err = svc.Exchange(context.Background(), DesktopAuthTokenInput{Code: code, CodeVerifier: verifier, RedirectURI: input.RedirectURI})
			require.ErrorIs(t, err, ErrDesktopAuthInvalidGrant)
		})
	}
}

func TestDesktopAuthStorageAndTokenFailureFailClosed(t *testing.T) {
	for _, scenario := range []string{"store", "consume", "token", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			svc, codes, _, tokens, input, verifier := newDesktopAuthTest(t)
			if scenario == "store" {
				codes.fail = true
				_, err := svc.Authorize(context.Background(), 7, input)
				require.ErrorIs(t, err, ErrDesktopAuthUnavailable)
				return
			}
			code := desktopAuthIssueTest(t, svc, input)
			if scenario == "consume" {
				codes.fail = true
			}
			if scenario == "token" {
				tokens.err = errors.New("refresh store down")
			}
			if scenario == "partial" {
				tokens.partial = true
			}
			pair, user, err := svc.Exchange(context.Background(), DesktopAuthTokenInput{Code: code, CodeVerifier: verifier, RedirectURI: input.RedirectURI})
			require.ErrorIs(t, err, ErrDesktopAuthUnavailable)
			require.Nil(t, pair)
			require.Nil(t, user)
		})
	}
}

func TestDesktopAuthStrictRequestValidation(t *testing.T) {
	_, _, _, _, input, _ := newDesktopAuthTest(t)
	for _, redirect := range []string{"http://localhost:49152/callback", "http://[::1]:49152/callback", "https://127.0.0.1:49152/callback", "http://127.0.0.1:80/callback", "http://127.0.0.1:049152/callback", "http://127.0.0.1:65536/callback", "http://127.0.0.1:49152/callback?", "http://127.0.0.1:49152/callback#", "http://127.0.0.1:49152/%63allback", "http://127.0.0.1:49152/callback/", "http://user@127.0.0.1:49152/callback", "http://127.0.0.1:49152/other", "http://example.test/callback"} {
		copy := input
		copy.RedirectURI = redirect
		require.Error(t, validateDesktopAuthAuthorize(copy), redirect)
	}
	copy := input
	copy.CodeChallengeMethod = "plain"
	require.Error(t, validateDesktopAuthAuthorize(copy))
	copy = input
	copy.State = "short"
	require.Error(t, validateDesktopAuthAuthorize(copy))
	copy = input
	copy.CodeChallenge = strings.Repeat("x", 43)
	require.Error(t, validateDesktopAuthAuthorize(copy)) // non-canonical last base64 bits
	for _, body := range []string{`{}`, `{"redirect_uri":null}`, `{"Redirect_URI":"a"}`, `{"state":"a","state":"b"}`, `{"extra":1}`, `[]`, `{} {}`} {
		_, err := ParseDesktopAuthAuthorize([]byte(body))
		require.ErrorIs(t, err, ErrDesktopAuthInvalid, body)
	}
}
