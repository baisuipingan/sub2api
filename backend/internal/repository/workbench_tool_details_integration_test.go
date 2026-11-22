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

func TestWorkbenchModelPhaseEventsExposeOnlyToolCount(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	input.BrowserDriven = true
	input.Mode = "chat"
	_, _, err := repo.AdmitRun(ctx, input)
	require.NoError(t, err)
	lease, _, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	const operationID = "browser:model:0"
	officePrepared(t, repo, *lease, operationID)
	_, err = repo.DispatchOperation(ctx, *lease, operationID)
	require.NoError(t, err)
	receipt := service.OfficeOperationResult{HTTPStatus: 200, Output: json.RawMessage(`{
		"native_reply":{"text":"visible model commentary","native":{"private":"not a phase event"},
		"calls":[{"name":"web_search","arguments":{"query":"weather"}},{"name":"web_read"}]}
	}`)}
	for range 2 {
		_, err := repo.RecordOperationResult(ctx, *lease, operationID, receipt)
		require.NoError(t, err)
	}
	events, err := repo.ListEvents(ctx, input.UserID, input.RunID, 0, 100)
	require.NoError(t, err)
	var phases []service.OfficeRunEvent
	for _, event := range events {
		if strings.HasPrefix(event.Type, "model.") {
			phases = append(phases, event)
		}
	}
	require.Len(t, phases, 2)
	require.Equal(t, "model.started", phases[0].Type)
	require.JSONEq(t, `{"operation_id":"browser:model:0","tool_count":0}`, string(phases[0].Payload))
	require.Equal(t, "model.completed", phases[1].Type)
	require.JSONEq(t, `{"operation_id":"browser:model:0","tool_count":2}`, string(phases[1].Payload))
}

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
	var started []service.OfficeRunEvent
	for _, event := range events {
		if event.Type == "tool.completed" {
			completed = append(completed, event)
		} else if event.Type == "tool.started" {
			started = append(started, event)
		}
	}
	require.Len(t, started, 2, "only dispatched tools emit a live start")
	require.JSONEq(t, `{"operation_id":"browser:tool:0:0","tool_name":"edit_docx","title":"修改文件","status":"running"}`, string(started[0].Payload))
	require.Len(t, completed, 1)
	require.Less(t, started[0].Sequence, completed[0].Sequence)
	require.JSONEq(t, `{"operation_id":"browser:tool:0:0","tool_name":"edit_docx","title":"修改文件","status":"partial"}`, string(completed[0].Payload))
}
