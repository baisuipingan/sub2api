ALTER TABLE usage_logs
    ALTER COLUMN account_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS search_provider VARCHAR(32);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'usage_logs'::regclass AND conname = 'usage_logs_account_or_search_provider_check'
    ) THEN
        ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_account_or_search_provider_check CHECK (
            (account_id IS NOT NULL AND search_provider IS NULL) OR
            (account_id IS NULL AND search_provider IS NOT NULL AND search_provider IN ('deepseek', 'brave', 'tavily'))
        ) NOT VALID;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION record_workbench_search_usage(charge workbench_search_billing) RETURNS void
LANGUAGE SQL AS $$
    INSERT INTO usage_logs (
        user_id, api_key_id, request_id, model, requested_model, group_id,
        total_cost, actual_cost, account_stats_cost, billing_type, billing_mode, request_type,
        inbound_endpoint, duration_ms, search_provider, charge_credits, billing_currency,
        credits_per_usd, usd_to_cny_rate, billing_version, created_at
    )
    SELECT charge.user_id, run.api_key_id, 'client:' || op.request_id, 'workbench-web-search', 'workbench-web-search', g.id,
           charge.amount_credits::numeric / 100000000, charge.amount_credits::numeric / 100000000, 0, 0, 'per_request', 1,
           '/workbench/chat', CASE WHEN op.dispatched_at IS NOT NULL THEN
               LEAST(2147483647, GREATEST(0, EXTRACT(EPOCH FROM (charge.settled_at - op.dispatched_at)) * 1000))::integer
           END, charge.provider, charge.amount_credits, charge.billing_currency,
           100000000, charge.usd_to_cny, charge.billing_version, charge.settled_at
    FROM office_agent_runs run
    JOIN office_agent_operations op ON op.run_id = run.id AND op.operation_id = charge.operation_id
    JOIN api_keys k ON k.id = run.api_key_id AND k.user_id = charge.user_id
    LEFT JOIN groups g ON g.id = run.group_id
    WHERE run.id = charge.run_id AND run.user_id = charge.user_id AND charge.state = 'charged'
    ON CONFLICT (request_id, api_key_id) DO NOTHING;
$$;

CREATE OR REPLACE FUNCTION project_workbench_search_usage() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM record_workbench_search_usage(NEW);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS workbench_search_usage_after_charge ON workbench_search_billing;
CREATE TRIGGER workbench_search_usage_after_charge
AFTER UPDATE OF state ON workbench_search_billing
FOR EACH ROW WHEN (NEW.state = 'charged' AND OLD.state <> NEW.state)
EXECUTE FUNCTION project_workbench_search_usage();

-- Keep historical settlement timestamps and quote snapshots. This only inserts report rows.
DO $$
BEGIN
    PERFORM record_workbench_search_usage(charge)
    FROM workbench_search_billing charge
    WHERE charge.state = 'charged';
END;
$$;
