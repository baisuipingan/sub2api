package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *officeAgentRepository) WorkerSeen(ctx context.Context, id, preset string, ready *bool) error {
	_, e := r.db.ExecContext(ctx, `INSERT INTO office_agent_workers(id,preset,executor_ready) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET preset=EXCLUDED.preset,executor_ready=EXCLUDED.executor_ready,last_seen_at=NOW()`, id, preset, ready)
	return e
}
func (r *officeAgentRepository) AdminOverview(ctx context.Context, days int) (*service.OfficeAdminOverview, error) {
	if days < 1 || days > 90 {
		return nil, service.ErrOfficeInvalid
	}
	now := time.Now().UTC()
	o := &service.OfficeAdminOverview{Since: now.AddDate(0, 0, -days), Until: now}
	tx, e := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback() }()
	// The period follows run admission, matching the default run-list date filter.
	// Missing usage receipts and blank error rows do not count as settled models.
	e = tx.QueryRowContext(ctx, `WITH selected_runs AS MATERIALIZED (
    SELECT r.id, r.user_id, r.api_key_id, r.status
    FROM office_agent_runs r JOIN users owner ON owner.id=r.user_id
    WHERE r.browser_driven AND owner.deleted_at IS NULL AND r.created_at >= $1 AND r.created_at < $2
), runs AS (
    SELECT COUNT(*) AS total,
           COUNT(*) FILTER (WHERE status='completed') AS completed,
           COUNT(*) FILTER (WHERE status='partially_completed') AS partially_completed,
           COUNT(*) FILTER (WHERE status='failed') AS failed,
           COUNT(*) FILTER (WHERE status='cancelled') AS cancelled
    FROM selected_runs
), models AS (
    SELECT COUNT(*) AS calls, COALESCE(SUM(u.input_tokens),0) AS input_tokens,
           COALESCE(SUM(u.output_tokens),0) AS output_tokens,
           COALESCE(SUM(COALESCE(NULLIF(u.charge_credits,0),ROUND(u.actual_cost*$3::numeric))),0) AS credits
    FROM selected_runs r
    JOIN office_agent_operations op ON op.run_id=r.id AND op.user_id=r.user_id AND op.kind='model'
    JOIN usage_logs u ON u.request_id='client:'||op.request_id AND u.user_id=r.user_id AND u.api_key_id=r.api_key_id
    WHERE u.created_at < $2 AND (
        u.charge_credits > 0 OR u.actual_cost > 0 OR
        (op.state='completed' AND (op.result_json->>'http_status')::integer BETWEEN 200 AND 299
         AND COALESCE(op.result_json->>'error_code','')='')
    )
), searches AS (
    SELECT COUNT(*) AS calls, COALESCE(SUM(b.amount_credits),0) AS credits
    FROM selected_runs r
    JOIN workbench_search_billing b ON b.run_id=r.id AND b.user_id=r.user_id
    WHERE b.state='charged' AND b.settled_at < $2
)
SELECT runs.total, runs.completed, runs.partially_completed, runs.failed, runs.cancelled,
       models.calls, models.input_tokens, models.output_tokens, models.credits, searches.calls, searches.credits
FROM runs CROSS JOIN models CROSS JOIN searches`, o.Since, o.Until, service.CreditsPerUSD).
		Scan(&o.TotalRuns, &o.Completed, &o.PartiallyCompleted, &o.Failed, &o.Cancelled,
			&o.ModelCalls, &o.InputTokens, &o.OutputTokens, &o.ModelChargeCredits, &o.SearchCalls, &o.SearchChargeCredits)
	if e != nil {
		return nil, e
	}
	o.ChargeCredits = o.ModelChargeCredits + o.SearchChargeCredits
	// Count the ready files visible in the admin file list. Storage also includes
	// earlier versions and snapshots, and excludes gallery objects held by reference.
	e = tx.QueryRowContext(ctx, `SELECT
    (SELECT COUNT(*) FROM office_resources r JOIN office_resource_versions v ON v.id=r.latest_version_id
      JOIN users u ON u.id=r.user_id WHERE r.source IN ('upload','generated')
      AND r.deleted_at IS NULL AND v.state='ready' AND u.deleted_at IS NULL),
    (SELECT COALESCE(SUM(v.byte_size+v.preview_bytes),0) FROM office_resource_versions v
      JOIN office_resources r ON r.id=v.resource_id JOIN users u ON u.id=r.user_id
      WHERE r.source<>'gallery' AND u.deleted_at IS NULL) +
    (SELECT COALESCE(SUM(s.byte_size),0) FROM office_workspace_snapshots s JOIN users u ON u.id=s.user_id
      WHERE u.deleted_at IS NULL)`).Scan(&o.ResourceCount, &o.UsedBytes)
	if e != nil {
		return nil, e
	}
	return o, tx.Commit()
}

// officeAdminRunJSON excludes credentials and implementation-only dispatch fields.
// The model is always the public model on the historical run; usage comes only
// from exact existing ledger correlations, never from a newly calculated price.
const officeAdminRunJSON = `(to_jsonb(r)-'lease_token'-'lease_owner'-'lease_until'-'api_key_id'-'idempotency_key'-'request_hash'-'limits_json'-'resume_value') || jsonb_build_object('limits',r.limits_json)`

func validOfficeAdminPage(f service.OfficeAdminFilter) bool {
	return f.Page >= 1 && f.Page <= 10000 && f.PageSize >= 1 && f.PageSize <= 100 && f.Days >= 0 && f.Days <= 90 && f.UserID >= 0
}

func (r *officeAgentRepository) AdminRuns(ctx context.Context, f service.OfficeAdminFilter) (*service.OfficeAdminRunPage, error) {
	if !validOfficeAdminPage(f) || (f.Execution != "" && f.Execution != "browser" && f.Execution != "historical") {
		return nil, service.ErrOfficeInvalid
	}
	p := &service.OfficeAdminRunPage{Items: []service.OfficeAdminRun{}, Page: f.Page, PageSize: f.PageSize}
	var since any
	if f.Days > 0 {
		since = time.Now().AddDate(0, 0, -f.Days)
	}
	// One MVCC snapshot covers count, page and ledger totals, including an empty
	// or out-of-range page. Aggregate only operations on the bounded page.
	const query = `WITH filtered AS MATERIALIZED (
 SELECT r.id,r.created_at FROM office_agent_runs r JOIN users u ON u.id=r.user_id
 WHERE u.deleted_at IS NULL AND ($1::bigint=0 OR r.user_id=$1) AND ($2::text='' OR r.mode=$2)
 AND ($3::text='' OR r.status=$3) AND ($4::text='' OR r.model=$4) AND ($5::timestamptz IS NULL OR r.created_at>=$5)
 AND ($8::text='' OR r.browser_driven=($8='browser'))
), page AS MATERIALIZED (
 SELECT r.*,u.email AS user_email,u.username AS user_name
 FROM (SELECT id FROM filtered ORDER BY created_at DESC,id DESC LIMIT $6 OFFSET $7) selected
 JOIN office_agent_runs r ON r.id=selected.id JOIN users u ON u.id=r.user_id
),
 usage AS (SELECT op.run_id,COALESCE(SUM(u.input_tokens),0) AS input_tokens,COALESCE(SUM(u.output_tokens),0) AS output_tokens,
 COALESCE(SUM(u.charge_credits),0) AS charge_credits
 FROM page r JOIN office_agent_operations op ON op.run_id=r.id AND op.user_id=r.user_id
 JOIN usage_logs u ON u.request_id='client:'||op.request_id AND u.user_id=r.user_id AND u.api_key_id=r.api_key_id GROUP BY op.run_id)
 SELECT totals.total,COALESCE(jsonb_agg((` + officeAdminRunJSON + `) || jsonb_build_object(
 'input_tokens',COALESCE(usage.input_tokens,0),'output_tokens',COALESCE(usage.output_tokens,0),'charge_credits',COALESCE(usage.charge_credits,0))
 ORDER BY r.created_at DESC,r.id DESC) FILTER(WHERE r.id IS NOT NULL),'[]'::jsonb)
 FROM (SELECT COUNT(*) AS total FROM filtered) totals LEFT JOIN page r ON TRUE LEFT JOIN usage ON usage.run_id=r.id GROUP BY totals.total`
	var raw []byte
	if err := r.db.QueryRowContext(ctx, query, f.UserID, f.Mode, f.Status, f.Model, since, f.PageSize, (f.Page-1)*f.PageSize, f.Execution).Scan(&p.Total, &raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &p.Items); err != nil {
		return nil, fmt.Errorf("decode office run page: %w", err)
	}
	p.Pages = (p.Total + int64(f.PageSize) - 1) / int64(f.PageSize)
	return p, nil
}

func (r *officeAgentRepository) AdminRun(ctx context.Context, id string) (*service.OfficeAdminRunDetail, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeNotFound
	}
	d := &service.OfficeAdminRunDetail{}
	var raw []byte
	var key int64
	err := r.db.QueryRowContext(ctx, `SELECT `+officeAdminRunJSON+`,r.api_key_id,u.email,u.username FROM office_agent_runs r JOIN users u ON u.id=r.user_id WHERE r.id=$1 AND u.deleted_at IS NULL`, id).Scan(&raw, &key, &d.UserEmail, &d.UserName)
	if err != nil {
		return nil, officeDBError(err)
	}
	if err = json.Unmarshal(raw, &d.Run); err != nil {
		return nil, err
	}
	d.Run.APIKeyID = key
	d.Operations, err = r.runUsage(ctx, d.Run, true)
	if err != nil {
		return nil, err
	}
	tools, err := r.WorkbenchToolOperations(ctx, d.Run.UserID, d.Run.ID)
	if err != nil {
		return nil, err
	}
	summaries := make(map[string]service.WorkbenchToolSummary, len(tools))
	for _, operation := range tools {
		summaries[operation.ID] = service.WorkbenchToolOperationSummary(d.Run, operation)
	}
	for i := range d.Operations {
		if summary, ok := summaries[d.Operations[i].ID]; ok {
			d.Operations[i].Tool = &summary
		}
	}
	return d, nil
}

func (r *officeAgentRepository) AdminRunMessages(ctx context.Context, id string, before int64) (*service.OfficeAdminRunMessages, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeNotFound
	}
	// Resolve ownership, check deletion and read the page in the same snapshot.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	var conversationID string
	page := &service.OfficeAdminRunMessages{}
	err = tx.QueryRowContext(ctx, `
SELECT r.user_id,r.conversation_id::text,c.title
FROM office_agent_runs r
JOIN users u ON u.id=r.user_id AND u.deleted_at IS NULL
JOIN office_agent_conversations c ON c.id=r.conversation_id AND c.user_id=r.user_id AND c.deleted_at IS NULL
WHERE r.id=$1`, id).Scan(&userID, &conversationID, &page.ConversationTitle)
	if err != nil {
		return nil, officeDBError(err)
	}
	page.Messages, page.Runs, page.HasMore, page.NextBefore, err = officeMessagePage(ctx, tx, userID, conversationID, before)
	if err != nil {
		return nil, err
	}
	for i := range page.Runs {
		page.Runs[i].ResumeValue = nil
	}

	selected, err := scanOfficeMessage(tx.QueryRowContext(ctx, `SELECT `+officeMessageColumns+`
FROM office_agent_messages WHERE user_id=$1 AND conversation_id=$2 AND run_id=$3 AND role='user'`, userID, conversationID, id))
	if err == nil {
		page.SelectedUserMessage = &selected
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return page, nil
}

func (r *officeAgentRepository) RunUsage(ctx context.Context, user int64, id string, admin bool) ([]service.OfficeCallDetail, error) {
	run, err := r.GetRun(ctx, user, id)
	if err != nil {
		return nil, err
	}
	return r.runUsage(ctx, *run, admin)
}

func (r *officeAgentRepository) runUsage(ctx context.Context, run service.OfficeRun, admin bool) ([]service.OfficeCallDetail, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT op.operation_id,op.kind,op.state,op.request_id,op.created_at,op.dispatched_at,op.completed_at,
 COUNT(u.id)>0,COALESCE(SUM(u.input_tokens),0),COALESCE(SUM(u.output_tokens),0),COALESCE(SUM(u.cache_read_tokens),0),COALESCE(SUM(u.actual_cost),0),COALESCE(SUM(u.charge_credits),0),MAX(u.duration_ms),
 CASE WHEN $4 THEN COALESCE(jsonb_agg(jsonb_build_object('account_id',u.account_id,'account_name',COALESCE(a.name,''),'upstream_model',u.upstream_model,'outcome','usage','recorded_at',u.created_at) ORDER BY u.created_at,u.id) FILTER(WHERE u.account_id IS NOT NULL),'[]'::jsonb) ELSE '[]'::jsonb END
 FROM office_agent_operations op LEFT JOIN usage_logs u ON u.request_id='client:'||op.request_id AND u.user_id=op.user_id AND u.api_key_id=$3
 LEFT JOIN accounts a ON $4 AND a.id=u.account_id WHERE op.user_id=$1 AND op.run_id=$2
 GROUP BY op.run_id,op.operation_id ORDER BY op.created_at,op.operation_id LIMIT 200`, run.UserID, run.ID, run.APIKeyID, admin)
	if err != nil {
		return nil, err
	}
	result := []service.OfficeCallDetail{}
	byRequest := map[string]int{}
	for rows.Next() {
		var op service.OfficeCallDetail
		var upstream []byte
		op.RunID = run.ID
		if err = rows.Scan(&op.ID, &op.Kind, &op.State, &op.RequestID, &op.CreatedAt, &op.DispatchedAt, &op.CompletedAt, &op.Usage.Settled, &op.Usage.InputTokens, &op.Usage.OutputTokens, &op.Usage.CacheReadTokens, &op.Usage.ActualCost, &op.Usage.ChargeCredits, &op.Usage.DurationMS, &upstream); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if admin {
			if err = json.Unmarshal(upstream, &op.Usage.Upstreams); err != nil {
				_ = rows.Close()
				return nil, err
			}
		}
		byRequest[op.RequestID] = len(result)
		result = append(result, op)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil || !admin || len(result) == 0 {
		return result, err
	}
	// At most 200 operations * 30 observed upstream attempts, in a single
	// bounded batch query. Correlation must match request + user + API key.
	rows, err = r.db.QueryContext(ctx, `WITH selected_ops AS MATERIALIZED (
 SELECT request_id FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 ORDER BY created_at,operation_id LIMIT 200
), observed AS (
 SELECT op.request_id,e.id AS error_id,e.created_at,ev.ordinality,
 COALESCE(CASE WHEN ev.value->>'account_id' ~ '^[1-9][0-9]{0,17}$' THEN (ev.value->>'account_id')::bigint END,e.account_id) AS account_id,
 NULLIF(ev.value->>'account_name','') AS account_name,
 NULLIF(COALESCE(ev.value->>'upstream_model',e.upstream_model),'') AS upstream_model,
 NULLIF(COALESCE(CASE WHEN ev.value->>'upstream_status_code' ~ '^[1-5][0-9]{2}$' THEN (ev.value->>'upstream_status_code')::int END,e.upstream_status_code),0) AS status_code
 FROM selected_ops op JOIN ops_error_logs e ON e.client_request_id=op.request_id AND e.user_id=$1 AND e.api_key_id=$3
 LEFT JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(e.upstream_errors)='array' THEN e.upstream_errors ELSE '[{}]'::jsonb END) WITH ORDINALITY ev(value,ordinality) ON TRUE
 WHERE COALESCE(ev.value->>'stage','') NOT IN ('account_auth','auth','routing')
), dedup AS (
 SELECT DISTINCT request_id,account_id,account_name,upstream_model,status_code,created_at FROM observed WHERE account_id>0
), ranked AS (
 SELECT *,ROW_NUMBER() OVER(PARTITION BY request_id ORDER BY created_at,account_id,upstream_model,status_code) AS ordinal FROM dedup
)
 SELECT ranked.request_id,ranked.account_id,COALESCE(ranked.account_name,a.name,''),ranked.upstream_model,ranked.status_code,ranked.created_at
 FROM ranked LEFT JOIN accounts a ON a.id=ranked.account_id WHERE ordinal<=30 ORDER BY ranked.request_id,ordinal`, run.UserID, run.ID, run.APIKeyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var request string
		u := service.WorkbenchTaskUpstream{Outcome: "failed"}
		if err = rows.Scan(&request, &u.AccountID, &u.AccountName, &u.UpstreamModel, &u.StatusCode, &u.RecordedAt); err != nil {
			return nil, err
		}
		if index, ok := byRequest[request]; ok {
			result[index].Usage.Upstreams = append(result[index].Usage.Upstreams, u)
		}
	}
	return result, rows.Err()
}
func (r *officeAgentRepository) AdminResources(ctx context.Context, f service.OfficeAdminFilter) (*service.OfficeAdminResourcePage, error) {
	if !validOfficeAdminPage(f) {
		return nil, service.ErrOfficeInvalid
	}
	p := &service.OfficeAdminResourcePage{Items: []service.OfficeAdminResource{}, Page: f.Page, PageSize: f.PageSize}
	const where = ` FROM office_resources r
		JOIN office_resource_versions v ON v.id=r.latest_version_id
		JOIN users u ON u.id=r.user_id
		WHERE r.source IN ('upload','generated') AND r.deleted_at IS NULL AND v.state='ready'
		AND u.deleted_at IS NULL AND ($1::bigint=0 OR r.user_id=$1)`
	if e := r.db.QueryRowContext(ctx, `SELECT COUNT(*)`+where, f.UserID).Scan(&p.Total); e != nil {
		return nil, e
	}
	p.Pages = (p.Total + int64(f.PageSize) - 1) / int64(f.PageSize)
	rows, e := r.db.QueryContext(ctx, `SELECT r.id::text,v.id::text,COALESCE(v.parent_version_id::text,''),v.name,v.mime,v.byte_size,v.sha256,v.preview_kind,r.source,v.created_at,r.user_id,u.email,u.username`+where+` ORDER BY r.created_at DESC,r.id DESC LIMIT $2 OFFSET $3`, f.UserID, f.PageSize, (f.Page-1)*f.PageSize)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v service.OfficeAdminResource
		if e = rows.Scan(&v.ID, &v.VersionID, &v.ParentVersionID, &v.Name, &v.MIME, &v.Bytes, &v.SHA256, &v.PreviewKind, &v.Source, &v.CreatedAt, &v.OwnerID, &v.UserEmail, &v.UserName); e != nil {
			return nil, e
		}
		p.Items = append(p.Items, v)
	}
	return p, rows.Err()
}
func (r *officeAgentRepository) AdminResourceOwner(ctx context.Context, version string) (int64, error) {
	if !officeUUID(version) {
		return 0, service.ErrOfficeNotFound
	}
	var user int64
	e := r.db.QueryRowContext(ctx, `SELECT v.user_id FROM office_resource_versions v
		JOIN office_resources r ON r.id=v.resource_id JOIN users u ON u.id=v.user_id
		WHERE v.id=$1 AND v.state='ready'
		AND r.deleted_at IS NULL AND u.deleted_at IS NULL`, version).Scan(&user)
	return user, officeDBError(e)
}
