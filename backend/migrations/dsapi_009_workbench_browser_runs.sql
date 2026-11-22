ALTER TABLE office_agent_runs ADD COLUMN IF NOT EXISTS browser_driven BOOLEAN NOT NULL DEFAULT false;
