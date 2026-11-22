//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchOfficeDispatchReservesCapacityAndKeepsAttemptOwnership(t *testing.T) {
	ctx := context.Background()
	repo := NewOfficeAgentRepository(integrationDB).(*officeAgentRepository)
	nodes := []string{uuid.NewString(), uuid.NewString()}
	for _, id := range nodes {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO workbench_office_nodes(id,name,url,state,max_concurrent,token_encrypted) VALUES($1,'test',$2,'active',1,'fixture')`, id, "https://"+id+".example.test")
		require.NoError(t, err)
	}
	keys := []string{uuid.NewString(), uuid.NewString()}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM workbench_office_dispatch WHERE job_key IN ($1,$2)`, keys[0], keys[1])
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM workbench_office_nodes WHERE id IN ($1,$2)`, nodes[0], nodes[1])
	})
	for _, key := range keys {
		job, err := repo.QueueOfficeDispatch(ctx, service.OfficeDispatch{Key: key, RunID: uuid.NewString(), UserID: 1})
		require.NoError(t, err)
		require.Equal(t, "waiting", job.State)
	}
	var wg sync.WaitGroup
	var assigned [2]*service.OfficeDispatch
	var assignmentErrors [2]error
	for i, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assigned[i], assignmentErrors[i] = repo.AssignOfficeDispatch(ctx, key, nodes[0], 1)
		}()
	}
	wg.Wait()
	require.NoError(t, assignmentErrors[0])
	require.NoError(t, assignmentErrors[1])
	winner, waiting := assigned[0], keys[1]
	if winner == nil {
		winner, waiting = assigned[1], keys[0]
	} else {
		require.Nil(t, assigned[1])
	}
	require.NotNil(t, winner)
	require.Equal(t, nodes[0], winner.NodeID)
	require.Equal(t, 1, winner.Attempt)

	_, err := integrationDB.ExecContext(ctx, `UPDATE workbench_office_nodes SET state='draining' WHERE id=$1`, nodes[0])
	require.NoError(t, err)
	none, err := repo.AssignOfficeDispatch(ctx, waiting, nodes[0], 1)
	require.NoError(t, err)
	require.Nil(t, none)
	other, err := repo.AssignOfficeDispatch(ctx, waiting, nodes[1], 1)
	require.NoError(t, err)
	require.Equal(t, nodes[1], other.NodeID)

	receipt := json.RawMessage(`{"status":"completed","output":{"sha256":"fixture"}}`)
	require.NoError(t, repo.SaveOfficeDispatchReceipt(ctx, *winner, receipt))
	recovered, err := repo.OfficeDispatch(ctx, winner.Key)
	require.NoError(t, err)
	require.Equal(t, nodes[0], recovered.NodeID)
	require.JSONEq(t, string(receipt), string(recovered.Receipt))
	checks, err := repo.ClaimOfficeDispatchChecks(ctx, 50, time.Minute)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(checks, func(item service.OfficeDispatch) bool { return item.Key == winner.Key }))
	require.NoError(t, repo.FinishOfficeDispatch(ctx, *winner, "done"))

	require.NoError(t, repo.FinishOfficeDispatch(ctx, *other, "waiting"))
	retry, err := repo.AssignOfficeDispatch(ctx, waiting, nodes[1], 1)
	require.NoError(t, err)
	require.Greater(t, retry.Attempt, other.Attempt)
	require.NoError(t, repo.FinishOfficeDispatch(ctx, *other, "done"))
	recovered, err = repo.OfficeDispatch(ctx, retry.Key)
	require.NoError(t, err)
	require.Equal(t, "active", recovered.State)
}

func TestWorkbenchOfficeDispatchRetriesCancelledPreviewOnSameVersion(t *testing.T) {
	ctx := context.Background()
	repo := NewOfficeAgentRepository(integrationDB).(*officeAgentRepository)
	_, owner, resources, _, _ := officeResourceFixture(t)
	node, key, version := uuid.NewString(), uuid.NewString(), uuid.NewString()
	file, err := resources.ReserveVersion(ctx, service.OfficeResource{
		ID: uuid.NewString(), VersionID: version, UserID: owner.UserID, Name: "fixture.docx",
		MIME:   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		SHA256: strings.Repeat("a", 64), PreviewSHA256: strings.Repeat("b", 64), Bytes: 100,
		PreviewKind: "unsupported", Source: "upload", PublicationID: "preview-fixture",
		Bucket: "fixture", ObjectKey: "workspace/v1/resources/" + version + "/original",
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, file)
	_, err = resources.FinishVersion(ctx, owner.UserID, version, nil)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO workbench_office_nodes(id,name,url,state,max_concurrent,token_encrypted) VALUES($1,'test',$2,'active',1,'fixture')`, node, "https://"+node+".example.test")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM workbench_office_dispatch WHERE job_key=$1`, key)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM workbench_office_nodes WHERE id=$1`, node)
	})
	_, err = repo.QueueOfficeDispatch(ctx, service.OfficeDispatch{Key: key, VersionID: version, UserID: owner.UserID})
	require.NoError(t, err)
	first, err := repo.AssignOfficeDispatch(ctx, key, node, 1)
	require.NoError(t, err)
	require.NotNil(t, first)
	receipt := json.RawMessage(`{"status":"completed","output":{"sha256":"fixture"}}`)
	require.NoError(t, repo.SaveOfficeDispatchReceipt(ctx, *first, receipt))
	require.NoError(t, repo.FinishOfficeDispatch(ctx, *first, "cancelled"))

	cancelled, err := repo.OfficeDispatch(ctx, key)
	require.NoError(t, err)
	reopened, err := repo.RetryOfficePreview(ctx, *cancelled)
	require.NoError(t, err)
	require.Equal(t, "waiting", reopened.State)
	require.Equal(t, first.Attempt, reopened.Attempt)
	require.Nil(t, reopened.AssignedAt)
	require.Empty(t, reopened.Receipt)
	require.NoError(t, repo.FinishOfficeDispatch(ctx, *first, "done"))

	second, err := repo.AssignOfficeDispatch(ctx, key, node, 1)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, first.Attempt+1, second.Attempt)
	require.ErrorIs(t, repo.SaveOfficeDispatchReceipt(ctx, *first, receipt), service.ErrOfficeConflict)
	latest, err := repo.RetryOfficePreview(ctx, *cancelled)
	require.NoError(t, err)
	require.Equal(t, "active", latest.State)
	require.Equal(t, second.Attempt, latest.Attempt)

	uploadCount := 0
	upload := func() error { uploadCount++; return nil }
	previewKey, previewHash := "workspace/v1/resources/"+version+"/print.pdf", strings.Repeat("c", 64)
	require.ErrorIs(t, resources.StorePrintPreview(ctx, owner.UserID, version, previewKey, 100, previewHash, first, upload), service.ErrOfficeConflict)
	require.Zero(t, uploadCount)
	require.NoError(t, resources.StorePrintPreview(ctx, owner.UserID, version, previewKey, 100, previewHash, second, upload))
	require.NoError(t, resources.StorePrintPreview(ctx, owner.UserID, version, previewKey, 100, previewHash, first, upload))
	require.Equal(t, 1, uploadCount)
}
