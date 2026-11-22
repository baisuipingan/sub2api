CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workbench_search_billing_charges
    ON workbench_search_billing(settled_at DESC, run_id, operation_id) WHERE state = 'charged';
