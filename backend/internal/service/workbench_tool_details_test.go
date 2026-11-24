package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchToolDetailsDistinguishEmptySearchFromStoredFailures(t *testing.T) {
	start := time.Now()
	finish := start.Add(700 * time.Millisecond)
	op := OfficeOperation{
		ID: "browser:tool:0:0", Kind: OfficeOperationTool, State: OfficeOperationCompleted,
		Input: json.RawMessage(`{"tool":"web_search","query":"  上海   天气 "}`), DispatchedAt: &start, CompletedAt: &finish,
		Result: &OfficeOperationResult{Output: json.RawMessage(`{"provider":"brave","results":[]}`)},
	}
	detail := workbenchToolDetail(OfficeRun{}, op)
	require.Equal(t, "completed", detail.State)
	require.Equal(t, "上海 天气", detail.Query)
	require.Equal(t, "brave", detail.Provider)
	require.NotNil(t, detail.ResultCount)
	require.Zero(t, *detail.ResultCount)
	require.NotNil(t, detail.DurationMS)
	require.EqualValues(t, 700, *detail.DurationMS)

	op.Result = &OfficeOperationResult{Output: json.RawMessage(`{"error":"SEARCH_FAILED","message":"private upstream body"}`)}
	detail = workbenchToolDetail(OfficeRun{}, op)
	require.Equal(t, "failed", detail.State)
	require.Nil(t, detail.ResultCount)
	require.NotContains(t, detail.ErrorMessage, "private")
}

func TestWorkbenchToolDetailsAllowlistAndBatchOutcome(t *testing.T) {
	const version = "d94c674b-e514-43c5-82b2-fe2c3cf40355"
	op := OfficeOperation{
		ID: "browser:tool:0:0", Kind: OfficeOperationTool, State: OfficeOperationCompleted,
		Input: json.RawMessage(`{"tool":"batch_edit_docx","arguments":{"version_id":"` + version + `","scope":"header","edits":["original private content"]}}`),
		Result: &OfficeOperationResult{Output: json.RawMessage(`{
			"processed":1,"skipped":0,"failed":1,"replacements":[2,3],
			"private_url":"https://example.com/?token=secret",
			"artifact":{"name":"result.zip","version_id":"` + version + `","secret":"hidden"},
			"files":[{"name":"in/report.docx","status":"modified","replacements":0},{"name":"in/fail.docx","status":"failed"}]
		}`)},
	}
	detail := workbenchToolDetail(OfficeRun{}, op)
	require.Equal(t, "partial", detail.State)
	require.Equal(t, version, detail.SourceVersion)
	require.Equal(t, "header", detail.Scope)
	require.NotNil(t, detail.Replacements)
	require.Equal(t, 5, *detail.Replacements)
	require.Len(t, detail.Artifacts, 1)
	require.Len(t, detail.Files, 2)
	require.NotNil(t, detail.Files[0].Replacements)
	require.Zero(t, *detail.Files[0].Replacements)
	encoded, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "token")
	require.NotContains(t, string(encoded), "private content")
	require.NotContains(t, string(encoded), "hidden")
	op.Input = json.RawMessage(`{"tool":"web_read","url":"https://example.com/path?token=secret"}`)
	op.Result.Output = json.RawMessage(`{"url":"https://redirect.example.com/private?session=hidden","text":"secret document"}`)
	detail = workbenchToolDetail(OfficeRun{}, op)
	require.Equal(t, "redirect.example.com", detail.Source)
}

func TestWorkbenchToolProjectionPreservesDetailsWithoutRawContent(t *testing.T) {
	const version = "d94c674b-e514-43c5-82b2-fe2c3cf40355"
	start := time.Now()
	finish := start.Add(700 * time.Millisecond)
	op := OfficeOperation{
		ID: "browser:tool:0:0", Kind: OfficeOperationTool, State: OfficeOperationCompleted,
		DispatchedAt: &start, CompletedAt: &finish,
		Input: json.RawMessage(`{"tool":"batch_edit_docx","arguments":{"version_id":"` + version + `","scope":"header","edits":["private source text"]}}`),
		Result: &OfficeOperationResult{ToolStatus: "partial", Output: json.RawMessage(`{
			"succeeded":1,"failed":1,"replacements":[2,3],
			"artifact":{"name":"result.zip","version_id":"` + version + `","mime":"application/zip"},
			"files":[{"name":"ok.docx","status":"completed","replacements":5},{"name":"bad.docx","status":"failed","message":"private source text"}]
		}`)},
	}
	projection := BuildWorkbenchToolProjection(op)
	require.NotNil(t, projection)
	snapshot, err := json.Marshal(projection)
	require.NoError(t, err)
	require.NotContains(t, string(snapshot), "private source text")

	op.Input, op.Result = nil, nil
	require.NoError(t, json.Unmarshal(snapshot, &op.ToolProjection))
	detail := workbenchToolDetail(OfficeRun{}, op)
	require.Equal(t, op.ID, detail.ID)
	require.Equal(t, "批量修改 Word", detail.Title)
	require.Equal(t, "partial", detail.State)
	require.Equal(t, version, detail.SourceVersion)
	require.Equal(t, "header", detail.Scope)
	require.EqualValues(t, 5, *detail.Replacements)
	require.EqualValues(t, 700, *detail.DurationMS)
	require.Equal(t, "result.zip", detail.Artifacts[0].Name)
	require.Equal(t, "bad.docx", detail.Files[1].Name)
	require.Empty(t, detail.Files[1].Message)

	pending := OfficeOperation{Kind: OfficeOperationTool, State: OfficeOperationDispatched, Input: json.RawMessage(`{"tool":"read_docx"}`)}
	pending.ToolProjection = BuildWorkbenchToolProjection(pending)
	require.Equal(t, "cancelled", WorkbenchToolOperationSummary(OfficeRun{Status: OfficeRunCancelled}, pending).State)
	require.Nil(t, BuildWorkbenchToolProjection(OfficeOperation{Kind: OfficeOperationModel}))
}

func TestWorkbenchToolProjectionRetainsConfirmedSourceLinksAndBoundsSize(t *testing.T) {
	op := officeCitationFixture("web_search", map[string]any{"provider": "brave", "results": []map[string]string{
		{"url": "https://source.example/report?chapter=1", "title": "公开结果", "snippet": "raw snippet"},
		{"url": "https://source.example/object?X-Amz-Signature=secret", "title": "signed link"},
	}})
	projection := BuildWorkbenchToolProjection(op)
	require.NotNil(t, projection)
	require.EqualValues(t, 2, *projection.Detail.ResultCount)
	require.Equal(t, "brave", projection.Detail.Provider)
	require.Len(t, projection.Citations, 1)
	require.Equal(t, "https://source.example/report?chapter=1", projection.Citations[0].URL)
	require.Empty(t, projection.Citations[0].Excerpt)
	op.Input, op.Result, op.ToolProjection = nil, nil, projection
	require.Equal(t, projection.Citations, officeCitations([]OfficeOperation{op}))
	op.Result = &OfficeOperationResult{Output: json.RawMessage(`null`)}
	require.Equal(t, projection.Citations, officeCitations([]OfficeOperation{op}))

	files := make([]map[string]string, 256)
	for i := range files {
		files[i] = map[string]string{"name": strings.Repeat("𠮷", 240), "status": "completed"}
	}
	batch := officeCitationFixture("batch_edit_docx", map[string]any{"processed": len(files), "files": files})
	bounded := BuildWorkbenchToolProjection(batch)
	require.EqualValues(t, 256, *bounded.Detail.Processed)
	require.NotEmpty(t, bounded.Detail.Files)
	encoded, err := json.Marshal(bounded)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 60<<10)
}

func TestWorkbenchNetworkFailureDepositsReplayableToolResult(t *testing.T) {
	ctx := context.Background()
	svc, repo := officeBridgeFixture(nil)
	svc.repo = &workbenchToolTestRepo{repo}
	result, err := svc.officeNetworkTool(ctx, repo.lease, "failed-search", "web_search",
		json.RawMessage(`{"tool":"web_search","query":"上海天气"}`), func(context.Context) (any, error) {
			return &WorkbenchSearchResult{Provider: "brave"}, ErrWorkbenchSearchUpstream.WithCause(context.DeadlineExceeded)
		})
	require.NoError(t, err)
	require.JSONEq(t, `{"error":"SEARCH_TIMEOUT","message":"搜索供应商响应超时","provider":"brave"}`, string(result))
	saved := repo.ops["failed-search"]
	require.Equal(t, "failed", saved.Result.ToolStatus)
	require.Equal(t, "SEARCH_TIMEOUT", saved.Result.ErrorCode)
	replayed, err := svc.officeNetworkTool(ctx, repo.lease, "failed-search", "web_search",
		json.RawMessage(`{"tool":"web_search","query":"上海天气"}`), func(context.Context) (any, error) {
			t.Fatal("completed tool was replayed")
			return nil, errors.New("unexpected replay")
		})
	require.NoError(t, err)
	require.Equal(t, result, replayed)
}

func TestWorkbenchOfficeToolPreservesKnownFailureReasons(t *testing.T) {
	cases := []struct {
		cause error
		code  string
		hint  string
	}{
		{ErrOfficeResourceQuota, "FILE_QUOTA_EXCEEDED", "文件存储空间已满"},
		{ErrOfficeStorageUnavailable, "FILE_STORAGE_UNAVAILABLE", "文件存储暂时不可用"},
		{ErrOfficeResourceTooLarge, "FILE_TOO_LARGE", "超过服务可处理的大小"},
		{ErrOfficeResourceType, "FILE_TYPE_INVALID", "文件格式不符合"},
		{ErrOfficeConflict, "OFFICE_VERSION_CONFLICT", "请选用最新版本"},
		{ErrOfficeCapacity, "OFFICE_WORKER_BUSY", "办公服务繁忙"},
		{ErrOfficeNotFound, "OFFICE_FILE_UNAVAILABLE", "文件无法读取"},
		{errOfficeOutputExpired, "OFFICE_OUTPUT_EXPIRED", "成品已过期"},
		{context.DeadlineExceeded, "TOOL_TIMEOUT", "工具执行超时"},
		{errors.New("private diagnostic"), "TOOL_FAILED", "文件处理失败"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			svc, repo := officeBridgeFixture(nil)
			svc.repo = &workbenchToolTestRepo{repo}
			ctx := context.Background()
			const id = "tool"
			_, _, err := svc.PrepareTool(ctx, repo.lease, id, json.RawMessage(`{"tool":"edit_docx"}`))
			require.NoError(t, err)
			require.NoError(t, svc.completeBrowserToolError(ctx, repo.lease, id, "edit_docx", fmt.Errorf("private diagnostic: %w", tc.cause)))
			op := repo.ops[id]
			require.Equal(t, tc.code, op.Result.ErrorCode)
			require.Contains(t, op.Result.ErrorMessage, tc.hint)
			require.NotContains(t, string(op.Result.Output), "private diagnostic")
			op.ToolProjection = BuildWorkbenchToolProjection(op)
			op.Input, op.Result = nil, nil
			detail := workbenchToolDetail(repo.lease.Run, op)
			require.Equal(t, "failed", detail.State)
			require.Contains(t, detail.ErrorMessage, tc.hint)
		})
	}
}

func TestWorkbenchInvalidNetworkToolArgumentsHaveReplayableErrors(t *testing.T) {
	cases := []struct {
		name, args, code string
		searchEnabled    bool
	}{
		{"web_search", `{"query":"  "}`, "SEARCH_QUERY_INVALID", true},
		{"web_search", `{"query":12}`, "SEARCH_QUERY_INVALID", true},
		{"web_read", `{"url":"missing-scheme.example"}`, "WEB_READ_URL_INVALID", true},
		{"web_read", `{"url":"https://example.test"}`, "WEB_READ_UNAVAILABLE", false},
		{"web_search", `{"query":"weather"}`, "SEARCH_UNAVAILABLE", false},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			svc, repo := officeBridgeFixture(nil)
			svc.repo = &workbenchToolTestRepo{repo}
			repo.lease.Run.SearchEnabled = tc.searchEnabled
			call := workbenchNativeCall{Name: tc.name, Arguments: json.RawMessage(tc.args)}
			ctx := context.Background()
			const id = "tool"
			require.NoError(t, svc.runBrowserTool(ctx, nil, repo.lease, workbenchBrowserParameters{}, id, call))
			first := repo.ops[id]
			require.Equal(t, tc.code, first.Result.ErrorCode)
			require.Equal(t, "failed", first.Result.ToolStatus)
			require.NotEmpty(t, workbenchToolDetail(repo.lease.Run, first).ErrorMessage)
			require.NoError(t, svc.runBrowserTool(ctx, nil, repo.lease, workbenchBrowserParameters{}, id, call))
			require.Equal(t, first, repo.ops[id])
		})
	}
}

func TestWorkbenchWebReadFailureCodesExcludeUpstreamDiagnostics(t *testing.T) {
	cases := []struct {
		cause error
		code  string
	}{
		{workbenchWebPageStatus(http.StatusUnauthorized), "WEB_READ_ACCESS_DENIED"},
		{workbenchWebPageStatus(http.StatusGone), "WEB_READ_NOT_FOUND"},
		{workbenchWebPageStatus(http.StatusTooManyRequests), "WEB_READ_RATE_LIMITED"},
		{workbenchWebPageStatus(http.StatusInternalServerError), "WEB_READ_FAILED"},
		{errWorkbenchWebReadContentType, "WEB_READ_UNSUPPORTED_CONTENT"},
		{errWorkbenchWebReadTooLarge, "WEB_READ_TOO_LARGE"},
		{context.DeadlineExceeded, "WEB_READ_TIMEOUT"},
	}
	for _, tc := range cases {
		result, err := officeNetworkReceipt("web_read", nil, fmt.Errorf("private diagnostic: %w", tc.cause), nil)
		require.NoError(t, err)
		require.Equal(t, tc.code, result.ErrorCode)
		require.NotContains(t, string(result.Output), "private diagnostic")
	}
}

type workbenchToolTestRepo struct{ *officeBridgeTestRepo }

func (r *workbenchToolTestRepo) GetOperation(_ context.Context, _ int64, _, id string) (*OfficeOperation, error) {
	op, ok := r.ops[id]
	if !ok {
		return nil, ErrOfficeNotFound
	}
	return &op, nil
}
