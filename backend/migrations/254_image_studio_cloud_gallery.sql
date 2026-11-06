-- Durable, user-scoped metadata for Image Studio's cloud gallery. Generated
-- media bytes live in private object storage; no presigned URL is persisted.
CREATE TABLE IF NOT EXISTS image_studio_gallery_assets (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    image_id VARCHAR(128) NOT NULL,
    sha256 CHAR(64) NOT NULL,
    mime_type VARCHAR(32) NOT NULL,
    byte_size BIGINT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    preview_byte_size BIGINT NOT NULL,
    preview_width INTEGER NOT NULL,
    preview_height INTEGER NOT NULL,
    thumbnail_byte_size BIGINT NOT NULL,
    thumbnail_width INTEGER NOT NULL,
    thumbnail_height INTEGER NOT NULL,
    storage_endpoint TEXT NOT NULL,
    storage_region TEXT NOT NULL,
    storage_bucket TEXT NOT NULL,
    original_key TEXT NOT NULL,
    preview_key TEXT NOT NULL,
    thumbnail_key TEXT NOT NULL,
    state VARCHAR(24) NOT NULL DEFAULT 'ready'
        CHECK (state IN ('uploading', 'ready', 'deleting')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, image_id),
    CHECK (byte_size > 0 AND byte_size <= 33554432),
    CHECK (width > 0 AND height > 0 AND width::BIGINT * height <= 20000000)
);
CREATE INDEX IF NOT EXISTS idx_image_studio_gallery_assets_cleanup
    ON image_studio_gallery_assets(state, created_at);

CREATE TABLE IF NOT EXISTS image_studio_gallery_tasks (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id VARCHAR(128) NOT NULL,
    task_json JSONB NOT NULL,
    sha256 CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, task_id)
);
CREATE INDEX IF NOT EXISTS idx_image_studio_gallery_tasks_page
    ON image_studio_gallery_tasks(user_id, created_at DESC, task_id DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS image_studio_gallery_task_assets (
    user_id BIGINT NOT NULL,
    task_id VARCHAR(128) NOT NULL,
    image_id VARCHAR(128) NOT NULL,
    role VARCHAR(32) NOT NULL,
    ordinal INTEGER NOT NULL,
    PRIMARY KEY (user_id, task_id, role, ordinal),
    FOREIGN KEY (user_id, task_id)
        REFERENCES image_studio_gallery_tasks(user_id, task_id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, image_id)
        REFERENCES image_studio_gallery_assets(user_id, image_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_image_studio_gallery_task_assets_image
    ON image_studio_gallery_task_assets(user_id, image_id);
