package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"
)

type WorkbenchToolSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Title string `json:"title"`
	State string `json:"state"`
}

type WorkbenchToolArtifact struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	VersionID string `json:"version_id"`
	MIME      string `json:"mime,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	Preview   string `json:"preview_kind,omitempty"`
}

type WorkbenchToolFile struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Replacements *int   `json:"replacements,omitempty"`
	Message      string `json:"message,omitempty"`
}

type WorkbenchToolDetail struct {
	WorkbenchToolSummary
	Query         string                  `json:"query,omitempty"`
	Provider      string                  `json:"provider,omitempty"`
	ResultCount   *int                    `json:"result_count,omitempty"`
	Source        string                  `json:"source,omitempty"`
	FileName      string                  `json:"file_name,omitempty"`
	SourceVersion string                  `json:"source_version_id,omitempty"`
	Scope         string                  `json:"scope,omitempty"`
	DurationMS    *int64                  `json:"duration_ms,omitempty"`
	Replacements  *int                    `json:"replacements,omitempty"`
	CellsChanged  *int                    `json:"cells_changed,omitempty"`
	Calculated    *int                    `json:"calculated,omitempty"`
	FormulaErrors *int                    `json:"formula_errors,omitempty"`
	Processed     *int                    `json:"processed,omitempty"`
	Skipped       *int                    `json:"skipped,omitempty"`
	Failed        *int                    `json:"failed,omitempty"`
	ErrorMessage  string                  `json:"error_message,omitempty"`
	Artifacts     []WorkbenchToolArtifact `json:"artifacts,omitempty"`
	Files         []WorkbenchToolFile     `json:"files,omitempty"`
	SearchCharge  *WorkbenchSearchCharge  `json:"search_charge,omitempty"`
}

// WorkbenchToolProjection retains display metadata when the original tool
// arguments and result are removed from the operation journal.
type WorkbenchToolProjection struct {
	Detail    WorkbenchToolDetail `json:"detail"`
	Citations []OfficeCitation    `json:"citations,omitempty"`
}

type workbenchToolReader interface {
	AdminToolRun(context.Context, string) (*OfficeRun, error)
	WorkbenchToolOperations(context.Context, int64, string) ([]OfficeOperation, error)
	WorkbenchToolOperation(context.Context, int64, string, string) (*OfficeOperation, error)
}

func workbenchToolTitle(name string) string {
	switch name {
	case "web_search":
		return "联网搜索"
	case "web_read":
		return "读取网页"
	case "create_docx", "create_xlsx", "create_pptx", "template_pptx", "create_text", "create_drawio", "write_text", "write_file":
		return "生成文件"
	case "read_docx", "read_xlsx", "read_pptx", "read_text", "read_drawio", "read_file":
		return "读取文件"
	case "edit_docx", "edit_xlsx", "edit_pptx", "edit_text", "edit_drawio", "edit_file":
		return "修改文件"
	case "batch_docx", "batch_edit_docx":
		return "批量修改 Word"
	case "recalc_xlsx":
		return "重算表格公式"
	case "inspect_rendered_image":
		return "检查文档预览"
	case "execute_office_python":
		return "处理办公文件"
	default:
		return "工具调用"
	}
}

type toolReceipt struct {
	Error         json.RawMessage         `json:"error"`
	Status        string                  `json:"status"`
	Provider      string                  `json:"provider"`
	Results       json.RawMessage         `json:"results"`
	URL           string                  `json:"url"`
	Resource      WorkbenchToolArtifact   `json:"resource"`
	Artifact      WorkbenchToolArtifact   `json:"artifact"`
	Artifacts     []WorkbenchToolArtifact `json:"artifacts"`
	Replacements  json.RawMessage         `json:"replacements"`
	CellsChanged  *int                    `json:"cells_changed"`
	Calculated    *int                    `json:"calculated"`
	FormulaErrors []json.RawMessage       `json:"formula_errors"`
	Processed     *int                    `json:"processed"`
	Skipped       *int                    `json:"skipped"`
	Failed        *int                    `json:"failed"`
	Succeeded     *int                    `json:"succeeded"`
	Files         []struct {
		WorkbenchToolFile
		Counts json.RawMessage `json:"replacements"`
	} `json:"files"`
}

func (receipt toolReceipt) processedCount() *int {
	if receipt.Succeeded != nil {
		return receipt.Succeeded
	}
	return receipt.Processed
}

func workbenchToolState(run OfficeRun, operation OfficeOperation, receipt toolReceipt) string {
	if operation.State == OfficeOperationCompleted && operation.Result != nil {
		for _, state := range []string{operation.Result.ToolStatus, receipt.Status} {
			if state == "partial" || state == "cancelled" || state == "failed" {
				return state
			}
		}
		if operation.Result.ErrorCode != "" || (len(receipt.Error) > 0 && string(receipt.Error) != "null" && string(receipt.Error) != `""` && string(receipt.Error) != "false") {
			return "failed"
		}
		if receipt.Failed != nil && *receipt.Failed > 0 {
			if processed := receipt.processedCount(); processed != nil && *processed > 0 {
				return "partial"
			}
			return "failed"
		}
		return "completed"
	}
	if run.Status == OfficeRunCancelled {
		return "cancelled"
	}
	if operation.State == OfficeOperationUnknown || run.Status == OfficeRunNeedsConfirmation {
		return "unknown"
	}
	if run.Status == OfficeRunFailed || run.Status == OfficeRunCompleted || run.Status == OfficeRunPartiallyCompleted {
		return "ended"
	}
	if operation.State == OfficeOperationPrepared {
		return "pending"
	}
	return "running"
}

// WorkbenchToolOperationSummary is also used by the journal's live event writer.
func WorkbenchToolOperationSummary(run OfficeRun, operation OfficeOperation) WorkbenchToolSummary {
	if operation.ToolProjection != nil {
		summary := operation.ToolProjection.Detail.WorkbenchToolSummary
		summary.ID = operation.ID
		summary.Title = workbenchToolTitle(summary.Name)
		if operation.State != OfficeOperationCompleted {
			summary.State = workbenchToolState(run, operation, toolReceipt{})
		}
		return summary
	}
	var input struct {
		Tool string `json:"tool"`
	}
	_ = json.Unmarshal(operation.Input, &input)
	var receipt toolReceipt
	if operation.Result != nil {
		_ = json.Unmarshal(operation.Result.Output, &receipt)
	}
	return WorkbenchToolSummary{ID: operation.ID, Name: toolSummaryText(input.Tool, 64), Title: workbenchToolTitle(input.Tool), State: workbenchToolState(run, operation, receipt)}
}

type toolArguments struct {
	Query     string `json:"query"`
	URL       string `json:"url"`
	VersionID string `json:"version_id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`
}

func toolSummaryText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func toolSource(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	return toolSummaryText(parsed.Hostname(), 253)
}

func toolReplacementCount(raw json.RawMessage) *int {
	var count int
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if json.Unmarshal(raw, &count) == nil && count >= 0 {
		return &count
	}
	var counts []int
	if json.Unmarshal(raw, &counts) != nil {
		return nil
	}
	for _, n := range counts {
		if n < 0 || n > 1_000_000-count {
			return nil
		}
		count += n
	}
	return &count
}

func workbenchToolErrorMessage(name string, result OfficeOperationResult, state string) string {
	if state == "cancelled" {
		return "工具操作已停止"
	}
	if state != "failed" && state != "partial" {
		return ""
	}
	switch result.ErrorCode {
	case "TOOL_TIMEOUT":
		return "工具执行超时，可以在当前会话中重试此操作"
	case "SEARCH_TIMEOUT":
		return "搜索供应商响应超时"
	case "SEARCH_UNAVAILABLE":
		return "工作台联网搜索暂不可用"
	case "SEARCH_QUERY_INVALID":
		return "搜索关键词无效或过长"
	case "SEARCH_INSUFFICIENT_BALANCE":
		return "余额不足，未发起联网搜索"
	case "SEARCH_PRICE_CHANGED":
		return "联网搜索价格已更新，请刷新工作台后重试"
	case "SEARCH_PROVIDER_AUTH_FAILED":
		return "搜索供应商拒绝了认证，请联系管理员检查供应商配置"
	case "SEARCH_PROVIDER_QUOTA_EXCEEDED":
		return "搜索供应商的额度或计费不可用，请联系管理员"
	case "SEARCH_PROVIDER_RATE_LIMITED":
		return "搜索供应商限制了请求频率，请稍后重试"
	case "SEARCH_PROVIDER_UNAVAILABLE":
		return "搜索供应商暂不可用，请稍后重试"
	case "WEB_READ_TIMEOUT":
		return "网页读取超时"
	case "WEB_READ_UNAVAILABLE":
		return "工作台网页读取暂不可用"
	case "WEB_READ_URL_INVALID":
		return "网页地址无效，请使用完整的 HTTP 或 HTTPS 链接"
	case "WEB_READ_ACCESS_DENIED":
		return "目标网站拒绝访问，请换一个公开链接"
	case "WEB_READ_NOT_FOUND":
		return "网页不存在或已下线，请检查链接"
	case "WEB_READ_RATE_LIMITED":
		return "目标网站限制了访问频率，请稍后再试"
	case "WEB_READ_UNSUPPORTED_CONTENT":
		return "该链接未返回可读取的网页或纯文本"
	case "WEB_READ_TOO_LARGE":
		return "网页内容超过读取上限"
	case "WEB_READ_FAILED":
		return "网页无法访问或返回了暂不支持的内容"
	case "OFFICE_OUTPUT_EXPIRED":
		return "办公节点上的成品已过期，请重试文档操作。"
	case "OFFICE_FILE_UNAVAILABLE", "INPUT_UNAVAILABLE":
		return "文件无法读取，请选用当前对话的文件或重新上传"
	case "FILE_QUOTA_EXCEEDED":
		return "文件存储空间已满，请删除不再需要的文件或会话后重试"
	case "FILE_TYPE_INVALID":
		return "文件格式不符合此工具的要求"
	case "FILE_STORAGE_UNAVAILABLE":
		return "文件存储暂时不可用"
	case "FILE_TOO_LARGE":
		return "文件超过服务可处理的大小"
	case "TOOL_RESULT_TOO_LARGE":
		return "工具结果过大，请缩小读取范围后重试"
	case "INVALID_ARGUMENT":
		return "文件或工具参数无法处理，请检查文件版本和操作要求"
	case "INVALID_DOCUMENT":
		return "文档格式或内容无法处理，请检查文件后重新上传"
	case "NO_MATCH":
		return "没有找到要替换的内容，请核对原文后重试"
	case "NO_DOCUMENTS_CHANGED":
		return "这次批量操作没有成功修改任何 Word 文件，请检查压缩包和替换条件"
	case "VERIFY_FAILED":
		return "无法核对修改结果，请检查输入文档后重试"
	case "CALC_UNAVAILABLE":
		return "表格公式重算暂不可用"
	case "CALC_FAILED":
		return "表格公式重算失败，请检查工作簿后重试"
	case "UNSUPPORTED_FORMULA":
		return "工作簿包含外部引用，请移除外部引用后重试公式重算"
	case "WORKER_BUSY", "OFFICE_WORKER_BUSY":
		return "办公服务繁忙"
	case "FILE_CONFLICT", "OFFICE_VERSION_CONFLICT":
		return "文件已被修改，请选用最新版本"
	}
	switch name {
	case "web_search":
		return "搜索供应商未返回可用结果，请检查供应商配置或稍后重试"
	case "web_read":
		return "网页读取失败"
	default:
		if state == "failed" {
			return "文件处理失败，请检查输入文件和操作要求"
		}
		return ""
	}
}

func workbenchToolDetail(run OfficeRun, operation OfficeOperation) WorkbenchToolDetail {
	summary := WorkbenchToolOperationSummary(run, operation)
	if operation.ToolProjection != nil {
		detail := operation.ToolProjection.Detail
		detail.WorkbenchToolSummary = summary
		detail.DurationMS = workbenchToolDuration(operation)
		return detail
	}
	detail := WorkbenchToolDetail{WorkbenchToolSummary: summary, DurationMS: workbenchToolDuration(operation)}
	var args struct {
		toolArguments
		Arguments toolArguments `json:"arguments"`
	}
	_ = json.Unmarshal(operation.Input, &args)
	var receipt toolReceipt
	if operation.Result != nil {
		_ = json.Unmarshal(operation.Result.Output, &receipt)
		detail.ErrorMessage = workbenchToolErrorMessage(detail.Name, *operation.Result, detail.State)
	}
	switch detail.Name {
	case "web_search":
		detail.Query = toolSummaryText(firstToolValue(args.Arguments.Query, args.Query), 300)
		if validWorkbenchSearchProvider(receipt.Provider) {
			detail.Provider = receipt.Provider
		}
		var results []json.RawMessage
		if detail.State == "completed" && len(receipt.Results) > 0 && json.Unmarshal(receipt.Results, &results) == nil {
			count := len(results)
			detail.ResultCount = &count
		}
	case "web_read":
		detail.Source = toolSource(receipt.URL)
		if detail.Source == "" {
			detail.Source = toolSource(firstToolValue(args.Arguments.URL, args.URL))
		}
	default:
		detail.FileName = toolSummaryText(receipt.Resource.Name, 240)
		if detail.FileName == "" {
			name := args.Arguments.Name
			if name == "" {
				name = args.Name
			}
			if name != "" {
				detail.FileName = toolSummaryText(path.Base(name), 240)
			}
		}
		detail.SourceVersion = args.Arguments.VersionID
		if detail.SourceVersion == "" {
			detail.SourceVersion = args.VersionID
		}
		if uuid.Validate(detail.SourceVersion) != nil {
			detail.SourceVersion = ""
		}
		switch value := args.Arguments.Scope; value {
		case "all", "body", "header", "footer", "title":
			detail.Scope = value
		}
		for _, artifact := range append([]WorkbenchToolArtifact{receipt.Artifact}, receipt.Artifacts...) {
			if uuid.Validate(artifact.VersionID) == nil && len(detail.Artifacts) < 64 {
				artifact.Name = toolSummaryText(artifact.Name, 240)
				if uuid.Validate(artifact.ID) != nil {
					artifact.ID = ""
				}
				artifact.MIME = toolSummaryText(artifact.MIME, 80)
				switch artifact.Preview {
				case "pdf", "image", "text", "markdown", "unsupported":
				default:
					artifact.Preview = ""
				}
				detail.Artifacts = append(detail.Artifacts, artifact)
			}
		}
		detail.Replacements = toolReplacementCount(receipt.Replacements)
		switch detail.Name {
		case "edit_xlsx":
			if receipt.CellsChanged != nil && *receipt.CellsChanged >= 0 {
				detail.CellsChanged = receipt.CellsChanged
			}
		case "recalc_xlsx":
			if receipt.Calculated != nil && *receipt.Calculated >= 0 {
				detail.Calculated = receipt.Calculated
			}
			if receipt.FormulaErrors != nil {
				count := len(receipt.FormulaErrors)
				detail.FormulaErrors = &count
			}
		}
		detail.Processed, detail.Skipped, detail.Failed = receipt.processedCount(), receipt.Skipped, receipt.Failed
		for _, entry := range receipt.Files[:min(len(receipt.Files), 256)] {
			f := entry.WorkbenchToolFile
			switch f.Status {
			case "completed", "modified", "skipped", "failed":
				f.Name = toolSummaryText(f.Name, 240)
				f.Message = toolSummaryText(f.Message, 300)
				f.Replacements = toolReplacementCount(entry.Counts)
				detail.Files = append(detail.Files, f)
			}
		}
	}
	return detail
}

func workbenchToolDuration(operation OfficeOperation) *int64 {
	if operation.CompletedAt == nil || operation.DispatchedAt == nil {
		return nil
	}
	duration := operation.CompletedAt.Sub(*operation.DispatchedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	return &duration
}

// BuildWorkbenchToolProjection derives a bounded snapshot for a single
// operation. The journal still owns replay, timestamps and search settlement.
func BuildWorkbenchToolProjection(operation OfficeOperation) *WorkbenchToolProjection {
	if operation.Kind != OfficeOperationTool {
		return nil
	}
	if operation.ToolProjection != nil {
		return operation.ToolProjection
	}
	detail := workbenchToolDetail(OfficeRun{}, operation)
	detail.ID, detail.Title, detail.DurationMS = "", "", nil
	for i := range detail.Files {
		detail.Files[i].Message = retainedToolFileMessage(detail.Files[i].Message)
	}
	projection := &WorkbenchToolProjection{Detail: detail}
	if detail.State == "completed" {
		for _, citation := range officeCitations([]OfficeOperation{operation}) {
			if retainedToolCitationURL(citation.URL) {
				citation.Excerpt = ""
				projection.Citations = append(projection.Citations, citation)
			}
		}
	}
	// Leave headroom for the extra whitespace in PostgreSQL's JSONB output.
	for {
		encoded, _ := json.Marshal(projection)
		if len(encoded) <= 60<<10 {
			return projection
		}
		switch {
		case len(projection.Detail.Files) > 0:
			projection.Detail.Files = projection.Detail.Files[:len(projection.Detail.Files)/2]
		case len(projection.Citations) > 0:
			projection.Citations = projection.Citations[:len(projection.Citations)/2]
		case len(projection.Detail.Artifacts) > 0:
			projection.Detail.Artifacts = projection.Detail.Artifacts[:len(projection.Detail.Artifacts)/2]
		default:
			return nil
		}
	}
}

// PreserveWorkbenchToolProjectionInputs keeps display fields from arguments
// that may have been compacted before the tool's result arrived.
func PreserveWorkbenchToolProjectionInputs(current, previous *WorkbenchToolProjection) {
	if current == nil || previous == nil {
		return
	}
	detail, prior := &current.Detail, previous.Detail
	detail.Query = firstToolValue(detail.Query, prior.Query)
	detail.Source = firstToolValue(detail.Source, prior.Source)
	detail.FileName = firstToolValue(detail.FileName, prior.FileName)
	detail.SourceVersion = firstToolValue(detail.SourceVersion, prior.SourceVersion)
	detail.Scope = firstToolValue(detail.Scope, prior.Scope)
}

func retainedToolFileMessage(message string) string {
	switch message {
	case "单个 Word 文件过大。", "没有找到待替换的内容。", "Office 文件结构无效。", "无法读取压缩包。", "此 Office 文件包含宏或活动内容。":
		return message
	}
	return ""
}

func retainedToolCitationURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		key = strings.ToLower(key)
		switch key {
		case "token", "access_token", "api_key", "apikey", "session", "session_id", "signature", "sig", "credential", "password", "awsaccesskeyid", "googleaccessid", "x-goog-signature", "x-goog-credential":
			return false
		}
		if strings.HasPrefix(key, "x-amz-") {
			return false
		}
	}
	return true
}

func firstToolValue(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func (s *OfficeAgentService) WorkbenchToolSummaries(ctx context.Context, user int64, runID string) ([]WorkbenchToolSummary, error) {
	run, err := s.PublicRun(ctx, user, runID)
	if err != nil {
		return nil, err
	}
	repo, ok := s.repo.(workbenchToolReader)
	if !ok {
		return nil, ErrOfficeDisabled
	}
	operations, err := repo.WorkbenchToolOperations(ctx, run.UserID, runID)
	if err != nil {
		return nil, err
	}
	items := make([]WorkbenchToolSummary, 0, len(operations))
	for _, operation := range operations {
		items = append(items, WorkbenchToolOperationSummary(*run, operation))
	}
	return items, nil
}

func (s *OfficeAgentService) WorkbenchToolDetail(ctx context.Context, user int64, runID, operationID string, admin bool) (*WorkbenchToolDetail, error) {
	repo, ok := s.repo.(workbenchToolReader)
	if !ok {
		return nil, ErrOfficeDisabled
	}
	var run *OfficeRun
	var err error
	if admin {
		run, err = repo.AdminToolRun(ctx, runID)
	} else {
		run, err = s.PublicRun(ctx, user, runID)
	}
	if err != nil {
		return nil, err
	}
	operation, err := repo.WorkbenchToolOperation(ctx, run.UserID, runID, operationID)
	if err != nil {
		return nil, err
	}
	if operation.Kind != OfficeOperationTool {
		return nil, ErrOfficeNotFound
	}
	detail := workbenchToolDetail(*run, *operation)
	if detail.Name == "web_search" {
		if repo, ok := s.repo.(workbenchSearchBillingRepository); ok {
			charge, chargeErr := repo.GetWorkbenchSearchCharge(ctx, run.UserID, runID, operationID)
			if chargeErr != nil && !errors.Is(chargeErr, ErrOfficeNotFound) {
				return nil, chargeErr
			}
			detail.SearchCharge = charge
		}
	}
	if detail.FileName == "" && detail.SourceVersion != "" && s.resources != nil {
		if resource, err := s.resources.repo.GetResourceVersion(ctx, run.UserID, detail.SourceVersion, true); err == nil {
			detail.FileName = toolSummaryText(resource.Name, 240)
		}
	}
	return &detail, nil
}
