ALTER TABLE office_agent_runs
    ADD COLUMN IF NOT EXISTS replay_pinned BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS replay_checkpoint_id UUID,
    ADD COLUMN IF NOT EXISTS replay_native_from BIGINT NOT NULL DEFAULT 0 CHECK (replay_native_from >= 0),
    ADD COLUMN IF NOT EXISTS replay_invalidated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS duplicate_payloads_compacted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS office_agent_replay_checkpoints (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL,
    conversation_id UUID NOT NULL,
    before_sequence BIGINT NOT NULL CHECK (before_sequence > 0),
    content JSONB NOT NULL CHECK (octet_length(content::text) <= 131072),
    published BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id, conversation_id)
        REFERENCES office_agent_conversations(user_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_office_agent_replay_published
    ON office_agent_replay_checkpoints(conversation_id) WHERE published;
