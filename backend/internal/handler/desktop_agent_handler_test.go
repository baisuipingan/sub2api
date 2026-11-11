package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type desktopHandlerSettings struct {
	service.SettingRepository
	raw    string
	err    error
	writes int
}

func (r *desktopHandlerSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyDesktopAgentConfig: r.raw}, r.err
}
func (r *desktopHandlerSettings) SetMultiple(_ context.Context, v map[string]string) error {
	r.writes++
	r.raw = v[service.SettingKeyDesktopAgentConfig]
	return r.err
}

type desktopHandlerUsers struct{ seen int64 }

func (r *desktopHandlerUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	r.seen = id
	return &service.User{ID: id, Status: service.StatusActive}, nil
}

type desktopHandlerGroups struct{}

func (r desktopHandlerGroups) GetByID(context.Context, int64) (*service.Group, error) {
	return &service.Group{ID: 12, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard}, nil
}

type desktopHandlerKeys struct {
	seen  int64
	calls int
}

func (r *desktopHandlerKeys) GetDesktopAgentCredential(_ context.Context, userID, groupID int64) (*service.APIKey, string, error) {
	r.seen = userID
	r.calls++
	return &service.APIKey{ID: 73, UserID: userID, Key: "private-managed-fixture", GroupID: &groupID, Status: service.StatusActive, KeyType: service.APIKeyTypeSystem, Purpose: service.APIKeyPurposeDesktopAgent}, service.APIKeyPurposeDesktopAgent, nil
}

const desktopHandlerConfig = `{"enabled":true,"group_id":12,"base_url":"https://gateway.example.test/v1","model":{"id":"verified-model","display_name":"Verified model","context_window":100000,"max_output_tokens":20000,"default_max_output_tokens":10000,"reasoning_levels":["none"],"default_reasoning":"none","supports_reasoning":false,"supports_streaming":true,"requires_mfjs_tool_schema":false,"input":{"text":true,"image":false,"video":false,"audio":false,"pdf":false},"output":{"text":true},"supports_tool_call":true,"supports_json_schema_output":false,"supports_native_web_search":false,"supports_mid_conversation_system":true}}`
const desktopBootstrapBody = `{"client_version":"fixture-1","protocol_version":1,"platform":"darwin"}`

func desktopHandlerRouter(h *DesktopAgentHandler, authenticated, restricted bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if authenticated {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		}
		if restricted {
			c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{Policy: adminaccess.Policy{}}))
		}
	})
	r.POST("/bootstrap", h.Bootstrap)
	r.GET("/config", h.GetConfig)
	r.PUT("/config", h.UpdateConfig)
	return r
}
func desktopRequest(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	return rec
}
func TestDesktopAgentHandlerAuthStrictBodyAndNoSecretFailures(t *testing.T) {
	h := NewDesktopAgentHandler(nil)
	rec := desktopRequest(t, desktopHandlerRouter(h, false, false), http.MethodPost, "/bootstrap", desktopBootstrapBody)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	router := desktopHandlerRouter(h, true, false)
	for _, raw := range []string{`{}`, desktopBootstrapBody + `{}`, strings.Replace(desktopBootstrapBody, `"platform":"darwin"`, `"platform":"darwin","user_id":8`, 1), strings.Replace(desktopBootstrapBody, `"platform":"darwin"`, `"platform":null`, 1), strings.Repeat("x", 2049)} {
		rec := desktopRequest(t, router, http.MethodPost, "/bootstrap", raw)
		require.Equal(t, http.StatusBadRequest, rec.Code, raw)
		require.NotContains(t, rec.Body.String(), "credential")
	}
	repo := &desktopHandlerSettings{err: errors.New("secret-db-password-and-key")}
	svc := service.NewDesktopAgentService(service.NewSettingService(repo, nil), nil, nil, nil)
	rec = desktopRequest(t, desktopHandlerRouter(NewDesktopAgentHandler(svc), true, false), http.MethodPost, "/bootstrap", desktopBootstrapBody)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, rec.Body.String(), "secret-db-password-and-key")
}
func TestDesktopAgentHandlerUsesAuthenticatedUserAndReadyEnvelope(t *testing.T) {
	repo := &desktopHandlerSettings{raw: desktopHandlerConfig}
	users := &desktopHandlerUsers{}
	keys := &desktopHandlerKeys{}
	svc := service.NewDesktopAgentService(service.NewSettingService(repo, nil), users, desktopHandlerGroups{}, keys)
	router := desktopHandlerRouter(NewDesktopAgentHandler(svc), true, false)
	rec := desktopRequest(t, router, http.MethodPost, "/bootstrap", desktopBootstrapBody)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), users.seen)
	require.Equal(t, int64(7), keys.seen)
	var result struct {
		Code int                           `json:"code"`
		Data service.DesktopAgentBootstrap `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Zero(t, result.Code)
	require.Equal(t, "ready", result.Data.ServiceStatus)
	require.Equal(t, "73", result.Data.Credential.ID)
	repo.raw = ""
	rec = desktopRequest(t, router, http.MethodPost, "/bootstrap", desktopBootstrapBody)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "credential")
	require.Equal(t, 1, keys.calls)
}
func TestDesktopAgentAdminRejectsRestrictedKeysAndInvalidConfiguration(t *testing.T) {
	restricted := desktopHandlerRouter(NewDesktopAgentHandler(nil), true, true)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		rec := desktopRequest(t, restricted, method, "/config", desktopHandlerConfig)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
	repo := &desktopHandlerSettings{}
	svc := service.NewDesktopAgentService(service.NewSettingService(repo, nil), nil, desktopHandlerGroups{}, nil)
	router := desktopHandlerRouter(NewDesktopAgentHandler(svc), true, false)
	for _, body := range []string{`{}`, `{"enabled":false,"enabled":true}`, strings.Replace(desktopHandlerConfig, `"supports_tool_call":true,`, "", 1), desktopHandlerConfig + `{}`, strings.Replace(desktopHandlerConfig, `"enabled":true`, `"enabled":true,"key":"override"`, 1)} {
		rec := desktopRequest(t, router, http.MethodPut, "/config", body)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	require.Zero(t, repo.writes)
	rec := desktopRequest(t, router, http.MethodPut, "/config", desktopHandlerConfig)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, repo.writes)
	require.NotContains(t, rec.Body.String(), "private-managed-fixture")
}
