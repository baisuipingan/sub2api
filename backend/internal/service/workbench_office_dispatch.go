package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

type OfficeDispatch struct {
	Key, RunID, VersionID, NodeID, State string
	UserID                               int64
	Attempt                              int
	AssignedAt                           *time.Time
	Receipt                              json.RawMessage
}

type officeDispatchRepository interface {
	QueueOfficeDispatch(context.Context, OfficeDispatch) (*OfficeDispatch, error)
	RetryOfficePreview(context.Context, OfficeDispatch) (*OfficeDispatch, error)
	OfficeDispatch(context.Context, string) (*OfficeDispatch, error)
	AssignOfficeDispatch(context.Context, string, string, int) (*OfficeDispatch, error)
	SaveOfficeDispatchReceipt(context.Context, OfficeDispatch, json.RawMessage) error
	FinishOfficeDispatch(context.Context, OfficeDispatch, string) error
	ClaimOfficeDispatchChecks(context.Context, int, time.Duration) ([]OfficeDispatch, error)
	CleanOfficeDispatch(context.Context) error
}

type officeDispatchContextKey struct{}

type officeDispatchTarget struct {
	item         *OfficeDispatch
	node         OfficeNode
	stepTimedOut bool
}

const officeDispatchDeadline = 12 * time.Minute

var (
	errOfficeWorkerRefused = errors.New("office worker refused admission")
	errOfficeAwaitingNode  = errors.Join(ErrOfficeStepBusy, errors.New("waiting for an office node"))
)

func (s *OfficeAgentService) remoteOfficeConfigured(ctx context.Context) bool {
	if s.cfg.ToolsURL != "" {
		return true
	}
	nodes, err := s.officeNodes(ctx)
	return err == nil && len(nodes) != 0
}

func (s *OfficeAgentService) existingOfficeDispatch(ctx context.Context, key string) (*OfficeDispatch, error) {
	if repo, ok := s.repo.(officeDispatchRepository); ok {
		return repo.OfficeDispatch(ctx, key)
	}
	return nil, nil
}

func (target *officeDispatchTarget) expired() bool {
	return target != nil && (target.stepTimedOut || target.item.AssignedAt != nil && time.Since(*target.item.AssignedAt) >= officeDispatchDeadline)
}

func (s *OfficeAgentService) officeDispatchTarget(ctx context.Context, seed OfficeDispatch, action string, retryPreview bool) (context.Context, *officeDispatchTarget, error) {
	repo, ok := s.repo.(officeDispatchRepository)
	if !ok {
		return ctx, nil, nil
	}
	item, err := repo.OfficeDispatch(ctx, seed.Key)
	if err != nil {
		return ctx, nil, err
	}
	if item == nil {
		nodes, err := s.officeNodes(ctx)
		if err != nil {
			return ctx, nil, err
		}
		if len(nodes) == 0 && s.cfg.ToolsURL != "" {
			return ctx, nil, nil
		}
		item, err = repo.QueueOfficeDispatch(ctx, seed)
		if err != nil {
			return ctx, nil, err
		}
	}
	if retryPreview && item != nil && item.VersionID != "" && item.State == "cancelled" {
		item, err = repo.RetryOfficePreview(ctx, *item)
		if err != nil {
			return ctx, nil, err
		}
	}
	if item == nil {
		return ctx, nil, ErrOfficeConflict
	}
	if item.State == "waiting" {
		item, err = s.assignOfficeDispatch(ctx, repo, *item, action)
		if err != nil {
			return ctx, nil, err
		}
	}
	if item.State != "active" {
		return ctx, nil, ErrOfficeConflict
	}
	target := &officeDispatchTarget{item: item}
	ctx = context.WithValue(ctx, officeDispatchContextKey{}, target)
	node, err := s.officeNode(ctx, item.NodeID)
	if err != nil && len(item.Receipt) == 0 && !target.expired() {
		return ctx, target, err
	}
	// A persisted receipt can recover an object that already reached storage,
	// even if the worker is gone by the time delivery is reconciled.
	target.node = node
	return ctx, target, nil
}

func (s *OfficeAgentService) assignOfficeDispatch(ctx context.Context, repo officeDispatchRepository, item OfficeDispatch, action string) (*OfficeDispatch, error) {
	nodes, err := s.officeNodes(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(nodes, func(a, b OfficeNode) int {
		if a.ID == item.NodeID && b.ID != item.NodeID {
			return 1
		}
		if b.ID == item.NodeID && a.ID != item.NodeID {
			return -1
		}
		return 0
	})
	for _, node := range nodes {
		if node.State != OfficeNodeActive || !node.officeNodeOnline() {
			continue
		}
		capacity := node.officeNodeCapacity()
		if capacity == 0 || !slices.Contains(node.Status.Actions, action) {
			continue
		}
		assigned, err := repo.AssignOfficeDispatch(ctx, item.Key, node.ID, capacity)
		if err != nil || assigned != nil {
			return assigned, err
		}
	}
	// A concurrent preview request may have won the same job in the meantime.
	current, err := repo.OfficeDispatch(ctx, item.Key)
	if err != nil {
		return nil, err
	}
	if current != nil && current.State == "active" {
		return current, nil
	}
	return nil, errOfficeAwaitingNode
}

func (target *officeDispatchTarget) endpoint(key string) string {
	if target != nil {
		key = officeDigest([]byte(key + ":" + strconv.Itoa(target.item.Attempt)))
	}
	return "/v1/jobs/" + key
}

func (s *OfficeAgentService) finishOfficeDispatch(ctx context.Context, target *officeDispatchTarget, state string) error {
	if target == nil {
		return nil
	}
	repo, ok := s.repo.(officeDispatchRepository)
	if !ok {
		return ErrOfficeDisabled
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return repo.FinishOfficeDispatch(finish, *target.item, state)
}

func (s *OfficeAgentService) officeWorkerReceipt(ctx context.Context, target *officeDispatchTarget, endpoint string, submit func() ([]byte, error)) ([]byte, error) {
	if target != nil && len(target.item.Receipt) != 0 {
		return target.item.Receipt, nil
	}
	if target.expired() {
		return nil, errOfficeOutputExpired
	}
	raw, err := s.callOfficeWorker(ctx, http.MethodGet, endpoint, nil, OfficeMaxOperationResultBytes)
	if errors.Is(err, ErrOfficeNotFound) {
		body, buildErr := submit()
		if buildErr != nil {
			return nil, buildErr
		}
		raw, err = s.callOfficeWorker(ctx, http.MethodPost, endpoint, body, OfficeMaxOperationResultBytes)
	}
	if target != nil {
		var receipt officeWorkerJob
		if err == nil && json.Unmarshal(raw, &receipt) == nil && receipt.Error == "WORKER_BUSY" {
			err = errOfficeWorkerRefused
		}
		if errors.Is(err, errOfficeWorkerRefused) {
			if finishErr := s.finishOfficeDispatch(ctx, target, "waiting"); finishErr != nil {
				return nil, finishErr
			}
			return nil, ErrOfficeStepBusy
		}
		if err == nil && receipt.Output != nil && (receipt.Status == "completed" || receipt.Status == "partial") {
			repo, ok := s.repo.(officeDispatchRepository)
			if !ok {
				return nil, ErrOfficeDisabled
			}
			if saveErr := repo.SaveOfficeDispatchReceipt(ctx, *target.item, raw); saveErr != nil {
				return nil, saveErr
			}
		}
	}
	return raw, err
}

func (s *OfficeAgentService) officeDispatchProgress(ctx context.Context, lease OfficeRunLease, op, tool, phase, message string, attempt int) {
	payload, _ := json.Marshal(map[string]string{
		"operation_id": op, "tool_name": tool, "title": workbenchToolTitle(tool),
		"status": "running", "message": message,
	})
	_, _ = s.repo.AppendEvent(ctx, lease, op+":office:"+strconv.Itoa(attempt)+":"+phase, "tool.started", payload)
}

func (s *OfficeAgentService) cleanOfficeDispatches() {
	repo, ok := s.repo.(officeDispatchRepository)
	if !s.Enabled() || !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := repo.CleanOfficeDispatch(ctx); err != nil {
		slog.Warn("clean office dispatch history", "error", err)
	}
}

func (s *OfficeAgentService) sweepOfficeDeliveries() {
	dispatch, ok := s.repo.(officeDispatchRepository)
	repo, workbench := s.repo.(workbenchBrowserRepository)
	if !s.Enabled() || !ok || !workbench {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	items, err := dispatch.ClaimOfficeDispatchChecks(ctx, 8, 45*time.Second)
	if err != nil {
		slog.Warn("load pending office deliveries", "error", err)
		return
	}
	var group sync.WaitGroup
	for _, item := range items {
		group.Add(1)
		go func() {
			defer group.Done()
			s.reconcileOfficeDelivery(ctx, repo, dispatch, item)
		}()
	}
	group.Wait()
}

func (s *OfficeAgentService) reconcileOfficeDelivery(ctx context.Context, repo workbenchBrowserRepository, dispatch officeDispatchRepository, item OfficeDispatch) {
	if item.RunID == "" {
		if s.resources == nil {
			if s.cancelAssignedOfficeJob(ctx, item) {
				_ = dispatch.FinishOfficeDispatch(ctx, item, "cancelled")
			}
			return
		}
		v, err := s.resources.repo.GetResourceVersion(ctx, item.UserID, item.VersionID, true)
		if err != nil {
			target := &officeDispatchTarget{item: &item}
			if (permanentOfficeDeliveryError(err) || target.expired()) && s.cancelAssignedOfficeJob(ctx, item) {
				_ = dispatch.FinishOfficeDispatch(ctx, item, "cancelled")
			}
			return
		}
		_, _ = s.preparePrintPreviewVersion(ctx, v, false)
		return
	}
	lease, run, err := repo.ClaimBrowserStep(ctx, item.UserID, item.RunID, browserStepLease)
	if errors.Is(err, ErrOfficeNotFound) && s.cancelAssignedOfficeJob(ctx, item) {
		_ = dispatch.FinishOfficeDispatch(ctx, item, "cancelled")
	}
	if err != nil || run == nil {
		return
	}
	if lease == nil {
		if s.cancelAssignedOfficeJob(ctx, item) {
			_ = dispatch.FinishOfficeDispatch(ctx, item, "cancelled")
		}
		return
	}
	defer func() { _ = repo.ReleaseBrowserStep(ctx, *lease) }()
	var params workbenchBrowserParameters
	if json.Unmarshal(run.Parameters, &params) != nil {
		return
	}
	operations, err := repo.BrowserOperations(ctx, item.UserID, item.RunID)
	if err != nil {
		return
	}
	state, err := workbenchState(params.Protocol, operations)
	if err != nil || state.RemoteOperation == nil || officeWorkerID(item.RunID, state.RemoteOperation.ID) != item.Key {
		_ = dispatch.FinishOfficeDispatch(ctx, item, "done")
		return
	}
	var input struct {
		VersionID string `json:"version_id"`
	}
	if json.Unmarshal(state.Pending.Arguments, &input) == nil {
		_ = s.runRemoteOfficeTool(ctx, repo, *lease, state.RemoteOperation, *state.Pending, input.VersionID)
	}
}

// A missing cancel acknowledgement keeps the assignment reserved until its deadline.
func (s *OfficeAgentService) cancelAssignedOfficeJob(ctx context.Context, item OfficeDispatch) bool {
	if item.State != "active" || item.AssignedAt != nil && time.Since(*item.AssignedAt) > officeDispatchDeadline {
		return true
	}
	node, err := s.officeNode(ctx, item.NodeID)
	if err != nil {
		return false
	}
	target := &officeDispatchTarget{item: &item, node: node}
	ctx = context.WithValue(ctx, officeDispatchContextKey{}, target)
	_, err = s.callOfficeWorker(ctx, http.MethodDelete, target.endpoint(item.Key), nil, 64<<10)
	return err == nil
}
