package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type workbenchRouteTarget struct {
	UserID    int64  `json:"user_id"`
	TaskID    string `json:"task_id"`
	APIKeyID  int64  `json:"api_key_id"`
	Ordinal   int    `json:"ordinal"`
	ClientID  string `json:"client_id"`
	BillingID string `json:"billing_id"`
}

// Read only the bounded page's calls and credential-free projections of existing
// gateway logs. sent_at means handed to the gateway, not sent to a supplier.
// An in-flight request has no persisted usage/error outcome yet, so its supplier
// remains pending rather than being guessed from current routing configuration.
func loadWorkbenchTaskRoutes(ctx context.Context, tx *sql.Tx, items []service.WorkbenchTaskSummary) error {
	if len(items) == 0 {
		return nil
	}
	indexes := make(map[string]int, len(items))
	for i := range items {
		items[i].RouteCalls = []service.WorkbenchTaskRouteCall{}
		indexes[workbenchRouteKey(items[i].UserID, items[i].TaskID)] = i
	}
	page, err := json.Marshal(items)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.user_id,c.task_id,j.api_key_id,c.ordinal,c.status,c.sent_at
        FROM jsonb_to_recordset($1::jsonb) AS p(user_id bigint,task_id text)
        JOIN image_studio_jobs j ON j.user_id=p.user_id AND j.task_id=p.task_id
        JOIN image_studio_job_calls c ON c.user_id=j.user_id AND c.task_id=j.task_id
        ORDER BY c.user_id,c.task_id,c.ordinal`, page)
	if err != nil {
		return err
	}
	targets := []workbenchRouteTarget{}
	callIndexes := map[string]int{}
	for rows.Next() {
		var t workbenchRouteTarget
		var status string
		var sent sql.NullTime
		if err := rows.Scan(&t.UserID, &t.TaskID, &t.APIKeyID, &t.Ordinal, &status, &sent); err != nil {
			_ = rows.Close()
			return err
		}
		key := workbenchRouteKey(t.UserID, t.TaskID)
		i := indexes[key]
		state := "unknown"
		if !sent.Valid && (status == "queued" || status == "cancelled" || status == "failed") {
			state = "not_dispatched"
		} else if status == "generating" || status == "saving" {
			state = "pending"
		}
		callIndexes[fmt.Sprintf("%s:%d", key, t.Ordinal)] = len(items[i].RouteCalls)
		items[i].RouteCalls = append(items[i].RouteCalls, service.WorkbenchTaskRouteCall{Ordinal: t.Ordinal, State: state, Upstreams: []service.WorkbenchTaskUpstream{}})
		t.ClientID = service.StudioJobCallRequestID(t.UserID, t.TaskID, t.Ordinal)
		t.BillingID = "client:" + t.ClientID
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	data, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	// Success rows provide the actual effective model. Historical error events
	// may omit a per-attempt model: preserve null rather than attributing the
	// final model to a different failed account. Recovered
	// attempts are included, but account-auth/local-routing events are excluded.
	rows, err = tx.QueryContext(ctx, `WITH targets AS (
        SELECT * FROM jsonb_to_recordset($1::jsonb) AS t(user_id bigint,task_id text,api_key_id bigint,ordinal integer,client_id text,billing_id text)
      ), observed AS (
        SELECT t.user_id,t.task_id,t.ordinal,u.account_id,COALESCE(a.name,'') AS account_name,
          NULLIF(u.upstream_model,'') AS upstream_model,
          'usage'::text AS outcome,NULL::integer AS status_code,u.created_at AS recorded_at,u.id AS log_id,0::bigint AS event_index
        FROM targets t JOIN usage_logs u ON u.request_id=t.billing_id AND u.user_id=t.user_id AND u.api_key_id=t.api_key_id
        LEFT JOIN accounts a ON a.id=u.account_id
        UNION ALL
        SELECT t.user_id,t.task_id,t.ordinal,(ev.value->>'account_id')::bigint,
          COALESCE(NULLIF(ev.value->>'account_name',''),a.name,''),NULLIF(ev.value->>'upstream_model',''),'failed'::text,
          NULLIF((ev.value->>'upstream_status_code')::integer,0),
          COALESCE(to_timestamp(NULLIF((ev.value->>'at_unix_ms')::bigint,0)/1000.0),e.created_at),e.id,ev.ordinality
        FROM targets t JOIN ops_error_logs e ON e.client_request_id=t.client_id AND e.user_id=t.user_id AND e.api_key_id=t.api_key_id
        CROSS JOIN LATERAL jsonb_array_elements(COALESCE(NULLIF(e.upstream_errors,'null'::jsonb),'[]'::jsonb)) WITH ORDINALITY AS ev(value,ordinality)
        LEFT JOIN accounts a ON a.id=(ev.value->>'account_id')::bigint
        WHERE COALESCE(ev.value->>'stage','') NOT IN ('account_auth','auth','routing')
          AND COALESCE((ev.value->>'account_id')::bigint,0)>0
          AND (COALESCE((ev.value->>'upstream_status_code')::integer,0)>0 OR COALESCE(ev.value->>'upstream_url','')<>'')
        UNION ALL
        SELECT t.user_id,t.task_id,t.ordinal,e.account_id,COALESCE(a.name,''),NULLIF(e.upstream_model,''),'failed'::text,
          e.upstream_status_code,e.created_at,e.id,0::bigint
        FROM targets t JOIN ops_error_logs e ON e.client_request_id=t.client_id AND e.user_id=t.user_id AND e.api_key_id=t.api_key_id
        LEFT JOIN accounts a ON a.id=e.account_id
        WHERE e.status_code>=400 AND e.account_id>0 AND COALESCE(e.upstream_model,'')<>''
          AND e.error_phase IN ('upstream','network')
          AND jsonb_array_length(COALESCE(NULLIF(e.upstream_errors,'null'::jsonb),'[]'::jsonb))=0
      ) SELECT user_id,task_id,ordinal,account_id,account_name,upstream_model,outcome,status_code,recorded_at
        FROM observed ORDER BY user_id,task_id,ordinal,recorded_at,log_id,event_index`, data)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var userID int64
		var taskID string
		var ordinal int
		var upstream service.WorkbenchTaskUpstream
		if err := rows.Scan(&userID, &taskID, &ordinal, &upstream.AccountID, &upstream.AccountName, &upstream.UpstreamModel, &upstream.Outcome, &upstream.StatusCode, &upstream.RecordedAt); err != nil {
			return err
		}
		key := workbenchRouteKey(userID, taskID)
		i, ok := indexes[key]
		if !ok {
			continue
		}
		callIndex, ok := callIndexes[fmt.Sprintf("%s:%d", key, ordinal)]
		if !ok {
			continue
		}
		call := &items[i].RouteCalls[callIndex]
		call.State = "recorded"
		call.Upstreams = append(call.Upstreams, upstream)
	}
	return rows.Err()
}

func workbenchRouteKey(userID int64, taskID string) string {
	return fmt.Sprintf("%d:%s", userID, taskID)
}
