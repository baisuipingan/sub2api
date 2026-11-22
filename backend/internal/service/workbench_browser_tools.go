package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func browserTools(search, office, remote bool) []workbenchNativeTool {
	var tools []workbenchNativeTool
	if search {
		tools = append(tools,
			workbenchNativeTool{"web_search", "Search the web and return source URLs and snippets.", json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"integer"}},"required":["query"],"additionalProperties":false}`)},
			workbenchNativeTool{"web_read", "Read a public web page from a search result or the user.", json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)},
		)
	}
	if office {
		tools = append(tools,
			workbenchNativeTool{"read_docx", "Read sections of a DOCX attached to or produced in this conversation. If next_offset is supplied, continue with that offset and the same scope.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"scope":{"type":"string","enum":["all","body","title","header","footer"]},"offset":{"type":"integer"}},"required":["version_id"],"additionalProperties":false}`)},
			workbenchNativeTool{"edit_docx", "Replace exact text in a DOCX without discarding its formatting. Returns a new version. Use read_docx on that version to verify the change.", json.RawMessage(`{"type":"object","properties":{"version_id":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old_text":{"type":"string"},"new_text":{"type":"string"},"scope":{"type":"string","enum":["all","body","title","header","footer"]}},"required":["old_text","new_text"],"additionalProperties":false}}},"required":["version_id","edits"],"additionalProperties":false}`)},
			workbenchNativeTool{"create_docx", "Create and deliver a Word document with a title and paragraphs. The result includes the published version ID.", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"title":{"type":"string"},"paragraphs":{"type":"array","items":{"type":"string"}}},"required":["name","title","paragraphs"],"additionalProperties":false}`)},
		)
		tools = append(tools, workbenchExtraTools(remote)...)
	}
	return tools
}

func (s *OfficeAgentService) runBrowserTool(ctx context.Context, repo workbenchBrowserRepository, lease OfficeRunLease, params workbenchBrowserParameters, id string, call workbenchNativeCall) error {
	var args struct {
		VersionID  string              `json:"version_id"`
		Edits      []WorkbenchDOCXEdit `json:"edits"`
		Query      string              `json:"query"`
		URL        string              `json:"url"`
		MaxResults int                 `json:"max_results"`
		WorkbenchDOCXCreate
		WorkbenchDOCXInspectOptions
	}
	decodeErr := json.Unmarshal(call.Arguments, &args)
	if decodeErr == nil && lease.Run.SearchEnabled {
		switch {
		case call.Name == "web_search" && strings.TrimSpace(args.Query) != "" && len(args.Query) <= 2000:
			_, err := s.Search(ctx, lease, id, args.Query, args.MaxResults)
			return err
		case call.Name == "web_read" && officeReadURL(args.URL) == nil:
			_, err := s.ReadWeb(ctx, lease, id, args.URL)
			return err
		}
	}
	remoteEnabled := params.RemoteOfficeTools
	if params.HistoryVersion < workbenchReplayHistoryVersion {
		remoteEnabled = s.remoteOfficeConfigured(ctx)
	}
	remote := params.OfficeTools && remoteEnabled && workbenchRemoteTool(call.Name)
	input, err := json.Marshal(struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Remote    bool            `json:"remote,omitempty"`
	}{call.Name, call.Arguments, remote})
	if err != nil {
		return err
	}
	op, execute, err := s.PrepareTool(ctx, lease, id, input)
	if err != nil {
		return err
	}
	if !execute {
		if op.Result == nil {
			return ErrOfficeResultUnknown
		}
		return nil
	}

	if remote && decodeErr == nil {
		if !strings.HasPrefix(call.Name, "create_") && args.VersionID == "" {
			return s.completeBrowserToolError(ctx, lease, id, call.Name, ErrOfficeInvalid)
		}
		return s.runRemoteOfficeTool(ctx, repo, lease, op, call, args.VersionID)
	}

	var output any
	err = ErrOfficeInvalid
	if decodeErr == nil && params.OfficeTools && s.resources != nil {
		switch {
		case workbenchTextTool(call.Name):
			output, err = s.runWorkbenchTextTool(ctx, repo, lease, id, call)
		case call.Name == "create_docx":
			output, err = s.resources.createWorkbenchDOCX(ctx, lease.Run.UserID, lease.Run.ID+":"+id, args.WorkbenchDOCXCreate, &lease)
		case call.Name == "read_docx" || call.Name == "edit_docx":
			var allowed bool
			allowed, err = repo.BrowserResourceAllowed(ctx, lease.Run, args.VersionID)
			if err == nil && !allowed {
				err = ErrOfficeNotFound
			}
			if err == nil {
				if call.Name == "read_docx" {
					output, err = s.resources.inspectWorkbenchDOCX(ctx, lease.Run.UserID, args.VersionID, args.WorkbenchDOCXInspectOptions)
				} else {
					output, err = s.resources.editWorkbenchDOCX(ctx, lease.Run.UserID, args.VersionID, lease.Run.ID+":"+id, args.Edits, &lease)
				}
			}
		}
	}
	if err != nil {
		return s.completeBrowserToolError(ctx, lease, id, call.Name, err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return err
	}
	store, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	_, err = s.CompleteTool(store, lease, id, OfficeOperationResult{Output: encoded})
	return err
}

func browserToolError(name string, err error) string {
	switch {
	case errors.Is(err, errOfficeOutputExpired):
		return "办公节点上的成品已过期，请重试文档操作。"
	case errors.Is(err, context.DeadlineExceeded):
		return "等待办公节点超时。可以在当前会话中重试文档操作。"
	case errors.Is(err, ErrOfficeNotFound), errors.Is(err, ErrGalleryGone):
		return "请使用当前对话已上传或生成的文件版本。"
	case errors.Is(err, ErrOfficeResourceQuota):
		return "文件存储空间已满，请删除不再需要的文件或会话后重试。"
	case errors.Is(err, ErrOfficeResourceType):
		return "文件格式不符合此工具的要求。"
	case errors.Is(err, ErrOfficeStorageUnavailable):
		return "文件存储暂时不可用。"
	case errors.Is(err, ErrOfficeResourceTooLarge):
		return "文档超过服务可处理的大小。"
	case errors.Is(err, ErrOfficeConflict):
		return "文件版本已变化，请读取最新版本后再编辑。"
	case errors.Is(err, ErrOfficeCapacity):
		return "文档工具当前繁忙，可以稍后重试。"
	case errors.Is(err, ErrOfficeInvalid):
		return fmt.Sprintf("工具 %s 的参数无效，或当前对话未启用该工具。请检查文件版本和工具参数。", name)
	default:
		return "文档操作暂未完成。请检查文件内容后重试。"
	}
}

func (s *OfficeAgentService) browserResourceImage(ctx context.Context, userID int64, versionID string) (workbenchNativeImage, error) {
	source, err := s.browserResourceImageSource(ctx, userID, versionID, false, "")
	if err != nil {
		return workbenchNativeImage{}, err
	}
	return s.loadBrowserResourceImage(ctx, source, false)
}

type workbenchBrowserImageSource struct {
	endpoint, region, bucket string
	key, mime, hash          string
	bytes                    int64
}

func (s *OfficeAgentService) browserResourceImageSource(ctx context.Context, userID int64, versionID string, pinned bool, previewHash string) (workbenchBrowserImageSource, error) {
	if s.resources == nil {
		return workbenchBrowserImageSource{}, ErrOfficeInvalid
	}
	v, err := s.resources.repo.GetResourceVersion(ctx, userID, versionID, true)
	if err != nil {
		return workbenchBrowserImageSource{}, err
	}
	source := workbenchBrowserImageSource{
		endpoint: v.Endpoint, region: v.Region, bucket: v.Bucket,
		key: v.ObjectKey, mime: v.MIME, hash: v.SHA256, bytes: v.Bytes,
	}
	preview := v.PreviewKey != "" && strings.HasPrefix(v.PreviewMIME, "image/")
	if pinned && previewHash != "" && (!preview || v.PreviewSHA256 != previewHash) {
		return workbenchBrowserImageSource{}, ErrOfficeConflict
	}
	if preview && (!pinned || previewHash != "") {
		source.key, source.mime, source.hash, source.bytes = v.PreviewKey, v.PreviewMIME, v.PreviewSHA256, v.PreviewBytes
	}
	if pinned {
		if source.bytes > OfficeResourceMaxBytes {
			return workbenchBrowserImageSource{}, ErrOfficeResourceTooLarge
		}
		if source.bytes <= 0 || source.key == "" || source.hash == "" || !strings.HasPrefix(source.mime, "image/") {
			return workbenchBrowserImageSource{}, ErrOfficeConflict
		}
	}
	return source, nil
}

func (s *OfficeAgentService) loadBrowserResourceImage(ctx context.Context, source workbenchBrowserImageSource, pinned bool) (workbenchNativeImage, error) {
	release, err := s.resources.AcquireIO()
	if err != nil {
		return workbenchNativeImage{}, err
	}
	defer release()
	store, err := s.resources.storage(ctx, source.endpoint, source.region, source.bucket)
	if err != nil {
		return workbenchNativeImage{}, err
	}
	reader, ok := store.(GalleryObjectReader)
	if !ok {
		return workbenchNativeImage{}, ErrOfficeStorageUnavailable
	}
	maxBytes := OfficeResourceMaxBytes
	if pinned {
		maxBytes = source.bytes
	}
	data, err := reader.Get(ctx, source.key, maxBytes)
	if err != nil {
		return workbenchNativeImage{}, err
	}
	if pinned && (int64(len(data)) != source.bytes || officeResourceHash(data) != source.hash) {
		return workbenchNativeImage{}, ErrOfficeConflict
	}
	return workbenchNativeImage{MIME: source.mime, Data: base64.StdEncoding.EncodeToString(data)}, nil
}
