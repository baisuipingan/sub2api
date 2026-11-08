//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchObservedRoutesKeepChildrenRetriesAndUnknownHistory(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `
      CREATE TEMP TABLE image_studio_jobs(user_id bigint,task_id text,api_key_id bigint);
      CREATE TEMP TABLE image_studio_job_calls(user_id bigint,task_id text,ordinal integer,status text,sent_at timestamptz);
      CREATE TEMP TABLE accounts(id bigint,name text);
      CREATE TEMP TABLE usage_logs(id bigint,user_id bigint,api_key_id bigint,request_id text,account_id bigint,upstream_model text,requested_model text,model text,created_at timestamptz);
      CREATE TEMP TABLE ops_error_logs(id bigint,user_id bigint,api_key_id bigint,client_request_id text,account_id bigint,upstream_model text,error_phase text,status_code integer,upstream_status_code integer,upstream_errors jsonb,created_at timestamptz);
      INSERT INTO image_studio_jobs VALUES(9,'same-task',77),(10,'same-task',88);
      INSERT INTO image_studio_job_calls VALUES
        (9,'same-task',0,'completed',NOW()),(9,'same-task',1,'generating',NOW()),
        (9,'same-task',2,'completed',NULL),(9,'same-task',3,'queued',NULL),
        (9,'same-task',4,'failed',NOW()),(9,'same-task',5,'completed',NOW()),(10,'same-task',0,'completed',NOW());
      INSERT INTO accounts VALUES(101,'first supplier'),(102,'final supplier'),(999,'do not expose');`)
	require.NoError(t, err)
	client := service.StudioJobCallRequestID(9, "same-task", 0)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs VALUES
      (1,9,77,$1,102,'actual-2k','public-image','public-image',NOW()),
      (2,10,77,$1,999,'wrong-user','public-image','public-image',NOW()),
      (3,9,88,$1,999,'wrong-key','public-image','public-image',NOW())`, "client:"+client)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs VALUES(4,9,77,$1,102,NULL,'public-alias','billing-name',NOW())`, "client:"+service.StudioJobCallRequestID(9, "same-task", 5))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO ops_error_logs VALUES
      (1,9,77,$1,101,'never-assign-final-model','upstream',200,503,
        '[{"account_id":101,"account_name":"first supplier snapshot","kind":"failover","upstream_status_code":503,"upstream_url":"https://private-url?secret=hidden","message":"private-error"},
          {"account_id":999,"stage":"account_auth","upstream_status_code":401,"upstream_url":"https://private-token-endpoint"},
          {"account_id":101,"kind":"failover","upstream_model":"attempt-2k","upstream_status_code":429,"upstream_url":"https://private-url"}]',NOW()-INTERVAL '1 second'),
      (2,10,77,$1,999,'wrong-user','upstream',502,502,'[]',NOW()),
      (3,9,88,$1,999,'wrong-key','upstream',502,502,'[]',NOW()),
      (4,9,77,$2,101,'failed-model','upstream',502,502,'[]',NOW())`, client, service.StudioJobCallRequestID(9, "same-task", 4))
	require.NoError(t, err)
	items := []service.WorkbenchTaskSummary{{UserID: 9, TaskID: "same-task"}, {UserID: 10, TaskID: "same-task"}}
	require.NoError(t, loadWorkbenchTaskRoutes(ctx, tx, items))
	require.Len(t, items[0].RouteCalls, 6)
	call := items[0].RouteCalls[0]
	require.Equal(t, "recorded", call.State)
	require.Len(t, call.Upstreams, 3)
	require.Equal(t, int64(101), call.Upstreams[0].AccountID)
	require.Equal(t, "first supplier snapshot", call.Upstreams[0].AccountName)
	require.Nil(t, call.Upstreams[0].UpstreamModel, "old errors must not inherit a different attempt's final model")
	require.Equal(t, "attempt-2k", *call.Upstreams[1].UpstreamModel)
	require.Equal(t, "usage", call.Upstreams[2].Outcome)
	require.Equal(t, "actual-2k", *call.Upstreams[2].UpstreamModel)
	require.Equal(t, "pending", items[0].RouteCalls[1].State)
	require.Equal(t, "unknown", items[0].RouteCalls[2].State, "completed legacy rows without logs remain unknown")
	require.Equal(t, "not_dispatched", items[0].RouteCalls[3].State)
	require.Equal(t, "failed-model", *items[0].RouteCalls[4].Upstreams[0].UpstreamModel)
	require.Nil(t, items[0].RouteCalls[5].Upstreams[0].UpstreamModel, "public and billing names do not prove the actual upstream model")
	require.Empty(t, items[1].RouteCalls[0].Upstreams, "same public task ID must not cross user boundaries")
	encoded, err := json.Marshal(items)
	require.NoError(t, err)
	for _, secret := range []string{"private-url", "secret=hidden", "private-error", "private-token-endpoint", "do not expose", "wrong-key", "wrong-user", "never-assign-final-model", "api_key_id", "request_id", "public-alias", "billing-name"} {
		require.NotContains(t, string(encoded), secret)
	}
}
