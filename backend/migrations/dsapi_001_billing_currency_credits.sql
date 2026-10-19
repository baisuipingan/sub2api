-- 人民币计费账本第一阶段：新增整数积分余额、费用快照和不可变流水。
-- users.balance 继续作为 USD 等值兼容列，由数据库触发器与 balance_credits
-- 保持同步；切换展示币种不会改动任何已发生的余额或扣费。

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS balance_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS balance_credits_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS frozen_balance_credits BIGINT NOT NULL DEFAULT 0;

UPDATE users
SET balance_credits = ROUND(balance * 100000000)::BIGINT
WHERE balance_credits = 0 AND balance <> 0;

UPDATE users
SET frozen_balance_credits = ROUND(COALESCE(frozen_balance, 0) * 100000000)::BIGINT
WHERE frozen_balance_credits = 0 AND COALESCE(frozen_balance, 0) <> 0;

CREATE OR REPLACE FUNCTION sync_sub2api_user_credit_columns()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    ledger_scale CONSTANT numeric := 100000000;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.balance_credits = 0 AND NEW.balance <> 0 THEN
            NEW.balance_credits := ROUND(NEW.balance * ledger_scale)::BIGINT;
        ELSIF NEW.balance_credits <> 0 THEN
            NEW.balance := NEW.balance_credits::numeric / ledger_scale;
        END IF;
        IF NEW.frozen_balance_credits = 0 AND COALESCE(NEW.frozen_balance, 0) <> 0 THEN
            NEW.frozen_balance_credits := ROUND(NEW.frozen_balance * ledger_scale)::BIGINT;
        ELSIF NEW.frozen_balance_credits <> 0 THEN
            NEW.frozen_balance := NEW.frozen_balance_credits::numeric / ledger_scale;
        END IF;
        RETURN NEW;
    END IF;

    -- Credits win when both representations changed. Legacy code that only
    -- updates balance still gets an exact credits value and preserved behavior.
    IF NEW.balance_credits IS DISTINCT FROM OLD.balance_credits
       AND NEW.balance IS NOT DISTINCT FROM OLD.balance THEN
        NEW.balance := NEW.balance_credits::numeric / ledger_scale;
    ELSIF NEW.balance IS DISTINCT FROM OLD.balance
          AND NEW.balance_credits IS NOT DISTINCT FROM OLD.balance_credits THEN
        NEW.balance_credits := ROUND(NEW.balance * ledger_scale)::BIGINT;
    ELSE
        NEW.balance := NEW.balance_credits::numeric / ledger_scale;
    END IF;

    IF NEW.frozen_balance_credits IS DISTINCT FROM OLD.frozen_balance_credits
       AND COALESCE(NEW.frozen_balance, 0) IS NOT DISTINCT FROM COALESCE(OLD.frozen_balance, 0) THEN
        NEW.frozen_balance := NEW.frozen_balance_credits::numeric / ledger_scale;
    ELSIF COALESCE(NEW.frozen_balance, 0) IS DISTINCT FROM COALESCE(OLD.frozen_balance, 0)
          AND NEW.frozen_balance_credits IS NOT DISTINCT FROM OLD.frozen_balance_credits THEN
        NEW.frozen_balance_credits := ROUND(COALESCE(NEW.frozen_balance, 0) * ledger_scale)::BIGINT;
    ELSE
        NEW.frozen_balance := NEW.frozen_balance_credits::numeric / ledger_scale;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_sync_sub2api_user_credit_columns ON users;
CREATE TRIGGER trg_sync_sub2api_user_credit_columns
BEFORE INSERT OR UPDATE OF balance, balance_credits, frozen_balance, frozen_balance_credits ON users
FOR EACH ROW EXECUTE FUNCTION sync_sub2api_user_credit_columns();

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS charge_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS billing_currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    ADD COLUMN IF NOT EXISTS credits_per_usd BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usd_to_cny_rate NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS billing_version BIGINT NOT NULL DEFAULT 0;

UPDATE usage_logs
SET charge_credits = ROUND(actual_cost * 100000000)::BIGINT,
    credits_per_usd = 100000000
WHERE charge_credits = 0 AND actual_cost <> 0;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS credited_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_credits BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS credits_per_usd_snapshot BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usd_to_cny_snapshot NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS recharge_rate_snapshot NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS billing_version BIGINT NOT NULL DEFAULT 0;

UPDATE payment_orders
SET credited_credits = ROUND(amount * 100000000)::BIGINT,
    credits_per_usd_snapshot = 100000000
WHERE credited_credits = 0 AND order_type = 'balance' AND amount <> 0;

CREATE TABLE IF NOT EXISTS billing_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    delta_credits BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    bucket_type VARCHAR(32) NOT NULL DEFAULT 'wallet',
    bucket_id BIGINT NOT NULL,
    reference_type VARCHAR(32) NOT NULL,
    reference_id VARCHAR(128) NOT NULL,
    request_id VARCHAR(128),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS billing_ledger_idempotency
    ON billing_ledger (bucket_type, bucket_id, reference_type, reference_id);
CREATE INDEX IF NOT EXISTS billing_ledger_user_created
    ON billing_ledger (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS billing_ledger_request_id
    ON billing_ledger (request_id) WHERE request_id IS NOT NULL;

INSERT INTO settings (key, value, updated_at)
VALUES (
    'billing_currency_config',
    '{"enabled":true,"engine":"shadow","credits_per_usd":100000000,"display_currency":"CNY","usd_to_cny":7.2,"recharge_rates":{"CNY":7.2,"USD":1},"updated_at":"2026-10-19T00:00:00Z","version":1}',
    NOW()
)
ON CONFLICT (key) DO NOTHING;
