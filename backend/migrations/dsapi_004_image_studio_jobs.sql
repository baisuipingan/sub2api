-- Durable execution state for Image Studio.  Image bytes are never stored in
-- PostgreSQL: inputs and outputs reference private gallery assets; a bounded
-- local spool covers the short window between a paid gateway response and R2.
CREATE TABLE IF NOT EXISTS image_studio_jobs (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id VARCHAR(128) NOT NULL,
    -- Historical billing attribution survives API-key rotation or hard delete.
    -- The worker revalidates this ID through the normal gateway before dispatch.
    api_key_id BIGINT NOT NULL,
    request_hash CHAR(64) NOT NULL,
    task_json JSONB NOT NULL,
    request_json JSONB NOT NULL,
    protocol VARCHAR(16) NOT NULL CHECK (protocol IN ('openai', 'grok', 'gemini')),
    model VARCHAR(128) NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'generating', 'saving', 'completed', 'partial', 'failed', 'unknown', 'cancelled')),
    storage_endpoint TEXT NOT NULL,
    storage_region TEXT NOT NULL,
    storage_bucket TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, task_id)
);
CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_user_page
    ON image_studio_jobs(user_id, created_at DESC, task_id DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_target_active
    ON image_studio_jobs(storage_endpoint, storage_region, storage_bucket)
    WHERE status IN ('queued', 'generating', 'saving') AND deleted_at IS NULL;

-- A child is one paid gateway call.  Most providers use n=1 per child;
-- Grok's native n request uses one child whose output may contain many images.
CREATE TABLE IF NOT EXISTS image_studio_job_calls (
    user_id BIGINT NOT NULL,
    task_id VARCHAR(128) NOT NULL,
    ordinal SMALLINT NOT NULL CHECK (ordinal >= 0 AND ordinal < 10),
    status VARCHAR(24) NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'generating', 'saving', 'completed', 'failed', 'unknown', 'cancelled')),
    attempt INTEGER NOT NULL DEFAULT 0,
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    spool_path TEXT,
    spool_owner UUID,
    output_image_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_image_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_code VARCHAR(64),
    error_message TEXT,
    billing_state VARCHAR(24) NOT NULL DEFAULT 'not_started'
        CHECK (billing_state IN ('not_started', 'pending', 'recorded', 'unknown')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, task_id, ordinal),
    FOREIGN KEY (user_id, task_id)
        REFERENCES image_studio_jobs(user_id, task_id) ON DELETE CASCADE,
    CHECK (spool_path IS NULL OR length(spool_path) <= 512)
);
CREATE INDEX IF NOT EXISTS idx_image_studio_job_calls_claim
    ON image_studio_job_calls(status, lease_until, updated_at)
    WHERE status IN ('queued', 'saving', 'generating');

-- Inputs remain referenced until a job becomes terminal or is tombstoned.
-- Gallery orphan cleanup must consult this table as well as archived links.
CREATE TABLE IF NOT EXISTS image_studio_job_inputs (
    user_id BIGINT NOT NULL,
    task_id VARCHAR(128) NOT NULL,
    image_id VARCHAR(128) NOT NULL,
    role VARCHAR(32) NOT NULL CHECK (role IN ('input', 'request_input', 'mask', 'mask_target')),
    ordinal SMALLINT NOT NULL CHECK (ordinal >= 0 AND ordinal < 20),
    PRIMARY KEY (user_id, task_id, role, ordinal),
    FOREIGN KEY (user_id, task_id)
        REFERENCES image_studio_jobs(user_id, task_id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, image_id)
        REFERENCES image_studio_gallery_assets(user_id, image_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_image_studio_job_inputs_asset
    ON image_studio_job_inputs(user_id, image_id);
