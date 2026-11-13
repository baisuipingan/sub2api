package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type desktopHandlerLifecycle struct {
	desktopHandlerKeys
	userID, keyID int64
	reset         bool
	err           error
	caller        service.DesktopAgentCredentialCaller
	calls         int
}

func (m *desktopHandlerLifecycle) ManageDesktopAgentCredential(_ context.Context, caller service.DesktopAgentCredentialCaller, userID, keyID int64, reset bool) (*service.DesktopAgentCredentialMutation, error) {
	m.calls++
	m.caller = caller
	m.userID = userID
	m.keyID = keyID
	m.reset = reset
	if m.err != nil {
		return nil, m.err
	}
	status := "revoked"
	if reset {
		status = "active"
	}
	return &service.DesktopAgentCredentialMutation{KeyID: keyID, Status: status}, nil
}

func TestDesktopAgentLifecycleHandlerUsesOnlyAuthenticatedOwner(t *testing.T) {
	manager := &desktopHandlerLifecycle{}
	svc := service.NewDesktopAgentService(nil, nil, nil, manager)
	h := NewDesktopAgentHandler(svc)
	router := desktopHandlerRouter(h, true, false)
	router.POST("/credential/revoke", h.RevokeCredential)
	router.POST("/credential/reset", h.ResetCredential)
	for _, action := range []string{"revoke", "reset"} {
		rec := desktopRequest(t, router, http.MethodPost, "/credential/"+action, `{"key_id":73}`)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, int64(7), manager.userID)
		require.Equal(t, int64(73), manager.keyID)
		require.Equal(t, action == "reset", manager.reset)
		require.Equal(t, service.DesktopAgentCredentialCaller{UserID: 7}, manager.caller)
		require.NotContains(t, rec.Body.String(), "private")
		require.NotContains(t, rec.Body.String(), `"key":`)
	}
	for _, raw := range []string{`{"key_id":73,"user_id":8}`, `{"key_id":73,"key_id":74}`, strings.Repeat("x", 129)} {
		rec := desktopRequest(t, router, http.MethodPost, "/credential/revoke", raw)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	manager.err = service.ErrAPIKeyNotFound
	rec := desktopRequest(t, router, http.MethodPost, "/credential/revoke", `{"key_id":73}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
	manager.err = service.ErrDesktopAgentRestoreAdminRequired
	rec = desktopRequest(t, router, http.MethodPost, "/credential/reset", `{"key_id":73}`)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "DESKTOP_CREDENTIAL_RESTORE_ADMIN_REQUIRED")
	unauth := desktopHandlerRouter(h, false, false)
	unauth.POST("/credential/revoke", h.RevokeCredential)
	rec = desktopRequest(t, unauth, http.MethodPost, "/credential/revoke", `{"key_id":73}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestDesktopAgentLifecycleAdminResetRequiresFullRoleAndServerStepUpGate(t *testing.T) {
	for _, test := range []struct {
		name          string
		authenticated bool
		restricted    bool
		role          string
		stepUpPassed  bool
		status        int
	}{
		{"no subject", false, false, service.RoleAdmin, true, http.StatusUnauthorized},
		{"user role", true, false, service.RoleUser, true, http.StatusForbidden},
		{"missing role", true, false, "", true, http.StatusForbidden},
		{"restricted admin", true, true, service.RoleAdmin, true, http.StatusForbidden},
		{"no step-up gate", true, false, service.RoleAdmin, false, http.StatusForbidden},
		{"full admin passed gate", true, false, service.RoleAdmin, true, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &desktopHandlerLifecycle{}
			h := NewDesktopAgentHandler(service.NewDesktopAgentService(nil, nil, nil, manager))
			router := desktopHandlerRouter(h, test.authenticated, test.restricted)
			router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUserRole), test.role) })
			if test.stepUpPassed {
				router.POST("/users/:id/credential/reset", DesktopAgentCredentialStepUpPassed, h.AdminResetCredential)
			} else {
				router.POST("/users/:id/credential/reset", h.AdminResetCredential)
			}
			rec := desktopRequest(t, router, http.MethodPost, "/users/8/credential/reset", `{"key_id":73}`)
			require.Equal(t, test.status, rec.Code)
			if test.status == http.StatusOK {
				require.Equal(t, service.DesktopAgentCredentialCaller{UserID: 7, Administrator: true, StepUpAuthorized: true}, manager.caller)
				require.Equal(t, int64(8), manager.userID)
				require.Equal(t, 1, manager.calls)
			} else {
				require.Zero(t, manager.calls)
			}
		})
	}
}

func TestDesktopAgentLifecycleHandlerAdminAndProfiles(t *testing.T) {
	repo := &desktopHandlerSettings{raw: desktopHandlerConfig}
	manager := &desktopHandlerLifecycle{}
	svc := service.NewDesktopAgentService(service.NewSettingService(repo, nil), nil, nil, manager)
	h := NewDesktopAgentHandler(svc)
	for _, restricted := range []bool{false, true} {
		router := desktopHandlerRouter(h, true, restricted)
		router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin) })
		router.POST("/users/:id/credential/revoke", DesktopAgentCredentialStepUpPassed, h.AdminRevokeCredential)
		router.GET("/profiles", h.GetProfiles)
		rec := desktopRequest(t, router, http.MethodPost, "/users/8/credential/revoke", `{"key_id":73}`)
		if restricted {
			require.Equal(t, http.StatusForbidden, rec.Code)
		} else {
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, int64(8), manager.userID)
		}
		rec = desktopRequest(t, router, http.MethodGet, "/profiles", "")
		if restricted {
			require.Equal(t, http.StatusForbidden, rec.Code)
		} else {
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), `"source":"official_openai"`)
			require.Contains(t, rec.Body.String(), "gpt-6.1-sol")
			require.NotContains(t, rec.Body.String(), `"key":`)
		}
	}
}
