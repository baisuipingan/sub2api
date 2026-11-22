-- Finish abandoned worker runs so their conversations can accept browser turns.
-- Keep transcript, operations, artifacts, and charges available for history.
UPDATE office_agent_runs
SET status = 'cancelled',
    lease_epoch = lease_epoch + 1,
    lease_token = NULL,
    lease_until = NULL,
    lease_owner = NULL,
    paused_at = NULL,
    finished_at = NOW(),
    updated_at = NOW()
WHERE NOT browser_driven
  AND status IN ('queued', 'running', 'recovering', 'waiting_user', 'needs_confirmation');
