package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminMessageTestRepo struct {
	service.OfficeAgentRepository
	service.OfficeAdminRepository
	runID  string
	cursor int64
}

func (r *adminMessageTestRepo) AdminRunMessages(_ context.Context, id string, before int64) (*service.OfficeAdminRunMessages, error) {
	r.runID, r.cursor = id, before
	if id == "deleted-run" {
		return nil, service.ErrOfficeNotFound
	}
	return &service.OfficeAdminRunMessages{
		ConversationTitle: "办公对话",
		Messages:          []service.OfficeMessage{},
		Runs:              []service.OfficeRun{},
	}, nil
}

func TestOfficeAdminRunMessagesRequiresFullAccessAndValidCursor(t *testing.T) {
	router := gin.New()
	repo := &adminMessageTestRepo{}
	svc := service.NewOfficeAgentService(repo, &config.Config{}, nil, nil, nil, nil, nil, nil)
	router.GET("/runs/:id/messages", NewOfficeAgentHandler(svc).AdminRunMessages)

	restricted := httptest.NewRequest(http.MethodGet, "/runs/run-one/messages", nil)
	restricted = restricted.WithContext(adminaccess.WithPrincipal(restricted.Context(), &adminaccess.Principal{}))
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, restricted)
	require.Equal(t, http.StatusForbidden, denied.Code)
	require.Empty(t, repo.runID)

	for _, cursor := range []string{"-1", "not-an-integer", "9223372036854775808"} {
		bad := httptest.NewRecorder()
		router.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/runs/run-one/messages?before_seq="+cursor, nil))
		require.Equal(t, http.StatusBadRequest, bad.Code)
		require.Empty(t, repo.runID)
	}

	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/runs/run-one/messages?before_seq=123", nil))
	require.Equal(t, http.StatusOK, allowed.Code)
	require.Equal(t, "no-store", allowed.Header().Get("Cache-Control"))
	require.Equal(t, "run-one", repo.runID)
	require.EqualValues(t, 123, repo.cursor)
	var response struct {
		Data service.OfficeAdminRunMessages `json:"data"`
	}
	require.NoError(t, json.Unmarshal(allowed.Body.Bytes(), &response))
	require.Equal(t, "办公对话", response.Data.ConversationTitle)
	require.NotNil(t, response.Data.Messages)

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/runs/deleted-run/messages", nil))
	require.Equal(t, http.StatusNotFound, missing.Code)
	require.Equal(t, "no-store", missing.Header().Get("Cache-Control"))
}
