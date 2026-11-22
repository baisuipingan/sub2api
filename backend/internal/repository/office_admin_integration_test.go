//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOfficeAdminResourcesListsAssistantFilesAcrossUsers(t *testing.T) {
	ctx := context.Background()
	agent, first, resources, files, _ := officeResourceFixture(t)
	secondAgent, second, _, secondFiles, _ := officeResourceFixture(t)
	admin := agent.(service.OfficeAdminRepository)
	filter := service.OfficeAdminFilter{Page: 1, PageSize: 100}
	before, err := admin.AdminResources(ctx, filter)
	require.NoError(t, err)

	upload, err := files.Upload(ctx, first.UserID, "meeting-notes.txt", []byte("notes"), "admin-file-upload")
	require.NoError(t, err)
	lease := officeResourceClaim(t, secondAgent, second)
	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 8, 8))))
	document := officeWorkspaceFile("output/assistant-image.png", imageData.Bytes())
	document.MIME = "image/png"
	workspace, err := secondFiles.SaveWorkspace(ctx, *lease, service.OfficeWorkspaceSave{
		OperationID: "admin-file-snapshot", Files: []service.OfficeWorkspaceFile{document},
	})
	require.NoError(t, err)
	generated, err := secondFiles.PublishArtifact(ctx, *lease, service.OfficeArtifactPublish{
		PublicationID: "admin-file-artifact", WorkspaceVersion: workspace.Version, Path: document.Path,
		MIME: document.MIME, Size: document.Size, SHA256: document.SHA256,
	})
	require.NoError(t, err)

	gallery := NewImageStudioGalleryRepository(integrationDB)
	imageID := "gallery-" + uuid.NewString()
	_, err = gallery.ReserveAsset(ctx, galleryTestAsset(first.UserID, imageID))
	require.NoError(t, err)
	require.NoError(t, gallery.MarkAssetReady(ctx, first.UserID, imageID))
	galleryTaskID := uuid.NewString()
	_, err = gallery.SaveTask(ctx, first.UserID, galleryTaskID, strings.Repeat("b", 64),
		json.RawMessage(`{"status":"done"}`), []service.GalleryTaskLink{{ImageID: imageID, Role: "output"}})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM image_studio_gallery_tasks WHERE user_id=$1 AND task_id=$2`, first.UserID, galleryTaskID)
		require.NoError(t, err)
	})
	require.NoError(t, resources.SyncGallery(ctx, first.UserID))
	personal, _, err := resources.ListResources(ctx, first.UserID, "", 20)
	require.NoError(t, err)
	require.Len(t, personal, 2)
	var galleryVersion string
	for _, item := range personal {
		if item.Source == "gallery" {
			galleryVersion = item.VersionID
		}
	}
	require.NotEmpty(t, galleryVersion, "the existing Studio reference is persisted")

	all, err := admin.AdminResources(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, before.Total+2, all.Total)
	require.Len(t, all.Items, int(before.Total+2))
	seen := make(map[string]service.OfficeAdminResource, len(all.Items))
	for _, item := range all.Items {
		seen[item.VersionID] = item
	}
	require.Equal(t, first.UserID, seen[upload.VersionID].OwnerID)
	require.Equal(t, "upload", seen[upload.VersionID].Source)
	require.Equal(t, second.UserID, seen[generated.VersionID].OwnerID)
	require.Equal(t, "generated", seen[generated.VersionID].Source)
	require.Equal(t, "image/png", seen[generated.VersionID].MIME)
	require.NotContains(t, seen, galleryVersion)

	filter.UserID = first.UserID
	page, err := admin.AdminResources(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, upload.VersionID, page.Items[0].VersionID)
	filter.UserID = second.UserID
	page, err = admin.AdminResources(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, generated.VersionID, page.Items[0].VersionID)

	overview, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, all.Total, overview.ResourceCount)
	owner, err := admin.AdminResourceOwner(ctx, generated.VersionID)
	require.NoError(t, err)
	require.Equal(t, second.UserID, owner)
	owner, err = admin.AdminResourceOwner(ctx, galleryVersion)
	require.NoError(t, err, "conversation details still preview existing gallery attachments")
	require.Equal(t, first.UserID, owner)
}

func TestRetireLegacyWorkbenchRunKeepsHistoryAndAllowsNewBrowserTurn(t *testing.T) {
	ctx := context.Background()
	repo, legacy := officeTestAdmission(t)
	legacy.Mode = "chat"
	_, _, err := repo.AdmitRun(ctx, legacy)
	require.NoError(t, err)

	browser := legacy
	browser.RunID, browser.IdempotencyKey, browser.BrowserDriven = uuid.NewString(), uuid.NewString(), true
	_, _, err = repo.AdmitRun(ctx, browser)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun)

	migration, err := migrations.FS.ReadFile("dsapi_013_retire_legacy_workbench_runs.sql")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	historical, err := repo.GetRun(ctx, legacy.UserID, legacy.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunCancelled, historical.Status)
	messages, err := repo.ListMessages(ctx, legacy.UserID, legacy.ConversationID, 0, 20)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, string(legacy.UserContent), string(messages[0].Content))
	_, created, err := repo.AdmitRun(ctx, browser)
	require.NoError(t, err)
	require.True(t, created)
}

func TestOfficeAdminOverviewCountsBrowserRunsAndSettledModelCalls(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	input.Mode = "chat"
	_, browser := officeTestAdmission(t)
	browser.Mode = "chat"
	browser.BrowserDriven = true
	admin, ok := base.(service.OfficeAdminRepository)
	require.True(t, ok)
	before, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	_, _, err = base.AdmitRun(ctx, input)
	require.NoError(t, err)
	_, _, err = base.AdmitRun(ctx, browser)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET status='running' WHERE id IN ($1, $2)`, input.RunID, browser.RunID)
	require.NoError(t, err)
	running, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before.TotalRuns+1, running.TotalRuns)
	require.Equal(t, before.Completed, running.Completed)
	for execution, id := range map[string]string{"browser": browser.RunID, "historical": input.RunID} {
		page, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 1, PageSize: 20, Execution: execution, Status: "running"})
		require.NoError(t, err)
		require.NotEmpty(t, page.Items)
		require.Equal(t, id, page.Items[0].ID)
	}
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET mode='office',status='completed',model_calls=2,tool_calls=1 WHERE id=$1`, input.RunID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET status='partially_completed',model_calls=4 WHERE id=$1`, browser.RunID)
	require.NoError(t, err)
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('overview-test','openai','apikey') RETURNING id`).Scan(&accountID))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		require.NoError(t, err)
	})
	insertOperation := func(run service.OfficeRunAdmission, result string, receipt bool, cost string) {
		t.Helper()
		request := uuid.NewString()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO office_agent_operations(user_id,run_id,operation_id,kind,state,input_hash,input_json,request_id,result_json) VALUES($1,$2,$3,'model','completed',$4,'{}',$3,$5::jsonb)`, run.UserID, run.RunID, request, run.RequestHash, result)
		require.NoError(t, err)
		if receipt {
			_, err = integrationDB.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,input_tokens,actual_cost) VALUES($1,$2,$3,$4,'public-model',10,$5::numeric)`, run.UserID, run.APIKeyID, accountID, "client:"+request, cost)
			require.NoError(t, err)
		}
	}
	insertOperation(input, `{"http_status":200}`, true, "0.25")
	insertOperation(browser, `{"http_status":200}`, true, "0.0033")
	insertOperation(browser, `{"http_status":200}`, true, "0")
	insertOperation(browser, `{"http_status":502}`, true, "0")
	insertOperation(browser, `{"http_status":200}`, false, "0")
	completed, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before.Completed, completed.Completed)
	require.Equal(t, before.PartiallyCompleted+1, completed.PartiallyCompleted)
	require.Equal(t, before.ModelCalls+2, completed.ModelCalls)
	require.Equal(t, before.InputTokens+20, completed.InputTokens)
	require.Equal(t, before.ModelChargeCredits+330_000, completed.ModelChargeCredits)
	require.Equal(t, before.ChargeCredits+330_000, completed.ChargeCredits)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, browser.UserID)
	require.NoError(t, err)
	deleted, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before.TotalRuns, deleted.TotalRuns)
	require.Equal(t, before.ModelCalls, deleted.ModelCalls)
}

func TestOfficeAdminPageAndUsageReadOriginalLedgerWithExactUserAndKey(t *testing.T) {
	ctx := context.Background()
	base, input, lease := officeAdmitAndClaim(t)
	admin, ok := base.(service.OfficeAdminRepository)
	require.True(t, ok)
	_, other := officeTestAdmission(t)
	var account, wrongKey int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('observed-upstream','openai','apikey') RETURNING id`).Scan(&account))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,group_id,key,name,key_type) VALUES($1,$2,$3,'other-system-key','system') RETURNING id`, input.UserID, input.GroupID, "sk-"+uuid.NewString()).Scan(&wrongKey))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, account)
		require.NoError(t, err, "clean office admin fixture account %d", account)
	})
	op := officePrepared(t, base, *lease, "model-one")
	unsettled := officePrepared(t, base, *lease, "model-two")
	insertUsage := func(user, key int64, request string, tokens, credits int64) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,upstream_model,input_tokens,output_tokens,cache_read_tokens,actual_cost,charge_credits,duration_ms) VALUES($1,$2,$3,$4,'public-model','observed-model',$5,7,3,0.123,$6,456)`, user, key, account, "client:"+request, tokens, credits)
		require.NoError(t, err)
	}
	insertUsage(input.UserID, input.APIKeyID, op.RequestID, 11, 12345)
	insertUsage(input.UserID, wrongKey, op.RequestID, 999, 99999)
	insertUsage(other.UserID, input.APIKeyID, unsettled.RequestID, 777, 77777)
	for range 35 {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(client_request_id,user_id,api_key_id,account_id,error_phase,error_type,upstream_status_code,upstream_model) VALUES($1,$2,$3,$4,'upstream','upstream_error',503,'failed-model')`, op.RequestID, input.UserID, input.APIKeyID, account)
		require.NoError(t, err)
	}
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(client_request_id,user_id,api_key_id,account_id,error_phase,error_type,upstream_status_code) VALUES($1,$2,$3,$4,'upstream','upstream_error',500),($1,$5,$6,$4,'upstream','upstream_error',500)`, unsettled.RequestID, input.UserID, wrongKey, account, other.UserID, input.APIKeyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM ops_error_logs WHERE client_request_id IN($1,$2)`, op.RequestID, unsettled.RequestID)
		require.NoError(t, err, "clean office admin fixture errors")
	})
	page, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 1, PageSize: 20, UserID: input.UserID})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, input.RunID, page.Items[0].ID)
	require.EqualValues(t, 11, page.Items[0].InputTokens)
	require.EqualValues(t, 7, page.Items[0].OutputTokens)
	require.EqualValues(t, 12345, page.Items[0].ChargeCredits)
	require.Equal(t, input.Limits, page.Items[0].Limits, "SQL JSON projection preserves the public run contract")
	emptyPage, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 2, PageSize: 20, UserID: input.UserID})
	require.NoError(t, err)
	require.Empty(t, emptyPage.Items)
	require.EqualValues(t, 1, emptyPage.Total, "out-of-range page still returns the exact count from the same SQL snapshot")
	filtered, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 1, PageSize: 20, UserID: other.UserID})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)
	detail, err := admin.AdminRun(ctx, input.RunID)
	require.NoError(t, err)
	require.Len(t, detail.Operations, 2)
	byID := map[string]service.OfficeCallDetail{}
	for _, call := range detail.Operations {
		byID[call.ID] = call
	}
	require.True(t, byID[op.ID].Usage.Settled)
	require.EqualValues(t, 11, byID[op.ID].Usage.InputTokens)
	require.EqualValues(t, 3, byID[op.ID].Usage.CacheReadTokens)
	require.InDelta(t, 0.123, byID[op.ID].Usage.ActualCost, 1e-9)
	require.Len(t, byID[op.ID].Usage.Upstreams, 31, "one observed usage plus at most thirty failed attempts per operation")
	require.False(t, byID[unsettled.ID].Usage.Settled, "same request under a different owner cannot settle this operation")
	require.Empty(t, byID[unsettled.ID].Usage.Upstreams)
	public, err := admin.RunUsage(ctx, input.UserID, input.RunID, false)
	require.NoError(t, err)
	for _, call := range public {
		require.Empty(t, call.Usage.Upstreams, "public call records never expose observed account identities")
	}
	encoded, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "observed-upstream")
	require.NotContains(t, string(encoded), "observed-model")
	_, err = admin.RunUsage(ctx, other.UserID, input.RunID, false)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	var usageCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE account_id=$1`, account).Scan(&usageCount))
	require.Equal(t, 3, usageCount, "admin reads do not write or reprice the original ledger")
}

func TestOfficeAdminRunMessagesPaginatesAndRespectsConversationDeletion(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	admin := base.(service.OfficeAdminRepository)
	chat := base.(service.OfficeChatRepository)
	input.BrowserDriven = true
	input.UserContent = json.RawMessage(`"旧版问题"`)
	_, _, err := base.AdmitRun(ctx, input)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
WITH extra_runs AS (
	SELECT gen_random_uuid() AS id,n FROM generate_series(2,27) AS seq(n)
), inserted AS (
	INSERT INTO office_agent_runs (id,user_id,conversation_id,idempotency_key,request_hash,api_key_id,group_id,model,status,limits_json)
	SELECT extra.id,r.user_id,r.conversation_id,extra.id::text,r.request_hash,r.api_key_id,r.group_id,r.model,'completed',r.limits_json
	FROM office_agent_runs r CROSS JOIN extra_runs extra WHERE r.id=$1
	RETURNING id,user_id,conversation_id
)
INSERT INTO office_agent_messages (id,user_id,conversation_id,run_id,sequence,role,content)
SELECT gen_random_uuid(),r.user_id,r.conversation_id,r.id,2*extra.n-2+part.sequence_delta,part.role,to_jsonb(part.text)
FROM inserted r JOIN extra_runs extra ON extra.id=r.id
CROSS JOIN (VALUES (0,'user','后续问题'::text),(1,'assistant','后续回答'::text)) AS part(sequence_delta,role,text)`, input.RunID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET resume_value='"旧版补充回答"'::jsonb WHERE id=$1`, input.RunID)
	require.NoError(t, err)

	otherConversation := uuid.NewString()
	_, err = base.CreateConversation(ctx, input.UserID, otherConversation, "另一场对话")
	require.NoError(t, err)
	other := input
	other.ConversationID, other.RunID, other.IdempotencyKey = otherConversation, uuid.NewString(), uuid.NewString()
	other.UserContent = json.RawMessage(`"同用户的另一场对话"`)
	otherTurn, err := base.CancelRun(ctx, input.UserID, input.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunCancelled, otherTurn.Status)
	_, _, err = base.AdmitRun(ctx, other)
	require.NoError(t, err)

	first, err := admin.AdminRunMessages(ctx, input.RunID, 0)
	require.NoError(t, err)
	require.Equal(t, "办公对话", first.ConversationTitle)
	require.Len(t, first.Messages, 50)
	require.EqualValues(t, 4, first.Messages[0].Sequence)
	require.EqualValues(t, 53, first.Messages[49].Sequence)
	require.True(t, first.HasMore)
	require.EqualValues(t, 4, first.NextBefore)
	require.NotNil(t, first.SelectedUserMessage)
	require.Equal(t, input.RunID, first.SelectedUserMessage.RunID)
	require.JSONEq(t, `"旧版问题"`, string(first.SelectedUserMessage.Content))
	for _, message := range first.Messages {
		require.Equal(t, input.ConversationID, message.ConversationID)
	}

	older, err := admin.AdminRunMessages(ctx, input.RunID, first.NextBefore)
	require.NoError(t, err)
	require.Len(t, older.Messages, 3)
	require.EqualValues(t, 1, older.Messages[0].Sequence)
	require.EqualValues(t, 3, older.Messages[2].Sequence)
	require.False(t, older.HasMore)
	require.Equal(t, "assistant", older.Messages[2].Role)
	require.Len(t, older.Runs, 2)
	for _, run := range older.Runs {
		require.Empty(t, run.ResumeValue)
	}

	require.NoError(t, chat.DeleteConversation(ctx, input.UserID, input.ConversationID))
	_, err = admin.AdminRunMessages(ctx, input.RunID, 0)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	detail, err := admin.AdminRun(ctx, input.RunID)
	require.NoError(t, err)
	require.Equal(t, input.RunID, detail.Run.ID)
	require.Empty(t, detail.Run.ResumeValue)
}
