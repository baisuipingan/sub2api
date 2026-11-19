-- Promote the closed office pilot to a mode-aware, persistent workbench.
ALTER TABLE office_agent_conversations ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS mode VARCHAR(16) NOT NULL DEFAULT 'office' CHECK (mode IN ('chat','office'));
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS search_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS parameters JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (octet_length(parameters::text)<=4096);
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS context_boundary BIGINT NOT NULL DEFAULT 0;
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS regeneration_of UUID;
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS selected BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS resume_value JSONB;
ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS input_request_id VARCHAR(128);
DROP INDEX IF EXISTS idx_office_agent_user_active_run;
CREATE INDEX IF NOT EXISTS idx_office_agent_runs_mode_queue ON office_agent_runs(mode,created_at,id) WHERE status IN ('queued','running','recovering');
CREATE SCHEMA IF NOT EXISTS office_checkpoints;
CREATE TABLE IF NOT EXISTS office_checkpoints.snapshots (
    run_id UUID PRIMARY KEY REFERENCES office_agent_runs(id) ON DELETE CASCADE,
    epoch BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    write_id VARCHAR(128) NOT NULL,
    envelope JSONB NOT NULL CHECK (octet_length(envelope::text)<=2097152),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS office_agent_user_inputs (
    run_id UUID NOT NULL REFERENCES office_agent_runs(id) ON DELETE CASCADE,
    request_id VARCHAR(128) NOT NULL,
    value JSONB NOT NULL CHECK (octet_length(value::text)<=32768),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(run_id,request_id)
);
CREATE TABLE IF NOT EXISTS office_agent_context_summaries (
 conversation_id UUID PRIMARY KEY REFERENCES office_agent_conversations(id) ON DELETE CASCADE,
 user_id BIGINT NOT NULL REFERENCES users(id),
 version BIGINT NOT NULL,
 operation_id VARCHAR(128) NOT NULL,
 content TEXT NOT NULL CHECK(octet_length(content)<=16384),
 through_message_id UUID NOT NULL REFERENCES office_agent_messages(id),
 source_hash CHAR(64) NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE office_agent_operations DROP CONSTRAINT IF EXISTS office_agent_operations_input_json_check;
ALTER TABLE office_agent_operations ADD CONSTRAINT office_agent_operations_input_json_check CHECK(octet_length(input_json::text)<=1179648);
CREATE TABLE IF NOT EXISTS office_agent_workers (
 id VARCHAR(80) PRIMARY KEY,
 preset VARCHAR(16) NOT NULL CHECK(preset IN ('chat','office')),
 last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 executor_ready BOOLEAN
);

ALTER TABLE office_agent_runs ADD COLUMN paused_at TIMESTAMPTZ;
ALTER TABLE office_agent_runs ADD COLUMN paused_seconds BIGINT NOT NULL DEFAULT 0 CHECK(paused_seconds>=0);
