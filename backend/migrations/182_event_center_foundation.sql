CREATE TABLE IF NOT EXISTS event_categories (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(100) NOT NULL,
    color VARCHAR(20) NOT NULL DEFAULT '#2563EB',
    icon VARCHAR(64) NOT NULL DEFAULT 'calendar',
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS event_categories_code_uq ON event_categories (code) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS event_categories_enabled_sort_idx ON event_categories (enabled, sort_order);

CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    category_id BIGINT REFERENCES event_categories(id) ON DELETE SET NULL,
    title VARCHAR(200) NOT NULL,
    summary VARCHAR(1000) NOT NULL DEFAULT '',
    description_markdown TEXT NOT NULL DEFAULT '',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    organizer_name VARCHAR(200) NOT NULL DEFAULT '',
    organizer_url VARCHAR(2048) NOT NULL DEFAULT '',
    fee_type VARCHAR(20) NOT NULL DEFAULT 'unknown',
    price_min DOUBLE PRECISION,
    price_max DOUBLE PRECISION,
    currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
    registration_url VARCHAR(2048) NOT NULL DEFAULT '',
    registration_deadline TIMESTAMPTZ,
    cover_url VARCHAR(2048) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    visibility VARCHAR(20) NOT NULL DEFAULT 'authenticated',
    audience JSONB NOT NULL DEFAULT '{}'::jsonb,
    visible_from TIMESTAMPTZ,
    visible_until TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    cancelled_reason VARCHAR(1000) NOT NULL DEFAULT '',
    manual_override_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT events_status_check CHECK (status IN ('draft', 'published', 'cancelled', 'archived')),
    CONSTRAINT events_visibility_check CHECK (visibility IN ('authenticated', 'targeted')),
    CONSTRAINT events_fee_type_check CHECK (fee_type IN ('free', 'paid', 'unknown')),
    CONSTRAINT events_visible_range_check CHECK (visible_until IS NULL OR visible_from IS NULL OR visible_until > visible_from),
    CONSTRAINT events_price_range_check CHECK (price_min IS NULL OR price_min >= 0),
    CONSTRAINT events_price_max_check CHECK (price_max IS NULL OR price_max >= 0),
    CONSTRAINT events_price_order_check CHECK (price_min IS NULL OR price_max IS NULL OR price_max >= price_min)
);
CREATE INDEX IF NOT EXISTS events_status_published_idx ON events (status, published_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS events_category_idx ON events (category_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS events_visible_from_idx ON events (visible_from) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS events_visible_until_idx ON events (visible_until) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS event_occurrences (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ,
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    all_day BOOLEAN NOT NULL DEFAULT FALSE,
    location_mode VARCHAR(20) NOT NULL DEFAULT 'offline',
    online_url VARCHAR(2048) NOT NULL DEFAULT '',
    venue_name VARCHAR(300) NOT NULL DEFAULT '',
    address VARCHAR(1000) NOT NULL DEFAULT '',
    country VARCHAR(100) NOT NULL DEFAULT '中国',
    province VARCHAR(100) NOT NULL DEFAULT '',
    city VARCHAR(100) NOT NULL DEFAULT '',
    district VARCHAR(100) NOT NULL DEFAULT '',
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    coordinate_source VARCHAR(20) NOT NULL DEFAULT 'wgs84',
    geocode_status VARCHAR(32) NOT NULL DEFAULT '',
    geocode_precision VARCHAR(32) NOT NULL DEFAULT '',
    provider_place_id VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT event_occurrences_time_check CHECK (ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT event_occurrences_location_mode_check CHECK (location_mode IN ('offline', 'online', 'hybrid')),
    CONSTRAINT event_occurrences_coordinate_source_check CHECK (coordinate_source IN ('wgs84', 'gcj02')),
    CONSTRAINT event_occurrences_coordinate_pair_check CHECK ((latitude IS NULL) = (longitude IS NULL)),
    CONSTRAINT event_occurrences_latitude_check CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CONSTRAINT event_occurrences_longitude_check CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180)
);
CREATE INDEX IF NOT EXISTS event_occurrences_event_start_idx ON event_occurrences (event_id, starts_at);
CREATE INDEX IF NOT EXISTS event_occurrences_time_idx ON event_occurrences (starts_at, ends_at);
CREATE INDEX IF NOT EXISTS event_occurrences_city_time_idx ON event_occurrences (city, starts_at);
CREATE INDEX IF NOT EXISTS event_occurrences_lng_lat_idx ON event_occurrences (longitude, latitude) WHERE longitude IS NOT NULL;

CREATE TABLE IF NOT EXISTS event_sources (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(100) NOT NULL,
    kind VARCHAR(20) NOT NULL DEFAULT 'json',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_sync_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT event_sources_kind_check CHECK (kind IN ('manual', 'json', 'crawler'))
);
CREATE UNIQUE INDEX IF NOT EXISTS event_sources_code_uq ON event_sources (code) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS event_sources_kind_enabled_idx ON event_sources (kind, enabled) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS event_source_records (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL REFERENCES event_sources(id) ON DELETE RESTRICT,
    event_id BIGINT REFERENCES events(id) ON DELETE SET NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '',
    source_url VARCHAR(2048) NOT NULL DEFAULT '',
    fingerprint VARCHAR(64) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    normalized_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_updated_at TIMESTAMPTZ,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS event_source_records_external_uq ON event_source_records (source_id, external_id) WHERE external_id <> '';
CREATE INDEX IF NOT EXISTS event_source_records_fingerprint_idx ON event_source_records (fingerprint);
CREATE INDEX IF NOT EXISTS event_source_records_event_idx ON event_source_records (event_id);
CREATE INDEX IF NOT EXISTS event_source_records_last_seen_idx ON event_source_records (last_seen_at);

CREATE TABLE IF NOT EXISTS event_import_batches (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL REFERENCES event_sources(id) ON DELETE RESTRICT,
    file_name VARCHAR(255) NOT NULL DEFAULT '',
    file_hash VARCHAR(64) NOT NULL,
    schema_version INTEGER NOT NULL,
    mode VARCHAR(20) NOT NULL DEFAULT 'upsert',
    status VARCHAR(20) NOT NULL DEFAULT 'previewed',
    total_count INTEGER NOT NULL DEFAULT 0,
    create_count INTEGER NOT NULL DEFAULT 0,
    update_count INTEGER NOT NULL DEFAULT 0,
    unchanged_count INTEGER NOT NULL DEFAULT 0,
    conflict_count INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    committed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT event_import_batches_mode_check CHECK (mode IN ('create_only', 'upsert')),
    CONSTRAINT event_import_batches_status_check CHECK (status IN ('previewed', 'committing', 'completed', 'partial', 'failed'))
);
CREATE INDEX IF NOT EXISTS event_import_batches_source_created_idx ON event_import_batches (source_id, created_at DESC);
CREATE INDEX IF NOT EXISTS event_import_batches_status_created_idx ON event_import_batches (status, created_at DESC);
CREATE INDEX IF NOT EXISTS event_import_batches_file_hash_idx ON event_import_batches (file_hash);

CREATE TABLE IF NOT EXISTS event_import_items (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES event_import_batches(id) ON DELETE CASCADE,
    item_index INTEGER NOT NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '',
    fingerprint VARCHAR(64) NOT NULL DEFAULT '',
    content_hash VARCHAR(64) NOT NULL DEFAULT '',
    action VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    event_id BIGINT REFERENCES events(id) ON DELETE SET NULL,
    error_code VARCHAR(100) NOT NULL DEFAULT '',
    error_detail VARCHAR(2000) NOT NULL DEFAULT '',
    normalized_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT event_import_items_action_check CHECK (action IN ('create', 'update', 'unchanged', 'conflict', 'error'))
);
CREATE UNIQUE INDEX IF NOT EXISTS event_import_items_batch_index_uq ON event_import_items (batch_id, item_index);
CREATE INDEX IF NOT EXISTS event_import_items_batch_action_idx ON event_import_items (batch_id, action);
CREATE INDEX IF NOT EXISTS event_import_items_event_idx ON event_import_items (event_id);

INSERT INTO event_categories (code, name, color, icon, sort_order)
VALUES
    ('conference', '会议', '#2563EB', 'presentation', 10),
    ('meetup', '沙龙', '#059669', 'users', 20),
    ('workshop', '工作坊', '#D97706', 'wrench', 30),
    ('hackathon', '黑客松', '#DC2626', 'code', 40),
    ('other', '其他', '#6B7280', 'calendar', 100)
ON CONFLICT DO NOTHING;

INSERT INTO event_sources (code, name, kind)
VALUES
    ('manual', '管理页录入', 'manual'),
    ('json', 'JSON 导入', 'json')
ON CONFLICT DO NOTHING;
