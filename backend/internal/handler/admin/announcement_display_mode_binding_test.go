package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func bindAnnouncementJSON(t *testing.T, body string, request any) error {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/announcements", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c.ShouldBindJSON(request)
}

func TestAdminAnnouncementDisplayModeBindingPreservesOmittedUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var request UpdateAnnouncementRequest
	require.NoError(t, bindAnnouncementJSON(t, `{"title":"Corrected title"}`, &request))
	require.Nil(t, request.DisplayMode, "editing another field must not reset a saved notice to timeline")
	for _, mode := range []string{"notice", "timeline"} {
		request = UpdateAnnouncementRequest{}
		require.NoError(t, bindAnnouncementJSON(t, `{"display_mode":"`+mode+`"}`, &request))
		require.NotNil(t, request.DisplayMode)
		require.Equal(t, mode, *request.DisplayMode)
	}
}

func TestAdminAnnouncementDisplayModeBindingRejectsUnknownValues(t *testing.T) {
	for _, mode := range []string{"both", "popup", "invalid", ""} {
		var update UpdateAnnouncementRequest
		require.Error(t, bindAnnouncementJSON(t, `{"display_mode":"`+mode+`"}`, &update), "a present invalid update must not silently become timeline")
	}
	var create CreateAnnouncementRequest
	require.NoError(t, bindAnnouncementJSON(t, `{"title":"Title","content":"Body"}`, &create))
	require.Empty(t, create.DisplayMode, "legacy creates let the service select timeline")
	create = CreateAnnouncementRequest{}
	require.Error(t, bindAnnouncementJSON(t, `{"title":"Title","content":"Body","display_mode":"both"}`, &create))
}
