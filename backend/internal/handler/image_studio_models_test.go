//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageStudioModelsGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r imageStudioModelsGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if r.group != nil && r.group.ID == id {
		return r.group, nil
	}
	return nil, service.ErrGroupNotFound
}

func TestImageStudioModelsRequireAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/image-studio/models", nil)
	(&ImageStudioHandler{}).GetModels(c)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestImageStudioModelsUseConfiguredStudioGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		enable string
		status string
		want   int
	}{
		{"enabled", "true", service.StatusActive, http.StatusOK},
		{"disabled studio", "false", service.StatusActive, http.StatusForbidden},
		{"inactive group", "true", service.StatusDisabled, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{ID: 14, Platform: service.PlatformOpenAI, Status: tc.status, AllowImageGeneration: true}
			accounts := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{14: {
				{ID: 72, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2.5": "gpt-image-2.5-private"}}},
			}}}
			gateway := newGatewayModelsHandlerForTest(accounts).gatewayService
			settings := service.NewSettingService(&settingHandlerPublicRepoStub{values: map[string]string{
				service.SettingKeyImageStudioEnabled: tc.enable, service.SettingKeyImageStudioGroupID: "14",
			}}, &config.Config{})
			h := NewImageStudioHandler(nil, settings, service.NewGroupService(imageStudioModelsGroupRepo{group: group}, nil), nil, gateway)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/image-studio/models", nil)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 100})
			h.GetModels(c)
			require.Equal(t, tc.want, rec.Code)
			if tc.want == http.StatusOK {
				require.Equal(t, "gpt-image-2.5", gjson.GetBytes(rec.Body.Bytes(), "data.models.0.id").String())
				require.Equal(t, "openai", gjson.GetBytes(rec.Body.Bytes(), "data.models.0.api").String())
				require.NotContains(t, rec.Body.String(), "private")
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			}
		})
	}
}
