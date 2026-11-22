CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_office_agent_runs_replay_variant
    ON office_agent_runs(conversation_id, regeneration_of, created_at DESC)
    WHERE regeneration_of IS NOT NULL AND selected;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_office_agent_runs_replay_checkpoint
    ON office_agent_runs(replay_checkpoint_id)
    WHERE replay_checkpoint_id IS NOT NULL
      AND status IN ('queued','running','recovering','waiting_user','needs_confirmation');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_office_agent_replay_checkpoint_cleanup
    ON office_agent_replay_checkpoints(created_at, id)
    WHERE NOT published;
