//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOfficeSearchInboxSourcesSurviveFinishAndHistoryReload(t *testing.T) {
	for _, searchFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed sources", true: "failed search has no sources"}[searchFailed], func(t *testing.T) {
			ctx := context.Background()
			base, input := officeTestAdmission(t)
			input.Mode, input.SearchEnabled, input.Limits.MaxToolCalls = "chat", true, 4
			_, _, err := base.AdmitRun(ctx, input)
			require.NoError(t, err)
			repo := officeChatRepo(t, base)
			lease := officeClaimPreset(t, repo, "chat", input.RunID)
			toolInput := json.RawMessage(`{"tool":"web_search","query":"source evidence","max_results":2}`)
			hash := sha256.Sum256(toolInput)
			_, execute, err := base.PrepareOperation(ctx, *lease, service.OfficeOperationPrepare{ID: "network-source", Kind: service.OfficeOperationTool,
				Input: toolInput, InputHash: hex.EncodeToString(hash[:]), RequestID: "network-source-request"})
			require.NoError(t, err)
			require.True(t, execute)
			_, err = base.DispatchOperation(ctx, *lease, "network-source")
			require.NoError(t, err)
			output := json.RawMessage(`{"results":[{"url":"https://source.example/report","title":"来源标题","snippet":"真实检索结果"}]}`)
			if searchFailed {
				output = json.RawMessage(`{"error":"SEARCH_FAILED","results":[{"url":"https://fake.example/"}]}`)
			}
			_, err = base.RecordOperationResult(ctx, *lease, "network-source", service.OfficeOperationResult{Output: output})
			require.NoError(t, err)
			operations, err := repo.ListOperations(ctx, input.UserID, input.RunID)
			require.NoError(t, err)
			require.Len(t, operations, 1)
			require.JSONEq(t, string(toolInput), string(operations[0].Input))
			require.NotNil(t, operations[0].Result)
			require.JSONEq(t, string(output), string(operations[0].Result.Output))
			_, err = repo.ListOperations(ctx, input.UserID+1, input.RunID)
			require.ErrorIs(t, err, service.ErrOfficeNotFound)
			svc := service.NewOfficeAgentService(base, nil, nil, nil, nil, nil, nil, nil)
			_, err = svc.Finish(ctx, *lease, service.OfficeRunCompleted, "已按真实资料回答", "", "")
			require.NoError(t, err)
			// Emulate an operation written before durable tool summaries existed.
			_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_operations SET tool_projection=NULL WHERE run_id=$1 AND operation_id='network-source'`, input.RunID)
			require.NoError(t, err)
			tx, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			require.NoError(t, compactWorkbenchOperations(ctx, tx, input.UserID, input.RunID, false))
			require.NoError(t, tx.Commit())
			operations, err = repo.ListOperations(ctx, input.UserID, input.RunID)
			require.NoError(t, err)
			require.Len(t, operations, 1)
			require.JSONEq(t, `{"tool":"web_search"}`, string(operations[0].Input))
			require.Equal(t, `null`, string(operations[0].Result.Output))
			if searchFailed {
				require.Nil(t, operations[0].ToolProjection)
			} else {
				require.NotNil(t, operations[0].ToolProjection)
				require.Len(t, operations[0].ToolProjection.Citations, 1)
				require.Equal(t, "https://source.example/report", operations[0].ToolProjection.Citations[0].URL)
				require.Empty(t, operations[0].ToolProjection.Citations[0].Excerpt)
			}
			for _, admin := range []bool{false, true} {
				detail, err := svc.WorkbenchToolDetail(ctx, input.UserID, input.RunID, "network-source", admin)
				require.NoError(t, err)
				require.Equal(t, "source evidence", detail.Query)
				require.Equal(t, map[bool]string{false: "completed", true: "failed"}[searchFailed], detail.State)
			}
			messages, _, _, _, err := repo.MessagePage(ctx, input.UserID, input.ConversationID, 0)
			require.NoError(t, err)
			var saved json.RawMessage
			for _, message := range messages {
				if message.Role == "assistant" && message.RunID == input.RunID {
					saved = message.Content
				}
			}
			require.NotEmpty(t, saved)
			if searchFailed {
				require.JSONEq(t, `"已按真实资料回答"`, string(saved))
				require.NotContains(t, string(saved), "fake.example")
			} else {
				var body struct {
					Text      string                   `json:"text"`
					Citations []service.OfficeCitation `json:"citations"`
				}
				require.NoError(t, json.Unmarshal(saved, &body))
				require.Equal(t, "已按真实资料回答", body.Text)
				require.Len(t, body.Citations, 1)
				require.Equal(t, "https://source.example/report", body.Citations[0].URL)
			}
		})
	}
}
