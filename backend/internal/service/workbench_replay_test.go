package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type replayTestRepository struct {
	workbenchBrowserRepository
	state      OfficeReplayState
	operations map[string][]OfficeOperation
	artifacts  map[string][]OfficeResource
}

func (r *replayTestRepository) BrowserReplayState(context.Context, OfficeRun) (OfficeReplayState, error) {
	return r.state, nil
}

func (r *replayTestRepository) PinBrowserReplay(_ context.Context, _ OfficeRunLease, checkpoint *OfficeReplayCheckpoint, nativeFrom int64) error {
	if checkpoint != nil {
		r.state.Checkpoint = checkpoint
		for len(r.state.Turns) > 0 && r.state.Turns[0].Sequence < checkpoint.BeforeSequence {
			r.state.Turns = r.state.Turns[1:]
		}
	}
	r.state.NativeFrom, r.state.Pinned = nativeFrom, true
	return nil
}

func (r *replayTestRepository) BrowserOperations(_ context.Context, _ int64, id string) ([]OfficeOperation, error) {
	return r.operations[id], nil
}

func (r *replayTestRepository) BrowserContextArtifacts(context.Context, OfficeRun, []string) (map[string][]OfficeResource, error) {
	return r.artifacts, nil
}

func (s *OfficeAgentService) replayTestInput(t *testing.T, repo workbenchBrowserRepository, run OfficeRun, params workbenchBrowserParameters, exchanges []workbenchNativeExchange) (workbenchNativeRequest, error) {
	t.Helper()
	var images workbenchReplayImageReservation
	defer images.release()
	return s.browserModelInput(context.Background(), repo, OfficeRunLease{Run: run}, params, exchanges, &images)
}

func workbenchReplayTestOperation(t *testing.T, operationID string, value any) OfficeOperation {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return OfficeOperation{ID: operationID, State: OfficeOperationCompleted, Result: &OfficeOperationResult{Output: data}}
}

func TestWorkbenchReplayContinuesExactResponsesPrefix(t *testing.T) {
	params := workbenchBrowserParameters{HistoryVersion: 2, Protocol: workbenchProtocolResponses, ContextWindow: 100000, ProfileModel: "gpt-6.2-sol"}
	first := OfficeRun{ID: "first", Model: "gpt-6.2-sol", GroupID: 4}
	second := first
	second.ID = "second"
	response := json.RawMessage(`[{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"create_docx","arguments":"{}"}]`)
	toolResult := json.RawMessage(`{"artifact":{"version_id":"published-version","name":"report.docx"}}`)
	final := workbenchNativeReply{Text: "Created report.", Native: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Created report."}]}]`)}
	call := workbenchNativeCall{ID: "call_1", Name: "create_docx", Arguments: json.RawMessage(`{}`)}
	firstTurn := OfficeReplayTurn{Sequence: 1, RunID: first.ID, Model: first.Model, GroupID: first.GroupID, Parameters: workbenchNativeJSON(params),
		User: OfficeMessage{Role: "user", Content: json.RawMessage(`{"text":"Create a report."}`)}}
	repo := &replayTestRepository{state: OfficeReplayState{Turns: []OfficeReplayTurn{firstTurn}},
		operations: map[string][]OfficeOperation{first.ID: {
			workbenchReplayTestOperation(t, "browser:model:0", OfficeGatewayReply{Status: 200, Native: &workbenchNativeReply{Native: response, Calls: []workbenchNativeCall{call}}}),
			workbenchReplayTestOperation(t, "browser:tool:0:0", toolResult),
			workbenchReplayTestOperation(t, "browser:model:1", OfficeGatewayReply{Status: 200, Native: &final}),
		}}, artifacts: map[string][]OfficeResource{first.ID: {{Name: "report.docx", VersionID: "published-version"}}}}
	svc := &OfficeAgentService{}
	exchanges := []workbenchNativeExchange{{Native: response, Results: []workbenchNativeToolResult{{ID: call.ID, Name: call.Name, Output: toolResult}}}}
	input, err := svc.replayTestInput(t, repo, first, params, exchanges)
	require.NoError(t, err)
	require.True(t, input.StableDeveloperInstructions)
	_, requestOne, err := buildWorkbenchNativeRequest(input)
	require.NoError(t, err)
	firstTurn.NativeAvailable, firstTurn.NativeFrom = true, 1
	firstTurn.Assistant = &OfficeMessage{Role: "assistant", Content: json.RawMessage(`{"text":"Created report.","reasoning_summary":"public UI only"}`)}
	secondTurn := OfficeReplayTurn{Sequence: 3, RunID: second.ID, Model: second.Model, GroupID: second.GroupID,
		User: OfficeMessage{Role: "user", Content: json.RawMessage(`{"text":"Continue and update the footer."}`)}}
	repo.state = OfficeReplayState{Turns: []OfficeReplayTurn{firstTurn, secondTurn}}
	input, err = svc.replayTestInput(t, repo, second, params, nil)
	require.NoError(t, err)
	_, requestTwo, err := buildWorkbenchNativeRequest(input)
	require.NoError(t, err)
	var before, after struct{ Input []json.RawMessage }
	require.NoError(t, json.Unmarshal(requestOne, &before))
	require.NoError(t, json.Unmarshal(requestTwo, &after))
	require.Len(t, after.Input, len(before.Input)+2, "first request: %s", requestOne)
	require.Equal(t, before.Input, after.Input[:len(before.Input)])
	require.EqualValues(t, 1, repo.state.NativeFrom)
	require.NotContains(t, string(requestTwo), "File delivered in this turn")
	require.NotContains(t, string(requestTwo), "public UI only")
}

func TestWorkbenchReplayCompactionPinsNewBoundaryAndRejectsOldSignatures(t *testing.T) {
	params := workbenchBrowserParameters{HistoryVersion: 2, Protocol: workbenchProtocolResponses, ContextWindow: 1800}
	run := OfficeRun{ID: "current", Model: "gpt-6-sol", GroupID: 3}
	turns := []OfficeReplayTurn{
		{Sequence: 1, RunID: "legacy", User: OfficeMessage{Role: "user", Content: workbenchNativeJSON(strings.Repeat("一", 3000))}},
		{Sequence: 3, RunID: "previous", Model: run.Model, GroupID: run.GroupID, NativeAvailable: true, NativeFrom: 3, Parameters: workbenchNativeJSON(params),
			User: OfficeMessage{Role: "user", Content: json.RawMessage(`"Update the title."`)}, Assistant: &OfficeMessage{Role: "assistant", Content: json.RawMessage(`"Title updated."`)}},
		{Sequence: 5, RunID: run.ID, User: OfficeMessage{Role: "user", Content: json.RawMessage(`"Update this document further."`)}},
	}
	operations := []OfficeOperation{workbenchReplayTestOperation(t, "browser:model:0", OfficeGatewayReply{Status: 200,
		Native: &workbenchNativeReply{Text: "Title updated.", Native: json.RawMessage(`[{"type":"reasoning","encrypted_content":"old-signed-prefix"}]`)}})}
	repo := &replayTestRepository{state: OfficeReplayState{Turns: turns}, operations: map[string][]OfficeOperation{"previous": operations},
		artifacts: map[string][]OfficeResource{"legacy": {{Name: "contract.docx", VersionID: "original-reference"}}}}
	svc := &OfficeAgentService{}
	input, err := svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.NotNil(t, repo.state.Checkpoint)
	require.EqualValues(t, 3, repo.state.Checkpoint.BeforeSequence)
	require.EqualValues(t, 5, repo.state.NativeFrom)
	require.Contains(t, input.History[0].Text, "original-reference")
	_, body, err := buildWorkbenchNativeRequest(input)
	require.NoError(t, err)
	require.NotContains(t, string(body), "old-signed-prefix")
	resumed, err := svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.Equal(t, input, resumed)
}

type replayTestImageRepo struct {
	OfficeResourcesRepository
	version OfficeResource
}

func (r replayTestImageRepo) GetResourceVersion(_ context.Context, _ int64, id string, retained bool) (*OfficeResource, error) {
	if id != r.version.VersionID || !retained {
		return nil, ErrOfficeNotFound
	}
	return &r.version, nil
}

type replayTestSettings struct{ SettingRepository }

func (replayTestSettings) GetValue(context.Context, string) (string, error) { return "", nil }

type replayTestImageStore struct {
	GalleryObjectStore
	reads int
	key   string
	limit int64
	data  []byte
	err   error
}

func (s *replayTestImageStore) Get(_ context.Context, key string, limit int64) ([]byte, error) {
	s.reads++
	s.key, s.limit = key, limit
	return s.data, s.err
}

func newReplayImageFixture(t *testing.T, previewBytes int64, data []byte) (*OfficeAgentService, *replayTestRepository, *replayTestImageStore, OfficeRun, workbenchBrowserParameters) {
	t.Helper()
	storage := config.ImageStorageConfig{Endpoint: "https://files.test", Region: "auto", Bucket: "workbench", AccessKeyID: "key", SecretAccessKey: "secret"}
	version := OfficeResource{
		VersionID: "image-version", Endpoint: storage.Endpoint, Region: storage.Region, Bucket: storage.Bucket,
		ObjectKey: "original", Bytes: OfficeResourceMaxBytes, MIME: "image/png",
		PreviewKey: "preview", PreviewBytes: previewBytes, PreviewMIME: "image/jpeg", PreviewSHA256: officeResourceHash(data),
	}
	store := &replayTestImageStore{data: data}
	settings := NewImageStorageSettingService(replayTestSettings{}, nil, nil, nil, storage)
	resourceService := NewOfficeResourcesService(replayTestImageRepo{version: version}, settings,
		func(context.Context, *config.ImageStorageConfig) (GalleryObjectStore, error) { return store, nil }, nil)
	svc := &OfficeAgentService{resources: resourceService, modelRequestBodyLimit: 4096}
	params := workbenchBrowserParameters{HistoryVersion: 2, Protocol: workbenchProtocolResponses, ContextWindow: 100000, SupportsVision: true}
	run := OfficeRun{ID: "current", Model: "gpt-6-sol", GroupID: 3}
	previous := OfficeReplayTurn{Sequence: 1, RunID: "previous", Model: run.Model, GroupID: run.GroupID, NativeAvailable: true, NativeFrom: 1,
		Parameters: workbenchNativeJSON(params), User: OfficeMessage{Role: "user", Content: workbenchNativeJSON(map[string]any{
			"text": "Describe the attached sketch.", "artifacts": []map[string]string{{
				"version_id": version.VersionID, "name": "sketch.png", "mime": "image/png", "model_image_preview_sha256": version.PreviewSHA256,
			}},
		})}, Assistant: &OfficeMessage{Role: "assistant", Content: json.RawMessage(`"It shows a floor plan."`)}}
	current := OfficeReplayTurn{Sequence: 3, RunID: run.ID, User: OfficeMessage{Role: "user", Content: json.RawMessage(`"Update the plan."`)}}
	repo := &replayTestRepository{state: OfficeReplayState{Turns: []OfficeReplayTurn{previous, current}},
		operations: map[string][]OfficeOperation{previous.RunID: {workbenchReplayTestOperation(t, "browser:model:0", OfficeGatewayReply{
			Status: 200, Native: &workbenchNativeReply{Text: "It shows a floor plan.", Native: json.RawMessage(`[{"type":"reasoning","encrypted_content":"image-prefix"}]`)},
		})}}}
	return svc, repo, store, run, params
}

func TestWorkbenchReplayDoesNotDownloadHistoryOverBudget(t *testing.T) {
	svc, repo, store, run, params := newReplayImageFixture(t, 3000, nil)
	input, err := svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.Zero(t, store.reads)
	require.EqualValues(t, 3, repo.state.NativeFrom)
	require.Len(t, input.History, 3)
	require.Contains(t, input.History[0].Text, "sketch.png")
	require.Contains(t, input.History[0].Text, "image-version")
	require.Empty(t, input.History[0].Images)
	require.Equal(t, "It shows a floor plan.", input.History[1].Text)
}

func TestWorkbenchReplayUsesPinnedPreviewWithinBudget(t *testing.T) {
	data := []byte("image bytes")
	svc, repo, store, run, params := newReplayImageFixture(t, int64(len(data)), data)
	input, err := svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
	require.Equal(t, "preview", store.key)
	require.EqualValues(t, len(data), store.limit)
	require.EqualValues(t, 1, repo.state.NativeFrom)
	require.Equal(t, []workbenchNativeImage{{MIME: "image/jpeg", Data: base64.StdEncoding.EncodeToString(data)}}, input.History[0].Images)
	require.Contains(t, string(input.History[1].Native), "image-prefix")

	// Later tool exchanges may exceed an already pinned prefix's budget.
	transcript := []workbenchNativeExchange{{Native: json.RawMessage(`"` + strings.Repeat("x", 4000) + `"`)}}
	_, err = svc.replayTestInput(t, repo, run, params, transcript)
	require.ErrorIs(t, err, errWorkbenchContextTooLarge)
	require.Equal(t, 1, store.reads)
}

func TestWorkbenchReplayRejectsCurrentImageOverBudgetBeforeDownload(t *testing.T) {
	svc, repo, store, run, params := newReplayImageFixture(t, 3000, nil)
	// The same attachment appears on the current user message in this case.
	repo.state.Turns[1].User = repo.state.Turns[0].User
	repo.state.Turns = repo.state.Turns[1:]
	_, err := svc.replayTestInput(t, repo, run, params, nil)
	require.ErrorIs(t, err, errWorkbenchContextTooLarge)
	require.Zero(t, store.reads)
	require.False(t, repo.state.Pinned)
}

func TestWorkbenchReplayKeepsReferenceWhenHistoricalImageExpires(t *testing.T) {
	svc, repo, store, run, params := newReplayImageFixture(t, 100, nil)
	store.err = ErrGalleryNotFound
	input, err := svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
	require.EqualValues(t, 3, repo.state.NativeFrom)
	require.Contains(t, input.History[0].Text, "image-version")
	require.Empty(t, input.History[0].Images)
	resumed, err := svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
	require.Equal(t, input, resumed)
}

func TestWorkbenchReplayDefersImageWhenProcessBudgetIsOccupied(t *testing.T) {
	data := []byte("image bytes")
	svc, repo, store, run, params := newReplayImageFixture(t, int64(len(data)), data)
	var first, second workbenchReplayImageReservation
	defer first.release()
	defer second.release()
	require.NoError(t, first.acquire(workbenchReplayMaxBytes))
	require.NoError(t, second.acquire(workbenchReplayMaxBytes))
	_, err := svc.replayTestInput(t, repo, run, params, nil)
	require.ErrorIs(t, err, ErrOfficeStepBusy)
	require.Zero(t, store.reads)
	require.False(t, repo.state.Pinned)
	second.release()
	_, err = svc.replayTestInput(t, repo, run, params, nil)
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
}
