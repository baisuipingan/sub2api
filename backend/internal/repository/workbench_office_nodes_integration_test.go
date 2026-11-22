//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchOfficeNodePreservesActiveAssignmentDuringDrain(t *testing.T) {
	ctx := context.Background()
	repo := &officeAgentRepository{db: integrationDB}
	id := uuid.NewString()
	node, err := repo.CreateOfficeNode(ctx, service.OfficeNode{
		ID: id, Name: "office test", URL: "https://" + id + ".example.com", State: service.OfficeNodeActive,
		MaxConcurrent: 1, TokenEncrypted: "ciphertext", Health: service.OfficeNodeUnchecked,
	})
	require.NoError(t, err)
	job := uuid.NewString()
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM workbench_office_dispatch WHERE job_key=$1`, job)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM workbench_office_nodes WHERE id=$1`, id)
	})
	checked := time.Now().UTC()
	node.WorkerIdentity, node.Health, node.LastCheckedAt = uuid.NewString(), service.OfficeNodeOnline, &checked
	node.Status = &service.OfficeNodeStatus{NodeID: node.WorkerIdentity, Concurrency: 1, Actions: []string{"read_docx"}}
	node, err = repo.SaveOfficeNodeStatus(ctx, node)
	require.NoError(t, err)
	require.True(t, node.TokenConfigured)
	require.Equal(t, []string{"read_docx"}, node.Status.Actions)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO workbench_office_dispatch(job_key,user_id,version_id,node_id,state)
		VALUES($1,1,$2,$3,'active')`, job, uuid.NewString(), id)
	require.NoError(t, err)
	node.State = service.OfficeNodeDraining
	node, err = repo.UpdateOfficeNode(ctx, node)
	require.NoError(t, err)
	require.Equal(t, 1, node.Assigned)
	require.Equal(t, service.OfficeNodeDraining, node.State)
	stable := node
	node.State = service.OfficeNodeDisabled
	_, err = repo.UpdateOfficeNode(ctx, node)
	require.ErrorIs(t, err, service.ErrOfficeNodeBusy)
	node.State, node.URL = service.OfficeNodeDraining, "https://different."+id+".example.com"
	_, err = repo.UpdateOfficeNode(ctx, node)
	require.ErrorIs(t, err, service.ErrOfficeNodeBusy)
	require.ErrorIs(t, repo.DeleteOfficeNode(ctx, id), service.ErrOfficeNodeBusy)
	_, err = integrationDB.ExecContext(ctx, `UPDATE workbench_office_dispatch SET state='done' WHERE job_key=$1`, job)
	require.NoError(t, err)
	stable.State = service.OfficeNodeDisabled
	stable, err = repo.UpdateOfficeNode(ctx, stable)
	require.NoError(t, err)
	require.Zero(t, stable.Assigned)
	require.NoError(t, repo.DeleteOfficeNode(ctx, id))

	// An older request can still arrive at the worker while its new registration is being checked.
	replacement := stable
	replacement.ID = uuid.NewString()
	t.Cleanup(func() { _ = repo.DeleteOfficeNode(ctx, replacement.ID) })
	replacement, err = repo.CreateOfficeNode(ctx, replacement)
	require.NoError(t, err)
	require.Greater(t, replacement.Revision, stable.Revision)
}
