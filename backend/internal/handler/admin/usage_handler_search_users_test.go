package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 捕获 ListUsers 入参、返回一个已删用户的 admin service 桩。
type searchUsersAdminStub struct {
	service.AdminService
	gotFilters service.UserListFilters
}

func (s *searchUsersAdminStub) ListUsers(ctx context.Context, page, pageSize int, filters service.UserListFilters, sortBy, sortOrder string) ([]service.User, int64, error) {
	s.gotFilters = filters
	ts := time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC)
	return []service.User{
		{ID: 1, Email: "active@test.com", Username: "派崔克", Notes: "private-user-notes", PasswordHash: "private-password-hash", APIKeys: []service.APIKey{{Key: "private-api-key"}}},
		{ID: 2, Email: "deleted@test.com", Username: "已删昵称", DeletedAt: &ts},
	}, 2, nil
}

func TestAdminUsageSearchUsers_IncludesDeletedAndFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &searchUsersAdminStub{}
	handler := NewUsageHandler(nil, nil, stub, nil)
	router := gin.New()
	router.GET("/admin/usage/search-users", handler.SearchUsers)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/search-users?q=test", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, stub.gotFilters.IncludeDeleted, "SearchUsers 必须请求 IncludeDeleted")
	require.Equal(t, "test", stub.gotFilters.Search)

	var resp struct {
		Data []struct {
			ID       int64  `json:"id"`
			Email    string `json:"email"`
			Username string `json:"username"`
			Deleted  bool   `json:"deleted"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	require.Equal(t, "派崔克", resp.Data[0].Username)
	require.Equal(t, "已删昵称", resp.Data[1].Username)
	require.False(t, resp.Data[0].Deleted)
	require.True(t, resp.Data[1].Deleted, "已删用户必须标记 deleted=true")
	for _, private := range []string{"private-user-notes", "private-password-hash", "private-api-key", "notes", "password_hash", "api_keys"} {
		require.NotContains(t, rec.Body.String(), private)
	}
	var projection struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &projection))
	for _, user := range projection.Data {
		require.Len(t, user, 4, "picker responses contain only id, email, username and deleted")
	}
}
