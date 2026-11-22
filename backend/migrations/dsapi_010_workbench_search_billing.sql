CREATE TABLE IF NOT EXISTS workbench_search_billing (
    run_id UUID NOT NULL,
    operation_id VARCHAR(128) NOT NULL,
    user_id BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL CHECK (provider IN ('deepseek','brave','tavily')),
    amount_credits BIGINT NOT NULL CHECK (amount_credits > 0),
    billing_engine VARCHAR(16) NOT NULL CHECK (billing_engine IN ('legacy','shadow','credits')),
    billing_currency VARCHAR(3) NOT NULL CHECK (billing_currency IN ('CNY','USD')),
    usd_to_cny NUMERIC(20,8) NOT NULL CHECK (usd_to_cny > 0),
    billing_version BIGINT NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved','charged','released','expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, operation_id),
    FOREIGN KEY (run_id, operation_id) REFERENCES office_agent_operations(run_id, operation_id) ON DELETE RESTRICT,
    FOREIGN KEY (user_id, run_id) REFERENCES office_agent_runs(user_id, id) ON DELETE RESTRICT,
    CHECK ((state = 'reserved') = (settled_at IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_workbench_search_billing_user_charges
    ON workbench_search_billing(user_id, settled_at DESC, run_id, operation_id) WHERE state = 'charged';
CREATE INDEX IF NOT EXISTS idx_workbench_search_billing_expiring
    ON workbench_search_billing(expires_at) WHERE state = 'reserved';
