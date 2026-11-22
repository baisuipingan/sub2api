CREATE TABLE IF NOT EXISTS workbench_office_nodes (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 120),
    url TEXT NOT NULL UNIQUE CHECK (octet_length(url) BETWEEN 1 AND 512),
    state TEXT NOT NULL DEFAULT 'disabled' CHECK (state IN ('active', 'draining', 'disabled')),
    max_concurrent INTEGER NOT NULL CHECK (max_concurrent BETWEEN 1 AND 8),
    token_encrypted TEXT NOT NULL CHECK (octet_length(token_encrypted) BETWEEN 1 AND 512),
    worker_identity TEXT NOT NULL DEFAULT '' CHECK (octet_length(worker_identity) <= 128),
    revision BIGINT NOT NULL DEFAULT 1,
    health TEXT NOT NULL DEFAULT 'unchecked' CHECK (health IN ('unchecked', 'online', 'unavailable')),
    checked_at TIMESTAMPTZ,
    status JSONB CHECK (octet_length(status::text) <= 16384),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workbench_office_nodes_worker_identity
    ON workbench_office_nodes(worker_identity) WHERE worker_identity <> '';
