package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchHandlerRepo struct{ calls int }

func (r *workbenchHandlerRepo) Overview(context.Context, time.Time, time.Time) (*service.WorkbenchOverview, error) {
	r.calls++
	return &service.WorkbenchOverview{}, nil
}
func (r *workbenchHandlerRepo) ListTasks(context.Context, service.WorkbenchTaskFilter) (*service.WorkbenchTaskPage, error) {
	r.calls++
	return &service.WorkbenchTaskPage{Items: []service.WorkbenchTaskSummary{{TaskID: "public-task", UserID: 7, Model: "gpt-image-2", ErrorCode: "UPSTREAM_RESULT_UNKNOWN", ErrorMessage: "Safety review rejected the image. access_token=private-test-token https://private-provider.test/error"}}}, nil
}

func TestWorkbenchHandlerRejectsInvalidQueriesBeforeRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &workbenchHandlerRepo{}
	h := NewWorkbenchHandler(service.NewWorkbenchAdminService(repo, nil, nil))
	router := gin.New()
	router.GET("/overview", h.Overview)
	router.GET("/tasks", h.ListTasks)
	for _, path := range []string{"/overview?days=365", "/overview?days=oops", "/tasks?page=-1", "/tasks?page_size=101", "/tasks?user_id=nope", "/tasks?user_id=-2", "/tasks?status=bad", "/tasks?model=%00"} {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusBadRequest, r.Code, path)
	}
	require.Zero(t, repo.calls)
}

func TestWorkbenchHandlerMetadataProjectionAndRestrictedKeyBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &workbenchHandlerRepo{}
	h := NewWorkbenchHandler(service.NewWorkbenchAdminService(repo, nil, nil))
	for _, restricted := range []bool{false, true} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if restricted {
				c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{Policy: adminaccess.Policy{}}))
			}
		})
		router.GET("/tasks", h.ListTasks)
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/tasks", nil))
		if restricted {
			require.Equal(t, http.StatusForbidden, r.Code)
			continue
		}
		require.Equal(t, http.StatusOK, r.Code)
		require.Equal(t, "no-store", r.Header().Get("Cache-Control"))
		var result struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
		require.Len(t, result.Data.Items, 1)
		require.Contains(t, result.Data.Items[0]["error_message"], "Safety review rejected")
		require.NotContains(t, r.Body.String(), "private-test-token")
		require.NotContains(t, r.Body.String(), "private-provider")
		for _, key := range []string{"prompt", "task_json", "request_json", "storage_bucket", "spool_path", "api_key_id", "url"} {
			require.NotContains(t, result.Data.Items[0], key)
		}
	}
	require.Equal(t, 1, repo.calls)
}

func TestWorkbenchConfigRejectsMissingUnknownAndTrailingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWorkbenchHandler(nil)
	router := gin.New()
	router.PUT("/config", h.UpdateConfig)
	for _, body := range []string{`{}`, `{"image_studio_enabled":true}`, `{"image_studio_enabled":false,"image_studio_group_id":0,"site_name":"override"}`, `{"image_studio_enabled":false,"image_studio_group_id":0} {}`} {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(r, req)
		require.Equal(t, http.StatusBadRequest, r.Code)
	}
}

type workbenchConfigSettings struct {
	service.SettingRepository
	values map[string]string
}

func (r *workbenchConfigSettings) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *workbenchConfigSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		values[key] = r.values[key]
	}
	return values, nil
}
func (r *workbenchConfigSettings) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestWorkbenchConfigAcceptsOldBudgetPayloadWithoutPersistingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &workbenchConfigSettings{values: map[string]string{}}
	h := NewWorkbenchHandler(service.NewWorkbenchAdminService(nil, service.NewSettingService(repo, nil), nil))
	router := gin.New()
	router.PUT("/config", h.UpdateConfig)
	send := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(body)))
		return w
	}
	first := send(`{"image_studio_enabled":false,"image_studio_group_id":0,"max_model_calls":3,"max_run_credits":700,"model_profiles":{"office-alias":{"profile_id":"gpt-6.1-sol","override":false}}}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.NotContains(t, first.Body.String(), `"deployment_limits"`)
	require.NotContains(t, repo.values, "workbench_max_model_calls")
	require.NotContains(t, repo.values, "workbench_max_run_credits")
	legacy := send(`{"image_studio_enabled":false,"image_studio_group_id":0}`)
	require.Equal(t, http.StatusOK, legacy.Code, legacy.Body.String())
	require.NotContains(t, legacy.Body.String(), `"max_model_calls"`)
	require.NotContains(t, legacy.Body.String(), `"max_run_credits"`)
	require.Contains(t, legacy.Body.String(), `"office-alias"`)
	for _, body := range []string{
		`{"image_studio_enabled":false,"image_studio_group_id":0,"model_profiles":{"office-alias":{"profile_id":"gpt-6.1-sol","override":true}}}`,
		`{"image_studio_enabled":false,"image_studio_group_id":0,"model_profiles":{"gpt-6.1-sol":{"profile_id":"gpt-6-luna","override":false}}}`,
	} {
		require.Equal(t, http.StatusBadRequest, send(body).Code, body)
	}
	cleared := send(`{"image_studio_enabled":false,"image_studio_group_id":0,"model_profiles":{}}`)
	require.Equal(t, http.StatusOK, cleared.Code, cleared.Body.String())
	require.Contains(t, cleared.Body.String(), `"model_profiles":{}`)
}
