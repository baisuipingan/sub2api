-- Screenshot objects live in private S3/R2. Pending/deleting rows retain every
-- known object key across process failures so cleanup never needs ListObjects.
ALTER TABLE ticket_messages ADD COLUMN IF NOT EXISTS operation_id VARCHAR(128);
ALTER TABLE ticket_messages ADD COLUMN IF NOT EXISTS operation_digest CHAR(64);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ticket_messages_operation
    ON ticket_messages(operation_id) WHERE operation_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS ticket_message_attachments (
    id CHAR(64) PRIMARY KEY,
    -- Deliberately no cascading user FK: deleting a user must not discard the
    -- object keys before the cleaner has removed the private screenshots.
    owner_id BIGINT NOT NULL,
    message_id BIGINT REFERENCES ticket_messages(id) ON DELETE SET NULL,
    operation_id VARCHAR(128) NOT NULL,
    ordinal SMALLINT NOT NULL CHECK (ordinal >= 0 AND ordinal < 3),
    digest CHAR(64) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','ready','deleting')),
    mime VARCHAR(32) NOT NULL,
    bytes BIGINT NOT NULL CHECK (bytes > 0),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    thumbnail_mime VARCHAR(32) NOT NULL,
    thumbnail_bytes BIGINT NOT NULL CHECK (thumbnail_bytes > 0),
    thumbnail_width INTEGER NOT NULL CHECK (thumbnail_width > 0),
    thumbnail_height INTEGER NOT NULL CHECK (thumbnail_height > 0),
    storage_endpoint TEXT NOT NULL,
    storage_region TEXT NOT NULL,
    storage_bucket TEXT NOT NULL,
    image_key TEXT NOT NULL,
    thumbnail_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (operation_id, ordinal)
);
CREATE INDEX IF NOT EXISTS idx_ticket_attachments_message ON ticket_message_attachments(message_id,ordinal);
CREATE INDEX IF NOT EXISTS idx_ticket_attachments_cleanup ON ticket_message_attachments(state,updated_at);
CREATE INDEX IF NOT EXISTS idx_tickets_user_open ON tickets(user_id) WHERE deleted_at IS NULL AND status IN ('pending','replied');
