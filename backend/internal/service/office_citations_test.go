package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type officeCitationFixtureRepo struct {
	OfficeAgentRepository
	OfficeChatRepository
	operations []OfficeOperation
	readError  error
	reads      int
	finishes   int
	saved      json.RawMessage
}

func (r *officeCitationFixtureRepo) ListOperations(context.Context, int64, string) ([]OfficeOperation, error) {
	r.reads++
	return r.operations, r.readError
}
func (r *officeCitationFixtureRepo) FinishRun(_ context.Context, lease OfficeRunLease, status string, content json.RawMessage, _, _ string) (*OfficeRun, error) {
	r.finishes++
	r.saved = append(json.RawMessage(nil), content...)
	run := lease.Run
	run.Status = status
	return &run, nil
}
func officeCitationFixture(tool string, output any) OfficeOperation {
	input, _ := json.Marshal(map[string]string{"tool": tool})
	result, _ := json.Marshal(output)
	return OfficeOperation{Kind: OfficeOperationTool, State: OfficeOperationCompleted, Input: input, Result: &OfficeOperationResult{Output: result}}
}

func TestOfficeFinishPersistsOnlyConfirmedToolSources(t *testing.T) {
	search := officeCitationFixture("web_search", map[string]any{"results": []map[string]string{
		{"url": "https://Example.com/report#part", "title": "真实资料", "snippet": "实际检索摘要"},
		{"url": "https://example.com/report#other", "title": "重复资料"},
		{"url": "javascript:alert(1)", "title": "危险协议"},
		{"url": "https://user:password@example.com/secret", "title": "含凭证地址"},
	}})
	read := officeCitationFixture("web_read", map[string]string{"url": "https://source.example/article", "text": "实际读取的正文"})
	failed := officeCitationFixture("web_search", map[string]any{"error": "SEARCH_FAILED", "results": []map[string]string{{"url": "https://fake.example/"}}})
	model := search
	model.Kind = OfficeOperationModel
	unknown := search
	unknown.State = OfficeOperationUnknown
	repo := &officeCitationFixtureRepo{operations: []OfficeOperation{search, read, failed, model, unknown}}
	svc := &OfficeAgentService{repo: repo}
	lease := OfficeRunLease{Run: OfficeRun{ID: "run", UserID: 1, SearchEnabled: true}}
	_, err := svc.Finish(context.Background(), lease, OfficeRunCompleted, "正文中模型自报的 https://invented.example/ 不是来源卡", "", "")
	require.NoError(t, err)
	var body struct {
		Text      string           `json:"text"`
		Citations []OfficeCitation `json:"citations"`
	}
	require.NoError(t, json.Unmarshal(repo.saved, &body))
	require.Contains(t, body.Text, "invented.example")
	require.Len(t, body.Citations, 2)
	require.Equal(t, "https://example.com/report", body.Citations[0].URL)
	require.Equal(t, "实际检索摘要", body.Citations[0].Excerpt)
	require.Equal(t, "source.example", body.Citations[1].Title)
	require.Equal(t, "实际读取的正文", body.Citations[1].Excerpt)
	require.NotContains(t, string(repo.saved), "fake.example")
	require.Equal(t, 1, repo.reads)
}

func TestOfficeFinishKeepsPlainTextAndNeverInventsFailedSearchSources(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repo := &officeCitationFixtureRepo{operations: []OfficeOperation{
			officeCitationFixture("web_search", map[string]string{"error": "SEARCH_FAILED"}),
			officeCitationFixture("web_read", map[string]string{"url": "https://empty.example/", "text": ""}),
		}}
		_, err := (&OfficeAgentService{repo: repo}).Finish(context.Background(), OfficeRunLease{Run: OfficeRun{SearchEnabled: enabled}}, OfficeRunCompleted, "未获得联网资料", "", "")
		require.NoError(t, err)
		require.JSONEq(t, `"未获得联网资料"`, string(repo.saved))
		if enabled {
			require.Equal(t, 1, repo.reads)
		} else {
			require.Zero(t, repo.reads, "ordinary chat must not query tool inboxes")
		}
	}
	repo := &officeCitationFixtureRepo{readError: errors.New("source query unavailable")}
	_, err := (&OfficeAgentService{repo: repo}).Finish(context.Background(), OfficeRunLease{Run: OfficeRun{SearchEnabled: true}}, OfficeRunCompleted, "answer", "", "")
	require.ErrorIs(t, err, repo.readError)
	require.Zero(t, repo.finishes)
}

func TestOfficeCitationsBoundMetadataAndRejectUnconfirmedInboxes(t *testing.T) {
	results := make([]map[string]string, 30)
	for i := range results {
		results[i] = map[string]string{"url": fmt.Sprintf("https://source.example/%d", i), "title": strings.Repeat("字", 250), "snippet": strings.Repeat("摘", 700)}
	}
	op := officeCitationFixture("web_search", map[string]any{"results": results})
	citations := officeCitations([]OfficeOperation{op})
	require.Len(t, citations, 24)
	require.Len(t, []rune(citations[0].Title), 200)
	require.Len(t, []rune(citations[0].Excerpt), 600)
	for _, mutate := range []func(*OfficeOperation){
		func(o *OfficeOperation) { o.State = OfficeOperationDispatched },
		func(o *OfficeOperation) { o.Kind = OfficeOperationModel },
		func(o *OfficeOperation) { o.Result = nil },
		func(o *OfficeOperation) { o.Result = &OfficeOperationResult{Output: op.Result.Output, HTTPStatus: 502} },
		func(o *OfficeOperation) {
			o.Result = &OfficeOperationResult{Output: op.Result.Output, ErrorCode: "FAILED"}
		},
		func(o *OfficeOperation) { o.Input = json.RawMessage(`{"tool":"write_file"}`) },
	} {
		candidate := op
		mutate(&candidate)
		require.Empty(t, officeCitations([]OfficeOperation{candidate}))
	}
}
