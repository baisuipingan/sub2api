//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchRemoteToolReceiptPersistsStatusAndEvent(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	input.BrowserDriven = true
	input.Mode = "chat"
	_, _, err := repo.AdmitRun(ctx, input)
	require.NoError(t, err)
	original, _, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	prepare := func(id, body string) {
		_, _, err := repo.PrepareOperation(ctx, *original, service.OfficeOperationPrepare{
			ID: id, Kind: service.OfficeOperationTool, InputHash: strings.Repeat("0", 64), Input: json.RawMessage(body), RequestID: uuid.NewString(),
		})
		require.NoError(t, err)
		_, err = repo.DispatchOperation(ctx, *original, id)
		require.NoError(t, err)
	}
	const remote, local = "browser:tool:0:0", "browser:tool:0:1"
	prepare(remote, `{"tool":"edit_docx","remote":true}`)
	prepare(local, `{"tool":"web_search","query":"query"}`)
	require.NoError(t, repo.ReleaseBrowserStep(ctx, *original))
	recovered, _, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	receipt := service.OfficeOperationResult{ToolStatus: "partial", Output: json.RawMessage(`{"processed":1,"failed":1}`)}
	_, err = repo.RecordOperationResult(ctx, *recovered, local, receipt)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, err = repo.RecordOperationResult(ctx, *recovered, remote, receipt)
	require.NoError(t, err)
	_, err = repo.RecordOperationResult(ctx, *recovered, remote, receipt)
	require.NoError(t, err)
	uncertain := *recovered
	uncertain.Token = uuid.NewString()
	_, err = repo.RecordOperationResult(ctx, uncertain, remote, receipt)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)

	operations, err := repo.WorkbenchToolOperations(ctx, input.UserID, input.RunID)
	require.NoError(t, err)
	require.Len(t, operations, 2)
	require.Equal(t, "partial", service.WorkbenchToolOperationSummary(recovered.Run, operations[0]).State)
	require.Equal(t, "running", service.WorkbenchToolOperationSummary(recovered.Run, operations[1]).State)
	foreign, err := repo.WorkbenchToolOperations(ctx, input.UserID+1000, input.RunID)
	require.NoError(t, err)
	require.Empty(t, foreign)

	events, err := repo.ListEvents(ctx, input.UserID, input.RunID, 0, 100)
	require.NoError(t, err)
	var completed []service.OfficeRunEvent
	for _, event := range events {
		if event.Type == "tool.completed" {
			completed = append(completed, event)
		}
	}
	require.Len(t, completed, 1)
	require.JSONEq(t, `{"operation_id":"browser:tool:0:0","tool_name":"edit_docx","title":"修改文件","status":"partial"}`, string(completed[0].Payload))
}
