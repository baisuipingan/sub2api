package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchRouteCorrelationMatchesWorkerPerChildAndUser(t *testing.T) {
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 9, TaskID: "same-task"}, Call: StudioJobCall{Ordinal: 2}}
	require.Equal(t, studioClientRequestID(claim), StudioJobCallRequestID(9, "same-task", 2))
	require.NotEqual(t, StudioJobCallRequestID(9, "same-task", 2), StudioJobCallRequestID(10, "same-task", 2))
	require.NotEqual(t, StudioJobCallRequestID(9, "same-task", 2), StudioJobCallRequestID(9, "same-task", 3))
}

func TestOpsErrorAttemptsSnapshotEffectiveModelBeforeFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetOpsUpstreamModel(c, "first-model-2k")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 1, Kind: "failover"})
	ClearOpsUpstreamModel(c)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 2, Stage: "account_auth"})
	SetOpsUpstreamModel(c, "second-model-2k")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 2, Kind: "failover"})
	raw, _ := c.Get(OpsUpstreamErrorsKey)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Equal(t, "first-model-2k", events[0].UpstreamModel)
	require.Empty(t, events[1].UpstreamModel)
	require.Equal(t, "second-model-2k", events[2].UpstreamModel)
}

func TestOpsErrorAttemptModelSnapshotIsBounded(t *testing.T) {
	entry := &OpsInsertErrorLogInput{UpstreamErrors: []*OpsUpstreamErrorEvent{{AccountID: 1, UpstreamStatusCode: 503, UpstreamModel: strings.Repeat("x", 1000)}}}
	require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
	require.NotNil(t, entry.UpstreamErrorsJSON)
	events, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
	require.NoError(t, err)
	require.Len(t, events[0].UpstreamModel, 256)
}
