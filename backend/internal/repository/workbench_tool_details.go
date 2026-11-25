package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *officeAgentRepository) AdminToolRun(ctx context.Context, runID string) (*service.OfficeRun, error) {
	if !officeUUID(runID) {
		return nil, service.ErrOfficeNotFound
	}
	run := &service.OfficeRun{ID: runID}
	err := r.db.QueryRowContext(ctx, `SELECT r.user_id,r.status FROM office_agent_runs r
		JOIN users u ON u.id=r.user_id WHERE r.id=$1 AND u.deleted_at IS NULL`, runID).Scan(&run.UserID, &run.Status)
	if err != nil {
		return nil, officeDBError(err)
	}
	return run, nil
}

func (r *officeAgentRepository) WorkbenchToolOperations(ctx context.Context, user int64, runID string) ([]service.OfficeOperation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT operation_id,run_id::text,kind,state,input_hash,
		CASE WHEN tool_projection IS NULL THEN jsonb_build_object('tool',input_json->'tool') || CASE input_json->>'tool'
			WHEN 'web_search' THEN jsonb_build_object('query',input_json->'query',
				'arguments',jsonb_build_object('query',input_json#>'{arguments,query}'))
			WHEN 'web_read' THEN jsonb_build_object('url',input_json->'url',
				'arguments',jsonb_build_object('url',input_json#>'{arguments,url}'))
			ELSE '{}'::jsonb END ELSE '{}'::jsonb END,request_id,
		CASE WHEN tool_projection IS NULL AND result_json IS NOT NULL THEN jsonb_build_object('tool_status',result_json->'tool_status',
			'error_code',result_json->'error_code','output',jsonb_build_object(
				'status',result_json->'output'->'status','error',result_json->'output'->'error',
				'processed',result_json->'output'->'processed','succeeded',result_json->'output'->'succeeded',
				'failed',result_json->'output'->'failed',
				'url',CASE WHEN input_json->>'tool'='web_read' THEN result_json->'output'->'url' END)) END,
		COALESCE(dispatch_epoch,0),COALESCE(dispatch_token::text,''),created_at,dispatched_at,completed_at,'',
		CASE WHEN tool_projection IS NOT NULL THEN jsonb_build_object('detail',jsonb_build_object(
			'name',tool_projection->'detail'->'name','state',tool_projection->'detail'->'state',
			'query',tool_projection->'detail'->'query','source',tool_projection->'detail'->'source')) END
		FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 AND kind='tool'
		ORDER BY created_at,operation_id LIMIT 200`, user, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	operations := make([]service.OfficeOperation, 0)
	for rows.Next() {
		operation, err := scanOfficeOperation(rows)
		if err != nil {
			return nil, err
		}
		operations = append(operations, *operation)
	}
	return operations, rows.Err()
}

func (r *officeAgentRepository) WorkbenchToolOperation(ctx context.Context, user int64, runID, operationID string) (*service.OfficeOperation, error) {
	if !officeUUID(runID) || !officeToken.MatchString(operationID) {
		return nil, service.ErrOfficeNotFound
	}
	return scanOfficeOperation(r.db.QueryRowContext(ctx, `SELECT operation_id,run_id::text,kind,state,input_hash,
		CASE WHEN tool_projection IS NULL THEN input_json ELSE '{}'::jsonb END,request_id,
		CASE WHEN tool_projection IS NULL THEN result_json END,
		COALESCE(dispatch_epoch,0),COALESCE(dispatch_token::text,''),created_at,dispatched_at,completed_at,'',tool_projection
		FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 AND operation_id=$3 AND kind='tool'`,
		user, runID, operationID))
}
