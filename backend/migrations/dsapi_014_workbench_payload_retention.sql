ALTER TABLE office_agent_operations
    ADD COLUMN IF NOT EXISTS result_hash CHAR(64),
    ADD COLUMN IF NOT EXISTS tool_projection JSONB;

ALTER TABLE office_agent_runs
    ADD COLUMN IF NOT EXISTS payloads_compacted_at TIMESTAMPTZ;
