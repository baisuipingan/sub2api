//go:build unit

package service

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type officeDeliveryTestRepo struct {
	*workbenchToolTestRepo
	officeDispatchRepository
	workbenchOfficeNodeRepository
	node OfficeNode
	job  OfficeDispatch
}

func (r *officeDeliveryTestRepo) AppendEvent(context.Context, OfficeRunLease, string, string, json.RawMessage) (*OfficeRunEvent, error) {
	return nil, nil
}

func (r *officeDeliveryTestRepo) OfficeDispatch(_ context.Context, key string) (*OfficeDispatch, error) {
	if r.job.Key != key {
		return nil, nil
	}
	job := r.job
	return &job, nil
}

func (r *officeDeliveryTestRepo) GetOfficeNode(context.Context, string) (OfficeNode, error) {
	return r.node, nil
}

func (r *officeDeliveryTestRepo) ListOfficeNodes(context.Context) ([]OfficeNode, error) {
	return []OfficeNode{r.node}, nil
}

func (r *officeDeliveryTestRepo) AssignOfficeDispatch(_ context.Context, key, node string, capacity int) (*OfficeDispatch, error) {
	if key != r.job.Key || r.job.State != "waiting" || node != r.node.ID || capacity != 1 {
		return nil, ErrOfficeConflict
	}
	r.job.NodeID, r.job.State = node, "active"
	r.job.Attempt++
	return &r.job, nil
}

func (r *officeDeliveryTestRepo) FinishOfficeDispatch(_ context.Context, job OfficeDispatch, state string) error {
	if job.Key != r.job.Key || job.Attempt != r.job.Attempt || job.State != r.job.State {
		return ErrOfficeConflict
	}
	r.job.State, r.job.Receipt = state, nil
	return nil
}

type officeDeliveryResourcesTestRepo struct {
	OfficeResourcesRepository
	reserveErr error
	previewErr error
	reserved   OfficeResource
	finished   bool
}

func (r *officeDeliveryResourcesTestRepo) ReserveVersion(_ context.Context, _ OfficeResource, _ *OfficeRunLease) (*OfficeResource, error) {
	return &r.reserved, r.reserveErr
}

func (r *officeDeliveryResourcesTestRepo) FinishVersion(_ context.Context, _ int64, _ string, _ *OfficeRunLease) (*OfficeResource, error) {
	r.finished = true
	r.reserved.State = "ready"
	return &r.reserved, nil
}

func (r *officeDeliveryResourcesTestRepo) StorePrintPreview(_ context.Context, _ int64, _, _ string, _ int64, _ string, _ *OfficeDispatch, upload func() error) error {
	if r.previewErr != nil {
		return r.previewErr
	}
	return upload()
}

type officeDeliveryTestStore struct {
	GalleryObjectStore
	data []byte
}

func (s officeDeliveryTestStore) Get(context.Context, string, int64) ([]byte, error) {
	return s.data, nil
}

func officeDeliveryFixture(t *testing.T) (*OfficeAgentService, *officeDeliveryTestRepo, *officeDeliveryResourcesTestRepo, *OfficeOperation, []byte) {
	t.Helper()
	svc, bridge := officeBridgeFixture(nil)
	now := time.Now()
	operation := OfficeOperation{ID: "browser:tool:0:0", Kind: OfficeOperationTool, State: OfficeOperationDispatched, DispatchedAt: &now}
	bridge.ops[operation.ID] = operation
	output := []byte("test output")
	checksum := md5.Sum(output)
	receipt, err := json.Marshal(officeWorkerJob{Status: "completed", Output: &officeWorkerOutput{Name: "result.docx",
		Bytes: int64(len(output)), SHA256: officeResourceHash(output), ContentMD5: base64.StdEncoding.EncodeToString(checksum[:])}})
	require.NoError(t, err)
	node := OfficeNode{ID: uuid.NewString(), State: OfficeNodeActive, Health: OfficeNodeOnline, WorkerIdentity: "test",
		LastCheckedAt: &now, MaxConcurrent: 1, Status: &OfficeNodeStatus{Concurrency: 1, Running: 1, Actions: []string{"create_docx"}}}
	repo := &officeDeliveryTestRepo{workbenchToolTestRepo: &workbenchToolTestRepo{bridge}, node: node, job: OfficeDispatch{
		Key: officeWorkerID(bridge.lease.Run.ID, operation.ID), UserID: bridge.lease.Run.UserID, RunID: bridge.lease.Run.ID,
		State: "active", NodeID: node.ID, Attempt: 1, AssignedAt: &now, Receipt: receipt,
	}}
	svc.repo = repo
	fallback := config.ImageStorageConfig{Endpoint: "https://store.test", Region: "auto", Bucket: "office",
		AccessKeyID: "test", SecretAccessKey: "test-secret"}
	settings, _, _ := newImageStorageFixture(t, fallback)
	resources := &officeDeliveryResourcesTestRepo{reserved: OfficeResource{Endpoint: fallback.Endpoint, Region: fallback.Region,
		Bucket: fallback.Bucket, Name: "result.docx", VersionID: uuid.NewString(), State: "pending"}}
	svc.resources = &OfficeResourcesService{repo: resources, settings: settings}
	return svc, repo, resources, &operation, output
}

func TestWorkbenchOfficeDeliveryQuotaReleasesAssignment(t *testing.T) {
	svc, repo, resources, op, _ := officeDeliveryFixture(t)
	resources.reserveErr = ErrOfficeResourceQuota
	err := svc.runRemoteOfficeTool(context.Background(), nil, repo.lease, op, workbenchNativeCall{Name: "create_docx"}, "")
	require.NoError(t, err)
	require.Equal(t, "done", repo.job.State)
	require.Contains(t, repo.ops[op.ID].Result.ErrorMessage, "文件存储空间已满")
}

func TestWorkbenchOfficeDeliveryReceiptBoundedByStepDeadline(t *testing.T) {
	for _, test := range []struct {
		name          string
		reserveErr    error
		enableStorage bool
		wantOutput    bool
	}{
		{name: "reservation unavailable", reserveErr: errors.New("database unavailable")},
		{name: "storage unavailable"},
		{name: "already uploaded", enableStorage: true, wantOutput: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, resources, op, output := officeDeliveryFixture(t)
			started := time.Now().Add(-officeDispatchDeadline - time.Second)
			op.DispatchedAt = &started
			resources.reserveErr = test.reserveErr
			if test.enableStorage {
				svc.resources.factory = func(context.Context, *config.ImageStorageConfig) (GalleryObjectStore, error) {
					return officeDeliveryTestStore{data: output}, nil
				}
			}
			err := svc.runRemoteOfficeTool(context.Background(), nil, repo.lease, op, workbenchNativeCall{Name: "create_docx"}, "")
			require.NoError(t, err)
			require.Equal(t, "done", repo.job.State)
			require.Equal(t, test.wantOutput, resources.finished)
			if test.wantOutput {
				require.Empty(t, repo.ops[op.ID].Result.ErrorCode)
			} else {
				require.Contains(t, repo.ops[op.ID].Result.ErrorMessage, "成品已过期")
			}
		})
	}
}

func TestWorkbenchOfficeDispatchAdmitsDespiteStaleLoad(t *testing.T) {
	svc, repo, _, _, _ := officeDeliveryFixture(t)
	repo.job.State = "waiting"
	job, err := svc.assignOfficeDispatch(context.Background(), repo, repo.job, "create_docx")
	require.NoError(t, err)
	require.Equal(t, "active", job.State)
}

func TestWorkbenchOfficePrintPreviewTerminalDeliveryReleasesAssignment(t *testing.T) {
	for _, cause := range []error{ErrOfficeResourceQuota, ErrOfficeNotFound} {
		t.Run(cause.Error(), func(t *testing.T) {
			svc, repo, resources, _, output := officeDeliveryFixture(t)
			resources.previewErr = cause
			source := resources.reserved
			source.UserID = repo.lease.Run.UserID
			source.VersionID = uuid.NewString()
			repo.job.Key, repo.job.VersionID, repo.job.RunID = officePrintJobKey(source.VersionID), source.VersionID, ""
			var receipt officeWorkerJob
			require.NoError(t, json.Unmarshal(repo.job.Receipt, &receipt))
			receipt.Output.Name = "preview.pdf"
			var err error
			repo.job.Receipt, err = json.Marshal(receipt)
			require.NoError(t, err)
			svc.resources.factory = func(context.Context, *config.ImageStorageConfig) (GalleryObjectStore, error) {
				return officeDeliveryTestStore{data: output}, nil
			}
			_, err = svc.renderPrintPreview(context.Background(), &source, "print.pdf", false)
			require.ErrorIs(t, err, cause)
			require.Equal(t, "cancelled", repo.job.State)
		})
	}
}
