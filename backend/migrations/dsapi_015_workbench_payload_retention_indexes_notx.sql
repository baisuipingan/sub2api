CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_office_agent_runs_payloads_retention
    ON office_agent_runs (finished_at, id)
    WHERE payloads_compacted_at IS NULL AND status IN ('completed', 'partially_completed', 'failed', 'cancelled');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_office_agent_runs_browser_idle
    ON office_agent_runs (updated_at, id)
    WHERE browser_driven AND status IN ('queued', 'running', 'recovering', 'waiting_user', 'needs_confirmation');
