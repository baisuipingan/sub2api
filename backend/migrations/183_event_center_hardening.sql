-- Keep source imports idempotent even when upstream data omits external_id.
CREATE UNIQUE INDEX IF NOT EXISTS event_source_records_source_fingerprint_uq
    ON event_source_records (source_id, fingerprint);

-- Targeted event visibility uses JSONB containment for subscription-group matching.
CREATE INDEX IF NOT EXISTS events_audience_gin_idx
    ON events USING GIN (audience jsonb_path_ops)
    WHERE deleted_at IS NULL;
