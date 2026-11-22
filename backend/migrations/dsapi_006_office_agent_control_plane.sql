-- Phase-0 office Agent control plane. This migration is additive; enabling the
-- module is a separate application setting. No image/key/usage tables change.
CREATE TABLE IF NOT EXISTS office_agent_conversations (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(200) NOT NULL DEFAULT '',
    next_message_seq BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, id)
);
CREATE INDEX IF NOT EXISTS idx_office_agent_conversations_user
    ON office_agent_conversations(user_id, updated_at DESC, id);

CREATE TABLE IF NOT EXISTS office_agent_runs (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL,
    conversation_id UUID NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    model VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued' CHECK (status IN
        ('queued','running','waiting_user','recovering','needs_confirmation',
         'completed','partially_completed','failed','cancelled')),
    limits_json JSONB NOT NULL,
    input_resources JSONB NOT NULL DEFAULT '[]'::jsonb,
    model_calls INTEGER NOT NULL DEFAULT 0 CHECK (model_calls >= 0),
    tool_calls INTEGER NOT NULL DEFAULT 0 CHECK (tool_calls >= 0),
    next_event_seq BIGINT NOT NULL DEFAULT 0,
    event_bytes BIGINT NOT NULL DEFAULT 0 CHECK (event_bytes >= 0),
    lease_owner VARCHAR(128),
    lease_token UUID,
    lease_epoch BIGINT NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    UNIQUE (user_id, id),
    UNIQUE (user_id, idempotency_key),
    FOREIGN KEY (user_id, conversation_id)
        REFERENCES office_agent_conversations(user_id, id) ON DELETE CASCADE,
    CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    CHECK (octet_length(limits_json::text) <= 2048),
    CHECK (jsonb_typeof(input_resources) = 'array' AND octet_length(input_resources::text) <= 32768)
);
-- Phase 0 deliberately admits only one unfinished run per user. Waiting and
-- uncertain runs remain active until explicitly cancelled or resolved.
CREATE UNIQUE INDEX IF NOT EXISTS idx_office_agent_user_active_run ON office_agent_runs(user_id)
    WHERE status IN ('queued','running','waiting_user','recovering','needs_confirmation');
CREATE UNIQUE INDEX IF NOT EXISTS idx_office_agent_conversation_active_run ON office_agent_runs(conversation_id)
    WHERE status IN ('queued','running','waiting_user','recovering','needs_confirmation');
CREATE INDEX IF NOT EXISTS idx_office_agent_runs_claim ON office_agent_runs(created_at, id)
    WHERE status IN ('queued','running','recovering');
CREATE INDEX IF NOT EXISTS idx_office_agent_runs_conversation
    ON office_agent_runs(user_id, conversation_id, created_at DESC);

CREATE TABLE IF NOT EXISTS office_agent_messages (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL,
    conversation_id UUID NOT NULL,
    run_id UUID NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    role VARCHAR(16) NOT NULL CHECK (role IN ('user','assistant')),
    content JSONB NOT NULL CHECK (octet_length(content::text) <= 2097152),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (conversation_id, sequence),
    UNIQUE (run_id, role),
    FOREIGN KEY (user_id, conversation_id)
        REFERENCES office_agent_conversations(user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, run_id) REFERENCES office_agent_runs(user_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS office_agent_run_outbox (
    run_id UUID PRIMARY KEY REFERENCES office_agent_runs(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS office_agent_operations (
    user_id BIGINT NOT NULL,
    run_id UUID NOT NULL,
    operation_id VARCHAR(128) NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('model','tool')),
    state VARCHAR(16) NOT NULL DEFAULT 'prepared'
        CHECK (state IN ('prepared','dispatched','completed','unknown')),
    input_hash CHAR(64) NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    input_json JSONB NOT NULL CHECK (octet_length(input_json::text) <= 262144),
    request_id VARCHAR(64) NOT NULL,
    dispatch_epoch BIGINT,
    dispatch_token UUID,
    result_json JSONB CHECK (octet_length(result_json::text) <= 2097152),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dispatched_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, operation_id),
    UNIQUE (request_id),
    FOREIGN KEY (user_id, run_id) REFERENCES office_agent_runs(user_id, id) ON DELETE CASCADE,
    CHECK (operation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$')
);

CREATE TABLE IF NOT EXISTS office_agent_run_events (
    user_id BIGINT NOT NULL,
    run_id UUID NOT NULL,
    event_id VARCHAR(128) NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    event_type VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL CHECK (octet_length(payload::text) <= 65536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (run_id, sequence),
    UNIQUE (run_id, event_id),
    FOREIGN KEY (user_id, run_id) REFERENCES office_agent_runs(user_id, id) ON DELETE CASCADE
);
