package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type studioJobRepository struct{ db *sql.DB }

func NewStudioJobRepository(db *sql.DB) service.StudioJobRepository {
	return &studioJobRepository{db: db}
}

func (r *studioJobRepository) Create(ctx context.Context, in service.StudioJobCreate) (*service.StudioJob, error) {
	request, err := json.Marshal(in.Spec)
	if err != nil {
		return nil, service.ErrStudioJobInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := galleryLockUser(ctx, tx, in.UserID); err != nil {
		return nil, err
	}
	var userDeleted sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM users WHERE id=$1 FOR SHARE`, in.UserID).Scan(&userDeleted); err != nil || userDeleted.Valid {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, service.ErrStudioJobInvalid
	}
	var existingDigest string
	err = tx.QueryRowContext(ctx, `SELECT request_hash FROM image_studio_jobs
        WHERE user_id=$1 AND task_id=$2`, in.UserID, in.TaskID).Scan(&existingDigest)
	if err == nil {
		if existingDigest != in.Digest {
			return nil, service.ErrStudioJobConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return r.Get(ctx, in.UserID, in.TaskID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var galleryTaskID string
	err = tx.QueryRowContext(ctx, `SELECT task_id FROM image_studio_gallery_tasks
        WHERE user_id=$1 AND task_id=$2`, in.UserID, in.TaskID).Scan(&galleryTaskID)
	if err == nil {
		return nil, service.ErrStudioJobConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var activeJobs, galleryTasks, assets int64
	var usedBytes, reservedAssets int64
	err = tx.QueryRowContext(ctx, `SELECT
        (SELECT COUNT(*) FROM image_studio_jobs WHERE user_id=$1 AND deleted_at IS NULL
          AND status IN ('queued','generating','saving')),
        (SELECT COUNT(*) FROM image_studio_gallery_tasks WHERE user_id=$1 AND deleted_at IS NULL),
        (SELECT COUNT(*) FROM image_studio_gallery_assets WHERE user_id=$1),
        (SELECT COALESCE(SUM(byte_size+preview_byte_size+thumbnail_byte_size),0)
          FROM image_studio_gallery_assets WHERE user_id=$1),
        (SELECT COALESCE(SUM((request_json->>'image_count')::bigint *
          CASE WHEN request_json->>'postprocess'='keyed' THEN 2 ELSE 1 END),0)
          FROM image_studio_jobs WHERE user_id=$1 AND deleted_at IS NULL
            AND status IN ('queued','generating','saving'))`, in.UserID).
		Scan(&activeJobs, &galleryTasks, &assets, &usedBytes, &reservedAssets)
	if err != nil {
		return nil, err
	}
	if activeJobs >= 10 {
		return nil, service.ErrStudioJobQueueFull
	}
	if galleryTasks+activeJobs >= galleryMaxUserTasks {
		return nil, service.ErrGalleryQuota
	}
	expectedAssets := int64(in.CallCount)
	if in.Spec.Protocol == "grok" {
		expectedAssets = int64(in.Spec.ImageCount)
	}
	if in.Spec.Postprocess == "keyed" {
		expectedAssets *= 2
	}
	if assets+reservedAssets+expectedAssets > galleryMaxUserAssets ||
		usedBytes+(reservedAssets+expectedAssets)*galleryReservedBytesPerJobAsset > galleryMaxUserBytes {
		return nil, service.ErrGalleryQuota
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO image_studio_jobs
        (user_id, task_id, api_key_id, request_hash, task_json, request_json,
         protocol, model, storage_endpoint, storage_region, storage_bucket)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
        ON CONFLICT (user_id,task_id) DO NOTHING`,
		in.UserID, in.TaskID, in.APIKeyID, in.Digest, in.Task, request,
		in.Spec.Protocol, in.Spec.Model, in.StorageEndpoint, in.StorageRegion, in.StorageBucket)
	if err != nil {
		return nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if inserted == 0 {
		var digest string
		err = tx.QueryRowContext(ctx, `SELECT request_hash FROM image_studio_jobs
            WHERE user_id=$1 AND task_id=$2 AND deleted_at IS NULL`, in.UserID, in.TaskID).Scan(&digest)
		if errors.Is(err, sql.ErrNoRows) || digest != in.Digest {
			return nil, service.ErrStudioJobConflict
		}
		if err != nil {
			return nil, err
		}
	} else {
		for ordinal := 0; ordinal < in.CallCount; ordinal++ {
			if _, err := tx.ExecContext(ctx, `INSERT INTO image_studio_job_calls
                (user_id,task_id,ordinal) VALUES ($1,$2,$3)`, in.UserID, in.TaskID, ordinal); err != nil {
				return nil, err
			}
		}
		for _, group := range []struct {
			role string
			ids  []string
		}{
			{"input", in.Spec.InputImageIDs},
			{"request_input", in.Spec.RequestInputImageIDs},
			{"mask", []string{in.Spec.MaskImageID}},
			{"mask_target", []string{in.Spec.MaskTargetImageID}},
		} {
			for ordinal, id := range group.ids {
				if id == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO image_studio_job_inputs
                    (user_id,task_id,image_id,role,ordinal) VALUES ($1,$2,$3,$4,$5)`,
					in.UserID, in.TaskID, id, group.role, ordinal); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, in.UserID, in.TaskID)
}

func (r *studioJobRepository) Get(ctx context.Context, userID int64, taskID string) (*service.StudioJob, error) {
	var job service.StudioJob
	var request []byte
	var task []byte
	err := r.db.QueryRowContext(ctx, `SELECT user_id,task_id,api_key_id,status,task_json,request_json,
        storage_endpoint,storage_region,storage_bucket,created_at,updated_at,deleted_at
        FROM image_studio_jobs WHERE user_id=$1 AND task_id=$2`,
		userID, taskID).Scan(&job.UserID, &job.TaskID, &job.APIKeyID, &job.Status, &task, &request,
		&job.StorageEndpoint, &job.StorageRegion, &job.StorageBucket,
		&job.CreatedAt, &job.UpdatedAt, &job.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrStudioJobNotFound
	}
	if err != nil {
		return nil, err
	}
	job.Task = json.RawMessage(task)
	if err := json.Unmarshal(request, &job.Spec); err != nil {
		return nil, fmt.Errorf("decode studio job spec: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT ordinal,status,attempt,output_image_ids,raw_image_ids,
        COALESCE(error_code,''),COALESCE(error_message,''),billing_state,
        COALESCE(lease_token::text,''),lease_until,sent_at,COALESCE(spool_path,''),COALESCE(spool_owner::text,''),updated_at
        FROM image_studio_job_calls WHERE user_id=$1 AND task_id=$2 ORDER BY ordinal`, userID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var call service.StudioJobCall
		var outputs, raws []byte
		var leaseUntil, sentAt sql.NullTime
		if err := rows.Scan(&call.Ordinal, &call.Status, &call.Attempt, &outputs, &raws,
			&call.ErrorCode, &call.ErrorMessage, &call.BillingState,
			&call.LeaseToken, &leaseUntil, &sentAt, &call.SpoolPath, &call.SpoolOwner, &call.UpdatedAt); err != nil {
			return nil, err
		}
		call.LeaseUntil = leaseUntil.Time
		if sentAt.Valid {
			call.SentAt = &sentAt.Time
		}
		if err := json.Unmarshal(outputs, &call.OutputImageIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raws, &call.RawImageIDs); err != nil {
			return nil, err
		}
		job.Calls = append(job.Calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if job.Status == service.StudioJobQueued || job.Status == service.StudioJobGenerating || job.Status == service.StudioJobSaving {
		active, pending := false, false
		for _, call := range job.Calls {
			if call.Status == service.StudioJobQueued || call.Status == service.StudioJobGenerating || call.Status == service.StudioJobSaving {
				active = true
			}
			if call.BillingState == "pending" {
				pending = true
			}
		}
		if !active && pending {
			job.Status = service.StudioJobSaving
		}
	}
	return &job, nil
}

type studioJobCursor struct {
	CreatedAt time.Time `json:"created_at"`
	TaskID    string    `json:"task_id"`
}

func (r *studioJobRepository) List(ctx context.Context, userID int64, cursor string, limit int, active bool) (service.StudioJobPage, error) {
	var before studioJobCursor
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(data) > 512 || json.Unmarshal(data, &before) != nil || before.CreatedAt.IsZero() || before.TaskID == "" {
			return service.StudioJobPage{}, service.ErrStudioJobInvalid
		}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT task_id,created_at FROM image_studio_jobs
        WHERE user_id=$1 AND deleted_at IS NULL
          AND ($2::timestamptz IS NULL OR (created_at,task_id)<($2, $3))
          AND (NOT $5 OR status IN ('queued','generating','saving','unknown'))
        ORDER BY created_at DESC,task_id DESC LIMIT $4`,
		userID, nullableTime(before.CreatedAt), before.TaskID, limit+1, active)
	if err != nil {
		return service.StudioJobPage{}, err
	}
	var ids []studioJobCursor
	for rows.Next() {
		var item studioJobCursor
		if err := rows.Scan(&item.TaskID, &item.CreatedAt); err != nil {
			_ = rows.Close()
			return service.StudioJobPage{}, err
		}
		ids = append(ids, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return service.StudioJobPage{}, err
	}
	page := service.StudioJobPage{Items: []service.StudioJob{}}
	if len(ids) > limit {
		last := ids[limit-1]
		encoded, _ := json.Marshal(last)
		value := base64.RawURLEncoding.EncodeToString(encoded)
		page.NextCursor = &value
		ids = ids[:limit]
	}
	for _, item := range ids {
		job, err := r.Get(ctx, userID, item.TaskID)
		if err != nil {
			return service.StudioJobPage{}, err
		}
		page.Items = append(page.Items, *job)
	}
	return page, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (r *studioJobRepository) Claim(ctx context.Context, spoolOwner string) (*service.StudioJobClaim, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var userID int64
	var taskID, status string
	var ordinal int
	err = tx.QueryRowContext(ctx, `SELECT c.user_id,c.task_id,c.ordinal,c.status
        FROM image_studio_job_calls c
        JOIN image_studio_jobs j ON j.user_id=c.user_id AND j.task_id=c.task_id
        JOIN users u ON u.id=j.user_id
        WHERE j.deleted_at IS NULL AND u.deleted_at IS NULL
          AND j.status IN ('queued','generating','saving')
          AND ((c.status='queued' AND NOT EXISTS (SELECT 1 FROM image_studio_job_calls stalled
                       WHERE stalled.status='saving' AND stalled.spool_owner::text=$1))
               OR (c.status='saving' AND c.spool_owner::text=$1
                                      AND (c.lease_until IS NULL OR c.lease_until<NOW())))
          AND (c.status='saving' OR NOT EXISTS (SELECT 1 FROM image_studio_job_calls busy
               WHERE busy.user_id=c.user_id AND busy.task_id=c.task_id
                 AND busy.status IN ('generating','saving')))
        ORDER BY CASE WHEN c.status='saving' THEN 0 ELSE 1 END,c.updated_at,c.ordinal
        FOR UPDATE OF j,c SKIP LOCKED LIMIT 1`, spoolOwner).Scan(&userID, &taskID, &ordinal, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	token := uuid.NewString()
	nextStatus := status
	if status == service.StudioJobQueued {
		nextStatus = service.StudioJobGenerating
	}
	_, err = tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET status=$4::text,
        lease_token=$5::uuid,lease_until=NOW()+INTERVAL '30 minutes',
        attempt=attempt+CASE WHEN $4::text='generating' THEN 1 ELSE 0 END,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3`,
		userID, taskID, ordinal, nextStatus, token)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE image_studio_jobs SET status=$3,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND status='queued'`, userID, taskID, nextStatus)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job, err := r.Get(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	return &service.StudioJobClaim{StudioJob: *job, Call: job.Calls[ordinal], SpoolOwner: spoolOwner}, nil
}

func (r *studioJobRepository) MarkDispatched(ctx context.Context, claim service.StudioJobClaim) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls
        SET sent_at=NOW(),updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
          AND status='generating' AND sent_at IS NULL
          AND EXISTS (SELECT 1 FROM users u WHERE u.id=$1 AND u.deleted_at IS NULL)`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) RecoverUnsent(ctx context.Context, claim service.StudioJobClaim) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls
        SET status='queued',lease_token=NULL,lease_until=NULL,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
          AND status='generating' AND sent_at IS NULL AND lease_until<NOW()`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) MarkSaving(ctx context.Context, claim service.StudioJobClaim, path, owner string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls SET
        status='saving',spool_path=$5,spool_owner=$6::uuid,lease_until=NOW()+INTERVAL '30 minutes',updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid AND status='generating'`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken, path, owner)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) RecoverSaving(ctx context.Context, claim service.StudioJobClaim, path, owner string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls SET
        status='saving',spool_path=$5,spool_owner=$6::uuid,lease_until=NOW()+INTERVAL '30 minutes',updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
          AND status='generating' AND lease_until<NOW()`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken, path, owner)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) ExtendLease(ctx context.Context, claim service.StudioJobClaim) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls
        SET lease_until=NOW()+INTERVAL '30 minutes',updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
          AND status IN ('generating','saving')`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) CancelClaim(ctx context.Context, claim service.StudioJobClaim) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls
        SET status='cancelled', billing_state=CASE WHEN sent_at IS NULL THEN billing_state ELSE 'unknown' END,
            lease_token=NULL,lease_until=NULL,spool_path=NULL,spool_owner=NULL,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
          AND status IN ('generating','saving')`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) CompleteCall(ctx context.Context, claim service.StudioJobClaim, outputs, raws []string, billingState string) error {
	outputJSON, _ := json.Marshal(outputs)
	rawJSON, _ := json.Marshal(raws)
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls SET
        status='completed',output_image_ids=$5,raw_image_ids=$6,billing_state=$7,
        lease_token=NULL,lease_until=NULL,spool_path=NULL,spool_owner=NULL,
        error_code=NULL,error_message=NULL,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid AND status='saving'`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken, outputJSON, rawJSON, billingState)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) FailCall(ctx context.Context, claim service.StudioJobClaim, code, message string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Use one explicit type for the error code: PostgreSQL otherwise infers
	// varchar from the assignment and text from LIKE and rejects this update.
	result, err := tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET
        status='failed',error_code=$5::text,error_message=$6,lease_token=NULL,lease_until=NULL,
        spool_path=NULL,spool_owner=NULL,
        billing_state=CASE WHEN sent_at IS NULL OR $7::boolean
          THEN 'not_started' ELSE 'pending' END,
        updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
          AND status IN ('generating','saving')`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken, code, truncateStudioError(message), claim.RejectedWithoutOutput || studioFailureIsNonBillable(code))
	if err := oneStudioJobRow(result, err); err != nil {
		return err
	}
	if code == "GALLERY_QUOTA_EXCEEDED" || code == "INVALID_IMAGE_RESULT" ||
		code == "MODERATION_BLOCKED" || code == "CONTENT_POLICY_VIOLATION" || code == "IMAGE_GENERATION_REJECTED" {
		if _, err := tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET status='cancelled',updated_at=NOW()
            WHERE user_id=$1 AND task_id=$2 AND status='queued'`, claim.UserID, claim.TaskID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func studioFailureIsNonBillable(code string) bool {
	if strings.HasPrefix(code, "GATEWAY_HTTP_4") {
		return true
	}
	// These codes are emitted only for explicit business errors with no image
	// result. Image download/post-processing/storage failures remain billable.
	switch code {
	case "MODERATION_BLOCKED", "CONTENT_POLICY_VIOLATION", "IMAGE_GENERATION_REJECTED",
		"INSUFFICIENT_BALANCE", "UPSTREAM_INVALID_RESPONSE", "UPSTREAM_NO_IMAGE", "UPSTREAM_ERROR":
		return true
	default:
		return false
	}
}

func (r *studioJobRepository) RetrySaving(ctx context.Context, claim service.StudioJobClaim, delay time.Duration, code string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls SET
        lease_token=NULL,lease_until=NOW()+($5::bigint * INTERVAL '1 millisecond'),
        error_code=$6,error_message=NULL,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid AND status='saving'`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken, delay.Milliseconds(), code)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) Finalize(ctx context.Context, userID int64, taskID, status string, task json.RawMessage) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_jobs SET status=$3,task_json=$4,
        finished_at=GREATEST(created_at,COALESCE((SELECT MAX(c.updated_at)
          FROM image_studio_job_calls c WHERE c.user_id=$1 AND c.task_id=$2),created_at)),
        updated_at=NOW() WHERE user_id=$1 AND task_id=$2
        AND deleted_at IS NULL AND status NOT IN ('cancelled','completed','partial','failed','unknown')
        AND NOT EXISTS (SELECT 1 FROM image_studio_job_calls c WHERE c.user_id=$1 AND c.task_id=$2
          AND (c.status IN ('queued','generating','saving') OR c.billing_state='pending'))`,
		userID, taskID, status, task)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) Cancel(ctx context.Context, userID int64, taskID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := galleryLockUser(ctx, tx, userID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE image_studio_jobs SET deleted_at=COALESCE(deleted_at,NOW()),status='cancelled',
        updated_at=NOW() WHERE user_id=$1 AND task_id=$2`, userID, taskID)
	if err != nil {
		return err
	}
	_, _ = result.RowsAffected() // an unknown ID still creates a cancellation tombstone
	_, err = tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET status='cancelled',
        billing_state=CASE WHEN sent_at IS NULL THEN billing_state ELSE 'unknown' END,
        lease_token=NULL,lease_until=NULL,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND status IN ('queued','generating','saving')`, userID, taskID)
	if err != nil {
		return err
	}
	// A queued job has no gallery row yet. Reserve the same task ID as a
	// tombstone so an old browser's delayed outbox cannot resurrect it.
	_, err = tx.ExecContext(ctx, `INSERT INTO image_studio_gallery_tasks
        (user_id,task_id,task_json,sha256,deleted_at)
        VALUES ($1,$2,'{}'::jsonb,$3,NOW())
        ON CONFLICT (user_id,task_id) DO NOTHING`,
		userID, taskID, strings.Repeat("0", 64))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *studioJobRepository) ExpiredGenerating(ctx context.Context, limit int) ([]service.StudioJobClaim, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id,task_id,ordinal FROM image_studio_job_calls
        WHERE status='generating' AND lease_until<NOW() ORDER BY lease_until LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type key struct {
		userID  int64
		taskID  string
		ordinal int
	}
	var keys []key
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.userID, &item.taskID, &item.ordinal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	claims := make([]service.StudioJobClaim, 0, len(keys))
	for _, key := range keys {
		job, err := r.Get(ctx, key.userID, key.taskID)
		if err != nil {
			return nil, err
		}
		claims = append(claims, service.StudioJobClaim{StudioJob: *job, Call: job.Calls[key.ordinal]})
	}
	return claims, nil
}

func (r *studioJobRepository) ExpiredSavingOtherOwner(ctx context.Context, owner string, limit int) ([]service.StudioJobClaim, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.user_id,c.task_id,c.ordinal
        FROM image_studio_job_calls c JOIN image_studio_jobs j
          ON j.user_id=c.user_id AND j.task_id=c.task_id
        WHERE j.deleted_at IS NULL AND c.status='saving'
          AND c.spool_owner::text<>$1 AND c.lease_until<NOW()-INTERVAL '1 hour'
        ORDER BY c.lease_until LIMIT $2`, owner, limit)
	if err != nil {
		return nil, err
	}
	type key struct {
		userID  int64
		taskID  string
		ordinal int
	}
	var keys []key
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.userID, &item.taskID, &item.ordinal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]service.StudioJobClaim, 0, len(keys))
	for _, item := range keys {
		job, err := r.Get(ctx, item.userID, item.taskID)
		if err != nil {
			return nil, err
		}
		out = append(out, service.StudioJobClaim{StudioJob: *job, Call: job.Calls[item.ordinal]})
	}
	return out, nil
}

func (r *studioJobRepository) MarkSavingLost(ctx context.Context, claim service.StudioJobClaim, expiredOnly bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := `UPDATE image_studio_job_calls SET status='unknown',billing_state='unknown',
        error_code='PAID_RESULT_LOST',error_message='paid result spool is unavailable; no automatic retry was made',
        lease_token=NULL,lease_until=NULL,spool_path=NULL,spool_owner=NULL,updated_at=NOW()
		WHERE user_id=$1 AND task_id=$2 AND ordinal=$3
          AND COALESCE(lease_token::text,'')=$4 AND status='saving'`
	if expiredOnly {
		query += " AND lease_until<NOW()-INTERVAL '1 hour'"
	}
	result, err := tx.ExecContext(ctx, query, claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken)
	if err := oneStudioJobRow(result, err); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET status='cancelled',updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND status='queued'`, claim.UserID, claim.TaskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *studioJobRepository) MarkUnknown(ctx context.Context, claim service.StudioJobClaim) error {
	return r.markUnknown(ctx, claim, true)
}

func (r *studioJobRepository) MarkUncertain(ctx context.Context, claim service.StudioJobClaim) error {
	return r.markUnknown(ctx, claim, false)
}

func (r *studioJobRepository) markUnknown(ctx context.Context, claim service.StudioJobClaim, expiredOnly bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := `UPDATE image_studio_job_calls SET status='unknown',
        billing_state='unknown',error_code='UPSTREAM_RESULT_UNKNOWN',
        error_message='upstream request may have been billed; it will not be retried automatically',
        lease_token=NULL,lease_until=NULL,updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3 AND lease_token=$4::uuid
		  AND status='generating'`
	if expiredOnly {
		query += " AND lease_until<NOW()"
	}
	result, err := tx.ExecContext(ctx, query,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, claim.Call.LeaseToken)
	if err := oneStudioJobRow(result, err); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET status='cancelled',updated_at=NOW()
        WHERE user_id=$1 AND task_id=$2 AND status='queued'`,
		claim.UserID, claim.TaskID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// FinalizationCandidates finds parents whose children have all reached a
// terminal state. A crash between gallery SaveTask and Finalize is recoverable.
func (r *studioJobRepository) FinalizationCandidates(ctx context.Context, limit int) ([]service.StudioJob, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT j.user_id,j.task_id FROM image_studio_jobs j
        WHERE j.deleted_at IS NULL AND j.status IN ('queued','generating','saving')
          AND NOT EXISTS (SELECT 1 FROM image_studio_job_calls c
            WHERE c.user_id=j.user_id AND c.task_id=j.task_id
              AND c.status IN ('queued','generating','saving'))
          AND NOT EXISTS (SELECT 1 FROM image_studio_job_calls c
            WHERE c.user_id=j.user_id AND c.task_id=j.task_id AND c.billing_state='pending')
        ORDER BY j.updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type key struct {
		userID int64
		taskID string
	}
	var keys []key
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.userID, &item.taskID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]service.StudioJob, 0, len(keys))
	for _, item := range keys {
		job, err := r.Get(ctx, item.userID, item.taskID)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, nil
}

func (r *studioJobRepository) CleanupCandidates(ctx context.Context, limit int) ([]service.StudioJob, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT j.user_id,j.task_id FROM image_studio_jobs j
        WHERE j.status IN ('completed','partial','failed','unknown','cancelled')
          AND (EXISTS (SELECT 1 FROM image_studio_job_inputs i
            WHERE i.user_id=j.user_id AND i.task_id=j.task_id)
            OR (j.status='cancelled' AND EXISTS (SELECT 1 FROM image_studio_gallery_tasks t
              WHERE t.user_id=j.user_id AND t.task_id=j.task_id AND t.deleted_at IS NULL)))
        ORDER BY j.updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type key struct {
		userID int64
		taskID string
	}
	var keys []key
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.userID, &item.taskID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]service.StudioJob, 0, len(keys))
	for _, item := range keys {
		job, err := r.Get(ctx, item.userID, item.taskID)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, nil
}

func (r *studioJobRepository) HasActive(ctx context.Context) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM image_studio_jobs
        WHERE deleted_at IS NULL AND status IN ('queued','generating','saving'))`).Scan(&active)
	return active, err
}

func (r *studioJobRepository) BillingRecorded(ctx context.Context, requestID string, apiKeyID int64) (bool, error) {
	var recorded bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_billing_dedup
        WHERE request_id=$1 AND api_key_id=$2) OR EXISTS(SELECT 1 FROM usage_billing_dedup_archive
        WHERE request_id=$1 AND api_key_id=$2)`, requestID, apiKeyID).Scan(&recorded)
	return recorded, err
}

func (r *studioJobRepository) PendingBilling(ctx context.Context, limit int) ([]service.StudioJobClaim, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT user_id,task_id,ordinal FROM image_studio_job_calls
        WHERE status IN ('completed','failed') AND billing_state='pending'
        ORDER BY updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		userID  int64
		taskID  string
		ordinal int
	}
	var keys []row
	for rows.Next() {
		var key row
		if err := rows.Scan(&key.userID, &key.taskID, &key.ordinal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	claims := make([]service.StudioJobClaim, 0, len(keys))
	for _, key := range keys {
		job, err := r.Get(ctx, key.userID, key.taskID)
		if err != nil {
			return nil, err
		}
		if key.ordinal >= len(job.Calls) {
			return nil, service.ErrStudioJobUnavailable
		}
		claims = append(claims, service.StudioJobClaim{StudioJob: *job, Call: job.Calls[key.ordinal]})
	}
	return claims, nil
}

func (r *studioJobRepository) SetBillingState(ctx context.Context, claim service.StudioJobClaim, state string) error {
	if state != "recorded" && state != "unknown" {
		return service.ErrStudioJobInvalid
	}
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_job_calls
        SET billing_state=$4
        WHERE user_id=$1 AND task_id=$2 AND ordinal=$3
		  AND status IN ('completed','failed') AND billing_state='pending'`,
		claim.UserID, claim.TaskID, claim.Call.Ordinal, state)
	return oneStudioJobRow(result, err)
}

func (r *studioJobRepository) ReleaseInputs(ctx context.Context, userID int64, taskID string) ([]service.GalleryAsset, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM image_studio_jobs
        WHERE user_id=$1 AND task_id=$2 FOR UPDATE`, userID, taskID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrStudioJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != service.StudioJobCompleted && status != service.StudioJobPartial &&
		status != service.StudioJobFailed && status != service.StudioJobUnknown && status != service.StudioJobCancelled {
		return nil, service.ErrStudioJobInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT image_id FROM image_studio_job_inputs
        WHERE user_id=$1 AND task_id=$2`, userID, taskID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM image_studio_job_inputs
        WHERE user_id=$1 AND task_id=$2`, userID, taskID); err != nil {
		return nil, err
	}
	var assets []service.GalleryAsset
	for _, id := range ids {
		asset, err := scanGalleryAsset(tx.QueryRowContext(ctx, `UPDATE image_studio_gallery_assets a
            SET state='deleting',updated_at=NOW()
            WHERE user_id=$1 AND image_id=$2 AND state='ready'
              AND NOT EXISTS (SELECT 1 FROM image_studio_gallery_task_assets t
                  WHERE t.user_id=a.user_id AND t.image_id=a.image_id)
              AND NOT EXISTS (SELECT 1 FROM image_studio_job_inputs j
                  WHERE j.user_id=a.user_id AND j.image_id=a.image_id)
            RETURNING `+galleryAssetUpdatedColumns, userID, id))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		assets = append(assets, *asset)
	}
	return assets, tx.Commit()
}

func oneStudioJobRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return service.ErrStudioJobNotFound
	}
	return nil
}

func truncateStudioError(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) > 1000 {
		value = value[:1000]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
