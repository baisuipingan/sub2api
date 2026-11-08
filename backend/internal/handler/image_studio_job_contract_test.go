//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStudioStaleParameterContractRequiresRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	studioJobHTTPError(ctx, service.ErrStudioJobRouteChanged)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "MODEL_ROUTE_CHANGED")
	require.Contains(t, recorder.Body.String(), "refresh the model list")
}
