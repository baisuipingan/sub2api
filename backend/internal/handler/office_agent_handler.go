package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	pkghttp "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var officePilotIngress = pkghttp.NewBodyMemoryBudget(8 << 20)

type OfficeAgentHandler struct {
	service     *service.OfficeAgentService
	streamMu    sync.Mutex
	streams     map[int64]int
	streamTotal int
}

func NewOfficeAgentHandler(svc *service.OfficeAgentService) *OfficeAgentHandler {
	return &OfficeAgentHandler{service: svc}
}

// Protect keeps ingress reservations alive through decode, persistence and the
// nested gateway call, rather than releasing them immediately after ReadAll.
func (h *OfficeAgentHandler) Protect(c *gin.Context) {
	ctx, release := pkghttp.WithRequestBodyMemoryBudget(c.Request.Context(), officePilotIngress)
	defer release()
	c.Request = c.Request.WithContext(ctx)
	c.Header("Cache-Control", "no-store")
	c.Next()
}

func (h *OfficeAgentHandler) PilotStatus(c *gin.Context) {
	response.Success(c, gin.H{"enabled": h.service.Enabled(), "stage": "internal_pilot", "durable_resume": false, "public_office_enabled": false})
}

func (h *OfficeAgentHandler) Submit(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return
	}
	var in service.OfficePilotSubmit
	if !officeDecode(c, &in, 40<<10) {
		return
	}
	run, err := h.service.SubmitPilot(c.Request.Context(), subject.UserID, in)
	if officeHandlerError(c, err) {
		return
	}
	response.Accepted(c, run)
}

func (h *OfficeAgentHandler) Get(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), subject.UserID, c.Param("id"))
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, run)
}

func (h *OfficeAgentHandler) Cancel(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return
	}
	run, err := h.service.Cancel(c.Request.Context(), subject.UserID, c.Param("id"))
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, run)
}

func (h *OfficeAgentHandler) Events(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return
	}
	after, ok := officeAfter(c)
	if !ok {
		return
	}
	events, err := h.service.Events(c.Request.Context(), subject.UserID, c.Param("id"), after)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, gin.H{"events": events})
}

func (h *OfficeAgentHandler) Messages(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return
	}
	after, ok := officeAfter(c)
	if !ok {
		return
	}
	rows, err := h.service.Messages(c.Request.Context(), subject.UserID, c.Param("id"), after)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, gin.H{"messages": rows})
}

func officeAfter(c *gin.Context) (int64, bool) {
	values, exists := c.Request.URL.Query()["after"]
	if !exists {
		return 0, true
	}
	if len(values) == 1 {
		after, err := strconv.ParseInt(values[0], 10, 64)
		if err == nil && after >= 0 {
			return after, true
		}
	}
	officeHandlerError(c, service.ErrOfficeInvalid)
	return 0, false
}

func (h *OfficeAgentHandler) Claim(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	if !h.service.BootstrapAuthorized(officeBearer(c)) {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return
	}
	var in struct {
		WorkerID      string `json:"worker_id"`
		Preset        string `json:"preset"`
		ExecutorReady *bool  `json:"executor_ready"`
	}
	if !officeDecode(c, &in, 1024) {
		return
	}
	if in.Preset == "" {
		in.Preset = "office"
	}
	if _, e := h.service.AdminRepository(); e == nil {
		if officeHandlerError(c, h.service.WorkerSeen(c.Request.Context(), in.WorkerID, in.Preset, in.ExecutorReady)) {
			return
		}
	}
	manifest, err := h.service.ClaimPreset(c.Request.Context(), in.WorkerID, in.Preset)
	if officeHandlerError(c, err) {
		return
	}
	if manifest == nil {
		c.Status(http.StatusNoContent)
		return
	}
	response.Success(c, manifest)
}

func (h *OfficeAgentHandler) Heartbeat(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	token, err := h.service.Heartbeat(c.Request.Context(), *lease)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, gin.H{"lease_token": token})
}

func (h *OfficeAgentHandler) Suspend(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	lease, err := h.service.AuthenticateSuspensionLease(c.Request.Context(), c.Param("id"), c.GetHeader("X-Office-Lease"), officeBearer(c))
	if officeHandlerError(c, err) {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !officeDecode(c, &in, 256) {
		return
	}
	if len(in.Code) > 64 {
		officeHandlerError(c, service.ErrOfficeInvalid)
		return
	}
	_, err = h.service.Suspend(c.Request.Context(), *lease, in.Code)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, gin.H{"status": service.OfficeRunNeedsConfirmation})
}

func (h *OfficeAgentHandler) Model(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	body, err := readOfficeBody(c, h.service.ModelRequestBodyLimit())
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		err = service.ErrOfficeModelInputTooLarge
	}
	if officeHandlerError(c, err) {
		return
	}
	reply, err := h.service.InvokeModelPurpose(c.Request.Context(), *lease, c.GetHeader("X-Office-Operation-ID"), body, c.GetHeader("X-Office-Model-Purpose"))
	if officeHandlerError(c, err) {
		return
	}
	writeOfficeModelReply(c, reply)
}

func writeOfficeModelReply(c *gin.Context, reply *service.OfficeGatewayReply) {
	// Invoke returned only after a complete response was deposited in its
	// durable inbox. This classifies a confirmed upstream failure, never billing.
	c.Header("X-Office-Operation-State", "completed")
	c.Header("X-Client-Request-ID", reply.RequestID)
	c.Data(reply.Status, reply.ContentType, reply.Body)
}

func (h *OfficeAgentHandler) Progress(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		EventID string          `json:"event_id"`
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if !officeDecode(c, &in, service.OfficeMaxEventBytes+1024) {
		return
	}
	event, err := h.service.StructuredProgress(c.Request.Context(), *lease, in.EventID, in.Type, in.Payload)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, event)
}

func (h *OfficeAgentHandler) Finish(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		Status       string `json:"status"`
		Content      string `json:"content"`
		ErrorCode    string `json:"error_code,omitempty"`
		ErrorMessage string `json:"error_message,omitempty"`
	}
	if !officeDecode(c, &in, 100<<10) {
		return
	}
	run, err := h.service.Finish(c.Request.Context(), *lease, in.Status, in.Content, in.ErrorCode, in.ErrorMessage)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, run)
}

func (h *OfficeAgentHandler) PrepareTool(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		Input json.RawMessage `json:"input"`
	}
	if !officeDecode(c, &in, service.OfficeMaxOperationInputBytes) {
		return
	}
	op, execute, err := h.service.PrepareTool(c.Request.Context(), *lease, c.Param("operation"), in.Input)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, gin.H{"state": op.State, "execute": execute, "result": op.Result})
}

func (h *OfficeAgentHandler) CompleteTool(c *gin.Context) {
	lease := h.lease(c, true)
	if lease == nil {
		return
	}
	var in service.OfficeOperationResult
	if !officeDecode(c, &in, service.OfficeMaxOperationResultBytes) {
		return
	}
	op, err := h.service.CompleteTool(c.Request.Context(), *lease, c.Param("operation"), in)
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, gin.H{"state": op.State})
}

func (h *OfficeAgentHandler) enabled(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if h == nil || !h.service.Enabled() {
		officeHandlerError(c, service.ErrOfficeDisabled)
		return false
	}
	return true
}

func officeBearer(c *gin.Context) string {
	value := c.GetHeader("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}

func (h *OfficeAgentHandler) lease(c *gin.Context, resultOnly bool) *service.OfficeRunLease {
	if !h.enabled(c) {
		return nil
	}
	var lease *service.OfficeRunLease
	var err error
	if resultOnly {
		lease, err = h.service.AuthenticateResultLease(c.Request.Context(), c.Param("id"), c.GetHeader("X-Office-Lease"), officeBearer(c))
	} else {
		lease, err = h.service.AuthenticateLease(c.Request.Context(), c.Param("id"), c.GetHeader("X-Office-Lease"), officeBearer(c))
	}
	if officeHandlerError(c, err) {
		return nil
	}
	return lease
}

func readOfficeBody(c *gin.Context, limit int64) ([]byte, error) {
	if encoding := c.GetHeader("Content-Encoding"); encoding != "" && encoding != "identity" {
		return nil, service.ErrOfficeInvalid
	}
	request := c.Request
	if limit > 0 {
		if request.ContentLength > limit {
			return nil, &http.MaxBytesError{Limit: limit}
		}
		request.Body = http.MaxBytesReader(c.Writer, request.Body, limit)
	}
	body, err := pkghttp.ReadRequestBodyWithPrealloc(request)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, tooLarge
		}
		if errors.Is(err, pkghttp.ErrRequestBodyMemoryBudget) {
			return nil, service.ErrOfficeCapacity
		}
		return nil, service.ErrOfficeInvalid
	}
	if pkghttp.ReserveRequestBodyMemory(request.Context(), int64(len(body))*2) != nil {
		return nil, service.ErrOfficeCapacity
	}
	return body, nil
}

func officeDecode(c *gin.Context, value any, limit int) bool {
	body, err := readOfficeBody(c, int64(limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"code": "INPUT_TOO_LARGE", "message": "request exceeds pilot input limit", "retryable": false}})
		return false
	}
	if officeHandlerError(c, err) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		officeHandlerError(c, service.ErrOfficeInvalid)
		return false
	}
	return true
}

func officeHandlerError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	status, code, message, retryable := http.StatusInternalServerError, "OFFICE_INTERNAL", "办公试验服务暂时不可用", false
	switch {
	case errors.Is(err, service.ErrOfficeDisabled):
		status, code, message = http.StatusNotFound, "OFFICE_DISABLED", "办公试验入口尚未启用"
	case errors.Is(err, service.ErrOfficeSearchUnavailable):
		status, code, message = http.StatusConflict, "WORKBENCH_SEARCH_UNAVAILABLE", "联网搜索暂不可用，请检查工作台的搜索开关和供应商配置"
	case errors.Is(err, service.ErrWorkbenchSearchPriceChanged):
		status, code, message = http.StatusConflict, "WORKBENCH_SEARCH_PRICE_CHANGED", "联网搜索价格已更新，请刷新工作台查看新价格后再发送"
	case errors.Is(err, service.ErrOfficeSearchModelUnsupported):
		status, code, message = http.StatusBadRequest, "WORKBENCH_SEARCH_MODEL_UNSUPPORTED", "当前模型未确认支持工具调用，请选择可使用联网搜索的模型"
	case errors.Is(err, service.ErrOfficeUnauthorized):
		status, code, message = http.StatusUnauthorized, "OFFICE_UNAUTHORIZED", "执行凭证无效或已过期"
	case errors.Is(err, service.ErrOfficeStepBusy):
		status, code, message, retryable = http.StatusConflict, "WORKBENCH_STEP_BUSY", "这一步仍在执行", true
		c.Header("Retry-After", "1")
	case errors.Is(err, service.ErrOfficeLeaseLost):
		status, code, message = http.StatusConflict, "LEASE_LOST", "任务已取消、租约失效或已被其他执行器接管"
	case errors.Is(err, service.ErrOfficeNotFound):
		status, code, message = http.StatusNotFound, "OFFICE_NOT_FOUND", "任务不存在或无权访问"
	case errors.Is(err, service.ErrOfficeResultUnknown), errors.Is(err, service.ErrOfficeOperationDispatched):
		status, code, message = http.StatusConflict, "NEEDS_CONFIRMATION", "这一步可能已经调用上游，但结果尚未确认；已停止自动重试，避免重复计费"
	case errors.Is(err, service.ErrOfficeRouteChanged):
		status, code, message = http.StatusConflict, "MODEL_ROUTE_CHANGED", "工作台模型、密钥或分组配置已变化，未发起新的模型调用"
	case errors.Is(err, service.ErrGroupNotAllowed), errors.Is(err, service.ErrInsufficientPerms):
		status, code, message = http.StatusForbidden, "WORKBENCH_AGENT_GROUP_FORBIDDEN", "当前账号无权使用工作台的 Agent 分组"
	case errors.Is(err, service.ErrOfficeModelInputTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, "MODEL_INPUT_TOO_LARGE", "模型请求超过网关允许的请求体大小，尚未发起这一步模型调用"
	case errors.Is(err, service.ErrOfficeBudgetExceeded):
		status, code, message = http.StatusUnprocessableEntity, "RUN_LIMIT_EXCEEDED", "本次任务已达到模型、工具或输入输出预算限制"
	case errors.Is(err, service.ErrOfficeSettlementPending):
		status, code, message = http.StatusConflict, "BILLING_PENDING", "上一轮模型已完成，计费记录仍在入账；本步骤尚未调用，任务已暂停"
	case errors.Is(err, service.ErrOfficeActiveRun), errors.Is(err, service.ErrOfficeConflict):
		status, code, message = http.StatusConflict, "OFFICE_CONFLICT", "已有活动任务，或同一请求编号对应的内容不同"
	case errors.Is(err, service.ErrOfficeResourceType):
		status, code, message = http.StatusBadRequest, "UNSUPPORTED_ATTACHMENT", "当前模型或工作台配置不支持这种附件"
	case errors.Is(err, service.ErrOfficeInvalid):
		status, code, message = http.StatusBadRequest, "OFFICE_INVALID", "办公试验请求参数不符合接口约定"
	case errors.Is(err, service.ErrOfficeCapacity):
		status, code, message, retryable = http.StatusServiceUnavailable, "OFFICE_CAPACITY", "执行容量暂时已满；尚未发起这一步模型调用", true
		c.Header("Retry-After", "3")
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "retryable": retryable, "needs_confirmation": code == "NEEDS_CONFIRMATION"}})
	return true
}
