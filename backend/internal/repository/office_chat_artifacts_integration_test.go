//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOfficeChatHistoryArtifactsPreserveRunVersionsWithoutEventReplay(t *testing.T) {
	ctx := context.Background()
	agent, first, resources, svc, _ := officeResourceFixture(t)
	chat := officeChatRepo(t, agent)
	first.Mode = "office"
	upload, err := svc.Upload(ctx, first.UserID, "uploaded.txt", []byte("original upload"), "history-input")
	require.NoError(t, err)
	first.InputResources = []service.OfficeResourceVersion{{ResourceID: upload.ID, VersionID: upload.VersionID}}
	_, _, err = agent.AdmitRun(ctx, first)
	require.NoError(t, err)
	lease := officeClaimPreset(t, chat, "office", first.RunID)
	files := []service.OfficeWorkspaceFile{
		officeWorkspaceFile("output/report.txt", []byte("first report")),
		officeWorkspaceFile("output/uploaded.txt", []byte("edited upload")),
		officeWorkspaceFile("output/deleted.txt", []byte("deleted report")),
	}
	_, err = svc.SaveWorkspace(ctx, *lease, service.OfficeWorkspaceSave{OperationID: "history-save-first", ExpectedVersion: 0, Files: files})
	require.NoError(t, err)
	publication := func(file service.OfficeWorkspaceFile, id string) service.OfficeArtifactPublish {
		return service.OfficeArtifactPublish{PublicationID: id, Path: file.Path, SHA256: file.SHA256, MIME: file.MIME, Size: file.Size, WorkspaceVersion: 1}
	}
	original, err := svc.PublishArtifact(ctx, *lease, publication(files[0], "history-report-first"))
	require.NoError(t, err)
	edit := publication(files[1], "history-upload-edit")
	edit.ResourceID, edit.ParentVersionID = upload.ID, upload.VersionID
	editedUpload, err := svc.PublishArtifact(ctx, *lease, edit)
	require.NoError(t, err)
	deleted, err := svc.PublishArtifact(ctx, *lease, publication(files[2], "history-deleted"))
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, first.UserID, deleted.ID))
	pending := service.OfficeResource{ID: uuid.NewString(), VersionID: uuid.NewString(), UserID: first.UserID, RunID: first.RunID, PublicationID: "history-pending", Name: "unpublished.txt", MIME: "text/plain", Bytes: 1, SHA256: strings.Repeat("b", 64), Source: "generated", PreviewKind: "text", Endpoint: "https://private.example", Region: "auto", Bucket: "private-files", ObjectKey: "workspace/v1/resources/pending-history"}
	_, err = resources.ReserveVersion(ctx, pending, lease)
	require.NoError(t, err)
	_, err = agent.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`"first answer without embedded artifacts"`), "", "")
	require.NoError(t, err)

	second := officeNextAdmission(first, "modify the first report")
	second.InputResources = []service.OfficeResourceVersion{{ResourceID: original.ID, VersionID: original.VersionID}}
	_, _, err = agent.AdmitRun(ctx, second)
	require.NoError(t, err)
	lease = officeClaimPreset(t, chat, "office", second.RunID)
	changed := officeWorkspaceFile("output/report.txt", []byte("second report"))
	_, err = svc.SaveWorkspace(ctx, *lease, service.OfficeWorkspaceSave{OperationID: "history-save-second", ExpectedVersion: 0, Files: []service.OfficeWorkspaceFile{changed}})
	require.NoError(t, err)
	edit = publication(changed, "history-report-second")
	edit.ResourceID, edit.ParentVersionID = original.ID, original.VersionID
	latest, err := svc.PublishArtifact(ctx, *lease, edit)
	require.NoError(t, err)
	_, err = agent.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`"second answer without embedded artifacts"`), "", "")
	require.NoError(t, err)

	// A current writer is returned alongside historical runs but must not be
	// mutated by the history projection, nor gain older runs' files.
	third := officeNextAdmission(second, "an in-flight question")
	_, _, err = agent.AdmitRun(ctx, third)
	require.NoError(t, err)
	activeLease := officeClaimPreset(t, chat, "office", third.RunID)
	beforeActive, err := agent.GetRun(ctx, first.UserID, third.RunID)
	require.NoError(t, err)
	messages, runs, more, _, err := chat.MessagePage(ctx, first.UserID, first.ConversationID, 0)
	require.NoError(t, err)
	require.False(t, more)
	require.Len(t, messages, 5)
	require.Len(t, runs, 3)
	byRun := map[string]service.OfficeRun{}
	for _, run := range runs {
		require.NotNil(t, run.Artifacts, "history must explicitly project even an empty artifact array")
		byRun[run.ID] = run
	}
	require.Len(t, *byRun[first.RunID].Artifacts, 2, "pending and deleted outputs must not become history cards")
	byVersion := map[string]service.OfficeResource{}
	for _, artifact := range *byRun[first.RunID].Artifacts {
		byVersion[artifact.VersionID] = artifact
		require.Equal(t, "output", artifact.Role)
		require.Empty(t, artifact.Endpoint)
		require.Empty(t, artifact.Bucket)
		require.Empty(t, artifact.ObjectKey, "history reads public metadata without private storage targets")
	}
	require.Equal(t, original.SHA256, byVersion[original.VersionID].SHA256)
	require.Equal(t, original.ID, byVersion[original.VersionID].ID)
	require.Equal(t, "upload", byVersion[editedUpload.VersionID].Source, "generated edits of uploaded files are outputs too")
	require.NotContains(t, byVersion, latest.VersionID, "old runs must not silently switch to the resource's latest version")
	require.NotContains(t, byVersion, upload.VersionID, "an input version is not an output")
	require.Len(t, *byRun[second.RunID].Artifacts, 1)
	require.Equal(t, latest.VersionID, (*byRun[second.RunID].Artifacts)[0].VersionID)
	require.Equal(t, original.VersionID, (*byRun[second.RunID].Artifacts)[0].ParentVersionID)
	require.Empty(t, *byRun[third.RunID].Artifacts)
	encoded, err := json.Marshal(byRun[third.RunID])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"artifacts":[]`)
	encoded, err = json.Marshal(runs)
	require.NoError(t, err)
	for _, private := range []string{"private-files", "private.example", "workspace/v1/", "temporary=", "object_key", "storage_bucket"} {
		require.NotContains(t, string(encoded), private)
	}
	for _, message := range messages {
		if message.RunID == first.RunID && message.Role == "assistant" {
			require.JSONEq(t, `"first answer without embedded artifacts"`, string(message.Content), "artifact projection must not rewrite stored assistant content")
		}
	}
	afterActive, err := agent.GetRun(ctx, first.UserID, third.RunID)
	require.NoError(t, err)
	require.Equal(t, *beforeActive, *afterActive)
	require.Nil(t, afterActive.Artifacts, "ordinary/internal run reads do not hydrate history metadata")
	require.NoError(t, agent.CheckLease(ctx, *activeLease))

	// An older message page still restores the version belonging to that page,
	// while the current writer is included only with its own empty projection.
	_, olderRuns, _, _, err := chat.MessagePage(ctx, first.UserID, first.ConversationID, 3)
	require.NoError(t, err)
	require.Len(t, olderRuns, 2)
	for _, run := range olderRuns {
		if run.ID == first.RunID {
			require.Equal(t, byRun[first.RunID].Artifacts, run.Artifacts)
		} else {
			require.Equal(t, third.RunID, run.ID)
			require.Empty(t, *run.Artifacts)
		}
	}
	_, _, _, _, err = chat.MessagePage(ctx, first.UserID+999, first.ConversationID, 0)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)

	// Explicit empty arrays allow a refreshed page to remove deleted cards,
	// even though retained conversation references still authorize old bytes.
	require.NoError(t, svc.Delete(ctx, first.UserID, original.ID))
	_, runs, _, _, err = chat.MessagePage(ctx, first.UserID, first.ConversationID, 0)
	require.NoError(t, err)
	for _, run := range runs {
		if run.ID == second.RunID {
			require.NotNil(t, run.Artifacts)
			require.Empty(t, *run.Artifacts)
		}
	}
}
