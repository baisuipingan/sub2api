CREATE TABLE IF NOT EXISTS workbench_office_dispatch (
    job_key TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    run_id UUID,
    version_id UUID,
    node_id UUID REFERENCES workbench_office_nodes(id) ON DELETE SET NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'waiting' CHECK (state IN ('waiting', 'active', 'done', 'cancelled')),
    assigned_at TIMESTAMPTZ,
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    receipt JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (num_nonnulls(run_id, version_id) = 1)
);
CREATE INDEX IF NOT EXISTS workbench_office_dispatch_active_node
    ON workbench_office_dispatch (node_id) WHERE state = 'active';
CREATE INDEX IF NOT EXISTS workbench_office_dispatch_active_check
    ON workbench_office_dispatch (next_check_at) WHERE state = 'active';
